package hypotheses

import (
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
)

func rebasePlan(version string, score int64) association.Plan {
	return association.Plan{PolicyVersion: version, PlannedAt: hypothesisTestBase, Decision: association.DecisionAmbiguous, Observation: chains.ObservationRef{ID: "rebase-observation", EventType: "vision.identity", Timestamp: hypothesisTestBase}, BestScore: score, ReasonCode: association.ReasonAmbiguous, Reason: "two candidates remain plausible", RankedCandidates: []association.CandidateScore{
		{ChainID: "chain-a", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: score, Facts: []association.ScoreFact{{Code: "entity.same", Score: score}}},
		{ChainID: "chain-b", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: score, Facts: []association.ScoreFact{{Code: "sequence.same", Score: score}}},
	}}
}

func TestRebasePreservesVersionsAndSupportsPolicyChange(t *testing.T) {
	first, err := FromAmbiguousAssociation(rebasePlan("association-v1", 70), hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := ProposeAssociationRebase(first.Snapshot(), rebasePlan("association-v2", 80), hypothesisTestBase.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.SourceRevision != 1 || proposal.PreviousAssessmentVersion != 1 || proposal.NewAssessment.Version != 2 {
		t.Fatalf("unexpected proposal: %+v", proposal)
	}
	command, err := proposal.Command(chains.MutationContext{At: hypothesisTestBase.Add(2 * time.Minute), Actor: "reviewer", Reason: "re-evaluate", CorrelationID: "rebase-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Rebase(command); err != nil {
		t.Fatal(err)
	}
	snapshot := first.Snapshot()
	if snapshot.Revision != 2 || snapshot.CurrentAssessmentVersion != 2 || len(snapshot.Assessments) != 2 || len(snapshot.History) != 2 {
		t.Fatalf("unexpected rebased snapshot: %+v", snapshot)
	}
	if snapshot.Assessments[0].Fingerprint == snapshot.Assessments[1].Fingerprint || snapshot.Status != StatusOpen {
		t.Fatal("rebase did not preserve append-only versions or status")
	}
	if snapshot.Assessments[1].Provenance.PolicyVersion != "association-v2" {
		t.Fatal("new assessment provenance was not preserved")
	}
}

func TestRebaseUnchangedAndStaleAreRejectedWithoutMutation(t *testing.T) {
	set, err := FromAmbiguousAssociation(rebasePlan("association-v1", 70), hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	before := set.Snapshot()
	if _, err := ProposeAssociationRebase(before, rebasePlan("association-v1", 70), hypothesisTestBase.Add(time.Minute)); !errors.Is(err, ErrHypothesisRebaseUnchanged) {
		t.Fatalf("expected unchanged rebase, got %v", err)
	}
	proposal, err := ProposeAssociationRebase(before, rebasePlan("association-v2", 80), hypothesisTestBase.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	command, err := proposal.Command(chains.MutationContext{At: hypothesisTestBase.Add(2 * time.Minute), Actor: "reviewer", Reason: "re-evaluate", CorrelationID: "rebase-2"})
	if err != nil {
		t.Fatal(err)
	}
	command.SourceRevision = 2
	if err := set.Rebase(command); !errors.Is(err, ErrStaleHypothesisRebase) {
		t.Fatalf("expected stale rebase, got %v", err)
	}
	if set.Snapshot().Revision != before.Revision || len(set.Snapshot().Assessments) != 1 {
		t.Fatal("stale rebase mutated the set")
	}
}

func TestLegacySnapshotSynthesizesInitialAssessment(t *testing.T) {
	set, err := FromAmbiguousAssociation(rebasePlan("association-v1", 70), hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := set.Snapshot()
	snapshot.Assessments = nil
	snapshot.CurrentAssessmentVersion = 0
	for i := range snapshot.Alternatives {
		snapshot.Alternatives[i].ResolutionEffect = nil
	}
	restored, err := Restore(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Snapshot().Revision != snapshot.Revision || len(restored.Snapshot().Assessments) != 1 || restored.Snapshot().CurrentAssessmentVersion != 1 {
		t.Fatal("legacy assessment was not synthesized deterministically")
	}
}

func TestEvidenceRebaseRequiresTheSameSubjectFingerprint(t *testing.T) {
	evaluation := ambiguousEvidenceEvaluation()
	set, err := FromAmbiguousEvidence(evaluation, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	updated := evaluation
	updated.PolicyVersion = "evidence-v2"
	updated.Facts = append([]evidence.EvidenceFact(nil), evaluation.Facts...)
	updated.Facts[0].Score++
	proposal, err := ProposeEvidenceRebase(set.Snapshot(), updated, hypothesisTestBase.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.NewAssessment.Version != 2 || proposal.NewAssessment.Provenance.PolicyVersion != "evidence-v2" {
		t.Fatalf("unexpected evidence proposal: %+v", proposal)
	}
	updated.EvidenceFingerprint = "different-fingerprint"
	if _, err := ProposeEvidenceRebase(set.Snapshot(), updated, hypothesisTestBase.Add(time.Minute)); !errors.Is(err, ErrRebaseSubjectMismatch) {
		t.Fatalf("expected subject mismatch, got %v", err)
	}
}
