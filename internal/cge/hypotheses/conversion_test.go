package hypotheses

import (
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
)

func conversionMutation(at time.Time) chains.MutationContext {
	return chains.MutationContext{At: at, Actor: "cge-test", Reason: "record hypothesis", CorrelationID: "hyp-1"}
}

func ambiguousAssociationPlan() association.Plan {
	return association.Plan{
		PolicyVersion: "association-v1", PlannedAt: hypothesisTestBase,
		Decision:    association.DecisionAmbiguous,
		Observation: chains.ObservationRef{ID: "obs-association", EventType: "vision.identity", Timestamp: hypothesisTestBase},
		BestScore:   80, ScoreMargin: 0, ReasonCode: association.ReasonAmbiguous, Reason: "two candidates remain plausible",
		RankedCandidates: []association.CandidateScore{
			{ChainID: "chain-a", SourceRevision: 4, Status: chains.StatusActive, Eligible: true, Score: 80, Facts: []association.ScoreFact{{Code: "entity.same", Score: 80}}},
			{ChainID: "chain-b", SourceRevision: 7, Status: chains.StatusActive, Eligible: true, Score: 80, Facts: []association.ScoreFact{{Code: "sequence.same", Score: 80}}},
		},
	}
}

func ambiguousEvidenceEvaluation() evidence.EvidenceEvaluation {
	return evidence.EvidenceEvaluation{
		ChainID: "chain-evidence", SourceRevision: 9, TargetObservationID: "obs-evidence", EvaluatedAt: hypothesisTestBase,
		PolicyNamespace: "synora.cge.evidence", PolicyVersion: "evidence-v1", EvidenceFingerprint: "sha256:abc123",
		ResolutionValues: evidence.ResolutionValues{SupportValue: 0.10, ContradictionValue: 0.15, NeutralValue: 0},
		Decision:         evidence.DecisionAmbiguous, SupportScore: 80, ContradictionScore: 70, DecisionMargin: 10,
		ContextObservationIDs: []string{"context-a", "context-b"},
		Facts: []evidence.EvidenceFact{
			{Code: "entity.context_same", Side: evidence.EvidenceSupport, Score: 60, ObservationIDs: []string{"obs-evidence", "context-a"}},
			{Code: "entity.context_conflict", Side: evidence.EvidenceContradiction, Score: 70, ObservationIDs: []string{"obs-evidence", "context-b"}},
		},
		ReasonCode: "evidence.ambiguous", Reason: "support and contradiction remain plausible",
	}
}

func TestFromAmbiguousAssociationPreservesCandidates(t *testing.T) {
	plan := ambiguousAssociationPlan()
	set, err := FromAmbiguousAssociation(plan, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	if set.Family() != FamilyAssociation || set.Status() != StatusOpen || set.Revision() != 1 {
		t.Fatal("unexpected hypothesis envelope")
	}
	snapshot := set.Snapshot()
	if len(snapshot.Alternatives) != 2 || snapshot.Alternatives[0].ChainID != "chain-a" || snapshot.Alternatives[1].ChainID != "chain-b" {
		t.Fatalf("unexpected alternatives: %+v", snapshot.Alternatives)
	}
	if snapshot.Alternatives[0].SourceRevision != 4 || snapshot.Alternatives[1].SourceRevision != 7 {
		t.Fatal("source revisions were not preserved")
	}
	repeated, err := FromAmbiguousAssociation(plan, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil || repeated.ID() != set.ID() {
		t.Fatalf("conversion is not deterministic: %v", err)
	}
	plan.Decision = association.DecisionAttachExisting
	if _, err := FromAmbiguousAssociation(plan, hypothesisTestBase, conversionMutation(hypothesisTestBase)); !errors.Is(err, ErrAssociationNotAmbiguous) {
		t.Fatalf("expected non-ambiguous rejection, got %v", err)
	}
}

func TestFromAmbiguousEvidencePreservesDirections(t *testing.T) {
	evaluation := ambiguousEvidenceEvaluation()
	set, err := FromAmbiguousEvidence(evaluation, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := set.Snapshot()
	if set.Family() != FamilyEvidence || snapshot.Subject.ChainID != evaluation.ChainID || snapshot.Subject.EvidenceFingerprint != evaluation.EvidenceFingerprint {
		t.Fatal("evidence subject was not preserved")
	}
	if len(snapshot.Alternatives) != 2 || snapshot.Alternatives[0].Kind != AlternativeSupport || snapshot.Alternatives[1].Kind != AlternativeContradiction {
		t.Fatalf("unexpected evidence alternatives: %+v", snapshot.Alternatives)
	}
	evaluation.Facts[0].ObservationIDs[0] = "changed"
	if snapshot.Alternatives[0].Facts[0].ObservationIDs[0] == "changed" {
		t.Fatal("conversion shared fact slices")
	}
	evaluation.Decision = evidence.DecisionProposeSupport
	if _, err := FromAmbiguousEvidence(evaluation, hypothesisTestBase, conversionMutation(hypothesisTestBase)); !errors.Is(err, ErrEvidenceNotAmbiguous) {
		t.Fatalf("expected non-ambiguous rejection, got %v", err)
	}
}

func TestHypothesisCreationHasNoChainOrContributionEffects(t *testing.T) {
	plan := ambiguousAssociationPlan()
	before := plan.Clone()
	set, err := FromAmbiguousAssociation(plan, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	if set.Snapshot().Subject.ObservationID != plan.Observation.ID {
		t.Fatal("subject mismatch")
	}
	if plan.Observation.ID != before.Observation.ID || len(plan.RankedCandidates) != len(before.RankedCandidates) {
		t.Fatal("plan was mutated")
	}
}
