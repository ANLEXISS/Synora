package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/state"
	"synora/pkg/contract"
)

func TestCreateRestoreIsCompleteAndChecksumVerified(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(source, []byte("schema_version: 3\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	store := state.NewStore()
	photo := contract.FacePhoto{ID: "photo-1", ResidentID: "resident-1", Status: string(contract.FacePhotoStored), SizeBytes: 1, Checksum: "sha", MediaType: "image/png"}
	if _, _, err := store.RegisterFacePhoto(&photo); err != nil {
		t.Fatal(err)
	}
	m := New(root, 1)
	created, err := m.Create(context.Background(), store, map[string]string{"config.yaml": source})
	if err != nil {
		t.Fatal(err)
	}
	if created.StateSummary.FacePhotos != 1 || len(created.Files) != 1 {
		t.Fatalf("unexpected manifest: %#v", created)
	}
	destination := filepath.Join(t.TempDir(), "restored", "config.yaml")
	restored := state.NewStore()
	if err := m.Restore(context.Background(), created.ID, restored, map[string]string{"config.yaml": destination}); err != nil {
		t.Fatal(err)
	}
	if _, ok := restored.FacePhoto(photo.ID); !ok {
		t.Fatal("face metadata was not restored")
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "schema_version: 3\n" {
		t.Fatalf("restored config=%q err=%v", data, err)
	}
	if err := os.WriteFile(filepath.Join(root, "snapshots", created.ID, "state.json"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(context.Background(), created.ID, state.NewStore(), nil); err == nil {
		t.Fatal("tampered snapshot restored")
	}
}

func TestInterruptedCreateLeavesNoCommittedSnapshot(t *testing.T) {
	root := t.TempDir()
	m := New(root, 1)
	m.BeforeCommit = func(string) error { return errors.New("simulated interruption") }
	if _, err := m.Create(context.Background(), state.NewStore(), nil); err == nil {
		t.Fatal("interrupted backup accepted")
	}
	entries, err := os.ReadDir(filepath.Join(root, "snapshots"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial snapshot committed: entries=%v err=%v", entries, err)
	}
}

func TestLowSpaceAndInterruptedExpirationAreSafe(t *testing.T) {
	root := t.TempDir()
	m := New(root, ^uint64(0))
	if _, err := m.Create(context.Background(), state.NewStore(), nil); err == nil {
		t.Fatal("low-space backup accepted")
	}

	root = t.TempDir()
	m = New(root, 1)
	m.Now = func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }
	manifest, err := m.Create(context.Background(), state.NewStore(), nil)
	if err != nil {
		t.Fatal(err)
	}
	deleting := filepath.Join(root, "snapshots", manifest.ID+".delete")
	if err := os.Rename(filepath.Join(root, "snapshots", manifest.ID), deleting); err != nil {
		t.Fatal(err)
	}
	if err := m.RecoverExpiredDeletes(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(deleting); !os.IsNotExist(err) {
		t.Fatalf("interrupted deletion was not recoverable: %v", err)
	}
}
