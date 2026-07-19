package replay

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/persistence"
	"synora/internal/cge/chains/registry"
)

var replayTestBase = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func replayMutation(at time.Time, actor, correlation, reason string) chains.MutationContext {
	return chains.MutationContext{At: at, Actor: actor, CorrelationID: correlation, Reason: reason}
}

func newReplayJournal(t *testing.T) *journal.FileJournal {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cge.ndjson")
	fileJournal, err := journal.NewFileJournal(path, journal.FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		t.Fatalf("new journal: %v", err)
	}
	if _, err := fileJournal.Initialize(context.Background(), journal.GenesisInput{
		JournalID: "replay-journal", CreatedAt: replayTestBase, RecordedAt: replayTestBase,
		Purpose: "replay test", Actor: "test", CorrelationID: "genesis",
	}); err != nil {
		t.Fatalf("initialize journal: %v", err)
	}
	return fileJournal
}

func newReplayChain(t *testing.T, id string, at time.Time) *chains.Chain {
	t.Helper()
	chain, err := chains.New(chains.ChainID(id), replayMutation(at, "builder", "create-"+id, "create chain"))
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	return chain
}

func appendChainAdded(t *testing.T, fileJournal *journal.FileJournal, chain *chains.Chain, at time.Time) journal.Record {
	t.Helper()
	record, err := fileJournal.AppendChainAdded(context.Background(), journal.ChainAddedInput{
		Chain: chain.Snapshot(), RecordedAt: at, Actor: "journal", CorrelationID: "add-" + string(chain.Snapshot().ID),
	})
	if err != nil {
		t.Fatalf("append chain.added: %v", err)
	}
	return record
}

func appendTransition(t *testing.T, fileJournal *journal.FileJournal, chain *chains.Chain) journal.Record {
	t.Helper()
	history := chain.History()
	revision := history[len(history)-1]
	record, err := fileJournal.AppendLifecycleTransition(context.Background(), journal.LifecycleTransitionInput{
		ChainID: chain.Snapshot().ID, PreviousRevision: revision.PreviousRevision, NewRevision: revision.NewRevision,
		From: revision.PreviousStatus, To: revision.NewStatus, Revision: revision,
		RecordedAt: revision.At, Actor: revision.Actor, CorrelationID: revision.CorrelationID,
	})
	if err != nil {
		t.Fatalf("append lifecycle transition: %v", err)
	}
	return record
}

func readReplayJournal(t *testing.T, fileJournal *journal.FileJournal) journal.JournalSnapshot {
	t.Helper()
	snapshot, err := fileJournal.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	return snapshot
}

func TestFromJournalReconstructsChainsAndLocalHistory(t *testing.T) {
	fileJournal := newReplayJournal(t)
	chainA := newReplayChain(t, "chain-a", replayTestBase.Add(time.Second))
	appendChainAdded(t, fileJournal, chainA, replayTestBase.Add(2*time.Second))
	if err := chainA.SetStatus(chains.StatusActive, replayMutation(replayTestBase.Add(3*time.Second), "lifecycle", "a-active", "activate")); err != nil {
		t.Fatalf("activate A: %v", err)
	}
	appendTransition(t, fileJournal, chainA)
	if err := chainA.SetStatus(chains.StatusDeclining, replayMutation(replayTestBase.Add(4*time.Second), "lifecycle", "a-decline", "decline")); err != nil {
		t.Fatalf("decline A: %v", err)
	}
	appendTransition(t, fileJournal, chainA)

	chainB := newReplayChain(t, "chain-b", replayTestBase.Add(5*time.Second))
	appendChainAdded(t, fileJournal, chainB, replayTestBase.Add(6*time.Second))
	journalSnapshot := readReplayJournal(t, fileJournal)

	replayed, metadata, err := FromJournal(context.Background(), journalSnapshot)
	if err != nil {
		t.Fatalf("replay journal: %v", err)
	}
	expected := registry.New()
	if err := expected.Add(chainA); err != nil {
		t.Fatalf("add expected A: %v", err)
	}
	if err := expected.Add(chainB); err != nil {
		t.Fatalf("add expected B: %v", err)
	}
	if got, want := replayed.List(), expected.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("replayed chains differ\ngot:  %#v\nwant: %#v", got, want)
	}
	if metadata.Mode != ReplayModeJournalOnly || metadata.RecordsExamined != 5 || metadata.RecordsApplied != 4 || metadata.ChainsAdded != 2 || metadata.TransitionsApplied != 2 || metadata.FinalChainCount != 2 {
		t.Fatalf("unexpected replay metadata: %#v", metadata)
	}
	if metadata.FirstAppliedSequence != 2 || metadata.LastAppliedSequence != 5 || metadata.FinalHeadSequence != journalSnapshot.HeadSequence || metadata.FinalHeadHash != journalSnapshot.HeadHash {
		t.Fatalf("unexpected replay sequence metadata: %#v", metadata)
	}
}

func TestFromJournalReplaysObservationAddedThroughDomainOperation(t *testing.T) {
	fileJournal := newReplayJournal(t)
	chain := newReplayChain(t, "observation-chain", replayTestBase)
	appendChainAdded(t, fileJournal, chain, replayTestBase.Add(time.Second))
	observation := chains.ObservationRef{ID: "observation-1", EventType: "vision.motion", Timestamp: replayTestBase.Add(2 * time.Second), NodeID: "entry"}
	mutation := replayMutation(replayTestBase.Add(3*time.Second), "observer", "observation-1", "explicit observation")
	if err := chain.AddObservation(observation, mutation); err != nil {
		t.Fatalf("domain observation: %v", err)
	}
	revision := chain.History()[len(chain.History())-1]
	if _, err := fileJournal.AppendObservationAdded(context.Background(), journal.ObservationAddedInput{
		ChainID: chain.Snapshot().ID, PreviousRevision: revision.PreviousRevision, NewRevision: revision.NewRevision,
		Observation: observation, Revision: revision, RecordedAt: replayTestBase.Add(time.Hour), Actor: mutation.Actor, CorrelationID: mutation.CorrelationID,
	}); err != nil {
		t.Fatalf("append observation: %v", err)
	}
	replayed, metadata, err := FromJournal(context.Background(), readReplayJournal(t, fileJournal))
	if err != nil {
		t.Fatalf("replay observation: %v", err)
	}
	want := registry.New()
	if err := want.Add(chain); err != nil {
		t.Fatalf("add expected chain: %v", err)
	}
	if !reflect.DeepEqual(replayed.List(), want.List()) || metadata.ObservationsAdded != 1 || metadata.RecordsApplied != 2 {
		t.Fatalf("observation replay mismatch: got=%#v want=%#v metadata=%#v", replayed.List(), want.List(), metadata)
	}
}

func TestFromJournalGenesisOnlyAndCheckpointAreReadOnly(t *testing.T) {
	fileJournal := newReplayJournal(t)
	journalSnapshot := readReplayJournal(t, fileJournal)
	replayed, metadata, err := FromJournal(context.Background(), journalSnapshot)
	if err != nil {
		t.Fatalf("genesis-only replay: %v", err)
	}
	if replayed.Count() != 0 || metadata.RecordsApplied != 0 || metadata.FinalChainCount != 0 {
		t.Fatalf("genesis-only replay changed state: count=%d metadata=%#v", replayed.Count(), metadata)
	}

	chain := newReplayChain(t, "checkpointed", replayTestBase.Add(time.Second))
	added := appendChainAdded(t, fileJournal, chain, replayTestBase.Add(2*time.Second))
	checkpoint, err := fileJournal.AppendSnapshotCheckpoint(context.Background(), journal.SnapshotCheckpointInput{
		SnapshotSchemaVersion: persistence.CurrentSchemaVersion,
		SnapshotCreatedAt:     replayTestBase.Add(time.Hour), SnapshotChainCount: 1,
		SnapshotPayloadSHA256: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		SnapshotSizeBytes:     100, JournalSequence: added.Sequence, JournalHeadHash: added.RecordHash,
		RecordedAt: replayTestBase.Add(3 * time.Second), Actor: "snapshot", CorrelationID: "checkpoint",
	})
	if err != nil {
		t.Fatalf("append checkpoint: %v", err)
	}
	read := readReplayJournal(t, fileJournal)
	replayed, metadata, err = FromJournal(context.Background(), read)
	if err != nil {
		t.Fatalf("checkpoint replay: %v", err)
	}
	if replayed.Count() != 1 || metadata.CheckpointsSkipped != 1 || metadata.RecordsApplied != 1 || checkpoint.Sequence != read.HeadSequence {
		t.Fatalf("unexpected checkpoint replay: count=%d metadata=%#v", replayed.Count(), metadata)
	}
}

func TestFromSnapshotAndJournalAppliesOnlyPostCheckpointDeltas(t *testing.T) {
	fileJournal := newReplayJournal(t)
	chainA := newReplayChain(t, "chain-a", replayTestBase.Add(time.Second))
	addedA := appendChainAdded(t, fileJournal, chainA, replayTestBase.Add(2*time.Second))

	snapshotRegistry := registry.New()
	if err := snapshotRegistry.Add(chainA); err != nil {
		t.Fatalf("add snapshot chain: %v", err)
	}
	store, err := persistence.NewFileStore(filepath.Join(t.TempDir(), "snapshot.json"))
	if err != nil {
		t.Fatalf("new snapshot store: %v", err)
	}
	snapshotMetadata, err := store.Save(context.Background(), snapshotRegistry, replayTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if _, err := fileJournal.AppendSnapshotCheckpoint(context.Background(), journal.SnapshotCheckpointInput{
		SnapshotSchemaVersion: snapshotMetadata.SchemaVersion,
		SnapshotCreatedAt:     snapshotMetadata.CreatedAt, SnapshotChainCount: snapshotMetadata.ChainCount,
		SnapshotPayloadSHA256: snapshotMetadata.PayloadSHA256, SnapshotSizeBytes: snapshotMetadata.SizeBytes,
		JournalSequence: addedA.Sequence, JournalHeadHash: addedA.RecordHash,
		RecordedAt: replayTestBase.Add(3 * time.Second), Actor: "snapshot", CorrelationID: "checkpoint",
	}); err != nil {
		t.Fatalf("append snapshot checkpoint: %v", err)
	}
	if err := chainA.SetStatus(chains.StatusActive, replayMutation(replayTestBase.Add(4*time.Second), "lifecycle", "a-active", "activate")); err != nil {
		t.Fatalf("activate A: %v", err)
	}
	appendTransition(t, fileJournal, chainA)
	chainB := newReplayChain(t, "chain-b", replayTestBase.Add(5*time.Second))
	appendChainAdded(t, fileJournal, chainB, replayTestBase.Add(6*time.Second))

	sourceBefore := snapshotRegistry.List()
	replayed, metadata, err := FromSnapshotAndJournal(context.Background(), snapshotRegistry, snapshotMetadata, readReplayJournal(t, fileJournal))
	if err != nil {
		t.Fatalf("snapshot+journal replay: %v", err)
	}
	expected := registry.New()
	if err := expected.Add(chainA); err != nil {
		t.Fatalf("add expected A: %v", err)
	}
	if err := expected.Add(chainB); err != nil {
		t.Fatalf("add expected B: %v", err)
	}
	if !reflect.DeepEqual(replayed.List(), expected.List()) {
		t.Fatalf("post-checkpoint replay differs\ngot: %#v\nwant: %#v", replayed.List(), expected.List())
	}
	if !reflect.DeepEqual(snapshotRegistry.List(), sourceBefore) {
		t.Fatal("snapshot source registry was mutated")
	}
	if metadata.Mode != ReplayModeSnapshotAndJournal || !metadata.SnapshotUsed || metadata.CheckpointSequence != 3 || metadata.RecordsApplied != 2 || metadata.FirstAppliedSequence != 4 || metadata.LastAppliedSequence != 5 {
		t.Fatalf("unexpected snapshot replay metadata: %#v", metadata)
	}
}

func TestFromSnapshotAndJournalRejectsCheckpointMismatchWithoutPartialResult(t *testing.T) {
	fileJournal := newReplayJournal(t)
	chain := newReplayChain(t, "chain-a", replayTestBase.Add(time.Second))
	added := appendChainAdded(t, fileJournal, chain, replayTestBase.Add(2*time.Second))
	source := registry.New()
	if err := source.Add(chain); err != nil {
		t.Fatalf("add source: %v", err)
	}
	metadata := persistence.SnapshotMetadata{
		SchemaVersion: persistence.CurrentSchemaVersion, CreatedAt: replayTestBase.Add(time.Hour), ChainCount: 1,
		PayloadSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SizeBytes: 99,
	}
	if _, err := fileJournal.AppendSnapshotCheckpoint(context.Background(), journal.SnapshotCheckpointInput{
		SnapshotSchemaVersion: metadata.SchemaVersion, SnapshotCreatedAt: metadata.CreatedAt, SnapshotChainCount: metadata.ChainCount,
		SnapshotPayloadSHA256: metadata.PayloadSHA256, SnapshotSizeBytes: metadata.SizeBytes,
		JournalSequence: added.Sequence, JournalHeadHash: added.RecordHash,
		RecordedAt: replayTestBase.Add(3 * time.Second), Actor: "snapshot", CorrelationID: "checkpoint",
	}); err != nil {
		t.Fatalf("append checkpoint: %v", err)
	}
	metadata.SizeBytes++
	_, _, err := FromSnapshotAndJournal(context.Background(), source, metadata, readReplayJournal(t, fileJournal))
	if err == nil || !errors.Is(err, ErrCheckpointMetadataMismatch) {
		t.Fatalf("expected checkpoint metadata mismatch, got %v", err)
	}
}

func TestReplayRejectsSemanticCorruptionAndReturnsNoPartialRegistry(t *testing.T) {
	fileJournal := newReplayJournal(t)
	chain := newReplayChain(t, "chain-a", replayTestBase.Add(time.Second))
	appendChainAdded(t, fileJournal, chain, replayTestBase.Add(2*time.Second))
	if err := chain.SetStatus(chains.StatusActive, replayMutation(replayTestBase.Add(3*time.Second), "lifecycle", "a-active", "activate")); err != nil {
		t.Fatalf("activate chain: %v", err)
	}
	appendTransition(t, fileJournal, chain)
	source := readReplayJournal(t, fileJournal).Clone()
	var payload journal.LifecycleTransitionPayload
	if err := decodePayload(source.Records[2].Payload, &payload); err != nil {
		t.Fatalf("decode lifecycle payload: %v", err)
	}
	payload.From = chains.StatusConfirmed
	// The payload is intentionally changed without changing the registry or
	// journal source. Replay must reject the logical mismatch and return nil.
	corrupted, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode corrupted payload: %v", err)
	}
	source.Records[2].Payload = corrupted
	if replayed, _, err := FromJournal(context.Background(), source); err == nil || replayed != nil || !errors.Is(err, ErrInvalidTransition) && !errors.Is(err, ErrRevisionRecordMismatch) && !errors.Is(err, ErrStatusMismatch) {
		t.Fatalf("expected semantic replay failure without partial result, registry=%v err=%v", replayed, err)
	}
}

func TestReplayIsDeterministicAndContextCancellationReturnsNoResult(t *testing.T) {
	fileJournal := newReplayJournal(t)
	for _, id := range []string{"a", "b", "c"} {
		appendChainAdded(t, fileJournal, newReplayChain(t, id, replayTestBase.Add(time.Second)), replayTestBase.Add(2*time.Second))
	}
	input := readReplayJournal(t, fileJournal)
	first, firstMetadata, err := FromJournal(context.Background(), input)
	if err != nil {
		t.Fatalf("first replay: %v", err)
	}
	second, secondMetadata, err := FromJournal(context.Background(), input)
	if err != nil {
		t.Fatalf("second replay: %v", err)
	}
	if !reflect.DeepEqual(first.List(), second.List()) || !reflect.DeepEqual(firstMetadata, secondMetadata) {
		t.Fatalf("replay is not deterministic: first=%#v/%#v second=%#v/%#v", first.List(), firstMetadata, second.List(), secondMetadata)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if replayed, _, err := FromJournal(cancelled, input); err == nil || replayed != nil || !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("expected cancelled replay, registry=%v err=%v", replayed, err)
	}
}

func TestReplayVolumeAndConcurrentReconstructions(t *testing.T) {
	fileJournal := newReplayJournal(t)
	const chainCount = 500
	for index := 0; index < chainCount; index++ {
		id := "chain-" + strconv.Itoa(index)
		appendChainAdded(t, fileJournal, newReplayChain(t, id, replayTestBase.Add(time.Second)), replayTestBase.Add(2*time.Second))
	}
	input := readReplayJournal(t, fileJournal)
	if input.RecordCount != chainCount+1 {
		t.Fatalf("record count = %d, want %d", input.RecordCount, chainCount+1)
	}

	const workers = 4
	results := make([][]chains.Snapshot, workers)
	errorsByWorker := make([]error, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, _, err := FromJournal(context.Background(), input)
			if err != nil {
				errorsByWorker[worker] = err
				return
			}
			results[worker] = result.List()
		}()
	}
	wait.Wait()
	for worker, err := range errorsByWorker {
		if err != nil {
			t.Fatalf("worker %d replay: %v", worker, err)
		}
		if len(results[worker]) != chainCount {
			t.Fatalf("worker %d chain count = %d, want %d", worker, len(results[worker]), chainCount)
		}
		if worker > 0 && !reflect.DeepEqual(results[0], results[worker]) {
			t.Fatalf("worker %d produced a different reconstruction", worker)
		}
	}
}
