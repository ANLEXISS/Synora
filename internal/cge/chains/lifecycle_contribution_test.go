package chains

import (
	"testing"
	"time"
)

func TestContradictionChangesLifecycleEvaluationButDoesNotApplyTransition(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	chain, err := New("lifecycle-contribution", MutationContext{At: base, Actor: "test", Reason: "create", CorrelationID: "create"})
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	if err := chain.SetStatus(StatusActive, MutationContext{At: base.Add(time.Second), Actor: "test", Reason: "activate", CorrelationID: "activate"}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := chain.AddContribution(ConfidenceContribution{ID: "contradiction", Source: "review", Kind: ContributionContradiction, Value: 1, Reason: "contradiction", CreatedAt: base.Add(2 * time.Second)}, MutationContext{At: base.Add(2 * time.Second), Actor: "reviewer", Reason: "contradiction", CorrelationID: "contradiction"}); err != nil {
		t.Fatalf("contribution: %v", err)
	}
	snapshot := chain.Snapshot()
	evaluation, err := EvaluateLifecycle(snapshot, base.Add(3*time.Second), DefaultLifecyclePolicy())
	if err != nil || evaluation.Proposal == nil || evaluation.Proposal.To != StatusDeclining {
		t.Fatalf("expected explicit declining proposal: evaluation=%#v err=%v", evaluation, err)
	}
	if got := chain.Snapshot(); got.Status != StatusActive || got.CurrentConfidence != 0 {
		t.Fatalf("evaluation applied a lifecycle transition: %#v", got)
	}
}

func TestHistoricalContributionDoesNotReactivateOrChangeReliability(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	chain, err := New("historical-contribution", MutationContext{At: base, Actor: "test", Reason: "create", CorrelationID: "create"})
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	if err := chain.SetStatus(StatusActive, MutationContext{At: base.Add(time.Second), Actor: "test", Reason: "activate", CorrelationID: "activate"}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := chain.SetStatus(StatusDormant, MutationContext{At: base.Add(2 * time.Second), Actor: "test", Reason: "dormant", CorrelationID: "dormant"}); err != nil {
		t.Fatalf("dormant: %v", err)
	}
	if err := chain.SetHistoricalReliability(0.8, MutationContext{At: base.Add(3 * time.Second), Actor: "reviewer", Reason: "reliability", CorrelationID: "reliability"}); err != nil {
		t.Fatalf("reliability: %v", err)
	}
	before := chain.Snapshot()
	if err := chain.AddContribution(ConfidenceContribution{ID: "historical-support", Source: "review", Kind: ContributionSupport, Value: 0.4, Reason: "historical support", CreatedAt: base.Add(5 * time.Second)}, MutationContext{At: base.Add(5 * time.Second), Actor: "reviewer", Reason: "historical support", CorrelationID: "historical-support"}); err != nil {
		t.Fatalf("historical contribution: %v", err)
	}
	after := chain.Snapshot()
	if after.Status != StatusDormant || after.HistoricalReliability != before.HistoricalReliability || after.CurrentConfidence != 0.4 {
		t.Fatalf("historical contribution changed forbidden state: before=%#v after=%#v", before, after)
	}
}
