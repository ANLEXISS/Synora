package persistence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/registry"
)

var persistenceTestBase = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func persistenceMutation(at time.Time, reason string) chains.MutationContext {
	return chains.MutationContext{At: at, Actor: "test", Reason: reason, CorrelationID: "persistence-test"}
}

func persistentChain(t *testing.T, id string) *chains.Chain {
	t.Helper()
	chain, err := chains.New(chains.ChainID(id), persistenceMutation(persistenceTestBase, "create chain"))
	if err != nil {
		t.Fatalf("create chain: %v", err)
	}
	observation := chains.ObservationRef{
		ID: "observation-" + id, EventType: "vision.motion",
		Timestamp: persistenceTestBase.Add(time.Second), NodeID: "entry",
		DeviceID: "camera-1", ActivationID: "activation-1", ClipID: "clip-1",
		ClipIndex: 2, TrackID: "track-1", SequenceKey: "sequence-1",
	}
	if err := chain.AddObservation(observation, persistenceMutation(persistenceTestBase.Add(time.Second), "add observation")); err != nil {
		t.Fatalf("add observation: %v", err)
	}
	if err := chain.AddContribution(chains.ConfidenceContribution{
		ID: "support-" + id, Source: "test-source", Kind: chains.ContributionSupport,
		Value: 0.6, ObservationIDs: []string{observation.ID}, Reason: "detached support",
		CreatedAt: persistenceTestBase.Add(2 * time.Second),
	}, persistenceMutation(persistenceTestBase.Add(2*time.Second), "add support")); err != nil {
		t.Fatalf("add contribution: %v", err)
	}
	if err := chain.AssignEntity("entity-"+id, persistenceMutation(persistenceTestBase.Add(3*time.Second), "assign entity")); err != nil {
		t.Fatalf("assign entity: %v", err)
	}
	if err := chain.SetStatus(chains.StatusActive, persistenceMutation(persistenceTestBase.Add(4*time.Second), "activate chain")); err != nil {
		t.Fatalf("set active: %v", err)
	}
	if err := chain.SetHistoricalReliability(0.8, persistenceMutation(persistenceTestBase.Add(5*time.Second), "set reliability")); err != nil {
		t.Fatalf("set reliability: %v", err)
	}
	if err := chain.SetConfidence(0.7, persistenceMutation(persistenceTestBase.Add(6*time.Second), "set confidence")); err != nil {
		t.Fatalf("set confidence: %v", err)
	}
	if err := chain.SetStatus(chains.StatusConfirmed, persistenceMutation(persistenceTestBase.Add(7*time.Second), "confirm chain")); err != nil {
		t.Fatalf("set confirmed: %v", err)
	}
	return chain
}

func persistentRegistry(t *testing.T, ids ...string) *registry.Registry {
	t.Helper()
	result := registry.New()
	for _, id := range ids {
		if err := result.Add(persistentChain(t, id)); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	return result
}

func TestRestorePreservesChainExactlyAndDeeply(t *testing.T) {
	original := persistentChain(t, "restore")
	expected := original.Snapshot()
	restored, err := chains.Restore(expected)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := restored.Snapshot(); !reflect.DeepEqual(expected, got) {
		t.Fatalf("restored snapshot differs:\nexpected=%#v\ngot=%#v", expected, got)
	}
	if restored.Snapshot().Revision != expected.Revision || len(restored.History()) != int(expected.Revision) {
		t.Fatalf("restore changed revision/history: %#v", restored.Snapshot())
	}

	expected.Observations[0].EventType = "mutated"
	expected.Contributions[0].ObservationIDs[0] = "mutated"
	expected.History[0].Reason = "mutated"
	got := restored.Snapshot()
	if got.Observations[0].EventType == "mutated" || got.Contributions[0].ObservationIDs[0] == "mutated" || got.History[0].Reason == "mutated" {
		t.Fatal("restore shares mutable snapshot data")
	}
}

func TestRestoreRejectsInvalidSnapshotWithoutCreatingRevision(t *testing.T) {
	snapshot := persistentChain(t, "invalid-restore").Snapshot()
	snapshot.Status = chains.StatusInvalidated
	if _, err := chains.Restore(snapshot); err == nil {
		t.Fatal("invalid snapshot was restored")
	}
	snapshot = persistentChain(t, "invalid-revision").Snapshot()
	snapshot.Revision++
	if _, err := chains.Restore(snapshot); err == nil {
		t.Fatal("incoherent revision was restored")
	}
}

func TestFileStoreRoundTripDeterministicAndAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "chains.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	source := persistentRegistry(t, "zeta", "alpha")
	createdAt := persistenceTestBase.Add(time.Hour)
	metadata, err := store.Save(context.Background(), source, createdAt)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	data1, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved snapshot: %v", err)
	}
	if metadata.SchemaVersion != CurrentSchemaVersion || metadata.CreatedAt != createdAt || metadata.ChainCount != 2 || metadata.SizeBytes != int64(len(data1)) || len(metadata.PayloadSHA256) != len("sha256:")+64 {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("snapshot mode = %o, want 640", info.Mode().Perm())
	}

	loaded, loadedMetadata, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(source.List(), loaded.List()) || !reflect.DeepEqual(metadata, loadedMetadata) {
		t.Fatalf("round-trip changed registry or metadata: source=%#v loaded=%#v metadata=%#v loadedMetadata=%#v", source.List(), loaded.List(), metadata, loadedMetadata)
	}

	if _, err := store.Save(context.Background(), source, createdAt); err != nil {
		t.Fatalf("repeat save: %v", err)
	}
	data2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read repeated snapshot: %v", err)
	}
	if !bytes.Equal(data1, data2) {
		t.Fatal("same registry, date, and schema produced different envelope bytes")
	}
}

func TestFileStoreEmptyRegistryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.Save(context.Background(), registry.New(), persistenceTestBase); err != nil {
		t.Fatalf("save empty registry: %v", err)
	}
	loaded, metadata, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load empty registry: %v", err)
	}
	if loaded.Count() != 0 || metadata.ChainCount != 0 {
		t.Fatalf("empty round-trip = count %d metadata %#v", loaded.Count(), metadata)
	}
}

func TestFileStoreRejectsUnsafeConfigurationAndMissingFile(t *testing.T) {
	if _, err := NewFileStore(""); err == nil || !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("expected invalid path, got %v", err)
	}
	if _, err := NewFileStoreWithOptions(filepath.Join(t.TempDir(), "snapshot"), FileStoreOptions{Mode: 0o666}); err == nil || !errors.Is(err, ErrInvalidFileMode) {
		t.Fatalf("expected invalid mode, got %v", err)
	}
	store, err := NewFileStore(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil || !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("expected missing snapshot, got %v", err)
	}
}

func TestFileStoreRejectsCorruptionVersionAndTrailingJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.Save(context.Background(), persistentRegistry(t, "one"), persistenceTestBase); err != nil {
		t.Fatalf("save source: %v", err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}

	if err := os.WriteFile(path, nil, 0o640); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil || !errors.Is(err, ErrSnapshotEmpty) {
		t.Fatalf("expected empty snapshot error, got %v", err)
	}
	if err := os.WriteFile(path, append(append([]byte(nil), original...), '{'), 0o640); err != nil {
		t.Fatalf("write truncated: %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil || !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected truncated envelope error, got %v", err)
	}
	if err := os.WriteFile(path, append(append([]byte(nil), original...), '\n', '{', '}', '\n'), 0o640); err != nil {
		t.Fatalf("write trailing JSON: %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil || !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected trailing JSON error, got %v", err)
	}

	var envelope FileEnvelope
	if err := json.Unmarshal(original, &envelope); err != nil {
		t.Fatalf("decode original envelope: %v", err)
	}
	envelope.Payload = json.RawMessage(`{"chain_count":1,"chains":[]}`)
	if err := writeEnvelope(path, envelope); err != nil {
		t.Fatalf("write checksum corruption: %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil || !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}

	envelope.SchemaVersion = CurrentSchemaVersion + 1
	envelope.Payload = json.RawMessage(originalPayload(t, original))
	envelope.PayloadSHA256 = payloadChecksum(envelope.Payload)
	if err := writeEnvelope(path, envelope); err != nil {
		t.Fatalf("write unsupported version: %v", err)
	}
	var schemaErr UnsupportedSchemaError
	if _, _, err := store.Load(context.Background()); err == nil || !errors.As(err, &schemaErr) || schemaErr.Found != CurrentSchemaVersion+1 {
		t.Fatalf("expected unsupported schema error, got %v", err)
	}
}

func TestFileStoreRejectsInvalidPayloadsAndDuplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	source := persistentRegistry(t, "one", "two")
	if _, err := store.Save(context.Background(), source, persistenceTestBase); err != nil {
		t.Fatalf("save source: %v", err)
	}
	var envelope FileEnvelope
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var payload RegistryPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	payload.ChainCount = 99
	if err := writePayload(path, envelope, payload); err != nil {
		t.Fatalf("write count mismatch: %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil || !errors.Is(err, ErrChainCountMismatch) {
		t.Fatalf("expected chain count mismatch, got %v", err)
	}

	payload.ChainCount = 2
	payload.Chains[1] = payload.Chains[0]
	if err := writePayload(path, envelope, payload); err != nil {
		t.Fatalf("write duplicate payload: %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil || !errors.Is(err, ErrDuplicateChainID) {
		t.Fatalf("expected duplicate chain id, got %v", err)
	}

	payload.Chains = source.List()
	payload.Chains[0].Status = chains.Status("unknown")
	if err := writePayload(path, envelope, payload); err != nil {
		t.Fatalf("write invalid chain payload: %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil || !errors.Is(err, ErrChainRestoreFailed) {
		t.Fatalf("expected chain restore failure, got %v", err)
	}

	envelope.SchemaVersion = CurrentSchemaVersion
	envelope.Payload = json.RawMessage(`{}`)
	envelope.PayloadSHA256 = payloadChecksum(envelope.Payload)
	if err := writeEnvelope(path, envelope); err != nil {
		t.Fatalf("write empty payload object: %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil || !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("expected invalid payload error, got %v", err)
	}
}

func TestFileStoreRejectsOversizeAndCancelledOperations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")
	store, err := NewFileStoreWithOptions(path, FileStoreOptions{MaxSize: 32, CreateParentDirs: true})
	if err != nil {
		t.Fatalf("new limited store: %v", err)
	}
	if _, err := store.Save(context.Background(), persistentRegistry(t, "large"), persistenceTestBase); err == nil || !errors.Is(err, ErrSnapshotTooLarge) {
		t.Fatalf("expected oversized save, got %v", err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, 33), 0o640); err != nil {
		t.Fatalf("write oversized file: %v", err)
	}
	if _, _, err := store.Load(context.Background()); err == nil || !errors.Is(err, ErrSnapshotTooLarge) {
		t.Fatalf("expected oversized load, got %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Save(cancelled, registry.New(), persistenceTestBase); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled save, got %v", err)
	}
	if _, _, err := store.Load(cancelled); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled load, got %v", err)
	}
}

func TestFileStoreFailedRenameLeavesDestinationAndTempFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.json")
	oldStore, err := NewFileStore(oldPath)
	if err != nil {
		t.Fatalf("new old store: %v", err)
	}
	if _, err := oldStore.Save(context.Background(), registry.New(), persistenceTestBase); err != nil {
		t.Fatalf("save old snapshot: %v", err)
	}
	oldData, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatalf("read old snapshot: %v", err)
	}
	oldStore.Mode = 0o666
	if _, err := oldStore.Save(context.Background(), registry.New(), persistenceTestBase.Add(time.Second)); err == nil || !errors.Is(err, ErrInvalidFileMode) {
		t.Fatalf("expected pre-write validation failure, got %v", err)
	}
	unchanged, err := os.ReadFile(oldPath)
	if err != nil || !bytes.Equal(oldData, unchanged) {
		t.Fatalf("old snapshot changed after failed save: err=%v", err)
	}

	target := filepath.Join(dir, "snapshot.json")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("create destination directory: %v", err)
	}
	marker := filepath.Join(target, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o640); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	store, err := NewFileStore(target)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.Save(context.Background(), registry.New(), persistenceTestBase); err == nil || !errors.Is(err, ErrAtomicWriteFailed) {
		t.Fatalf("expected atomic rename failure, got %v", err)
	}
	markerData, err := os.ReadFile(marker)
	if err != nil || string(markerData) != "keep" {
		t.Fatalf("destination changed after failed rename: data=%q err=%v", markerData, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".snapshot.json-") {
			t.Fatalf("temporary file remained after failure: %s", entry.Name())
		}
	}
}

func TestFileStoreConcurrentRegistryAccessProducesValidSnapshots(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(filepath.Join(dir, "concurrent.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	source := persistentRegistry(t, "active")
	snapshot, err := source.Get(chains.ChainID("active"))
	if err != nil {
		t.Fatalf("get active chain: %v", err)
	}
	proposalEvaluation, err := chains.EvaluateLifecycle(snapshot, persistenceTestBase.Add(6*time.Hour+time.Second), chains.DefaultLifecyclePolicy())
	if err != nil || proposalEvaluation.Proposal == nil {
		t.Fatalf("make lifecycle proposal: evaluation=%#v err=%v", proposalEvaluation, err)
	}
	proposal := *proposalEvaluation.Proposal
	added := persistentChain(t, "added")

	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		if _, err := store.Save(context.Background(), source, persistenceTestBase); err != nil {
			t.Errorf("concurrent save: %v", err)
		}
	}()
	go func() {
		defer wait.Done()
		if err := source.Add(added); err != nil {
			t.Errorf("concurrent add: %v", err)
		}
	}()
	go func() {
		defer wait.Done()
		if _, err := source.ApplyLifecycleProposal(proposal, "test", "concurrent"); err != nil {
			t.Errorf("concurrent application: %v", err)
		}
	}()
	wait.Wait()

	loaded, _, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load concurrent snapshot: %v", err)
	}
	if loaded.Count() < 1 || loaded.Count() > 2 {
		t.Fatalf("unexpected coherent snapshot count: %d", loaded.Count())
	}
	for _, snapshot := range loaded.List() {
		if _, err := chains.Restore(snapshot); err != nil {
			t.Fatalf("loaded partial chain: %v", err)
		}
	}
}

func originalPayload(t *testing.T, data []byte) []byte {
	t.Helper()
	var envelope FileEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode envelope payload: %v", err)
	}
	return append([]byte(nil), envelope.Payload...)
}

func writeEnvelope(path string, envelope FileEnvelope) error {
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o640)
}

func writePayload(path string, envelope FileEnvelope, payload RegistryPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	envelope.Payload = data
	envelope.PayloadSHA256 = payloadChecksum(data)
	return writeEnvelope(path, envelope)
}

func TestPersistedIDsAreSorted(t *testing.T) {
	r := persistentRegistry(t, "zeta", "alpha", "middle")
	path := filepath.Join(t.TempDir(), "sorted.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.Save(context.Background(), r, persistenceTestBase); err != nil {
		t.Fatalf("save: %v", err)
	}
	var envelope FileEnvelope
	data, _ := os.ReadFile(path)
	_ = json.Unmarshal(data, &envelope)
	var payload RegistryPayload
	_ = json.Unmarshal(envelope.Payload, &payload)
	ids := make([]string, 0, len(payload.Chains))
	for _, snapshot := range payload.Chains {
		ids = append(ids, string(snapshot.ID))
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("persisted IDs are not sorted: %v", ids)
	}
	if payload.ChainCount != len(payload.Chains) {
		t.Fatalf("persisted chain count mismatch: %#v", payload)
	}
	if len(ids) != 3 {
		t.Fatalf("unexpected persisted IDs: %v", ids)
	}
}
