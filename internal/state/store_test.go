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

func TestActionResultsHistoryIsBounded(t *testing.T) {
	store := NewStore()
	base := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 205; i++ {
		store.SetActionResult(&contract.ActionResult{
			ID:         "result-" + time.Duration(i).String(),
			RequestID:  "action-" + time.Duration(i).String(),
			Status:     contract.ActionStatusSuccess,
			FinishedAt: base.Add(time.Duration(i) * time.Second),
		})
	}

	items := store.Snapshot("action_results")
	if len(items) != maxActionResults {
		t.Fatalf("expected bounded action result history, got %d", len(items))
	}
	if _, ok := items["result-0s"]; ok {
		t.Fatalf("oldest action result should be evicted: %#v", items["result-0s"])
	}
}

func TestSimulatedEventsAndActionResultsVisibleInSnapshot(t *testing.T) {
	store := NewStore()
	store.SetRecentEvents([]*contract.Event{{
		ID:   "evt-sim",
		Type: contract.EventVisionUnknown,
		Payload: map[string]any{
			"metadata": map[string]any{
				"simulated":   true,
				"test_run_id": "sim-1",
			},
		},
	}})
	store.SetActionResult(&contract.ActionResult{
		ID:        "ares-sim",
		RequestID: "areq-sim",
		Status:    contract.ActionStatusSimulatedSuccess,
		Data: map[string]any{
			"metadata": map[string]any{
				"simulated":   true,
				"test_run_id": "sim-1",
			},
		},
	})

	public := contract.PublicSnapshotFromCoreState(map[string]any{
		"state_store": map[string]any{
			"events":         store.Snapshot("events"),
			"action_results": store.Snapshot("action_results"),
		},
	})
	eventMetadata := public.Events[0]["payload"].(map[string]any)["metadata"].(map[string]any)
	if eventMetadata["simulated"] != true || eventMetadata["test_run_id"] != "sim-1" {
		t.Fatalf("simulated event metadata should be visible: %#v", public.Events)
	}
	resultMetadata := public.ActionResults[0]["data"].(map[string]any)["metadata"].(map[string]any)
	if resultMetadata["simulated"] != true || resultMetadata["test_run_id"] != "sim-1" {
		t.Fatalf("simulated action result metadata should be visible: %#v", public.ActionResults)
	}
}
