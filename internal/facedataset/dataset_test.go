package facedataset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"synora/internal/facestore"
	"synora/pkg/contract"
)

type testEmbedder struct{ fail bool }

func (e testEmbedder) Embed(context.Context, string, contract.FacePhoto) ([]float32, string, error) {
	if e.fail {
		return nil, "", errors.New("embedding unavailable")
	}
	return []float32{1, 2, 3}, "arcface-test", nil
}

type testLoader struct {
	mu      sync.Mutex
	fail    bool
	calls   int
	version string
}

func (l *testLoader) ReloadFaceDataset(context.Context, string, string) (ReloadResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.fail {
		return ReloadResult{}, errors.New("reload failed")
	}
	version := l.version
	if version == "" {
		version = "v-1"
	}
	return ReloadResult{Version: version}, nil
}

func (l *testLoader) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func TestBuildManifestAndAtomicCurrentAfterReload(t *testing.T) {
	store := facestore.New(t.TempDir(), facestore.Limits{})
	received, err := store.Receive("resident-1", bytes.NewReader(bytesForDataset(t)))
	if err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder(store)
	loader := &testLoader{}
	manifest, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 1, testEmbedder{}, loader)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "v-1" || manifest.EmbeddingDimension != 3 || len(manifest.Entries) != 1 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	data, err := os.ReadFile(filepath.Join(store.Root, "datasets", "current"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v-1\n" {
		t.Fatalf("current=%q", data)
	}
	if _, err := ReadManifest(filepath.Join(store.Root, "datasets", "versions", "v-1")); err != nil {
		t.Fatal(err)
	}
}

func TestManifestContractVersionExposesOnlyBoundaryMetadata(t *testing.T) {
	manifest := Manifest{
		SchemaVersion: 1, Version: "dataset-1", DesiredRevision: 4,
		BuiltAt:          time.Date(2026, 7, 4, 10, 11, 12, 0, time.UTC),
		ModelFingerprint: "model-fingerprint", EmbeddingDimension: 512,
		Checksum: "manifest-checksum", Entries: []Entry{{StorageKey: "resident-1/photo-1.png"}},
	}
	value := manifest.ContractVersion()
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	if value.Version != manifest.Version || value.ManifestChecksum != manifest.Checksum || value.EmbeddingDimension != manifest.EmbeddingDimension {
		t.Fatalf("unexpected boundary contract: %#v", value)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("storage_key")) || bytes.Contains(encoded, []byte(`"embedding"`)) {
		t.Fatalf("internal dataset fields crossed boundary: %s", encoded)
	}
}

func TestFailedBuildOrReloadDoesNotPublishCurrent(t *testing.T) {
	store := facestore.New(t.TempDir(), facestore.Limits{})
	received, err := store.Receive("resident-1", bytes.NewReader(bytesForDataset(t)))
	if err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder(store)
	loader := &testLoader{fail: true}
	if _, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 1, testEmbedder{}, loader); err == nil {
		t.Fatal("reload failure accepted")
	}
	if _, err := os.Stat(filepath.Join(store.Root, "datasets", "current")); !os.IsNotExist(err) {
		t.Fatalf("failed build published current: %v", err)
	}
	if _, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 1, testEmbedder{fail: true}, &testLoader{}); err == nil {
		t.Fatal("embedding failure accepted")
	}
}

func TestRetryReusesCommittedVersionAfterActivationInterruption(t *testing.T) {
	store := facestore.New(t.TempDir(), facestore.Limits{})
	received, err := store.Receive("resident-1", bytes.NewReader(bytesForDataset(t)))
	if err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder(store)
	loader := &testLoader{}
	if _, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 1, testEmbedder{}, loader); err != nil {
		t.Fatal(err)
	}
	failedLoader := &testLoader{fail: true}
	if _, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 1, testEmbedder{}, failedLoader); err == nil {
		t.Fatal("failed retry accepted")
	}
	if failedLoader.callCount() != 1 {
		t.Fatalf("existing committed version was not reloaded, calls=%d", failedLoader.callCount())
	}
	if _, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 1, testEmbedder{}, &testLoader{}); err != nil {
		t.Fatalf("retry after loader recovery failed: %v", err)
	}
}

func TestFailedReloadPreservesPreviousCurrentVersion(t *testing.T) {
	store := facestore.New(t.TempDir(), facestore.Limits{})
	received, err := store.Receive("resident-1", bytes.NewReader(bytesForDataset(t)))
	if err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder(store)
	if _, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 1, testEmbedder{}, &testLoader{}); err != nil {
		t.Fatal(err)
	}
	loader := &testLoader{fail: true, version: "v-2"}
	if _, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 2, testEmbedder{}, loader); err == nil {
		t.Fatal("failed reload accepted")
	}
	current, err := ReadCurrent(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != "v-1" {
		t.Fatalf("failed activation replaced current version: %s", current.Version)
	}
	if _, err := os.Stat(filepath.Join(store.Root, "datasets", "versions", "v-2")); err != nil {
		t.Fatalf("staged immutable version was not retained for retry: %v", err)
	}
}

func TestRollbackReloadsPreviousImmutableVersionAndPreservesItOnFailure(t *testing.T) {
	store := facestore.New(t.TempDir(), facestore.Limits{})
	received, err := store.Receive("resident-1", bytes.NewReader(bytesForDataset(t)))
	if err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder(store)
	if _, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 1, testEmbedder{}, &testLoader{version: "v-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 2, testEmbedder{}, &testLoader{version: "v-2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Rollback(context.Background(), "v-1", &testLoader{version: "v-1"}); err != nil {
		t.Fatal(err)
	}
	current, err := ReadCurrent(store.Root)
	if err != nil || current.Version != "v-1" {
		t.Fatalf("rollback current=%#v err=%v", current, err)
	}
	if _, err := builder.Rollback(context.Background(), "v-2", &testLoader{version: "wrong"}); err == nil {
		t.Fatal("failed rollback accepted")
	}
	current, err = ReadCurrent(store.Root)
	if err != nil || current.Version != "v-1" {
		t.Fatalf("failed rollback replaced current=%#v err=%v", current, err)
	}
}

func TestConcurrentBuildsReuseSameImmutableVersion(t *testing.T) {
	store := facestore.New(t.TempDir(), facestore.Limits{})
	received, err := store.Receive("resident-1", bytes.NewReader(bytesForDataset(t)))
	if err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder(store)
	loader := &testLoader{}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, buildErr := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 1, testEmbedder{}, loader)
			errs <- buildErr
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent build failed: %v", err)
		}
	}
	current, err := ReadCurrent(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != "v-1" {
		t.Fatalf("unexpected current version: %s", current.Version)
	}
}

func TestMissingPhotoIsExcludedFromDatasetManifest(t *testing.T) {
	store := facestore.New(t.TempDir(), facestore.Limits{})
	received, err := store.Receive("resident-1", bytes.NewReader(bytesForDataset(t)))
	if err != nil {
		t.Fatal(err)
	}
	received.Photo.Status = string(contract.FacePhotoMissing)
	builder := NewBuilder(store)
	manifest, err := builder.BuildAndActivate(context.Background(), []contract.FacePhoto{received.Photo}, 1, testEmbedder{}, &testLoader{})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 0 {
		t.Fatalf("missing source was included in manifest: %#v", manifest.Entries)
	}
}

func TestCorruptCurrentManifestIsRejected(t *testing.T) {
	store := facestore.New(t.TempDir(), facestore.Limits{})
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	versionDir := filepath.Join(store.Root, "datasets", "versions", "v-bad")
	if err := os.MkdirAll(versionDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "manifest.json"), []byte(`{"schema_version":1,"version":"v-bad","checksum":"spoof"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root, "datasets", "current"), []byte("v-bad\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCurrent(store.Root); err == nil {
		t.Fatal("corrupt current manifest accepted")
	}
}

func bytesForDataset(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
