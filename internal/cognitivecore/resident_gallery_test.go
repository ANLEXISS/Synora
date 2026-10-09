package cognitivecore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResidentGalleryStorePersistenceIdempotenceAndRedaction(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	input := ResidentGalleryRecord{ResidentRef: "res_0123456789abcdef0123456789abcdef", GalleryStatus: "not_enrolled", PolicyVersion: ResidentGalleryPolicyVersion, CreatedAt: now, UpdatedAt: now}
	first, duplicate, err := store.CreateResidentGallery(input, "idempotency-create-001")
	if err != nil || duplicate || first.ResidentRef != input.ResidentRef || store.Revision() != 1 {
		t.Fatalf("create failed: record=%+v duplicate=%t err=%v", first, duplicate, err)
	}
	retry, duplicate, err := store.CreateResidentGallery(input, "idempotency-create-001")
	if err != nil || !duplicate || retry.ResidentRef != input.ResidentRef || store.Revision() != 1 {
		t.Fatalf("idempotent retry failed: record=%+v duplicate=%t err=%v", retry, duplicate, err)
	}

	store, err = OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok := store.ResidentGallery(input.ResidentRef)
	if !ok || loaded.GalleryStatus != "not_enrolled" || loaded.Generation != 0 {
		t.Fatalf("resident state did not survive restart: %+v exists=%t", loaded, ok)
	}
	rolled, err := store.RollbackResidentGallery(input.ResidentRef, now.Add(time.Minute))
	if err != nil || rolled.GalleryStatus != "not_enrolled" {
		t.Fatalf("empty gallery rollback failed: %+v err=%v", rolled, err)
	}

	statePath := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]json.RawMessage
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	var residents map[string]map[string]json.RawMessage
	if err := json.Unmarshal(disk["resident_galleries"], &residents); err != nil || len(residents) != 1 {
		t.Fatalf("resident projection missing: err=%v", err)
	}
	for _, fields := range residents {
		for key := range fields {
			if strings.Contains(strings.ToLower(key), "name") || strings.Contains(strings.ToLower(key), "embedding") || strings.Contains(strings.ToLower(key), "score") || strings.Contains(strings.ToLower(key), "photo") || strings.Contains(strings.ToLower(key), "image") || strings.Contains(strings.ToLower(key), "path") || strings.Contains(strings.ToLower(key), "track") {
				t.Fatalf("Universal Store resident projection contains forbidden field %q", key)
			}
		}
	}
}

func TestResidentGalleryRejectsInvalidReferenceAndStatus(t *testing.T) {
	store := NewUniversalStore()
	now := time.Now().UTC()
	invalid := ResidentGalleryRecord{ResidentRef: "resident-one", GalleryStatus: "not_enrolled", PolicyVersion: ResidentGalleryPolicyVersion, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.CreateResidentGallery(invalid, "idempotency-key-01"); err == nil {
		t.Fatal("non-opaque resident reference accepted")
	}
	invalid.ResidentRef = "res_0123456789abcdef0123456789abcdef"
	invalid.GalleryStatus = "recognized"
	if _, _, err := store.CreateResidentGallery(invalid, "idempotency-key-02"); err == nil {
		t.Fatal("unlisted gallery status accepted")
	}
}
