package state

import (
	"testing"
	"time"

	"synora/pkg/contract"
)

func testFacePhoto(id, checksum string) contract.FacePhoto {
	now := time.Now().UTC()
	return contract.FacePhoto{ID: id, ResidentID: "resident-1", StorageKey: "resident-1/" + id + ".png", CreatedAt: now, UpdatedAt: now, Status: string(contract.FacePhotoStored), SizeBytes: 12, Checksum: checksum, MediaType: "image/png", Width: 2, Height: 2}
}

func TestFacePhotoRegistrationIsIdempotentAndCopies(t *testing.T) {
	store := NewStore()
	photo := testFacePhoto("photo-1", "sha-1")
	got, created, err := store.RegisterFacePhoto(&photo)
	if err != nil || !created {
		t.Fatalf("register created=%v err=%v", created, err)
	}
	got.ResidentID = "mutated"
	again, created, err := store.RegisterFacePhoto(&photo)
	if err != nil || created || again.ResidentID != "resident-1" {
		t.Fatalf("retry got=%#v created=%v err=%v", again, created, err)
	}
	photo2 := testFacePhoto("photo-2", "sha-1")
	if result, created, err := store.RegisterFacePhoto(&photo2); err != nil || created || result.ID != "photo-1" {
		t.Fatalf("checksum dedup result=%#v created=%v err=%v", result, created, err)
	}
	photo3 := testFacePhoto("photo-1", "different")
	if _, _, err := store.RegisterFacePhoto(&photo3); contract.APIErrorCode(err) != contract.ErrorConflict {
		t.Fatalf("collision err=%v", err)
	}
	otherResident := photo2
	otherResident.ID = "photo-3"
	otherResident.ResidentID = "resident-2"
	if _, _, err := store.RegisterFacePhoto(&otherResident); contract.APIErrorCode(err) != contract.ErrorConflict {
		t.Fatalf("cross-resident duplicate accepted: %v", err)
	}
}

func TestFacePhotoTransitionsAndLegacyRestore(t *testing.T) {
	store := NewStore()
	photo := testFacePhoto("photo-1", "sha-1")
	if _, _, err := store.RegisterFacePhoto(&photo); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.TransitionFacePhoto(photo.ID, contract.FacePhotoActive, ""); contract.APIErrorCode(err) != contract.ErrorConflict {
		t.Fatalf("inverse transition accepted err=%v", err)
	}
	if _, _, err := store.TransitionFacePhoto(photo.ID, contract.FacePhotoValidating, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.TransitionFacePhoto(photo.ID, contract.FacePhotoActive, ""); err != nil {
		t.Fatal(err)
	}
	loaded := NewStore()
	loaded.applyPersistedState(&PersistedState{Version: PersistedStateVersion, FacePhotos: nil, FaceDataset: nil})
	if list := loaded.FacePhotosList("", 10); len(list) != 0 {
		t.Fatalf("legacy state produced photos: %#v", list)
	}
}

func TestFacePhotoPersistsAndRestores(t *testing.T) {
	path := t.TempDir() + "/state.json"
	store := NewStore(WithPersistencePath(path))
	photo := testFacePhoto("photo-persisted", "sha-persisted")
	if _, _, err := store.RegisterFacePhoto(&photo); err != nil {
		t.Fatal(err)
	}
	restored := NewStore(WithPersistencePath(path))
	if _, err := restored.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	value, ok := restored.FacePhoto(photo.ID)
	if !ok || value.StorageKey != photo.StorageKey || value.Status != string(contract.FacePhotoStored) {
		t.Fatalf("restored=%#v ok=%v", value, ok)
	}
	value.StorageKey = "mutated"
	again, _ := restored.FacePhoto(photo.ID)
	if again.StorageKey == "mutated" {
		t.Fatal("face photo accessor did not make defensive copy")
	}
}

func TestMissingFacePhotoAdvancesDatasetRevision(t *testing.T) {
	store := NewStore()
	photo := testFacePhoto("photo-missing", "sha-missing")
	if _, _, err := store.RegisterFacePhoto(&photo); err != nil {
		t.Fatal(err)
	}
	before := store.FaceDatasetState().DesiredRevision
	if _, changed, err := store.TransitionFacePhoto(photo.ID, contract.FacePhotoMissing, "source_missing"); err != nil || !changed {
		t.Fatalf("missing transition changed=%t err=%v", changed, err)
	}
	after := store.FaceDatasetState()
	if after.DesiredRevision != before+1 || after.Status == contract.FaceDatasetActive {
		t.Fatalf("missing photo did not invalidate desired dataset: before=%d after=%#v", before, after)
	}
}
