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
	fail  bool
	calls int
}

func (l *testLoader) ReloadFaceDataset(context.Context, string, string) (ReloadResult, error) {
	l.calls++
	if l.fail {
		return ReloadResult{}, errors.New("reload failed")
	}
	return ReloadResult{Version: "v-1"}, nil
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
