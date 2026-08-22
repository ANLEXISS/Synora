package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanSupportBundleRemovesBiometricsAndRedactsText(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "face-photo.png"), []byte("image"), 0o640); err != nil {
		t.Fatal(err)
	}
	textPath := filepath.Join(root, "diagnostic.log")
	if err := os.WriteFile(textPath, []byte(`token: "secret" path=/var/lib/synora/state.json`), 0o640); err != nil {
		t.Fatal(err)
	}
	removed, err := CleanSupportBundle(root)
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "face-photo.png")); !os.IsNotExist(err) {
		t.Fatalf("biometric artifact remains: %v", err)
	}
	data, err := os.ReadFile(textPath)
	if err != nil || strings.Contains(string(data), "secret") || strings.Contains(string(data), "/var/lib") {
		t.Fatalf("text was not redacted: %q err=%v", data, err)
	}
}

func TestCleanSupportBundleRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := CleanSupportBundle(root); err == nil {
		t.Fatal("support bundle symlink accepted")
	}
}
