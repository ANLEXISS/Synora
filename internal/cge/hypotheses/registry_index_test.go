package hypotheses

import (
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/evidence"
)

func TestDerivedSubjectIndexesFollowRebaseSupersessionStatusAndClone(t *testing.T) {
	evaluation := ambiguousEvidenceEvaluation()
	set, err := FromAmbiguousEvidence(evaluation, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Add(set); err != nil {
		t.Fatal(err)
	}
	if current, found, err := registry.FindCurrentEvidenceSubject(evaluation.ChainID, evaluation.TargetObservationID); err != nil || !found || current.ID != set.ID() {
		t.Fatalf("initial index lookup: %#v %v", current, err)
	}

	updated := evaluation
	updated.PolicyVersion = "evidence-v2"
	updated.Facts = append([]evidence.EvidenceFact(nil), evaluation.Facts...)
	updated.Facts[0].Score++
	proposal, err := ProposeEvidenceRebase(set.Snapshot(), updated, hypothesisTestBase.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	command, err := proposal.Command(chains.MutationContext{At: hypothesisTestBase.Add(2 * time.Minute), Actor: "test", Reason: "rebase", CorrelationID: "index-rebase"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Rebase(command); err != nil {
		t.Fatal(err)
	}
	if open, err := registry.ListOpenEvidenceForChain(evaluation.ChainID); err != nil || len(open) != 1 || open[0].Revision != 2 {
		t.Fatalf("rebase index: %#v %v", open, err)
	}

	changed := updated
	changed.EvidenceFingerprint = "different-fingerprint"
	changed.PolicyVersion = "evidence-v3"
	currentBeforeSupersession, found, err := registry.FindCurrentEvidenceSubject(evaluation.ChainID, evaluation.TargetObservationID)
	if err != nil || !found {
		t.Fatalf("current before supersession: %#v %v", currentBeforeSupersession, err)
	}
	supersession, err := ProposeEvidenceSupersession(currentBeforeSupersession, changed, hypothesisTestBase.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	supersedeCommand, err := supersession.Command(chains.MutationContext{At: hypothesisTestBase.Add(4 * time.Minute), Actor: "test", Reason: "supersede", CorrelationID: "index-supersede"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Supersede(supersedeCommand); err != nil {
		t.Fatal(err)
	}
	current, found, err := registry.FindCurrentEvidenceSubject(evaluation.ChainID, evaluation.TargetObservationID)
	if err != nil || !found || current.ID != supersession.NewSetID || current.Status != StatusOpen {
		t.Fatalf("successor index: %#v %v", current, err)
	}
	if open, err := registry.ListOpenEvidenceForChain(evaluation.ChainID); err != nil || len(open) != 1 || open[0].ID != supersession.NewSetID {
		t.Fatalf("successor open index: %#v %v", open, err)
	}

	clone, err := registry.CloneShallow()
	if err != nil {
		t.Fatal(err)
	}
	if cloned, found, err := clone.FindCurrentEvidenceSubject(evaluation.ChainID, evaluation.TargetObservationID); err != nil || !found || cloned.ID != current.ID {
		t.Fatalf("clone index: %#v %v", cloned, err)
	}
}
