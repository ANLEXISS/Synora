package clipstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSafeComponentsAndAtomicPaths(t *testing.T) {
	for _, value := range []string{"cam-1", "clip_01.mp4", "a.b"} {
		if !SafeComponent(value) {
			t.Fatalf("safe component rejected: %q", value)
		}
	}
	for _, value := range []string{"", ".", "..", "../escape", "/absolute", "cam/clip", "cam\\clip", "a b"} {
		if SafeComponent(value) {
			t.Fatalf("unsafe component accepted: %q", value)
		}
	}
	final, err := FinalPath("/spool", "cam-1", "clip-1")
	if err != nil || final != filepath.Join("/spool", "cam-1", "clip-1.mp4") {
		t.Fatalf("unexpected final path=%q err=%v", final, err)
	}
}

func TestReconcileOrphansPreservesReferencedAndFreshFiles(t *testing.T) {
	root := t.TempDir()
	cameraDir := filepath.Join(root, "cam-1")
	if err := os.MkdirAll(cameraDir, 0700); err != nil {
		t.Fatal(err)
	}
	referenced := filepath.Join(cameraDir, "referenced.mp4")
	orphan := filepath.Join(cameraDir, "orphan.mp4")
	fresh := filepath.Join(cameraDir, "fresh.mp4")
	for _, path := range []string{referenced, orphan, fresh} {
		if err := os.WriteFile(path, []byte("clip"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)
	if err := os.Chtimes(referenced, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}
	removed, err := ReconcileOrphans(root, map[string]struct{}{referenced: {}}, now, time.Hour)
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(referenced); err != nil {
		t.Fatalf("referenced clip removed: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("old orphan remains: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh orphan should be retained: %v", err)
	}
}

func TestEnsureCameraDirRejectsSymlinkAndVerifyStreamsChecksum(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "cam-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := EnsureCameraDir(root, "cam-link"); err == nil {
		t.Fatal("camera directory symlink must be rejected")
	}

	dir, err := EnsureCameraDir(root, "cam-1")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "clip-1.mp4")
	if err := os.WriteFile(path, []byte("clip"), 0600); err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyRegularFile(path, 4, ""); err != nil || !ok {
		t.Fatalf("regular file verification failed ok=%t err=%v", ok, err)
	}
	if ok, err := VerifyRegularFile(path, 3, ""); err != nil || ok {
		t.Fatalf("size mismatch accepted ok=%t err=%v", ok, err)
	}
}
