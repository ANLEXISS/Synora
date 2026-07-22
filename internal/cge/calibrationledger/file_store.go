package calibrationledger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type FileStore struct {
	mu           sync.RWMutex
	path         string
	file         *os.File
	policy       Policy
	index        ledgerIndex
	closed       bool
	lastRecovery RecoveryResult
}

func OpenFileStore(path string, policy Policy) (*FileStore, error) {
	if policy.Validate() != nil || filepath.Clean(path) != path || path == "" {
		return nil, ErrInvalidPolicy
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(parent); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidPolicy
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidPolicy
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return nil, err
	}
	s := &FileStore{path: path, file: f, policy: policy, index: newIndex()}
	if _, err := s.Recover(context.Background()); err != nil {
		_ = f.Close()
		return nil, err
	}
	return s, nil
}

func NewFileStore(path string, policy Policy) (*FileStore, error) { return OpenFileStore(path, policy) }

func (s *FileStore) Append(ctx context.Context, input CalibrationRecord) (AppendResult, error) {
	if err := contextErr(ctx); err != nil {
		return AppendResult{}, err
	}
	if s == nil {
		return AppendResult{}, ErrStoreClosed
	}
	if err := input.Validate(s.policy); err != nil {
		return AppendResult{}, err
	}
	// Canonicalize and reject an oversized record before taking the writer
	// lock. The final envelope is encoded and bounded again after sequence and
	// chain values are assigned under the lock.
	canonicalRecord, err := canonicalJSON(input)
	if err != nil {
		return AppendResult{}, fmt.Errorf("%w: %v", ErrInvalidRecord, err)
	}
	if len(canonicalRecord)+1 > s.policy.MaxRecordBytes {
		return AppendResult{}, ErrRecordTooLarge
	}
	if !shouldStore(input.Category, s.policy) {
		return AppendResult{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return AppendResult{}, ErrStoreClosed
	}
	if err := contextErr(ctx); err != nil {
		return AppendResult{}, err
	}
	if s.policy.DeduplicateByComparisonFingerprint {
		if pos, ok := s.index.byComparison[input.ComparisonFingerprint]; ok {
			old := s.index.records[pos]
			if old.RecordFingerprint != input.RecordFingerprint {
				return AppendResult{}, ErrDuplicateComparisonConflict
			}
			return AppendResult{Duplicate: true, Sequence: old.Sequence, RecordFingerprint: old.RecordFingerprint, EnvelopeFingerprint: s.index.envelopes[pos].EnvelopeHash}, nil
		}
	}
	expected := uint64(len(s.index.records) + 1)
	if input.Sequence == 0 {
		input.Sequence = expected
	} else if input.Sequence < expected {
		return AppendResult{}, ErrDuplicateSequence
	} else if input.Sequence > expected {
		return AppendResult{}, ErrSequenceGap
	}
	previousRecord := ""
	previousEnvelope := genesisHash(s.policy)
	if len(s.index.records) > 0 {
		previousRecord = s.index.records[len(s.index.records)-1].RecordFingerprint
		previousEnvelope = s.index.envelopes[len(s.index.envelopes)-1].EnvelopeHash
	}
	if input.PreviousRecordFingerprint != previousRecord {
		return AppendResult{}, ErrHashChainMismatch
	}
	envelope := makeEnvelope(input, previousEnvelope)
	encoded, err := canonicalJSON(envelope)
	if err != nil {
		return AppendResult{}, fmt.Errorf("%w: %v", ErrAppendFailed, err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > s.policy.MaxRecordBytes {
		return AppendResult{}, ErrRecordTooLarge
	}
	if s.index.bytes+int64(len(encoded)) > s.policy.MaxLedgerBytes || uint64(len(s.index.records)) >= s.policy.MaxRecords {
		return AppendResult{}, ErrLedgerLimitReached
	}
	if err := writeFull(s.file, encoded); err != nil {
		return AppendResult{}, fmt.Errorf("%w: %v", ErrAppendFailed, err)
	}
	if s.policy.Fsync {
		if err := s.file.Sync(); err != nil {
			return AppendResult{}, fmt.Errorf("%w: %v", ErrSyncFailed, err)
		}
	}
	if err := s.index.add(envelope, int64(len(encoded))); err != nil {
		return AppendResult{}, err
	}
	return AppendResult{Appended: true, Sequence: input.Sequence, RecordFingerprint: input.RecordFingerprint, EnvelopeFingerprint: envelope.EnvelopeHash}, nil
}

func (s *FileStore) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.index.snapshot())
}
func (s *FileStore) Summary() CalibrationSummary { return makeSummary(s.Snapshot()) }
func (s *FileStore) LastRecovery() RecoveryResult {
	if s == nil {
		return RecoveryResult{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRecovery
}
func (s *FileStore) LastRecord() (CalibrationRecord, bool) {
	if s == nil {
		return CalibrationRecord{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.index.records) == 0 {
		return CalibrationRecord{}, false
	}
	return s.index.records[len(s.index.records)-1].Clone(), true
}
func (s *FileStore) LedgerBytes() int64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.bytes
}

func (s *FileStore) Query(q Query) (QueryResult, error) {
	if s == nil {
		return QueryResult{}, ErrSnapshotUnavailable
	}
	if err := validateQuery(q); err != nil {
		return QueryResult{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	category := map[string]bool{}
	for _, v := range q.Categories {
		if v != "" {
			category[v] = true
		}
	}
	out := QueryResult{}
	for _, r := range s.index.records {
		if q.SequenceFrom != 0 && r.Sequence < q.SequenceFrom || q.SequenceTo != 0 && r.Sequence > q.SequenceTo {
			continue
		}
		if len(category) > 0 && !category[r.Category] {
			continue
		}
		if q.Comparable != nil && r.Comparable != *q.Comparable {
			continue
		}
		if q.SignificantDivergence != nil && r.SignificantDivergence != *q.SignificantDivergence {
			continue
		}
		out.Matched++
		if len(out.Records) < q.Limit {
			out.Records = append(out.Records, r.Clone())
		}
	}
	return out, nil
}

func (s *FileStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func shouldStore(category string, p Policy) bool {
	switch category {
	case "incomparable":
		return p.StoreIncomparable
	case "stale":
		return p.StoreStale
	case "invalidated":
		return p.StoreInvalidated
	default:
		return true
	}
}
func writeFull(f *os.File, data []byte) error {
	for len(data) > 0 {
		n, err := f.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return errors.New("zero write")
		}
		data = data[n:]
	}
	return nil
}
func cloneSnapshot(s Snapshot) Snapshot {
	original := s.CategoryCounts
	s.CategoryCounts = make(map[string]uint64, len(original))
	for k, v := range original {
		s.CategoryCounts[k] = v
	}
	return s
}
