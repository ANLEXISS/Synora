// Package recovery owns the Core lifecycle gate. It is deliberately small:
// it records dependency evidence and computes readiness, while Core remains
// the owner of business state and persistence.
package recovery

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type State string

const (
	Starting   State = "starting"
	Recovering State = "recovering"
	Running    State = "running"
	Degraded   State = "degraded"
	Failed     State = "failed"
)

var (
	ErrInvalidTransition    = errors.New("invalid recovery state transition")
	ErrRequiredDependencies = errors.New("required recovery dependencies are not healthy")
)

type Dependency struct {
	Name     string
	Required bool
}

type DependencyStatus struct {
	Name     string    `json:"name"`
	Required bool      `json:"required"`
	Healthy  bool      `json:"healthy"`
	Checked  time.Time `json:"checked_at"`
	Reason   string    `json:"reason,omitempty"`
}

type Snapshot struct {
	State            State              `json:"state"`
	Ready            bool               `json:"ready"`
	Healthy          bool               `json:"healthy"`
	RecoveryComplete bool               `json:"recovery_complete"`
	Reason           string             `json:"reason,omitempty"`
	UpdatedAt        time.Time          `json:"updated_at"`
	Dependencies     []DependencyStatus `json:"dependencies"`
}

type Machine struct {
	mu      sync.RWMutex
	now     func() time.Time
	state   State
	reason  string
	updated time.Time
	deps    map[string]DependencyStatus
}

func New(dependencies []Dependency, now func() time.Time) (*Machine, error) {
	if now == nil {
		now = time.Now
	}
	m := &Machine{now: now, state: Starting, deps: make(map[string]DependencyStatus, len(dependencies))}
	for _, dependency := range dependencies {
		name := strings.TrimSpace(dependency.Name)
		if name == "" {
			return nil, errors.New("recovery dependency name is required")
		}
		if _, exists := m.deps[name]; exists {
			return nil, fmt.Errorf("duplicate recovery dependency %q", name)
		}
		m.deps[name] = DependencyStatus{Name: name, Required: dependency.Required}
	}
	m.updated = now().UTC()
	return m, nil
}

func (m *Machine) BeginRecovery() error {
	return m.transition(Recovering, "recovery started")
}

func (m *Machine) SetDependency(name string, healthy bool, reason string) error {
	if m == nil {
		return errors.New("nil recovery machine")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	status, ok := m.deps[strings.TrimSpace(name)]
	if !ok {
		return fmt.Errorf("unknown recovery dependency %q", name)
	}
	if m.state == Running || m.state == Degraded {
		// Runtime failures are observable but cannot make a running Core claim
		// healthy. Required failures also revoke readiness immediately.
		status.Healthy = healthy
		status.Reason = strings.TrimSpace(reason)
		status.Checked = m.now().UTC()
		m.deps[status.Name] = status
		m.updated = status.Checked
		m.recomputeRuntimeLocked()
		return nil
	}
	if m.state != Recovering && m.state != Starting {
		return fmt.Errorf("cannot update dependency while recovery is %s", m.state)
	}
	status.Healthy = healthy
	status.Reason = strings.TrimSpace(reason)
	status.Checked = m.now().UTC()
	m.deps[status.Name] = status
	m.updated = status.Checked
	return nil
}

func (m *Machine) CompleteRecovery() error {
	if m == nil {
		return errors.New("nil recovery machine")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != Recovering && m.state != Starting {
		return fmt.Errorf("cannot complete recovery from %s", m.state)
	}
	if !m.requiredHealthyLocked() {
		m.state = Recovering
		m.reason = ErrRequiredDependencies.Error()
		m.updated = m.now().UTC()
		return ErrRequiredDependencies
	}
	m.state = Running
	m.reason = "recovery complete"
	m.updated = m.now().UTC()
	m.recomputeRuntimeLocked()
	return nil
}

func (m *Machine) Fail(reason string) error {
	return m.transition(Failed, strings.TrimSpace(reason))
}

func (m *Machine) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{State: Failed, Reason: "nil recovery machine"}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshotLocked()
}

func (m *Machine) transition(next State, reason string) error {
	if m == nil {
		return errors.New("nil recovery machine")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validState(next) {
		return ErrInvalidTransition
	}
	if m.state == next {
		m.reason = reason
		m.updated = m.now().UTC()
		return nil
	}
	valid := false
	switch m.state {
	case Starting:
		valid = next == Recovering || next == Failed
	case Recovering:
		valid = next == Running || next == Degraded || next == Failed
	case Running:
		valid = next == Degraded || next == Failed
	case Degraded:
		valid = next == Recovering || next == Running || next == Failed
	case Failed:
		valid = next == Recovering
	}
	if !valid {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, m.state, next)
	}
	m.state = next
	m.reason = reason
	m.updated = m.now().UTC()
	return nil
}

func (m *Machine) requiredHealthyLocked() bool {
	for _, dependency := range m.deps {
		if dependency.Required && !dependency.Healthy {
			return false
		}
	}
	return true
}

func (m *Machine) recomputeRuntimeLocked() {
	if m.state != Running && m.state != Degraded {
		return
	}
	if !m.requiredHealthyLocked() {
		m.state = Degraded
		m.reason = "required dependency degraded"
		return
	}
	for _, dependency := range m.deps {
		if !dependency.Required && !dependency.Healthy {
			m.state = Degraded
			if dependency.Reason != "" {
				m.reason = dependency.Reason
			} else {
				m.reason = "optional dependency degraded"
			}
			return
		}
	}
	m.state = Running
	m.reason = "recovery complete"
}

func (m *Machine) snapshotLocked() Snapshot {
	dependencies := make([]DependencyStatus, 0, len(m.deps))
	for _, dependency := range m.deps {
		dependencies = append(dependencies, dependency)
	}
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].Name < dependencies[j].Name })
	ready := (m.state == Running || m.state == Degraded) && m.requiredHealthyLocked()
	healthy := m.state == Running && ready
	return Snapshot{
		State:            m.state,
		Ready:            ready,
		Healthy:          healthy,
		RecoveryComplete: m.state == Running || m.state == Degraded || m.state == Failed,
		Reason:           m.reason,
		UpdatedAt:        m.updated,
		Dependencies:     dependencies,
	}
}

func validState(state State) bool {
	switch state {
	case Starting, Recovering, Running, Degraded, Failed:
		return true
	default:
		return false
	}
}
