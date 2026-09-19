package cognitivecore

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"synora/pkg/contract"
)

const (
	MaxJournalEntries  = 512
	MaxDecisionEntries = 512
	MaxActionOutbox    = 128
	MaxProcessedIDs    = 1024
)

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
	Action      *ActionRequest    `json:"action,omitempty"`
	CommittedAt time.Time         `json:"committed_at"`
}

type CommitResult struct {
	Revision  uint64
	Duplicate bool
	Action    *ActionRequest
}

type storeDiskState struct {
	SchemaVersion string            `json:"schema_version"`
	Revision      uint64            `json:"revision"`
	Snapshot      CognitiveSnapshot `json:"snapshot"`
	Journal       []Commit          `json:"journal"`
	Decisions     []Decision        `json:"decisions"`
	ActionOutbox  []ActionRequest   `json:"action_outbox"`
	Processed     []string          `json:"processed"`
	Claimed       []string          `json:"claimed"`
}

type storeJournalRecord struct {
	SchemaVersion string `json:"schema_version"`
	Revision      uint64 `json:"revision"`
	Commit        Commit `json:"commit"`
}

// CompactionOptions controls archived-segment retention. A zero value keeps
// every immutable audit segment and never drops useful captures.
type CompactionOptions struct {
	RetainSegments int
	MaxSegmentAge  time.Duration
}

type StoreDiskReport struct {
	Revision           uint64        `json:"revision"`
	ActiveJournalBytes int64         `json:"active_journal_bytes"`
	ArchivedBytes      int64         `json:"archived_bytes"`
	SegmentCount       int           `json:"segment_count"`
	OldestSegmentAge   time.Duration `json:"oldest_segment_age"`
}

// PersistenceHooks are test-only fault injection points. A production Store
// leaves them nil. The hooks make crash-before/after-commit behavior explicit
// without weakening the normal fsync+rename path.
type PersistenceHooks struct {
	BeforeWAL   func() error
	AfterWAL    func() error
	BeforeState func() error
	AfterState  func() error
}

type UniversalStore struct {
	mu             sync.RWMutex
	revision       uint64
	journal        []Commit
	decisions      []Decision
	actionOutbox   []ActionRequest
	processed      map[string]struct{}
	processedOrder []string
	snapshot       CognitiveSnapshot
	claimed        map[string]struct{}
	dir            string
	hooks          PersistenceHooks
}

func NewUniversalStore() *UniversalStore {
	return newUniversalStore(1)
}

func newUniversalStore(revision uint64) *UniversalStore {
	return &UniversalStore{revision: revision, journal: make([]Commit, 0, MaxJournalEntries), decisions: make([]Decision, 0, MaxDecisionEntries), actionOutbox: make([]ActionRequest, 0, MaxActionOutbox), processed: make(map[string]struct{}), claimed: make(map[string]struct{}), snapshot: CognitiveSnapshot{SchemaVersion: SnapshotSchemaVersion, CapturedAt: time.Unix(0, 0).UTC(), Topology: "unknown", Episode: EpisodeFacts{Phase: "initial"}}}
}

// OpenUniversalStore opens or creates a durable local Store. The directory is
// private to Core; the append-only journal is the recovery source and the
// materialized state file makes inspection and restart cheap.
func OpenUniversalStore(dir string) (*UniversalStore, error) {
	if dir == "" {
		return nil, errors.New("universal store directory is required")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create universal store directory: %w", err)
	}
	s := newUniversalStore(1)
	s.dir = filepath.Clean(dir)
	statePath := filepath.Join(s.dir, "state.json")
	if body, err := os.ReadFile(statePath); err == nil {
		var disk storeDiskState
		if err := json.Unmarshal(body, &disk); err != nil {
			return nil, fmt.Errorf("universal store state is corrupt: %w", err)
		}
		if disk.SchemaVersion != "universal-store/v1" {
			return nil, fmt.Errorf("unsupported universal store state schema %q", disk.SchemaVersion)
		}
		s.revision, s.snapshot, s.journal, s.decisions, s.actionOutbox = disk.Revision, disk.Snapshot.Normalized(), disk.Journal, disk.Decisions, disk.ActionOutbox
		for _, id := range disk.Processed {
			s.processed[id] = struct{}{}
			s.processedOrder = append(s.processedOrder, id)
		}
		for _, id := range disk.Claimed {
			s.claimed[id] = struct{}{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read universal store state: %w", err)
	}
	if err := s.replayJournal(); err != nil {
		return nil, err
	}
	if err := s.validateArchivedSegments(); err != nil {
		return nil, err
	}
	if err := s.ValidateBounds(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *UniversalStore) SetPersistenceHooks(hooks PersistenceHooks) {
	if s != nil {
		s.hooks = hooks
	}
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
	nextRevision := s.revision + 1
	value.Snapshot.Revision = nextRevision
	value.Snapshot = value.Snapshot.Normalized()
	value.Snapshot.Revision = nextRevision
	if value.Action != nil {
		value.Action.DryRun = true
	}
	if s.dir != "" {
		if s.hooks.BeforeWAL != nil {
			if err := s.hooks.BeforeWAL(); err != nil {
				return CommitResult{}, err
			}
		}
		if err := s.appendWAL(storeJournalRecord{SchemaVersion: "universal-store/v1", Revision: nextRevision, Commit: value}); err != nil {
			return CommitResult{}, err
		}
		if s.hooks.AfterWAL != nil {
			if err := s.hooks.AfterWAL(); err != nil {
				return CommitResult{}, err
			}
		}
	}
	// Apply only after the durable record exists. A restart can recover a WAL
	// record left behind by a crash between WAL and materialized-state writes.
	s.revision = nextRevision
	s.applyCommitLocked(value)
	if s.dir != "" {
		if s.hooks.BeforeState != nil {
			if err := s.hooks.BeforeState(); err != nil {
				return CommitResult{}, err
			}
		}
		if err := s.persistStateLocked(); err != nil {
			return CommitResult{}, err
		}
		if s.hooks.AfterState != nil {
			if err := s.hooks.AfterState(); err != nil {
				return CommitResult{}, err
			}
		}
	}
	return CommitResult{Revision: s.revision, Action: cloneAction(value.Action)}, nil
}

func (s *UniversalStore) applyCommitLocked(value Commit) {
	s.processed[value.Event.ID] = struct{}{}
	s.processedOrder = append(s.processedOrder, value.Event.ID)
	if len(s.processedOrder) > MaxProcessedIDs {
		oldest := s.processedOrder[0]
		s.processedOrder = s.processedOrder[1:]
		delete(s.processed, oldest)
	}
	s.journal = appendBounded(s.journal, value, MaxJournalEntries)
	s.decisions = appendBounded(s.decisions, value.Decision, MaxDecisionEntries)
	if value.Action != nil {
		s.actionOutbox = appendBounded(s.actionOutbox, *value.Action, MaxActionOutbox)
	}
	s.snapshot = value.Snapshot
}

func (s *UniversalStore) appendWAL(record storeJournalRecord) error {
	file, err := os.OpenFile(filepath.Join(s.dir, "journal.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("open universal store journal: %w", err)
	}
	body, err := json.Marshal(record)
	if err == nil {
		_, err = file.Write(append(body, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("append universal store journal: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close universal store journal: %w", closeErr)
	}
	return nil
}

// Compact rotates the active WAL into an immutable audit segment after the
// materialized state has been durably written. Reopening after the rotation
// therefore has exactly the same state while History still sees every commit.
func (s *UniversalStore) Compact(options CompactionOptions) error {
	if s == nil {
		return errors.New("universal store is nil")
	}
	if s.dir == "" {
		return errors.New("universal store is not durable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.persistStateLocked(); err != nil {
		return err
	}
	journalPath := filepath.Join(s.dir, "journal.jsonl")
	body, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		body = nil
	} else if err != nil {
		return fmt.Errorf("read universal store journal for compaction: %w", err)
	}
	if len(body) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(body))
		scanner.Buffer(make([]byte, 64*1024), 8<<20)
		var first, last uint64
		for scanner.Scan() {
			var record storeJournalRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				return fmt.Errorf("universal store journal is corrupt: %w", err)
			}
			if first == 0 {
				first = record.Revision
			}
			last = record.Revision
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		if first != 0 {
			if err := os.MkdirAll(filepath.Join(s.dir, "segments"), 0o750); err != nil {
				return err
			}
			name := fmt.Sprintf("journal-%020d-%020d-%d.jsonl", first, last, time.Now().UnixNano())
			segmentPath := filepath.Join(s.dir, "segments", name)
			if err := os.WriteFile(segmentPath, body, 0o640); err != nil {
				return fmt.Errorf("write universal store audit segment: %w", err)
			}
			if err := syncFile(segmentPath); err != nil {
				return err
			}
			if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("rotate universal store journal: %w", err)
			}
		}
	}
	file, err := os.OpenFile(journalPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return fmt.Errorf("create compacted universal store journal: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := s.applyRetentionLocked(options); err != nil {
		return err
	}
	return syncDir(s.dir)
}

// History reads immutable audit segments plus the active journal in revision
// order. Journal() remains the bounded hot in-memory view.
func (s *UniversalStore) History() ([]Commit, error) {
	if s == nil {
		return nil, errors.New("universal store is nil")
	}
	if s.dir == "" {
		return s.Journal(), nil
	}
	records, err := s.readAllJournalRecords()
	if err != nil {
		return nil, err
	}
	commits := make([]Commit, 0, len(records))
	seen := make(map[uint64]struct{}, len(records))
	for _, record := range records {
		if _, ok := seen[record.Revision]; ok {
			continue
		}
		seen[record.Revision] = struct{}{}
		record.Commit.Snapshot.Revision = record.Revision
		commits = append(commits, record.Commit)
	}
	sort.Slice(commits, func(i, j int) bool { return commits[i].Snapshot.Revision < commits[j].Snapshot.Revision })
	return commits, nil
}

func (s *UniversalStore) DiskReport() (StoreDiskReport, error) {
	if s == nil || s.dir == "" {
		return StoreDiskReport{}, errors.New("universal store is not durable")
	}
	report := StoreDiskReport{Revision: s.Revision()}
	active, err := os.Stat(filepath.Join(s.dir, "journal.jsonl"))
	if err == nil {
		report.ActiveJournalBytes = active.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}
	entries, err := os.ReadDir(filepath.Join(s.dir, "segments"))
	if errors.Is(err, os.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return report, err
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return report, statErr
		}
		report.SegmentCount++
		report.ArchivedBytes += info.Size()
		if age := now.Sub(info.ModTime()); age > report.OldestSegmentAge {
			report.OldestSegmentAge = age
		}
	}
	return report, nil
}

func (s *UniversalStore) readAllJournalRecords() ([]storeJournalRecord, error) {
	paths := []string{}
	entries, err := os.ReadDir(filepath.Join(s.dir, "segments"))
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
				paths = append(paths, filepath.Join(s.dir, "segments", entry.Name()))
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	paths = append(paths, filepath.Join(s.dir, "journal.jsonl"))
	sort.Strings(paths)
	var records []storeJournalRecord
	for _, path := range paths {
		body, readErr := os.ReadFile(path)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return nil, readErr
		}
		scanner := bufio.NewScanner(bytes.NewReader(body))
		scanner.Buffer(make([]byte, 64*1024), 8<<20)
		for scanner.Scan() {
			var record storeJournalRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				return nil, fmt.Errorf("universal store audit segment %s is corrupt: %w", path, err)
			}
			if record.SchemaVersion != "universal-store/v1" || record.Revision == 0 {
				return nil, fmt.Errorf("universal store audit segment %s has invalid record", path)
			}
			records = append(records, record)
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (s *UniversalStore) validateArchivedSegments() error {
	if s == nil || s.dir == "" {
		return nil
	}
	_, err := s.readAllJournalRecords()
	return err
}

func (s *UniversalStore) applyRetentionLocked(options CompactionOptions) error {
	entries, err := os.ReadDir(filepath.Join(s.dir, "segments"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	type segment struct {
		path string
		mod  time.Time
	}
	segments := make([]segment, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		segments = append(segments, segment{filepath.Join(s.dir, "segments", entry.Name()), info.ModTime()})
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].mod.Before(segments[j].mod) })
	remove := map[string]bool{}
	if options.RetainSegments > 0 && len(segments) > options.RetainSegments {
		for _, item := range segments[:len(segments)-options.RetainSegments] {
			remove[item.path] = true
		}
	}
	if options.MaxSegmentAge > 0 {
		cutoff := time.Now().Add(-options.MaxSegmentAge)
		for _, item := range segments {
			if item.mod.Before(cutoff) {
				remove[item.path] = true
			}
		}
	}
	for path := range remove {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (s *UniversalStore) persistStateLocked() error {
	state := s.diskStateLocked()
	body, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create universal store state: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(append(body, '\n')); err != nil {
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
	if err := os.Rename(tmpName, filepath.Join(s.dir, "state.json")); err != nil {
		return fmt.Errorf("replace universal store state: %w", err)
	}
	dirFile, err := os.Open(s.dir)
	if err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return err
}

func (s *UniversalStore) diskStateLocked() storeDiskState {
	processed := append([]string(nil), s.processedOrder...)
	if len(processed) == 0 {
		for id := range s.processed {
			processed = append(processed, id)
		}
	}
	claimed := make([]string, 0, len(s.claimed))
	for id := range s.claimed {
		claimed = append(claimed, id)
	}
	sort.Strings(processed)
	sort.Strings(claimed)
	return storeDiskState{"universal-store/v1", s.revision, s.snapshot, append([]Commit(nil), s.journal...), append([]Decision(nil), s.decisions...), append([]ActionRequest(nil), s.actionOutbox...), processed, claimed}
}

func (s *UniversalStore) replayJournal() error {
	file, err := os.Open(filepath.Join(s.dir, "journal.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open universal store journal: %w", err)
	}
	defer file.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	for scanner.Scan() {
		var record storeJournalRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("universal store journal is corrupt: %w", err)
		}
		if record.SchemaVersion != "universal-store/v1" || record.Revision == 0 {
			return errors.New("universal store journal record is invalid")
		}
		if _, exists := s.processed[record.Commit.Event.ID]; exists {
			continue
		}
		if record.Revision <= s.revision {
			continue
		}
		s.revision = record.Revision
		record.Commit.Snapshot.Revision = record.Revision
		s.applyCommitLocked(record.Commit)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read universal store journal: %w", err)
	}
	return nil
}

// ClaimPendingActions atomically marks durable outbox entries as delivered.
// The request ID is stable, so Discovery can retry safely without replaying a
// physical action.
func (s *UniversalStore) ClaimPendingActions() ([]ActionRequest, error) {
	if s == nil {
		return nil, errors.New("universal store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	claimed := make([]ActionRequest, 0)
	for _, action := range s.actionOutbox {
		if _, ok := s.claimed[action.RequestID]; ok {
			continue
		}
		s.claimed[action.RequestID] = struct{}{}
		claimed = append(claimed, action)
	}
	if len(claimed) > 0 && s.dir != "" {
		if err := s.persistStateLocked(); err != nil {
			return nil, err
		}
	}
	return claimed, nil
}

func (s *UniversalStore) AcknowledgeAction(requestID string) error {
	if requestID == "" {
		return errors.New("request id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimed == nil {
		s.claimed = make(map[string]struct{})
	}
	s.claimed[requestID] = struct{}{}
	if s.dir != "" {
		return s.persistStateLocked()
	}
	return nil
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

// HistoryJSON is the read-only Discovery view of the append-only audit. The
// Store remains the writer; callers receive a serialized copy only.
func (s *UniversalStore) HistoryJSON() ([]byte, error) {
	if s == nil {
		return nil, errors.New("universal store is nil")
	}
	history, err := s.History()
	if err != nil {
		return nil, err
	}
	return json.Marshal(history)
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
	return json.Marshal(s.diskStateLocked())
}
func (s *UniversalStore) ValidateBounds() error {
	if s == nil {
		return errors.New("universal store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.journal) > MaxJournalEntries || len(s.decisions) > MaxDecisionEntries || len(s.actionOutbox) > MaxActionOutbox || len(s.processedOrder) > MaxProcessedIDs {
		return fmt.Errorf("universal store bound exceeded")
	}
	return nil
}
