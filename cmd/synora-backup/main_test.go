package main

import (
	"os"
	"path/filepath"
	"testing"

	"synora/internal/runtimeconfig"
)

func TestBackupPathsIncludesPersistentScopeAndSkipsTransientFaceData(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "etc")
	faceRoot := filepath.Join(root, "face")
	if err := os.MkdirAll(filepath.Join(faceRoot, "sources", "resident-1"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(faceRoot, "uploads"), 0o750); err != nil {
		t.Fatal(err)
	}
	face := filepath.Join(faceRoot, "sources", "resident-1", "face.png")
	part := filepath.Join(faceRoot, "uploads", ".part-face")
	if err := os.WriteFile(face, []byte("face"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	auth := filepath.Join(config, "auth.yaml")
	if err := os.MkdirAll(config, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auth, []byte("users: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sources, destinations, err := backupPaths(runtimeconfig.Paths{Auth: auth, FaceDataRoot: faceRoot})
	if err != nil {
		t.Fatal(err)
	}
	if sources["config/auth.yaml"] != auth || destinations["config/auth.yaml"] != auth {
		t.Fatalf("auth path missing: sources=%#v destinations=%#v", sources, destinations)
	}
	if sources["faces/sources/resident-1/face.png"] != face {
		t.Fatalf("face source missing: %#v", sources)
	}
	if _, ok := sources["faces/uploads/.part-face"]; ok {
		t.Fatal("transient face upload was included")
	}
}
