package main

import (
	"testing"
	"time"

	"synora/internal/event"
	"synora/internal/state"
	"synora/pkg/contract"
)

func TestManualRiskStateIsVisibleAndExpires(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	store := state.NewStore()
	app := &coreApp{state: store}
	eventValue := &contract.Event{
		ID:        "evt-manual-risk",
		Type:      contract.EventManualRisk,
		Source:    "admin",
		Timestamp: now,
		Payload: map[string]any{
			"danger_level":     "high",
			"duration_seconds": 2,
		},
	}
	if !app.applyManualRiskState(eventValue, false) {
		t.Fatal("manual risk should change the state")
	}
	current := store.SystemState()
	if current.LastState != "suspicious" || current.DangerSource != "manual" || !current.ManualRiskActive || current.DangerLevel == string(contract.DangerNone) {
		t.Fatalf("manual state=%#v", current)
	}
	if app.expireManualRisk(now.Add(time.Second)) {
		t.Fatal("manual risk must remain active before expiration")
	}
	if !app.expireManualRisk(now.Add(3 * time.Second)) {
		t.Fatal("manual risk should expire")
	}
	current = store.SystemState()
	if current.LastState != "idle" || current.DangerLevel != string(contract.DangerNone) || current.ManualRiskActive {
		t.Fatalf("expired manual state=%#v", current)
	}
}

func TestManualRiskChainCanBeClosedAtExpiration(t *testing.T) {
	manager := event.NewChainManager(event.DefaultChainConfig())
	when := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	manual := &contract.Event{ID: "manual", Type: contract.EventManualRisk, Source: "admin", Timestamp: when}
	manager.Process(manual, &contract.ChainEvaluation{EventID: manual.ID, Timestamp: when, DangerLevel: "high", DangerScore: 0.8})
	updates := manager.CloseManualRiskChains(when.Add(2 * time.Second))
	if len(updates) != 1 || updates[0].Chain.ClosedReason != "manual_risk_expired" {
		t.Fatalf("updates=%#v", updates)
	}
}

func TestDiscoveryRuntimeStatusIsStoredForDiagnostics(t *testing.T) {
	store := state.NewStore()
	current := store.SystemState()
	applyDiscoveryRuntimeStatus(&current, map[string]any{
		"status":         "degraded",
		"network":        "degraded",
		"vision_worker":  map[string]any{"status": "unavailable", "reason": "model_missing"},
		"vision_ingress": map[string]any{"status": "disabled", "reason": "tls_cert_missing"},
	})
	if current.RuntimeComponents["discovery"] != "degraded" || current.RuntimeComponents["vision_worker"] != "unavailable" || current.RuntimeComponents["vision_ingress"] != "disabled" || !current.Degraded {
		t.Fatalf("runtime state=%#v", current)
	}
}
