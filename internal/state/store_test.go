package state

import (
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestDeviceCollectionAlias(t *testing.T) {
	store := NewStore()
	store.SetDeviceState(&DeviceState{ID: "cam_01", Type: "camera"})

	if _, ok := store.Snapshot("devices")["cam_01"]; !ok {
		t.Fatal("devices snapshot should include device state")
	}

	if _, ok := store.Snapshot("device")["cam_01"]; !ok {
		t.Fatal("device alias snapshot should include device state")
	}

	store.Delete("device", "cam_01")

	if _, ok := store.DeviceState("cam_01"); ok {
		t.Fatal("device alias delete should remove device state")
	}
}

func TestActionResultsSnapshot(t *testing.T) {
	store := NewStore()
	store.SetActionResult(&contract.ActionResult{
		ID:         "result-1",
		RequestID:  "action-1",
		Status:     "success",
		StartedAt:  time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 7, 8, 12, 0, 1, 0, time.UTC),
	})

	items := store.Snapshot("action_results")
	if _, ok := items["result-1"]; !ok {
		t.Fatalf("action_results snapshot should include result: %#v", items)
	}
	if store.Size() != 0 {
		t.Fatalf("action results should not inflate runtime state size, got %d", store.Size())
	}
}
