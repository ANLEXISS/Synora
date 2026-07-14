package main

import (
	"testing"

	"synora/internal/actionpolicy"
	"synora/internal/engine"
	"synora/internal/state"
	"synora/pkg/contract"
)

func TestActionPolicyEnrichesHighDecisionWithoutExecuting(t *testing.T) {
	policy := actionpolicy.NewStore("")
	app := &coreApp{policy: policy, state: state.NewStore()}
	result := &engine.Result{Decision: &contract.Decision{DangerLevel: string(contract.DangerHigh), DangerScore: .75}, DangerAssessment: &contract.DangerAssessment{RiskLevel: string(contract.DangerHigh), Score: .75, RecommendedSystemActions: []contract.SystemActionRecommendation{{Type: contract.SystemActionRecordClipIfAvailable}}}}
	app.applyActionPolicy(&contract.Event{ID: "event-1", Payload: map[string]any{}}, result)
	if len(result.Decision.RecommendedActionsFromCGE) != 1 || len(result.Decision.FinalActionPlan) != 3 {
		t.Fatalf("unexpected enriched decision: %#v", result.Decision)
	}
	if result.Decision.FinalActionPlan[0].Command != "notify.whatsapp" || result.Decision.FinalActionPlan[0].Source != "policy" {
		t.Fatalf("unexpected policy plan: %#v", result.Decision.FinalActionPlan)
	}
}

func TestMediumHighPolicyDoesNotActivateHighOnlyAction(t *testing.T) {
	policy := actionpolicy.NewStore("")
	app := &coreApp{policy: policy, state: state.NewStore()}
	result := &engine.Result{Decision: &contract.Decision{DangerLevel: string(contract.DangerMediumHigh), DangerScore: .65}, DangerAssessment: &contract.DangerAssessment{RiskLevel: string(contract.DangerMediumHigh), Score: .65}}
	app.applyActionPolicy(&contract.Event{ID: "event-2", Payload: map[string]any{}}, result)
	for _, action := range result.Decision.PolicyActions {
		if action.Command == "mark_intrusion_candidate" {
			t.Fatalf("high-only action leaked into medium_high: %#v", result.Decision.PolicyActions)
		}
	}
}
