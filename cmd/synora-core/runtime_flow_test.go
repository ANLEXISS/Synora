package main

import (
	"testing"
	"time"

	"synora/internal/automation"
	"synora/internal/event"
	"synora/internal/state"
	"synora/pkg/contract"
)

func TestManualRiskDangerLevelConditionDispatchesActionRequest(t *testing.T) {
	app, bus := newTestCoreApp(t)
	if err := app.automation.Add(automation.Rule{
		ID:        "manual-high-push",
		Enabled:   true,
		EventType: contract.EventManualRisk,
		Conditions: []automation.Condition{{
			Field: "danger.level", Op: ">", Value: "medium",
		}},
		Actions: []automation.AutomationAction{{
			Type:    "push",
			Target:  "owner",
			Enabled: true,
		}},
	}); err != nil {
		t.Fatalf("add manual risk automation: %v", err)
	}

	app.processEvent(&contract.Event{
		ID:        "evt-manual-high",
		Type:      contract.EventManualRisk,
		Source:    "admin",
		Timestamp: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"danger_level": "high", "duration_seconds": 60},
	})

	requests := bus.messagesOfType(contract.EventActionRequest)
	if len(requests) != 1 {
		t.Fatalf("expected one action.request for manual high danger, got %d", len(requests))
	}
	if current := app.state.SystemState(); len(current.BlockingReasons) != 0 {
		t.Fatalf("matched automation should not leave blocking reasons: %#v", current.BlockingReasons)
	}
}

func TestManualRiskNoMatchingAutomationKeepsReason(t *testing.T) {
	app, _ := newTestCoreApp(t)
	if err := app.automation.Add(automation.Rule{
		ID:        "manual-critical-only",
		Enabled:   true,
		EventType: contract.EventManualRisk,
		Conditions: []automation.Condition{{
			Field: "danger.level", Op: ">", Value: "critical",
		}},
		Actions: []automation.AutomationAction{{Type: "push", Target: "owner", Enabled: true}},
	}); err != nil {
		t.Fatalf("add manual risk automation: %v", err)
	}

	app.processEvent(&contract.Event{
		ID:        "evt-manual-high-no-match",
		Type:      contract.EventManualRisk,
		Source:    "admin",
		Timestamp: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"danger_level": "high", "duration_seconds": 60},
	})

	current := app.state.SystemState()
	if len(current.BlockingReasons) != 1 || current.BlockingReasons[0] != "no_matching_automation" {
		t.Fatalf("unexpected no-match blocking reasons: %#v", current.BlockingReasons)
	}
}

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
	if !app.applyManualRiskState(eventValue, false, store.SystemState()) {
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
	if current.LastState != "idle" || current.DangerLevel != string(contract.DangerNone) || current.DangerSource != "none" || current.DangerScore != 0 || current.ManualRiskActive {
		t.Fatalf("expired manual state=%#v", current)
	}
}

func TestActionServiceStartedUpdatesRuntimeComponent(t *testing.T) {
	store := state.NewStore()
	app := &coreApp{state: store}
	app.recordRuntimeEvent(&contract.Event{
		Type:    contract.EventActionServiceStarted,
		Source:  "actions",
		Payload: map[string]any{"status": "ok"},
	})
	current := store.SystemState()
	if current.RuntimeComponents["actions"] != "ok" || current.RuntimeComponentInfo["actions"] != "bus client registered" {
		t.Fatalf("runtime actions=%#v info=%#v", current.RuntimeComponents, current.RuntimeComponentInfo)
	}
}

func TestManualRiskTestDoesNotChangeRealDanger(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	store := state.NewStore()
	previous := store.SystemState()
	previous.LastState = "activity"
	previous.DangerLevel = string(contract.DangerLow)
	previous.DangerScore = 0.4
	previous.DangerKnown = true
	previous.DangerSource = "real"
	store.SetSystemState(previous)
	app := &coreApp{state: store}
	eventValue := &contract.Event{Type: contract.EventManualRisk, Timestamp: now, Payload: map[string]any{
		"danger_level":     "high",
		"duration_seconds": 2,
		"metadata":         map[string]any{"simulated": true, "dry_run": true},
	}}
	app.applyManualRiskState(eventValue, false, previous)
	current := store.SystemState()
	if !current.ManualRiskTest || current.ManualRiskLevel != "high" || current.LastState != "activity" || current.DangerLevel != string(contract.DangerLow) || current.DangerSource != "real" {
		t.Fatalf("test risk changed real state=%#v", current)
	}
	if !app.expireManualRisk(now.Add(3 * time.Second)) {
		t.Fatal("test risk should expire")
	}
	current = store.SystemState()
	if current.LastState != "activity" || current.DangerLevel != string(contract.DangerLow) || current.ManualRiskActive {
		t.Fatalf("real state after test expiration=%#v", current)
	}
}

func TestManualRiskExpirationPreservesOtherRealChain(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	store := state.NewStore()
	chains := event.NewChainManager(event.DefaultChainConfig())
	app := &coreApp{state: store, chains: chains}
	chains.Process(&contract.Event{
		ID:        "real-risk",
		Type:      contract.EventVisionUnknown,
		Timestamp: now,
		Payload:   map[string]any{"activation_id": "real"},
	}, &contract.ChainEvaluation{
		State:       "intrusion",
		DangerLevel: "critical",
		DangerScore: 0.95,
	})
	manual := &contract.Event{
		ID:        "manual-risk",
		Type:      contract.EventManualRisk,
		Timestamp: now.Add(time.Second),
		Payload: map[string]any{
			"danger_level":     "high",
			"duration_seconds": 2,
		},
	}
	app.applyManualRiskState(manual, false, store.SystemState())
	if !app.expireManualRisk(now.Add(4 * time.Second)) {
		t.Fatal("manual risk should expire")
	}
	current := store.SystemState()
	if current.DangerLevel != string(contract.DangerCritical) || current.DangerSource != "real" || current.LastState != "intrusion" {
		t.Fatalf("real chain was lost on expiration: %#v", current)
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
