package cge

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"synora/pkg/contract"
)

func TestFeedbackStorePersistsEvaluationAndChainFeedback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feedback.json")
	store := NewFeedbackStore(path)
	evaluation, err := store.AddEvaluation(contract.CgeEvaluationFeedback{ChainID: "chain-1", EventID: "event-1", CorrectionType: contract.CgeCorrectionFalsePositive})
	if err != nil || evaluation.ID == "" {
		t.Fatalf("evaluation feedback: %#v %v", evaluation, err)
	}
	chain, err := store.AddChain(contract.CgeChainFeedback{ChainID: "chain-1", FinalOutcome: contract.CgeOutcomeRealIncident})
	if err != nil || chain.ID == "" {
		t.Fatalf("chain feedback: %#v %v", chain, err)
	}
	reloaded := NewFeedbackStore(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("load feedback: %v", err)
	}
	if got := reloaded.List("chain-1"); len(got) != 2 {
		t.Fatalf("feedback count = %d", len(got))
	}
}

func TestFeedbackStoreAcceptsIntentPayloadAndNormalizesDefaults(t *testing.T) {
	store := NewFeedbackStore(filepath.Join(t.TempDir(), "feedback.json"))
	evaluation, err := store.AddEvaluation(contract.CgeEvaluationFeedback{
		ChainID: "chain-1", EventID: "event-1", CorrectionType: contract.CgeCorrectionReactionTooStrong,
		Scope:            contract.CgeFeedbackApplyToSimilar,
		PreferredActions: []string{string(contract.CgeActionObserve), string(contract.CgeActionRequestUserValidation)},
		AdminNote:        "réduire la réaction automatique",
	})
	if err != nil || evaluation.Scope != contract.CgeFeedbackApplyToSimilar || len(evaluation.PreferredActions) != 2 {
		t.Fatalf("intent evaluation feedback: %#v %v", evaluation, err)
	}
	chain, err := store.AddChain(contract.CgeChainFeedback{
		ChainID: "chain-1", CorrectionType: contract.CgeCorrectionCorrectTuneActions,
		Scope: contract.CgeFeedbackCaseOnly, PreferredActions: []string{}, AdminNote: "observer d’abord",
	})
	if err != nil || chain.Scope != contract.CgeFeedbackCaseOnly || chain.AdminNote == "" {
		t.Fatalf("intent chain feedback: %#v %v", chain, err)
	}
}

func TestFeedbackStoreRejectsUnknownPreferredAction(t *testing.T) {
	store := NewFeedbackStore(filepath.Join(t.TempDir(), "feedback.json"))
	if _, err := store.AddEvaluation(contract.CgeEvaluationFeedback{
		ChainID: "chain-1", EventID: "event-1", CorrectionType: contract.CgeCorrectionFalsePositive,
		PreferredActions: []string{"rewrite_engine"},
	}); err == nil {
		t.Fatal("unknown preferred action should be rejected")
	}
}

func TestFeedbackStoreAcceptsStructuredSuggestedActionsAndLegacyStrings(t *testing.T) {
	var feedback contract.CgeChainFeedback
	if err := json.Unmarshal([]byte(`{"chain_id":"chain-1","correction_type":"reaction_too_weak","scope":"apply_to_similar_future_chains","preferred_actions":[{"command":"notify.whatsapp","target":"owner","enabled":true}],"blocked_actions":[{"command":"siren","reason":"too_aggressive"}]}`), &feedback); err != nil {
		t.Fatal(err)
	}
	if len(feedback.PreferredActionDetails) != 1 || feedback.PreferredActions[0] != "notify.whatsapp" || len(feedback.BlockedActions) != 1 {
		t.Fatalf("structured feedback not normalized: %#v", feedback)
	}
	store := NewFeedbackStore(filepath.Join(t.TempDir(), "feedback.json"))
	created, err := store.AddChain(feedback)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.PreferredActions) != 1 {
		t.Fatalf("legacy-compatible action missing: %#v", created)
	}
}
