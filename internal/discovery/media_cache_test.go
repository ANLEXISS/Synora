package discovery

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMediaCacheIsBoundedOpaqueAndExpires(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	cache, err := NewMediaCache(MediaCacheConfig{Root: root, TTL: time.Minute, MaxBytes: 8, MaxEntries: 1})
	if err != nil {
		t.Fatal(err)
	}
	item, err := cache.Put(bytes.NewReader([]byte("clip")), 4, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Token) != 48 || filepath.Dir(item.Path) != root {
		t.Fatalf("non-opaque cache item: %+v", item)
	}
	if _, err := cache.Resolve(item.Token, now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Put(bytes.NewReader([]byte("full")), 4, now); !errors.Is(err, ErrMediaCacheFull) {
		t.Fatalf("expected quota error, got %v", err)
	}
	if _, err := cache.Resolve("../escape", now); !errors.Is(err, ErrMediaCacheToken) {
		t.Fatalf("expected token validation error, got %v", err)
	}
	if err := cache.Cleanup(now.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(item.Path); !os.IsNotExist(err) {
		t.Fatalf("expired media remained: %v", err)
	}
}

func TestMediaCacheRejectsOversizedInput(t *testing.T) {
	cache, err := NewMediaCache(MediaCacheConfig{Root: t.TempDir(), TTL: time.Minute, MaxBytes: 3, MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cache.Put(bytes.NewReader([]byte("long")), 4, time.Now().UTC())
	if !errors.Is(err, ErrMediaCacheTooLarge) {
		t.Fatalf("expected size error, got %v", err)
	}
}
