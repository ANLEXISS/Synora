package clipstore

import (
	"os"
	"path/filepath"
	"testing"
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
