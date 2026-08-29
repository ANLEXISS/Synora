package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestRestoreRollsBackConfigurationFilesWhenInterrupted(t *testing.T) {
	root := t.TempDir()
	sourceRoot := t.TempDir()
	sourceA := filepath.Join(sourceRoot, "a")
	sourceB := filepath.Join(sourceRoot, "b")
	if err := os.WriteFile(sourceA, []byte("new-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceB, []byte("new-b"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(root, 1)
	manifest, err := m.Create(context.Background(), state.NewStore(), map[string]string{"config/a.yaml": sourceA, "config/b.yaml": sourceB})
	if err != nil {
		t.Fatal(err)
	}
	destinationRoot := t.TempDir()
	destinationA := filepath.Join(destinationRoot, "a.yaml")
	destinationB := filepath.Join(destinationRoot, "b.yaml")
	if err := os.WriteFile(destinationA, []byte("old-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destinationB, []byte("old-b"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.BeforeRestore = func(name string) error {
		if name == "config/b.yaml" {
			return errors.New("simulated restore interruption")
		}
		return nil
	}
	err = m.Restore(context.Background(), manifest.ID, state.NewStore(), map[string]string{"config/a.yaml": destinationA, "config/b.yaml": destinationB})
	if err == nil {
		t.Fatal("interrupted restore succeeded")
	}
	for path, want := range map[string]string{destinationA: "old-a", destinationB: "old-b"} {
		data, readErr := os.ReadFile(path)
		if readErr != nil || string(data) != want {
			t.Fatalf("destination %s=%q err=%v, want %q", path, data, readErr, want)
		}
	}
}

func TestBackupRejectsTraversalSymlinkAndFutureState(t *testing.T) {
	root := t.TempDir()
	m := New(root, 1)
	if _, err := m.Create(context.Background(), state.NewStore(), map[string]string{"../escape": filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("traversal source name was accepted")
	}
	target := filepath.Join(t.TempDir(), "target")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := m.Create(context.Background(), state.NewStore(), map[string]string{"config/link": link}); err == nil {
		t.Fatal("symlink source was accepted")
	}
	stateValue := state.NewStore()
	manifest, err := m.Create(context.Background(), stateValue, nil)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "snapshots", manifest.ID, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"version": 2`, `"version": 99`, 1))
	if err := os.WriteFile(statePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest.StateBytes = int64(len(data))
	manifest.StateSHA256 = digest(data)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "snapshots", manifest.ID, "manifest.json"), append(manifestData, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(context.Background(), manifest.ID, state.NewStore(), nil); err == nil || !strings.Contains(err.Error(), "unsupported backup state version") {
		t.Fatalf("future state restore error=%v", err)
	}
}

func TestEncryptedBackupRequiresTheCorrectSecret(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(source, []byte("password_hash: protected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	creator := New(root, 1)
	creator.Secret = "correct-local-backup-secret"
	manifest, err := creator.Create(context.Background(), state.NewStore(), map[string]string{"config/auth.yaml": source})
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Encrypted || manifest.Encryption != "aes-256-gcm" {
		t.Fatalf("backup encryption metadata=%#v", manifest)
	}
	wrong := New(root, 1)
	wrong.Secret = "wrong-local-backup-secret"
	if err := wrong.Restore(context.Background(), manifest.ID, state.NewStore(), nil); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("wrong secret restore error=%v", err)
	}
	destination := filepath.Join(t.TempDir(), "auth.yaml")
	if err := creator.Restore(context.Background(), manifest.ID, state.NewStore(), map[string]string{"config/auth.yaml": destination}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "password_hash: protected\n" {
		t.Fatalf("decrypted restore=%q err=%v", data, err)
	}
}
