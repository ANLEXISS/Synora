package durableworkflow

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"synora/internal/cge/episodes"
)

func testEpisode() *episodes.EpisodeSnapshot {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &episodes.EpisodeSnapshot{
		ID: "episode-test", Status: episodes.StatusOpen, CreatedAt: when, StartedAt: when,
		LastObservedAt: when, StatusChangedAt: when, Revision: 1,
		Observations: []episodes.ObservationRef{{EventID: "event-test", ObservedAt: when, Subject: episodes.SubjectRef{Kind: episodes.SubjectUnknown}}},
	}
}

func sourceMutation(state WorkflowState, episode *episodes.EpisodeSnapshot) WorkflowMutation {
	return WorkflowMutation{EpisodeID: string(episode.ID), Episode: episode, SourceWorkflowRevision: state.Revision, SourceWorkflowDigest: state.Digest}
}

func TestPlanPropagatesStalenessAndIsDefensive(t *testing.T) {
	policy := DefaultPolicy()
	state := initialWorkflowState(policy)
	episode := testEpisode()
	tx, result, err := PlanTransaction(state, sourceMutation(state, episode), "tx-one", 1, time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC), policy)
	if err != nil {
		t.Fatal(err)
	}
	if tx.ResultWorkflowDigest != result.Digest || result.Revision != 1 || result.LastSequence != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Episodes[0].Freshness[LayerEpisode] != FreshnessFresh || result.Episodes[0].Freshness[LayerSituationFacts] != FreshnessStale {
		t.Fatalf("unexpected freshness: %#v", result.Episodes[0].Freshness)
	}
	if err := ValidateWorkflowState(result); err != nil {
		t.Fatal(err)
	}
	episode.ID = "episode-mutated-outside"
	if result.Episodes[0].Episode.ID != "episode-test" {
		t.Fatal("planner leaked episode pointer")
	}
	if _, _, err := PlanTransaction(result, sourceMutation(state, testEpisode()), "tx-two", 2, time.Date(2026, 1, 2, 3, 6, 0, 0, time.UTC), policy); !errors.Is(err, ErrSourceRevisionConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
}

func TestFileStoreCommitReplayCheckpointAndIdempotence(t *testing.T) {
	directory := t.TempDir()
	policy := DefaultPolicy()
	store, err := OpenFileStore(directory, policy)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := Open(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	state := coordinator.Snapshot()
	episode := testEpisode()
	tx, err := coordinator.Plan(sourceMutation(state, episode), "tx-one", 1, time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	first, err := coordinator.Commit(tx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := coordinator.Commit(tx)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Applied || !second.Idempotent || first.Digest != second.Digest {
		t.Fatalf("unexpected commit results: %+v %+v", first, second)
	}
	if _, err := coordinator.CheckpointAt(time.Date(2026, 1, 2, 3, 7, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}

	reopenedStore, err := OpenFileStore(directory, policy)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(reopenedStore, policy)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Digest != first.Digest {
		t.Fatalf("replay digest mismatch: %s != %s", reopened.Snapshot().Digest, first.Digest)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecordChecksumAndTruncatedTail(t *testing.T) {
	policy := DefaultPolicy()
	payload := []byte(`{"ok":true}`)
	encoded, err := EncodeRecord(Record{Version: recordVersion, Sequence: 1, Kind: RecordTransaction, Payload: payload}, policy.MaxRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeRecord(encoded[:len(encoded)-1], policy.MaxRecordBytes); !errors.Is(err, ErrTruncatedRecord) {
		t.Fatalf("expected truncation, got %v", err)
	}
	checksumIndex := bytes.LastIndex(encoded, []byte("durable-workflow-record-v1:")) + len("durable-workflow-record-v1:")
	encoded[checksumIndex] = '0'
	if _, err := DecodeRecord(encoded, policy.MaxRecordBytes); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestPolicyAndReadiness(t *testing.T) {
	if err := DefaultPolicy().Validate(); err != nil {
		t.Fatal(err)
	}
	readiness := Readiness()
	if !readiness.Validate() || readiness.RuntimeIntegrated || readiness.SecurityAuthority {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
	if err := os.Chmod(t.TempDir(), 0700); err != nil {
		t.Fatal(err)
	}
}
