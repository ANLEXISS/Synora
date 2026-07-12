package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"synora/internal/configfile"
	"synora/pkg/contract"
)

const PersistedStateVersion = 2

type Persistence interface {
	Load() (*PersistedState, error)
	Save(*PersistedState) error
	Close() error
}

type BackupPersistence interface {
	Backup() error
}

type PersistedState struct {
	Version           int                                     `json:"version"`
	SavedAt           time.Time                               `json:"saved_at"`
	Clips             map[string]ClipState                    `json:"clips,omitempty"`
	Validations       map[string]contract.ValidationRequest   `json:"validations,omitempty"`
	BehaviorOverrides map[string]json.RawMessage              `json:"learned_behavior_overrides,omitempty"`
	ActionResults     map[string]contract.ActionResult        `json:"action_results,omitempty"`
	Danger            []*contract.DangerAssessment            `json:"danger_assessments,omitempty"`
	Events            []*contract.Event                       `json:"events,omitempty"`
	Identities        map[string]IdentityState                `json:"identities,omitempty"`
	Presence          map[string]PresenceState                `json:"presence,omitempty"`
	EventChains       map[string]contract.EventChain          `json:"event_chains,omitempty"`
	CriticalChains    map[string]contract.CriticalChainMemory `json:"critical_chain_memories,omitempty"`
}

type PersistedSummary struct {
	Events        int
	Clips         int
	Validations   int
	ActionResults int
	Danger        int
	Identities    int
	Presence      int
}

type FilePersistence struct {
	path string
	mu   sync.Mutex
}

func NewFilePersistence(path string) *FilePersistence {
	return &FilePersistence{path: path}
}

func DefaultStatePath() string {
	if path := os.Getenv("SYNORA_STATE_PATH"); path != "" {
		return path
	}
	if dir := os.Getenv("SYNORA_STATE_DIR"); dir != "" {
		return filepath.Join(dir, "state.json")
	}
	return "/var/lib/synora/state/state.json"
}

func (p *FilePersistence) Load() (*PersistedState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyPersistedState(), nil
	}
	if err != nil {
		return emptyPersistedState(), err
	}

	var state PersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		renameCorrupt(p.path)
		return emptyPersistedState(), fmt.Errorf("decode persisted state: %w", err)
	}
	if err := migratePersistedState(&state); err != nil {
		return emptyPersistedState(), err
	}
	return &state, nil
}

func (p *FilePersistence) Save(state *PersistedState) error {
	if state == nil {
		state = emptyPersistedState()
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(p.path), 0750); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(p.path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(state); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, p.path); err != nil {
		return err
	}
	committed = true
	syncDir(filepath.Dir(p.path))
	return nil
}

func (p *FilePersistence) Close() error {
	return nil
}

func (p *FilePersistence) Backup() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := configfile.BackupExisting(p.path, 0o640)
	return err
}

func migratePersistedState(state *PersistedState) error {
	if state == nil {
		return nil
	}
	switch state.Version {
	case PersistedStateVersion:
		return nil
	case 1:
		state.Version = PersistedStateVersion
		if state.BehaviorOverrides == nil {
			state.BehaviorOverrides = map[string]json.RawMessage{}
		}
		return nil
	default:
		return fmt.Errorf("unsupported persisted state version %d", state.Version)
	}
}

func emptyPersistedState() *PersistedState {
	return &PersistedState{
		Version:           PersistedStateVersion,
		Clips:             map[string]ClipState{},
		Validations:       map[string]contract.ValidationRequest{},
		BehaviorOverrides: map[string]json.RawMessage{},
		ActionResults:     map[string]contract.ActionResult{},
		Danger:            []*contract.DangerAssessment{},
		Events:            []*contract.Event{},
		Identities:        map[string]IdentityState{},
		Presence:          map[string]PresenceState{},
		EventChains:       map[string]contract.EventChain{},
		CriticalChains:    map[string]contract.CriticalChainMemory{},
	}
}

func renameCorrupt(path string) {
	suffix := time.Now().UTC().Format("20060102T150405Z")
	_ = os.Rename(path, path+".corrupt."+suffix)
}

func syncDir(path string) {
	dir, err := os.Open(path)
	if err != nil {
		return
	}
	defer dir.Close()
	_ = dir.Sync()
}
