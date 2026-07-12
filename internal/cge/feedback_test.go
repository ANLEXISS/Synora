package cge

import (
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
