package facestore

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 220, G: 180, B: 120, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestReceiveFinalizesRealImageAndHidesPart(t *testing.T) {
	store := New(t.TempDir(), Limits{MaxUploadSize: 1 << 20, MaxPixels: 100})
	result, err := store.Receive("resident-1", bytes.NewReader(pngBytes(t, 4, 4)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Photo.Status != "stored" || result.Photo.StorageKey == "" || result.Photo.Path != "" {
		t.Fatalf("unexpected public/internal metadata: %#v", result.Photo)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(store.Root, "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".part" {
			t.Fatalf("part file became visible: %s", entry.Name())
		}
	}
}

func TestReceiveRejectsFakeContentPixelLimitAndTraversal(t *testing.T) {
	store := New(t.TempDir(), Limits{MaxUploadSize: 1 << 20, MaxPixels: 4})
	if _, err := store.Receive("../escape", bytes.NewReader(pngBytes(t, 1, 1))); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, err := store.Receive("resident-1", bytes.NewBufferString("not an image")); err == nil {
		t.Fatal("fake image accepted")
	}
	if _, err := store.Receive("resident-1", bytes.NewReader(pngBytes(t, 3, 3))); err == nil {
		t.Fatal("pixel limit ignored")
	}
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(t.TempDir(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root, Limits{}).Receive("resident-1", bytes.NewReader(pngBytes(t, 1, 1))); err == nil {
		t.Fatal("symlink root accepted")
	}
}

func TestCleanupPartsIsBoundedAndDeterministic(t *testing.T) {
	store := New(t.TempDir(), Limits{PartMaxAge: time.Hour})
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Root, "uploads", ".part-old")
	if err := os.WriteFile(path, []byte("partial"), 0o640); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	count, err := store.CleanupParts(time.Now())
	if err != nil || count != 1 {
		t.Fatalf("cleanup count=%d err=%v", count, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("old part still exists: %v", err)
	}
}

func TestRemoveResidentSourcesIsScopedAndRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	store := New(root, Limits{})
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	path, err := store.SourcePath("resident-1", "resident-1/photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("photo"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveResidentSources("resident-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("resident source remains: %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "sources", "resident-2")); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveResidentSources("resident-2"); err == nil {
		t.Fatal("symlink resident source accepted")
	}
}
