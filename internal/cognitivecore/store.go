package cognitivecore

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"synora/pkg/contract"
)

const (
	MaxJournalEntries  = 512
	MaxDecisionEntries = 512
	MaxActionOutbox    = 128
)

type TrainingTrace struct {
	SchemaVersion string            `json:"schema_version"`
	Snapshot      CognitiveSnapshot `json:"snapshot"`
	Encoded       EncodedSnapshot   `json:"encoded"`
	Decision      Decision          `json:"decision"`
	Label         string            `json:"label,omitempty"`
	Provenance    string            `json:"provenance,omitempty"`
}

type ActionRequest struct {
	SchemaVersion string       `json:"schema_version"`
	RequestID     string       `json:"request_id"`
	EpisodeID     string       `json:"episode_id,omitempty"`
	Action        ActionIntent `json:"action"`
	DryRun        bool         `json:"dry_run"`
}

type Commit struct {
	Event       contract.Event    `json:"event"`
	Snapshot    CognitiveSnapshot `json:"snapshot"`
	Decision    Decision          `json:"decision"`
	Training    TrainingTrace     `json:"training"`
	Action      *ActionRequest    `json:"action,omitempty"`
	CommittedAt time.Time         `json:"committed_at"`
}

type CommitResult struct {
	Revision  uint64
	Duplicate bool
	Action    *ActionRequest
}

type UniversalStore struct {
	mu           sync.RWMutex
	revision     uint64
	journal      []Commit
	decisions    []Decision
	actionOutbox []ActionRequest
	processed    map[string]struct{}
	snapshot     CognitiveSnapshot
}

func NewUniversalStore() *UniversalStore {
	return &UniversalStore{revision: 1, journal: make([]Commit, 0, MaxJournalEntries), decisions: make([]Decision, 0, MaxDecisionEntries), actionOutbox: make([]ActionRequest, 0, MaxActionOutbox), processed: make(map[string]struct{}), snapshot: CognitiveSnapshot{SchemaVersion: SnapshotSchemaVersion, CapturedAt: time.Unix(0, 0).UTC(), Topology: "unknown", Episode: EpisodeFacts{Phase: "initial"}}}
}

func (s *UniversalStore) Commit(value Commit) (CommitResult, error) {
	if s == nil {
		return CommitResult{}, errors.New("universal store is nil")
	}
	if value.Event.ID == "" || value.Event.Type == "" {
		return CommitResult{}, errors.New("commit event id and type are required")
	}
	if err := value.Snapshot.Validate(); err != nil {
		return CommitResult{}, err
	}
	value.CommittedAt = value.CommittedAt.UTC()
	if value.CommittedAt.IsZero() {
		value.CommittedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.processed[value.Event.ID]; exists {
		return CommitResult{Revision: s.revision, Duplicate: true}, nil
	}
	s.revision++
	value.Snapshot.Revision = s.revision
	value.Snapshot = value.Snapshot.Normalized()
	value.Snapshot.Revision = s.revision
	value.Training.Snapshot = value.Snapshot
	s.processed[value.Event.ID] = struct{}{}
	s.journal = appendBounded(s.journal, value, MaxJournalEntries)
	s.decisions = appendBounded(s.decisions, value.Decision, MaxDecisionEntries)
	if value.Action != nil {
		value.Action.DryRun = true
		s.actionOutbox = appendBounded(s.actionOutbox, *value.Action, MaxActionOutbox)
	}
	s.snapshot = value.Snapshot
	return CommitResult{Revision: s.revision, Action: cloneAction(value.Action)}, nil
}

func (s *UniversalStore) Snapshot() CognitiveSnapshot {
	if s == nil {
		return CognitiveSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot.Normalized()
}
func (s *UniversalStore) SnapshotJSON() ([]byte, error) {
	if s == nil {
		return nil, errors.New("universal store is nil")
	}
	return s.Snapshot().CanonicalJSON()
}
func (s *UniversalStore) Revision() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}
func (s *UniversalStore) Journal() []Commit {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Commit(nil), s.journal...)
}
func (s *UniversalStore) Decisions() []Decision {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Decision(nil), s.decisions...)
}
func (s *UniversalStore) ActionOutbox() []ActionRequest {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ActionRequest(nil), s.actionOutbox...)
}

func (s *UniversalStore) Replay(events []contract.Event, processor func(contract.Event) error) error {
	for _, event := range events {
		if err := processor(event); err != nil {
			return err
		}
	}
	return nil
}

func appendBounded[T any](values []T, value T, max int) []T {
	values = append(values, value)
	if len(values) > max {
		values = values[len(values)-max:]
	}
	return values
}
func cloneAction(value *ActionRequest) *ActionRequest {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func (s *UniversalStore) MarshalJSON() ([]byte, error) {
	if s == nil {
		return nil, errors.New("universal store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return json.Marshal(struct {
		SchemaVersion string            `json:"schema_version"`
		Revision      uint64            `json:"revision"`
		Snapshot      CognitiveSnapshot `json:"snapshot"`
		Journal       []Commit          `json:"journal"`
		Decisions     []Decision        `json:"decisions"`
		ActionOutbox  []ActionRequest   `json:"action_outbox"`
	}{"universal-store/v1", s.revision, s.snapshot, s.journal, s.decisions, s.actionOutbox})
}
func (s *UniversalStore) ValidateBounds() error {
	if s == nil {
		return errors.New("universal store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.journal) > MaxJournalEntries || len(s.decisions) > MaxDecisionEntries || len(s.actionOutbox) > MaxActionOutbox {
		return fmt.Errorf("universal store bound exceeded")
	}
	return nil
}
