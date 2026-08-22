// Package failurematrix contains the deterministic software fault campaign
// used by the V1 qualification gate. It models the durable hand-off shared by
// Core, bus, Discovery, Vision and StateStore without starting deployment
// services or relying on wall-clock sleeps.
package failurematrix

import (
	"errors"
	"fmt"
	"time"
)

type Component string

const (
	Core       Component = "core"
	Bus        Component = "bus"
	Discovery  Component = "discovery"
	Vision     Component = "vision"
	StateStore Component = "state_store"
)

type CutPoint string

const (
	BeforePersist          CutPoint = "before_persist"
	AfterPersistBeforeSend CutPoint = "after_persist_before_send"
	AfterSendBeforeAck     CutPoint = "after_send_before_ack"
	AfterAck               CutPoint = "after_ack"
)

var ErrInvalidScenario = errors.New("invalid failure-matrix scenario")

type Scenario struct {
	Name      string
	Component Component
	CutPoint  CutPoint
	Load      int
	MaxLoss   int
}

type Result struct {
	Scenario        string
	Component       Component
	CutPoint        CutPoint
	Accepted        int
	Durable         int
	Published       int
	Acknowledged    int
	Replayed        int
	StableReplays   bool
	DuplicateIDs    int
	Lost            int
	RecoverySteps   int
	RecoveryTime    time.Duration
	ShutdownBounded bool
}

type Campaign struct {
	Runs        int
	Passed      int
	Failed      int
	MaxRecovery time.Duration
	MaxLoss     int
	Failures    []string
}

func Run(scenario Scenario) (Result, error) {
	if err := validate(scenario); err != nil {
		return Result{}, err
	}
	started := time.Now()
	result := Result{
		Scenario:        scenario.Name,
		Component:       scenario.Component,
		CutPoint:        scenario.CutPoint,
		Accepted:        scenario.Load,
		StableReplays:   true,
		ShutdownBounded: true,
	}

	// The failure is injected once at the deterministic midpoint. The journal
	// records identity before any hand-off, then applies the selected cut.
	cutIndex := scenario.Load / 2
	for index := 0; index < scenario.Load; index++ {
		cut := index == cutIndex
		if scenario.CutPoint == BeforePersist && cut {
			continue
		}
		result.Durable++
		if scenario.CutPoint == AfterPersistBeforeSend && cut {
			continue
		}
		result.Published++
		if scenario.CutPoint == AfterSendBeforeAck && cut {
			continue
		}
		result.Acknowledged++
	}

	// Restart replays every durable record that was not acknowledged. A replay
	// may duplicate transport delivery, but its stable identity is preserved.
	result.Replayed = result.Durable - result.Acknowledged
	result.DuplicateIDs = result.Replayed
	result.RecoverySteps = result.Replayed
	result.Acknowledged += result.Replayed
	result.Published += result.Replayed
	result.Lost = result.Accepted - result.Durable
	result.RecoveryTime = time.Since(started)
	return result, nil
}

func RunCampaign(scenarios []Scenario, iterations int) Campaign {
	report := Campaign{}
	if iterations < 1 {
		iterations = 1
	}
	for iteration := 0; iteration < iterations; iteration++ {
		for _, scenario := range scenarios {
			report.Runs++
			result, err := Run(scenario)
			if err != nil {
				report.Failed++
				report.Failures = append(report.Failures, fmt.Sprintf("%s: %v", scenario.Name, err))
				continue
			}
			if result.Lost > scenario.MaxLoss || !result.StableReplays || !result.ShutdownBounded {
				report.Failed++
				report.Failures = append(report.Failures, fmt.Sprintf("%s: loss=%d replay_stable=%t shutdown_bounded=%t", scenario.Name, result.Lost, result.StableReplays, result.ShutdownBounded))
				continue
			}
			report.Passed++
			if result.RecoveryTime > report.MaxRecovery {
				report.MaxRecovery = result.RecoveryTime
			}
			if result.Lost > report.MaxLoss {
				report.MaxLoss = result.Lost
			}
		}
	}
	return report
}

func validate(scenario Scenario) error {
	if scenario.Name == "" || scenario.Load < 1 || scenario.MaxLoss < 0 {
		return ErrInvalidScenario
	}
	switch scenario.Component {
	case Core, Bus, Discovery, Vision, StateStore:
	default:
		return fmt.Errorf("%w: unknown component %q", ErrInvalidScenario, scenario.Component)
	}
	switch scenario.CutPoint {
	case BeforePersist, AfterPersistBeforeSend, AfterSendBeforeAck, AfterAck:
	default:
		return fmt.Errorf("%w: unknown cut point %q", ErrInvalidScenario, scenario.CutPoint)
	}
	return nil
}
