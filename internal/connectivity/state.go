package connectivity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"synora/internal/configfile"
	"synora/pkg/contracts"
)

const StateFile = "state.json"

type StateStore struct {
	mu    sync.Mutex
	path  string
	state contracts.Status
}

func NewStateStore(dir string) *StateStore {
	return &StateStore{path: filepath.Join(dir, StateFile)}
}

func (s *StateStore) Load() (contracts.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *StateStore) loadLocked() (contracts.Status, error) {
	if err := rejectSymlink(s.path); err != nil {
		return contracts.Status{}, err
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return contracts.Status{}, err
	}
	var status contracts.Status
	if err := json.Unmarshal(data, &status); err != nil {
		return contracts.Status{}, errors.New("invalid connectivity state")
	}
	if err := validateStatus(status); err != nil {
		return contracts.Status{}, err
	}
	s.state = status
	return status, nil
}

func (s *StateStore) Initialize(cfg Config, identity Identity) (contracts.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.path); err == nil {
		status, err := s.loadLocked()
		if err != nil {
			return contracts.Status{}, err
		}
		if status.DeviceID != "" && status.DeviceID != identity.DeviceID() {
			return contracts.Status{}, errors.New("connectivity identity does not match state")
		}
		status.DeviceID = identity.DeviceID()
		status.IdentityFingerprint = identity.Fingerprint()
		status.Enabled = cfg.Enabled
		status.InterfaceName = cfg.Interface.Name
		if !cfg.Enabled {
			status.State = contracts.StateDisabled
			status.Mode = contracts.ModeNone
			status.ControlConnected = false
			status.PeerConnected = false
		} else if !status.Provisioned && status.State == contracts.StateDisabled {
			status = transition(status, contracts.StateUnprovisioned, contracts.ModeNone, "")
		}
		if status.LastTransition.IsZero() {
			status.LastTransition = time.Now().UTC()
		}
		s.state = status
		return status, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return contracts.Status{}, errors.New("inspect connectivity state")
	}
	state := contracts.Status{
		SchemaVersion: contracts.ConnectivitySchemaVersion,
		DeviceID:      identity.DeviceID(), IdentityFingerprint: identity.Fingerprint(),
		Enabled: cfg.Enabled, Provisioned: false,
		State: contracts.StateUnprovisioned, Mode: contracts.ModeNone,
		InterfaceName: cfg.Interface.Name, LastTransition: time.Now().UTC(),
	}
	if !cfg.Enabled {
		state.State = contracts.StateDisabled
	}
	s.state = state
	return state, s.saveLocked(state)
}

func (s *StateStore) Current() contracts.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *StateStore) Save(status contracts.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(status)
}

func (s *StateStore) saveLocked(status contracts.Status) error {
	if err := validateStatus(status); err != nil {
		return err
	}
	if err := rejectSymlink(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return errors.New("encode connectivity state")
	}
	data = append(data, '\n')
	if err := configfile.WriteAtomicWithBackup(s.path, data, 0640); err != nil {
		return errors.New("persist connectivity state")
	}
	s.state = status
	return nil
}

func ReadStatus(dir string, cfg Config) (contracts.Status, error) {
	store := NewStateStore(dir)
	status, err := store.Load()
	if err == nil {
		return status, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return contracts.Status{}, err
	}
	state := contracts.Status{SchemaVersion: contracts.ConnectivitySchemaVersion, Enabled: cfg.Enabled, State: contracts.StateUnprovisioned, Mode: contracts.ModeNone, InterfaceName: cfg.Interface.Name}
	if !cfg.Enabled {
		state.State = contracts.StateDisabled
	}
	return state, nil
}

func validateStatus(status contracts.Status) error {
	if status.SchemaVersion != contracts.ConnectivitySchemaVersion {
		return errors.New("unsupported connectivity state version")
	}
	if status.State == "" || status.Mode == "" {
		return errors.New("invalid connectivity state values")
	}
	return nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("connectivity state must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return errors.New("connectivity state must be regular")
	}
	return nil
}

func transition(status contracts.Status, next contracts.State, mode contracts.Mode, code string) contracts.Status {
	status.State = next
	status.Mode = mode
	status.LastErrorCode = strings.TrimSpace(code)
	status.LastTransition = time.Now().UTC()
	return status
}
