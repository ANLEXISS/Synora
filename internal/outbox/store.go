// Package outbox owns durable, at-least-once delivery records. It is kept
// outside StateStore so file I/O never runs while the business-state mutex is
// held.
package outbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"synora/pkg/contract"
)

const fileVersion = 1

var (
	ErrNotFound      = errors.New("outbox record not found")
	ErrIdentityClash = errors.New("outbox identity already contains another message")
	ErrClosed        = errors.New("outbox is closed")
	ErrInvalidAck    = errors.New("outbox ACK does not match record")
)

type fileState struct {
	Version int                       `json:"version"`
	Records []contract.DeliveryRecord `json:"records"`
}

// Store is a small single-file durable outbox. Mutation serialization is
// separate from the record mutex: records are snapshotted under mu, then
// synced and renamed without holding mu.
type Store struct {
	path string

	mu      sync.RWMutex
	writeMu sync.Mutex
	closed  bool
	records map[string]contract.DeliveryRecord
	order   []string
	now     func() time.Time
}

func Open(path string, now func() time.Time) (*Store, error) {
	if path == "" {
		return nil, errors.New("outbox path is required")
	}
	if now == nil {
		now = time.Now
	}
	s := &Store{path: path, records: make(map[string]contract.DeliveryRecord), now: now}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read outbox: %w", err)
	}
	var state fileState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode outbox: %w", err)
	}
	if state.Version != fileVersion {
		return fmt.Errorf("unsupported outbox version %d", state.Version)
	}
	for _, record := range state.Records {
		if err := record.Validate(); err != nil {
			return fmt.Errorf("validate outbox record: %w", err)
		}
		key := record.Identity.Key()
		if _, exists := s.records[key]; exists {
			return fmt.Errorf("duplicate outbox identity %s", key)
		}
		s.records[key] = cloneRecord(record)
		s.order = append(s.order, key)
	}
	return nil
}

func (s *Store) Enqueue(message contract.Message) error {
	identity := message.DeliveryIdentity()
	if err := identity.Validate(); err != nil {
		return err
	}
	record := contract.DeliveryRecord{
		Identity:  identity,
		Message:   cloneMessage(message),
		State:     contract.DeliveryPending,
		CreatedAt: s.now().UTC(),
		UpdatedAt: s.now().UTC(),
	}
	return s.mutate(func() error {
		key := identity.Key()
		if previous, exists := s.records[key]; exists {
			if sameMessage(previous.Message, message) {
				return nil
			}
			return ErrIdentityClash
		}
		s.records[key] = cloneRecord(record)
		s.order = append(s.order, key)
		return nil
	})
}

func (s *Store) MarkInFlight(identity contract.DeliveryIdentity) error {
	return s.mutate(func() error {
		record, err := s.recordLocked(identity)
		if err != nil {
			return err
		}
		if err := contract.NextDeliveryState(record.State, contract.DeliveryInFlight); err != nil {
			return err
		}
		record.State = contract.DeliveryInFlight
		record.Attempts++
		record.UpdatedAt = s.now().UTC()
		s.records[identity.Key()] = record
		return nil
	})
}

func (s *Store) MarkRetry(identity contract.DeliveryIdentity, reason string, next time.Time) error {
	return s.mutate(func() error {
		record, err := s.recordLocked(identity)
		if err != nil {
			return err
		}
		if err := contract.NextDeliveryState(record.State, contract.DeliveryRetryWait); err != nil {
			return err
		}
		record.State = contract.DeliveryRetryWait
		record.NextAttemptAt = next.UTC()
		record.LastError = reason
		record.UpdatedAt = s.now().UTC()
		s.records[identity.Key()] = record
		return nil
	})
}

func (s *Store) Ack(ack contract.DeliveryAck) error {
	if err := ack.Validate(); err != nil {
		return err
	}
	return s.mutate(func() error {
		record, err := s.recordLocked(ack.Identity)
		if err != nil {
			return err
		}
		if record.Identity != ack.Identity {
			return ErrInvalidAck
		}
		if err := contract.NextDeliveryState(record.State, ack.State); err != nil {
			return err
		}
		record.State = ack.State
		record.AckedAt = s.now().UTC()
		record.UpdatedAt = record.AckedAt
		record.LastError = ack.Reason
		s.records[ack.Identity.Key()] = record
		return nil
	})
}

func (s *Store) Fail(identity contract.DeliveryIdentity, reason string) error {
	return s.finish(identity, contract.DeliveryFailed, reason)
}

func (s *Store) Quarantine(identity contract.DeliveryIdentity, reason string) error {
	return s.finish(identity, contract.DeliveryQuarantined, reason)
}

func (s *Store) finish(identity contract.DeliveryIdentity, state contract.DeliveryState, reason string) error {
	return s.mutate(func() error {
		record, err := s.recordLocked(identity)
		if err != nil {
			return err
		}
		if err := contract.NextDeliveryState(record.State, state); err != nil {
			return err
		}
		record.State = state
		record.LastError = reason
		record.UpdatedAt = s.now().UTC()
		s.records[identity.Key()] = record
		return nil
	})
}

func (s *Store) Get(identity contract.DeliveryIdentity) (contract.DeliveryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return contract.DeliveryRecord{}, ErrClosed
	}
	record, err := s.recordLocked(identity)
	if err != nil {
		return contract.DeliveryRecord{}, err
	}
	return cloneRecord(record), nil
}

// Ready returns bounded copies in insertion order. A retry is eligible only
// once its backoff deadline has elapsed.
func (s *Store) Ready(now time.Time, limit int) []contract.DeliveryRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || limit <= 0 {
		return []contract.DeliveryRecord{}
	}
	result := make([]contract.DeliveryRecord, 0, min(limit, len(s.order)))
	for _, key := range s.order {
		record, ok := s.records[key]
		if !ok || record.State == contract.DeliveryAcknowledged || record.State == contract.DeliveryFailed || record.State == contract.DeliveryQuarantined {
			continue
		}
		if record.State == contract.DeliveryRetryWait && record.NextAttemptAt.After(now) {
			continue
		}
		result = append(result, cloneRecord(record))
		if len(result) == limit {
			break
		}
	}
	return result
}

func (s *Store) Snapshot() []contract.DeliveryRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]contract.DeliveryRecord, 0, len(s.order))
	for _, key := range s.order {
		if record, ok := s.records[key]; ok {
			result = append(result, cloneRecord(record))
		}
	}
	return result
}

func (s *Store) CompactAcknowledged() error {
	return s.mutate(func() error {
		kept := s.order[:0]
		for _, key := range s.order {
			record, ok := s.records[key]
			if !ok || record.State == contract.DeliveryAcknowledged {
				delete(s.records, key)
				continue
			}
			kept = append(kept, key)
		}
		s.order = kept
		return nil
	})
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *Store) mutate(fn func() error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	previous := s.snapshotLocked()
	if err := fn(); err != nil {
		s.mu.Unlock()
		return err
	}
	state := s.snapshotLocked()
	s.mu.Unlock()
	if err := writeState(s.path, state); err != nil {
		s.mu.Lock()
		s.restoreLocked(previous)
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) recordLocked(identity contract.DeliveryIdentity) (contract.DeliveryRecord, error) {
	record, ok := s.records[identity.Key()]
	if !ok {
		return contract.DeliveryRecord{}, ErrNotFound
	}
	return cloneRecord(record), nil
}

func (s *Store) snapshotLocked() fileState {
	state := fileState{Version: fileVersion, Records: make([]contract.DeliveryRecord, 0, len(s.order))}
	for _, key := range s.order {
		if record, ok := s.records[key]; ok {
			state.Records = append(state.Records, cloneRecord(record))
		}
	}
	return state
}

func (s *Store) restoreLocked(state fileState) {
	s.records = make(map[string]contract.DeliveryRecord, len(state.Records))
	s.order = make([]string, 0, len(state.Records))
	for _, record := range state.Records {
		key := record.Identity.Key()
		s.records[key] = cloneRecord(record)
		s.order = append(s.order, key)
	}
}

func writeState(path string, state fileState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("create outbox directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".outbox-*.tmp")
	if err != nil {
		return fmt.Errorf("create outbox temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := json.NewEncoder(tmp).Encode(state); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encode outbox: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync outbox: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close outbox: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("commit outbox: %w", err)
	}
	committed = true
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func cloneRecord(record contract.DeliveryRecord) contract.DeliveryRecord {
	record.Message = cloneMessage(record.Message)
	return record
}

func cloneMessage(message contract.Message) contract.Message {
	if message.Payload != nil {
		message.Payload = append([]byte(nil), message.Payload...)
	}
	return message
}

func sameMessage(a, b contract.Message) bool {
	return a.ID == b.ID && a.Version == b.Version && a.Epoch == b.Epoch && a.Sequence == b.Sequence && a.Revision == b.Revision && a.Type == b.Type && a.Kind == b.Kind && a.Source == b.Source && a.Target == b.Target && a.SourceType == b.SourceType && a.Timestamp.Equal(b.Timestamp) && a.Priority == b.Priority && a.TrackID == b.TrackID && a.CorrelationID == b.CorrelationID && a.RequestID == b.RequestID && string(a.Payload) == string(b.Payload)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sortRecords is kept private for deterministic tests and future migration
// tooling; wire order remains insertion order for delivery semantics.
func sortRecords(records []contract.DeliveryRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Identity.Sequence < records[j].Identity.Sequence
	})
}
