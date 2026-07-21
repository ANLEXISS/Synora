package main

import (
	"testing"
	"time"

	"synora/internal/engine"
	"synora/internal/state"
	"synora/pkg/contract"
)

func TestBuildHistoricalDecisionRefIsRedactedAndAuthoritative(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	event := &contract.Event{ID: "adapter-event", Type: "vision.identity", Identity: "sensitive-identity", Payload: map[string]any{"raw": "secret-marker"}}
	result := &engine.Result{Decision: &contract.Decision{ID: "decision-adapter", EventID: event.ID, State: "activity", EffectiveScore: 0.5, Timestamp: at}}
	ref, err := buildHistoricalDecisionRef(event, result, state.SystemState{LastState: "idle"}, state.SystemState{LastState: "activity"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if ref == nil || !ref.HistoricalDecisionHasProductionAuthority || ref.SourceEventRef != event.ID || ref.DecisionScorePermille != 500 {
		t.Fatalf("ref=%+v", ref)
	}
	if ref.CurrentStateCode == "sensitive-identity" || ref.Fingerprint == "" {
		t.Fatalf("unredacted or unfingerprinted ref=%+v", ref)
	}
}
