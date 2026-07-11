package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"synora/pkg/contract"
)

type fakeAutomationCatalogState struct {
	snapshot *contract.PublicSnapshot
	err      error
}

func (f fakeAutomationCatalogState) State() (*contract.PublicSnapshot, error) {
	return f.snapshot, f.err
}

func TestAutomationCatalogEndpointReturnsControlledOptions(t *testing.T) {
	handler := handleAutomationCatalog(fakeAutomationCatalogState{
		snapshot: &contract.PublicSnapshot{
			Devices: []map[string]any{
				{"id": "cam_01", "name": "Caméra Entrée", "type": contract.DeviceTypeCamera},
				{"id": "siren_01", "name": "Sirène Entrée", "type": contract.DeviceTypeSiren},
			},
			Nodes: []map[string]any{
				{"id": "zoneA", "name": "zoneA", "type": "zone"},
				{"id": "zoneA.L0", "name": "L0", "type": "floor"},
				{"id": "zoneA.L0.entree", "name": "entree", "type": "room"},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/automations/catalog", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, key := range []string{"condition_kinds", "action_kinds", "action_commands", "targets"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing key %q in %v", key, payload)
		}
	}

	conditionKinds := payload["condition_kinds"].([]any)
	if len(conditionKinds) == 0 {
		t.Fatalf("condition_kinds empty")
	}

	seen := map[string]bool{}
	for _, raw := range conditionKinds {
		item := raw.(map[string]any)
		seen[item["kind"].(string)] = true
		if ops, ok := item["operators"].([]any); !ok || len(ops) == 0 {
			t.Fatalf("operators missing for %v", item["kind"])
		}
	}
	for _, kind := range []string{"event.type", "system.state", "node.id", "danger.level", "device.id"} {
		if !seen[kind] {
			t.Fatalf("missing condition kind %q", kind)
		}
	}

	actionKinds := payload["action_kinds"].([]any)
	actionSeen := map[string]bool{}
	for _, raw := range actionKinds {
		actionSeen[raw.(map[string]any)["kind"].(string)] = true
	}
	for _, kind := range []string{"device.command", "record.clip", "notify", "siren"} {
		if !actionSeen[kind] {
			t.Fatalf("missing action kind %q", kind)
		}
	}

	targets := payload["targets"].(map[string]any)
	if len(targets["cameras"].([]any)) == 0 || len(targets["sirens"].([]any)) == 0 {
		t.Fatalf("dynamic targets not populated: %#v", targets)
	}
	if len(targets["notify"].([]any)) == 0 {
		t.Fatalf("notify targets missing")
	}
}
