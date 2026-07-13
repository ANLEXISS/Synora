package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"synora/pkg/contract"
)

type diagnosticFakeCore struct {
	state  *contract.PublicSnapshot
	health *contract.RuntimeHealth
}

func (f diagnosticFakeCore) State() (*contract.PublicSnapshot, error) {
	return f.state, nil
}

func (f diagnosticFakeCore) SystemHealth() (*contract.RuntimeHealth, error) {
	return f.health, nil
}

func TestRuntimeDiagnosticsUsesCoreStateAndRuntimeComponents(t *testing.T) {
	recorder := httptest.NewRecorder()
	core := diagnosticFakeCore{
		state: &contract.PublicSnapshot{System: map[string]any{
			"last_state":       "suspicious",
			"danger_known":     true,
			"danger_level":     "high",
			"danger_score":     0.8,
			"runtime_components": map[string]any{
				"discovery":    "degraded",
				"vision_worker": "unavailable",
				"actions":      "ok",
			},
		}},
		health: &contract.RuntimeHealth{Status: "degraded", Components: map[string]contract.RuntimeServiceHealth{
			"discovery": {Name: "discovery", Status: "degraded"},
		}},
	}
	handleRuntimeDiagnostics(core).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/runtime/diagnostics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["current_state"] != "suspicious" || response["danger_level"] != "high" || response["discovery_status"] != "degraded" || response["vision_worker_status"] != "unavailable" {
		t.Fatalf("diagnostics=%#v", response)
	}
}
