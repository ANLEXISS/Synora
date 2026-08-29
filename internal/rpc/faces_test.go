package rpc

import (
	"encoding/json"
	"sync"
	"testing"

	"synora/internal/snapshot"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

func TestFacePhotoRPCPersistsMetadataAndDoesNotExposeStorageKey(t *testing.T) {
	store := state.NewStore()
	server := NewServer(Config{State: store, Snapshot: &snapshot.Builder{Mu: &sync.RWMutex{}, Residents: map[string]*topology.Resident{"alexis": {ID: "alexis", Name: "Alexis"}}}})
	photo := contract.FacePhoto{ID: "photo-1", ResidentID: "alexis", StorageKey: "alexis/photo-1.png", Status: string(contract.FacePhotoStored), SizeBytes: 10, Checksum: "sha", MediaType: "image/png"}
	value, err := server.Handler("residents.photos.register")(messageWithPayload(photo))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(value)
	if containsString(string(encoded), "storage_key") || containsString(string(encoded), "face-absolute") {
		t.Fatalf("internal storage data leaked: %s", encoded)
	}
	listed, err := server.Handler("residents.photos.list")(rpcMessage(`{"resident_id":"alexis"}`))
	if err != nil || len(listed.([]contract.FacePhoto)) != 1 {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
}

func messageWithPayload(value any) contract.Message {
	data, _ := json.Marshal(value)
	return contract.Message{Payload: data}
}

func TestFacePhotoDeletionRequiresDatasetExclusionBeforeRemoved(t *testing.T) {
	store := state.NewStore()
	server := NewServer(Config{State: store, Snapshot: &snapshot.Builder{Mu: &sync.RWMutex{}, Residents: map[string]*topology.Resident{"alexis": {ID: "alexis", Name: "Alexis"}}}})
	photo := contract.FacePhoto{ID: "photo-delete", ResidentID: "alexis", StorageKey: "alexis/photo-delete.png", Status: string(contract.FacePhotoStored), SizeBytes: 10, Checksum: "sha-delete", MediaType: "image/png"}
	if _, err := server.Handler("residents.photos.register")(messageWithPayload(photo)); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Handler("residents.photos.delete")(rpcMessage(`{"id":"photo-delete","resident_id":"alexis"}`)); err != nil {
		t.Fatal(err)
	}
	value, _ := store.FacePhoto(photo.ID)
	if value.Status != string(contract.FacePhotoRemovalPending) {
		t.Fatalf("status=%s", value.Status)
	}
	dataset := store.FaceDatasetState()
	payload, _ := json.Marshal(map[string]any{"version": "v-delete", "desired_revision": dataset.DesiredRevision, "photo_ids": []string{}})
	result, err := server.Handler("face_dataset.activate")(contract.Message{Payload: payload})
	if err != nil || !containsString(string(mustJSON(result)), "photo-delete") {
		t.Fatalf("activation result=%#v err=%v", result, err)
	}
	if _, err := server.Handler("residents.photos.remove_confirmed")(rpcMessage(`{"id":"photo-delete"}`)); err != nil {
		t.Fatal(err)
	}
	value, _ = store.FacePhoto(photo.ID)
	if value.Status != string(contract.FacePhotoRemoved) {
		t.Fatalf("final status=%s", value.Status)
	}
}

func TestFaceDatasetBuildingIsIdempotentAndPersistent(t *testing.T) {
	store := state.NewStore()
	server := NewServer(Config{State: store, Snapshot: &snapshot.Builder{Mu: &sync.RWMutex{}, Residents: map[string]*topology.Resident{}}})
	first, err := server.Handler("face_dataset.building")(contract.Message{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := server.Handler("face_dataset.building")(contract.Message{})
	if err != nil {
		t.Fatal(err)
	}
	firstState := first.(*contract.FaceDatasetState)
	secondState := second.(*contract.FaceDatasetState)
	if firstState.Status != contract.FaceDatasetBuilding || secondState.Status != contract.FaceDatasetBuilding {
		t.Fatalf("building states=%#v %#v", firstState, secondState)
	}
	if secondState.DesiredRevision != firstState.DesiredRevision {
		t.Fatalf("idempotent request changed revision: first=%d second=%d", firstState.DesiredRevision, secondState.DesiredRevision)
	}
}

func TestResidentPrivacyExportExcludesStorageFields(t *testing.T) {
	store := state.NewStore()
	server := NewServer(Config{State: store, Snapshot: &snapshot.Builder{Mu: &sync.RWMutex{}, Residents: map[string]*topology.Resident{"alexis": {ID: "alexis", Name: "Alexis"}}}})
	photo := contract.FacePhoto{ID: "photo-export", ResidentID: "alexis", StorageKey: "alexis/photo-export.png", Path: "/var/lib/synora/vision/face/sources/alexis/photo-export.png", Status: string(contract.FacePhotoStored), SizeBytes: 10, Checksum: "sha-export", MediaType: "image/png"}
	if _, err := server.Handler("residents.photos.register")(messageWithPayload(photo)); err != nil {
		t.Fatal(err)
	}
	value, err := server.Handler("resident.privacy.export")(rpcMessage(`{"id":"alexis"}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(mustJSON(value))
	if containsString(encoded, "storage_key") || containsString(encoded, "var/lib") || !containsString(encoded, "biometric_data") {
		t.Fatalf("privacy export leaked internal data: %s", encoded)
	}
}

func mustJSON(value any) []byte { data, _ := json.Marshal(value); return data }
