package discovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"synora/internal/discovery/runtime"
	"synora/internal/discovery/vision"
	"synora/internal/facestore"
	"synora/pkg/contract"
)

func newResetTestManager(t *testing.T) (*Manager, string, string) {
	t.Helper()
	root := t.TempDir()
	clipRoot := filepath.Join(root, "clips")
	faceRoot := filepath.Join(root, "face")
	p := vision.NewWorkerPoolWithConfig(1, func(*vision.ClipJob) error { return nil }, vision.WorkerPoolConfig{PersistencePath: filepath.Join(clipRoot, ".vision-queue.json")})
	m := &Manager{
		clipRoot:      clipRoot,
		pool:          p,
		faceStore:     facestore.New(faceRoot, facestore.Limits{}),
		snapshotCache: NewSnapshotCache(),
		devices:       runtime.NewRegistry(),
		actionResults: make(map[string]contract.Event),
	}
	t.Cleanup(func() { _ = p.Close() })
	return m, clipRoot, faceRoot
}

func TestResetDataLeavesNoDiscoveryFilesAndRemovesMarker(t *testing.T) {
	m, clipRoot, faceRoot := newResetTestManager(t)
	if err := os.MkdirAll(filepath.Join(clipRoot, "cam-1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clipRoot, "cam-1", "clip.mp4"), []byte("clip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.faceStore.Init(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(faceRoot, "uploads", "old.part"), []byte("face"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.writeResetMarker(); err != nil {
		t.Fatal(err)
	}
	if err := m.ResetData(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exists, err := m.hasResetMarker(); err != nil || exists {
		t.Fatalf("reset marker remains exists=%v err=%v", exists, err)
	}
	if err := assertNoRegularFiles(clipRoot); err != nil {
		t.Fatal(err)
	}
	if err := assertNoRegularFiles(faceRoot); err != nil {
		t.Fatal(err)
	}
}

func assertNoRegularFiles(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path != root && info.Mode().IsRegular() {
			return os.ErrExist
		}
		return nil
	})
}
