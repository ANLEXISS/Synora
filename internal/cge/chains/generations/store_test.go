package generations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/registry"
)

var generationTestBase = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func generationMutation(at time.Time, actor, correlation, reason string) chains.MutationContext {
	return chains.MutationContext{At: at, Actor: actor, CorrelationID: correlation, Reason: reason}
}

func generationFixture(t *testing.T) (*Store, *journal.FileJournal, *registry.Registry, journal.Record) {
	t.Helper()
	root := t.TempDir()
	store, err := NewStore(root, StoreOptions{})
	if err != nil {
		t.Fatalf("new generation store: %v", err)
	}
	journalPath := filepath.Join(t.TempDir(), "cge.ndjson")
	fileJournal, err := journal.NewFileJournal(journalPath, journal.FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		t.Fatalf("new journal: %v", err)
	}
	genesis, err := fileJournal.Initialize(context.Background(), journal.GenesisInput{
		JournalID: "generation-test", CreatedAt: generationTestBase, RecordedAt: generationTestBase,
		Purpose: "generation tests", Actor: "test", CorrelationID: "genesis",
	})
	if err != nil {
		t.Fatalf("initialize journal: %v", err)
	}
	chain, err := chains.New("generation-chain", generationMutation(generationTestBase, "builder", "create-chain", "create"))
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	registryValue := registry.New()
	if err := registryValue.Add(chain); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	return store, fileJournal, registryValue, genesis
}

func checkpointFor(t *testing.T, fileJournal *journal.FileJournal, pending PendingGeneration, at time.Time) journal.Record {
	t.Helper()
	record, err := fileJournal.AppendSnapshotCheckpoint(context.Background(), journal.SnapshotCheckpointInput{
		SnapshotSchemaVersion: pending.Metadata.SchemaVersion,
		SnapshotCreatedAt:     pending.Metadata.CreatedAt,
		SnapshotChainCount:    pending.Metadata.ChainCount,
		SnapshotPayloadSHA256: pending.Metadata.PayloadSHA256,
		SnapshotSizeBytes:     pending.Metadata.SizeBytes,
		JournalSequence:       pending.IncludedJournalSequence,
		JournalHeadHash:       pending.IncludedJournalHeadHash,
		RecordedAt:            at, Actor: "snapshot", CorrelationID: "checkpoint",
	})
	if err != nil {
		t.Fatalf("append checkpoint: %v", err)
	}
	return record
}

func TestCreateFinalizePublishAndLoadGeneration(t *testing.T) {
	store, fileJournal, source, genesis := generationFixture(t)
	sourceBefore := source.List()
	pending, err := store.CreateGeneration(context.Background(), source, genesis.Sequence, genesis.RecordHash, generationTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("create generation: %v", err)
	}
	if pending.GenerationID == "" || pending.RelativePath == "" || pending.Metadata.ChainCount != 1 {
		t.Fatalf("invalid pending generation: %#v", pending)
	}
	if _, err := store.LoadManifest(context.Background()); !errors.Is(err, ErrManifestNotFound) {
		t.Fatalf("generation became active before checkpoint: %v", err)
	}
	checkpoint := checkpointFor(t, fileJournal, pending, generationTestBase.Add(2*time.Hour))
	generation, err := pending.Finalize(checkpoint)
	if err != nil {
		t.Fatalf("finalize generation: %v", err)
	}
	if err := store.PublishManifest(context.Background(), generation, checkpoint, generationTestBase.Add(2*time.Hour)); err != nil {
		t.Fatalf("publish manifest: %v", err)
	}
	manifest, err := store.LoadManifest(context.Background())
	if err != nil || !reflect.DeepEqual(manifest.Active, generation) {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
	loaded, metadata, err := store.LoadGeneration(context.Background(), generation)
	if err != nil {
		t.Fatalf("load generation: %v", err)
	}
	if metadata.PayloadSHA256 != generation.SnapshotPayloadSHA256 || !reflect.DeepEqual(loaded.List(), sourceBefore) {
		t.Fatalf("loaded generation differs: metadata=%#v loaded=%#v source=%#v", metadata, loaded.List(), sourceBefore)
	}
	info, err := os.Stat(filepath.Join(store.rootDir, filepath.FromSlash(generation.SnapshotPath)))
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("snapshot permissions: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(filepath.Join(store.rootDir, "manifest.json")); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
}

func TestGenerationNamesAreDeterministicAndImmutable(t *testing.T) {
	store, fileJournal, source, genesis := generationFixture(t)
	pending, err := store.CreateGeneration(context.Background(), source, genesis.Sequence, genesis.RecordHash, generationTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	checkpoint := checkpointFor(t, fileJournal, pending, generationTestBase.Add(2*time.Hour))
	generation, err := pending.Finalize(checkpoint)
	if err != nil {
		t.Fatalf("finalize first: %v", err)
	}
	if err := store.PublishManifest(context.Background(), generation, checkpoint, generationTestBase.Add(2*time.Hour)); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if _, err := store.CreateGeneration(context.Background(), source, genesis.Sequence, genesis.RecordHash, generationTestBase.Add(time.Hour)); err == nil || !errors.Is(err, ErrGenerationAlreadyExists) {
		t.Fatalf("expected generation overwrite rejection, got %v", err)
	}
	data, err := os.ReadFile(filepath.Join(store.rootDir, filepath.FromSlash(generation.SnapshotPath)))
	if err != nil {
		t.Fatalf("read immutable snapshot: %v", err)
	}
	if err := store.PublishManifest(context.Background(), Generation{}, checkpoint, generationTestBase.Add(3*time.Hour)); err == nil || !errors.Is(err, ErrManifestGenerationMismatch) {
		t.Fatalf("expected invalid replacement rejection, got %v", err)
	}
	unchanged, err := os.ReadFile(filepath.Join(store.rootDir, filepath.FromSlash(generation.SnapshotPath)))
	if err != nil || !reflect.DeepEqual(data, unchanged) {
		t.Fatalf("snapshot changed after rejected publish: err=%v", err)
	}
}

func TestManifestValidationAndContext(t *testing.T) {
	store, fileJournal, source, genesis := generationFixture(t)
	pending, err := store.CreateGeneration(context.Background(), source, genesis.Sequence, genesis.RecordHash, generationTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	checkpoint := checkpointFor(t, fileJournal, pending, generationTestBase.Add(2*time.Hour))
	generation, err := pending.Finalize(checkpoint)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if err := store.PublishManifest(context.Background(), generation, checkpoint, generationTestBase.Add(2*time.Hour)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	manifestPath := filepath.Join(store.rootDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	raw["schema_version"] = 99
	corrupt, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("encode corrupt manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, corrupt, 0o640); err != nil {
		t.Fatalf("write corrupt manifest: %v", err)
	}
	if _, err := store.LoadManifest(context.Background()); err == nil || !errors.Is(err, ErrManifestSchemaUnsupported) {
		t.Fatalf("expected unsupported manifest, got %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.LoadManifest(cancelled); err == nil || !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("expected cancelled load, got %v", err)
	}
}

func TestManifestValidatesSnapshotAndListsOnlyGenerationFiles(t *testing.T) {
	store, fileJournal, source, genesis := generationFixture(t)
	pending, err := store.CreateGeneration(context.Background(), source, genesis.Sequence, genesis.RecordHash, generationTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	checkpoint := checkpointFor(t, fileJournal, pending, generationTestBase.Add(2*time.Hour))
	generation, err := pending.Finalize(checkpoint)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if err := store.PublishManifest(context.Background(), generation, checkpoint, generationTestBase.Add(2*time.Hour)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.rootDir, "snapshots", "orphan.txt"), []byte("retained"), 0o640); err != nil {
		t.Fatalf("write unrelated file: %v", err)
	}
	files, err := store.ListGenerations(context.Background())
	if err != nil || len(files) != 1 || files[0].GenerationID != generation.GenerationID || files[0].RelativePath != generation.SnapshotPath {
		t.Fatalf("unexpected generation listing: files=%#v err=%v", files, err)
	}
	if err := os.Remove(filepath.Join(store.rootDir, filepath.FromSlash(generation.SnapshotPath))); err != nil {
		t.Fatalf("remove snapshot for corruption test: %v", err)
	}
	if _, err := store.LoadManifest(context.Background()); err == nil || !errors.Is(err, ErrGenerationNotFound) {
		t.Fatalf("missing active snapshot was not detected: %v", err)
	}
}

func TestFinalizeRejectsUnvalidatedCheckpointRecord(t *testing.T) {
	store, fileJournal, source, genesis := generationFixture(t)
	pending, err := store.CreateGeneration(context.Background(), source, genesis.Sequence, genesis.RecordHash, generationTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	record := checkpointFor(t, fileJournal, pending, generationTestBase.Add(2*time.Hour))
	record.RecordHash = "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := pending.Finalize(record); err == nil || !errors.Is(err, ErrCheckpointMismatch) {
		t.Fatalf("invalid checkpoint record was accepted: %v", err)
	}
}

func TestManifestRejectsUnknownJSONFieldsAndUnsafeGenerationPath(t *testing.T) {
	store, fileJournal, source, genesis := generationFixture(t)
	pending, err := store.CreateGeneration(context.Background(), source, genesis.Sequence, genesis.RecordHash, generationTestBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	checkpoint := checkpointFor(t, fileJournal, pending, generationTestBase.Add(2*time.Hour))
	generation, err := pending.Finalize(checkpoint)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	unsafe := generation
	unsafe.SnapshotPath = "../../outside.json"
	if err := store.PublishManifest(context.Background(), unsafe, checkpoint, generationTestBase.Add(2*time.Hour)); err == nil || !errors.Is(err, ErrManifestGenerationMismatch) {
		t.Fatalf("unsafe manifest path was accepted: %v", err)
	}
	manifestPath := filepath.Join(store.rootDir, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"schema_version":1,"updated_at":"2026-07-17T14:00:00Z","active":{},"checksum":"sha256:0000000000000000000000000000000000000000000000000000000000000000","extra":true}`), 0o640); err != nil {
		t.Fatalf("write unknown manifest: %v", err)
	}
	if _, err := store.LoadManifest(context.Background()); err == nil || !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("unknown manifest field was accepted: %v", err)
	}
}
