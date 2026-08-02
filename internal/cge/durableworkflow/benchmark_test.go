package durableworkflow

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"
)

type benchmarkStore struct {
	records    []Record
	checkpoint *Checkpoint
}

func (s *benchmarkStore) Append(record Record) error {
	s.records = append(s.records, record.Clone())
	return nil
}
func (s *benchmarkStore) Sync() error { return nil }
func (s *benchmarkStore) Load() (RecoveryInput, error) {
	input := RecoveryInput{}
	for _, record := range s.records {
		input.Records = append(input.Records, record.Clone())
	}
	if s.checkpoint != nil {
		copy := s.checkpoint.Clone()
		input.Checkpoint = &copy
	}
	return input, nil
}
func (s *benchmarkStore) WriteCheckpoint(checkpoint Checkpoint) error {
	copy := checkpoint.Clone()
	s.checkpoint = &copy
	return nil
}
func (s *benchmarkStore) Close() error { return nil }

func BenchmarkPlanTransactionEpisode(b *testing.B) {
	policy := DefaultPolicy()
	state := initialWorkflowState(policy)
	episode := testEpisode()
	mutation := sourceMutation(state, episode)
	created := time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, err := PlanTransaction(state, mutation, WorkflowTransactionID("benchmark-tx"), 1, created, policy)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecordEncode(b *testing.B) {
	policy := DefaultPolicy()
	payload, err := json.Marshal(Genesis{SchemaFingerprint: SchemaFingerprint(), PolicyFingerprint: policy.Fingerprint(), State: initialWorkflowState(policy), StateDigest: initialWorkflowState(policy).Digest})
	if err != nil {
		b.Fatal(err)
	}
	record := Record{Version: recordVersion, Sequence: 0, Kind: RecordGenesis, Payload: payload}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := EncodeRecord(record, policy.MaxRecordBytes); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStateDigest(b *testing.B) {
	state := initialWorkflowState(DefaultPolicy())
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = WorkflowStateFingerprint(state)
	}
}

func BenchmarkCommitEpisode(b *testing.B) {
	policy := DefaultPolicy()
	store := &benchmarkStore{}
	coordinator, err := Open(store, policy)
	if err != nil {
		b.Fatal(err)
	}
	created := time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		state := coordinator.Snapshot()
		episode := testEpisode()
		if len(state.Episodes) > 0 {
			episode.Revision = state.Episodes[0].Episode.Revision + 1
		}
		mutation := sourceMutation(state, episode)
		transaction, err := coordinator.Plan(mutation, WorkflowTransactionID("benchmark-commit-"+strconv.Itoa(i)), state.LastSequence+1, created)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := coordinator.Commit(transaction); err != nil {
			b.Fatal(err)
		}
	}
	_ = coordinator.Close()
}

func BenchmarkReplay(b *testing.B) {
	policy := DefaultPolicy()
	store := &benchmarkStore{}
	coordinator, err := Open(store, policy)
	if err != nil {
		b.Fatal(err)
	}
	state := coordinator.Snapshot()
	tx, err := coordinator.Plan(sourceMutation(state, testEpisode()), "benchmark-replay", 1, time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC))
	if err != nil {
		b.Fatal(err)
	}
	if _, err := coordinator.Commit(tx); err != nil {
		b.Fatal(err)
	}
	recovery, err := store.Load()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Replay(recovery, policy); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckpoint(b *testing.B) {
	policy := DefaultPolicy()
	store := &benchmarkStore{}
	coordinator, err := Open(store, policy)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := coordinator.CheckpointAt(time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC)); err != nil {
			b.Fatal(err)
		}
	}
	_ = coordinator.Close()
}

func TestConcurrentCommitOneWins(t *testing.T) {
	store, err := OpenFileStore(t.TempDir(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := Open(store, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	state := coordinator.Snapshot()
	first, err := coordinator.Plan(sourceMutation(state, testEpisode()), "tx-a", 1, time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.Plan(sourceMutation(state, testEpisode()), "tx-b", 1, time.Date(2026, 1, 2, 3, 6, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, transaction := range []WorkflowTransaction{first, second} {
		group.Add(1)
		go func(value WorkflowTransaction) {
			defer group.Done()
			_, commitErr := coordinator.Commit(value)
			results <- commitErr
		}(transaction)
	}
	group.Wait()
	close(results)
	var successes, conflicts int
	for err := range results {
		if err == nil {
			successes++
		} else if err == ErrSourceRevisionConflict {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}
