package chains

import (
	"errors"
	"testing"
	"time"
)

func TestContributionMutationUsesExistingFormulaAndOneRevision(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	chain, err := New("contribution-domain", MutationContext{At: base, Actor: "test", Reason: "create", CorrelationID: "create"})
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	observation := ObservationRef{ID: "observation-1", EventType: "vision.identity", Timestamp: base.Add(time.Second)}
	if err := chain.AddObservation(observation, MutationContext{At: base.Add(time.Second), Actor: "test", Reason: "observe", CorrelationID: "observe"}); err != nil {
		t.Fatalf("add observation: %v", err)
	}
	before := chain.Snapshot()
	if err := chain.AddContribution(ConfidenceContribution{ID: "support-1", Source: "review", Kind: ContributionSupport, Value: 0.7, ObservationIDs: []string{"observation-1"}, Reason: "support", CreatedAt: base.Add(2 * time.Second)}, MutationContext{At: base.Add(2 * time.Second), Actor: "reviewer", Reason: "support", CorrelationID: "support-1"}); err != nil {
		t.Fatalf("support contribution: %v", err)
	}
	if err := chain.AddContribution(ConfidenceContribution{ID: "contradiction-1", Source: "review", Kind: ContributionContradiction, Value: 0.4, ObservationIDs: []string{"observation-1"}, Reason: "contradiction", CreatedAt: base.Add(3 * time.Second)}, MutationContext{At: base.Add(3 * time.Second), Actor: "reviewer", Reason: "contradiction", CorrelationID: "contradiction-1"}); err != nil {
		t.Fatalf("contradiction contribution: %v", err)
	}
	if err := chain.AddContribution(ConfidenceContribution{ID: "neutral-1", Source: "context", Kind: ContributionNeutral, Value: 1, ObservationIDs: []string{"observation-1"}, Reason: "neutral", CreatedAt: base.Add(4 * time.Second)}, MutationContext{At: base.Add(4 * time.Second), Actor: "reviewer", Reason: "neutral", CorrelationID: "neutral-1"}); err != nil {
		t.Fatalf("neutral contribution: %v", err)
	}
	after := chain.Snapshot()
	if after.Revision != before.Revision+3 || after.CurrentConfidence != 0.3 || after.MaxHistoricalConfidence != 0.7 || after.ConfirmationCount != 1 || after.ContradictionCount != 1 || after.Status != before.Status || after.HistoricalReliability != before.HistoricalReliability {
		t.Fatalf("unexpected contribution effects: before=%#v after=%#v", before, after)
	}
	if len(after.History) != len(before.History)+3 || after.History[len(after.History)-1].Operation != OperationContributionAdded {
		t.Fatalf("contribution did not produce exactly one local revision each: %#v", after.History)
	}
	duplicateBefore := chain.Snapshot()
	duplicate := ConfidenceContribution{ID: "support-1", Source: "review", Kind: ContributionSupport, Value: 0.1, Reason: "duplicate", CreatedAt: base.Add(5 * time.Second)}
	if err := chain.AddContribution(duplicate, MutationContext{At: base.Add(5 * time.Second), Actor: "reviewer", Reason: "duplicate", CorrelationID: "duplicate"}); err == nil || !errors.Is(err, ErrDuplicateContribution) {
		t.Fatalf("duplicate contribution error = %v", err)
	}
	if got := chain.Snapshot(); got.Revision != duplicateBefore.Revision || len(got.Contributions) != len(duplicateBefore.Contributions) || got.CurrentConfidence != duplicateBefore.CurrentConfidence {
		t.Fatalf("duplicate changed chain: before=%#v after=%#v", duplicateBefore, got)
	}
}

func TestContributionStatusPolicyIsSeparateFromObservationPolicy(t *testing.T) {
	accepted := []Status{StatusCandidate, StatusActive, StatusConfirmed, StatusDeclining, StatusDormant, StatusArchived, StatusReactivated}
	for _, status := range accepted {
		if !status.CanAcceptContribution() || status.ValidateContributionMutation() != nil {
			t.Fatalf("status %s should accept explicit contributions", status)
		}
	}
	refused := []Status{StatusMerged, StatusSplit, StatusInvalidated}
	for _, status := range refused {
		if status.CanAcceptContribution() || !errors.Is(status.ValidateContributionMutation(), ErrContributionNotAllowed) {
			t.Fatalf("status %s should reject contributions", status)
		}
	}
}

func TestContributionReferencesMustExistAndBeUnique(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	contribution := ConfidenceContribution{ID: "c", Source: "test", Kind: ContributionNeutral, Value: 0.1, ObservationIDs: []string{"a", "a"}, Reason: "duplicate refs", CreatedAt: base}
	if err := contribution.Validate(); err == nil {
		t.Fatal("duplicate contribution references were accepted")
	}
	chain, err := New("reference-chain", MutationContext{At: base, Actor: "test", Reason: "create", CorrelationID: "create"})
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	if err := chain.AddContribution(ConfidenceContribution{ID: "missing-ref", Source: "test", Kind: ContributionSupport, Value: 0.1, ObservationIDs: []string{"missing"}, Reason: "missing", CreatedAt: base.Add(time.Second)}, MutationContext{At: base.Add(time.Second), Actor: "test", Reason: "missing", CorrelationID: "missing"}); err == nil || !errors.Is(err, ErrUnknownObservationReference) {
		t.Fatalf("unknown reference error = %v", err)
	}
}
