package hypotheses

import (
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

func TestEvidenceSupersessionCreatesSuccessorAndClosesPredecessor(t *testing.T) {
	firstEvaluation := ambiguousEvidenceEvaluation()
	first, err := FromAmbiguousEvidence(firstEvaluation, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	secondEvaluation := firstEvaluation
	secondEvaluation.EvidenceFingerprint = "different-fingerprint"
	secondEvaluation.PolicyVersion = "evidence-v2"
	proposal, err := ProposeEvidenceSupersession(first.Snapshot(), secondEvaluation, hypothesisTestBase.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.NewSetID == proposal.PreviousSetID || proposal.NewSet.Status != StatusOpen || proposal.NewSet.Lineage.PredecessorSetID != proposal.PreviousSetID || proposal.NewSet.Lineage.Generation != 2 {
		t.Fatalf("invalid supersession proposal: %+v", proposal)
	}
	command, err := proposal.Command(chains.MutationContext{At: hypothesisTestBase.Add(2 * time.Minute), Actor: "reviewer", Reason: "changed evidence subject", CorrelationID: "supersede-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.MarkSuperseded(command.NewSet, command.Mutation); err != nil {
		t.Fatal(err)
	}
	old := first.Snapshot()
	if old.Status != StatusSuperseded || old.Lineage.SuccessorSetID != proposal.NewSetID || old.Revision != 2 || len(old.Assessments) != 1 {
		t.Fatalf("invalid superseded predecessor: %+v", old)
	}
	if command.NewSet.History[0].Actor != command.Mutation.Actor || command.NewSet.History[0].Operation != OperationHypothesisOpened {
		t.Fatal("successor opening provenance was not bound to the mutation")
	}
	if err := old.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSupersessionRequiresNewEvidenceFingerprint(t *testing.T) {
	evaluation := ambiguousEvidenceEvaluation()
	set, err := FromAmbiguousEvidence(evaluation, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProposeEvidenceSupersession(set.Snapshot(), evaluation, hypothesisTestBase.Add(time.Minute)); !errors.Is(err, ErrSupersessionNotRequired) {
		t.Fatalf("expected supersession not required, got %v", err)
	}
	association := rebasePlan("association-v1", 70)
	associationSet, err := FromAmbiguousAssociation(association, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProposeEvidenceSupersession(associationSet.Snapshot(), evaluation, hypothesisTestBase.Add(time.Minute)); !errors.Is(err, ErrSupersessionNotAllowed) {
		t.Fatalf("expected family rejection, got %v", err)
	}
}

func TestEvidenceSupersessionLineageSupportsSuccessiveGenerations(t *testing.T) {
	firstEvaluation := ambiguousEvidenceEvaluation()
	first, err := FromAmbiguousEvidence(firstEvaluation, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Add(first); err != nil {
		t.Fatal(err)
	}
	previous := first.Snapshot()
	for generation, fingerprint := range []string{"fingerprint-two", "fingerprint-three"} {
		evaluation := firstEvaluation
		evaluation.EvidenceFingerprint = fingerprint
		evaluation.PolicyVersion = "evidence-v" + string(rune('2'+generation))
		proposal, err := ProposeEvidenceSupersession(previous, evaluation, hypothesisTestBase.Add(time.Duration(generation+1)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		command, err := proposal.Command(chains.MutationContext{At: hypothesisTestBase.Add(time.Duration(generation+2) * time.Minute), Actor: "reviewer", Reason: "new evidence subject", CorrelationID: "supersede-lineage"})
		if err != nil {
			t.Fatal(err)
		}
		result, err := registry.Supersede(command)
		if err != nil {
			t.Fatal(err)
		}
		if result.PreviousAfter.Status != StatusSuperseded || result.NewAfter.Status != StatusOpen || result.NewAfter.Lineage.Generation != uint64(generation+2) {
			t.Fatalf("invalid generation %d result: %+v", generation+2, result)
		}
		previous = result.NewAfter
	}
	lineage, err := registry.Lineage(previous.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lineage) != 3 || lineage[0].Lineage.Generation != 1 || lineage[1].Lineage.Generation != 2 || lineage[2].Lineage.Generation != 3 {
		t.Fatalf("unexpected lineage: %+v", lineage)
	}
	if lineage[0].Status != StatusSuperseded || lineage[1].Status != StatusSuperseded || lineage[2].Status != StatusOpen {
		t.Fatalf("unexpected lineage statuses: %+v", lineage)
	}
}
