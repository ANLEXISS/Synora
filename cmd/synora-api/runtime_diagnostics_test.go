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
			"last_state":   "suspicious",
			"danger_known": true,
			"danger_level": "high",
			"danger_score": 0.8,
			"runtime_components": map[string]any{
				"discovery":     "degraded",
				"vision_worker": "unavailable",
				"actions":       "ok",
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
	runtime, ok := response["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime=%#v", response["runtime"])
	}
	components, ok := runtime["components"].(map[string]any)
	if !ok || components["discovery"].(map[string]any)["status"] != "degraded" {
		t.Fatalf("runtime components=%#v", runtime["components"])
	}
	runtimeComponents := response["runtime_components"].(map[string]any)
	if runtimeComponents["discovery"] != "degraded" || runtimeComponents["vision_worker"] != "unavailable" {
		t.Fatalf("runtime_components=%#v", runtimeComponents)
	}
}

func TestRuntimeDiagnosticsExposesSecurityModeAndManualRisk(t *testing.T) {
	response := runtimeDiagnosticsResponse(&contract.PublicSnapshot{System: map[string]any{
		"security":           map[string]any{"mode": "high_security", "armed": true, "expected_occupancy": "empty"},
		"manual_risk_active": true, "manual_risk_level": "high", "manual_risk_score": 0.75,
		"danger_level": "high", "danger_source": "manual", "danger_known": true,
	}}, nil, nil, nil)
	if response["security_mode"] != "high_security" || response["security_armed"] != true || response["manual_risk_active"] != true {
		t.Fatalf("runtime security projection=%#v", response)
	}
}

func TestRuntimeDiagnosticsDefaultsSecurityFields(t *testing.T) {
	response := runtimeDiagnosticsResponse(&contract.PublicSnapshot{System: map[string]any{}}, nil, nil, nil)
	if response["security_mode"] != "home" || response["security_armed"] != false || response["expected_occupancy"] != "unknown" {
		t.Fatalf("runtime security defaults=%#v", response)
	}
}

func TestSystemHealthMarksServingAPIAndReachableBus(t *testing.T) {
	recorder := httptest.NewRecorder()
	core := diagnosticFakeCore{health: &contract.RuntimeHealth{Status: "degraded", Services: map[string]contract.RuntimeServiceHealth{
		"synora-core": {Name: "synora-core", Status: "ok", Active: true},
	}}}
	handleSystemHealth(core, nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/system/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	services, ok := response["services"].(map[string]any)
	if !ok || services["synora-api"].(map[string]any)["status"] != "ok" || services["synora-bus"].(map[string]any)["status"] != "ok" {
		t.Fatalf("services=%#v", response["services"])
	}
}

func TestRuntimeDiagnosticsKeepsExplicitManualExpirationState(t *testing.T) {
	response := runtimeDiagnosticsResponse(
		&contract.PublicSnapshot{
			System: map[string]any{
				"danger_known":  true,
				"danger_level":  "none",
				"danger_score":  0.0,
				"danger_source": "none",
				"last_state":    "idle",
			},
			EventChains: map[string]any{
				"highest_real_danger_level": "critical",
			},
		},
		nil,
		nil,
		nil,
	)
	if response["danger_level"] != "none" || response["danger_source"] != "none" || response["danger_score"] != 0.0 {
		t.Fatalf("stale chain danger overwrote state: %#v", response)
	}
}
