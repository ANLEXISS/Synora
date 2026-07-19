package evidence

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

var testBase = time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)

func observation(id, eventType, entity, sequence string, at time.Time) chains.ObservationRef {
	return chains.ObservationRef{
		ID: id, EventType: eventType, EntityID: entity, SequenceKey: sequence,
		ActivationID: "activation-1", TrackID: "track-1", DeviceID: "device-1", NodeID: "node-1", Timestamp: at,
	}
}

func testChain(t *testing.T, id chains.ChainID, entity string, status chains.Status, observations ...chains.ObservationRef) *chains.Chain {
	t.Helper()
	chain, err := chains.New(id, chains.MutationContext{At: testBase, Actor: "test", Reason: "create", CorrelationID: "create"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range observations {
		if err := chain.AddObservation(item, chains.MutationContext{
			At:             item.Timestamp.Add(time.Second),
			Actor:          "test",
			Reason:         "observation",
			CorrelationID:  "observation-" + item.ID,
			ObservationIDs: []string{item.ID},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if entity != "" {
		if err := chain.AssignEntity(entity, chains.MutationContext{At: testBase.Add(20 * time.Minute), Actor: "test", Reason: "assign", CorrelationID: "assign-" + string(id)}); err != nil {
			t.Fatal(err)
		}
	}
	if status != chains.StatusCandidate {
		if err := chain.SetStatus(status, chains.MutationContext{At: testBase.Add(21 * time.Minute), Actor: "test", Reason: "status", CorrelationID: "status-" + string(id)}); err != nil {
			t.Fatal(err)
		}
	}
	return chain
}

func TestDefaultPolicyAndValidation(t *testing.T) {
	if err := DefaultPolicy().Validate(); err != nil {
		t.Fatalf("default policy invalid: %v", err)
	}
	cases := []struct {
		name string
		edit func(*Policy)
	}{
		{"namespace", func(p *Policy) { p.Namespace = "" }},
		{"version newline", func(p *Policy) { p.Version = "evidence-v1\nunsafe" }},
		{"window", func(p *Policy) { p.ContextWindow = 0 }},
		{"context limit", func(p *Policy) { p.MaxContextObservations = 0 }},
		{"negative score", func(p *Policy) { p.SameNodeScore = -1 }},
		{"impossible support threshold", func(p *Policy) { p.MinimumSupportScore = 2701 }},
		{"impossible contradiction threshold", func(p *Policy) { p.MinimumContradictionScore = 876 }},
		{"isolated criterion", func(p *Policy) { p.SameNodeScore = p.MinimumSupportScore }},
		{"negative margin", func(p *Policy) { p.MinimumDecisionMargin = -1 }},
		{"support value", func(p *Policy) { p.SupportValue = 1.1 }},
		{"neutral value", func(p *Policy) { p.NeutralValue = 0.01 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			policy := DefaultPolicy()
			test.edit(&policy)
			if !errors.Is(policy.Validate(), ErrInvalidEvidencePolicy) {
				t.Fatalf("expected invalid policy, got %v", policy.Validate())
			}
		})
	}
}

func TestEvidenceEligibilityAndInputValidation(t *testing.T) {
	for _, status := range []chains.Status{
		chains.StatusCandidate, chains.StatusActive, chains.StatusConfirmed,
		chains.StatusDeclining, chains.StatusDormant, chains.StatusArchived,
		chains.StatusReactivated,
	} {
		if !IsEvidenceEvaluationAllowed(status) {
			t.Fatalf("status %s should be evaluable", status)
		}
	}
	for _, status := range []chains.Status{chains.StatusMerged, chains.StatusSplit, chains.StatusInvalidated} {
		if IsEvidenceEvaluationAllowed(status) {
			t.Fatalf("status %s should not be evaluable", status)
		}
	}

	chain := testChain(t, "chain-input", "", chains.StatusCandidate, observation("target", "vision.identity", "", "", testBase.Add(time.Minute)))
	if _, err := EvaluateObservation(chain.Snapshot(), "missing", testBase.Add(2*time.Minute), DefaultPolicy()); !errors.Is(err, ErrTargetObservationNotFound) {
		t.Fatalf("expected missing target, got %v", err)
	}
	if _, err := EvaluateObservation(chain.Snapshot(), "target", testBase, DefaultPolicy()); !errors.Is(err, ErrInvalidEvaluationTime) {
		t.Fatalf("expected invalid evaluation time, got %v", err)
	}
	unsupported := observation("unsupported", "vision.weapon", "", "", testBase.Add(2*time.Minute))
	unsupportedChain := testChain(t, "chain-unsupported", "", chains.StatusCandidate, unsupported)
	if _, err := EvaluateObservation(unsupportedChain.Snapshot(), unsupported.ID, testBase.Add(3*time.Minute), DefaultPolicy()); !errors.Is(err, ErrUnsupportedObservationType) {
		t.Fatalf("expected unsupported type, got %v", err)
	}
	merged := testChain(t, "chain-merged", "", chains.StatusCandidate, observation("merged-target", "vision.identity", "", "", testBase.Add(time.Minute)))
	// Build a valid replacement snapshot through the domain's explicit status path.
	if err := merged.SetStatus(chains.StatusInvalidated, chains.MutationContext{At: testBase.Add(2 * time.Minute), Actor: "test", Reason: "invalidate", CorrelationID: "invalidate"}); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateObservation(merged.Snapshot(), "merged-target", testBase.Add(3*time.Minute), DefaultPolicy()); !errors.Is(err, ErrEvidenceEvaluationNotAllowed) {
		t.Fatalf("expected forbidden status, got %v", err)
	}
}

func TestSupportEvaluationUsesExplicitFactsAndFixedValue(t *testing.T) {
	target := observation("target", "vision.identity", "person-a", "seq-1", testBase.Add(2*time.Minute))
	context := observation("context", "vision.identity", "person-a", "seq-1", testBase.Add(time.Minute))
	chain := testChain(t, "chain-support", "person-a", chains.StatusActive, context, target)
	policy := DefaultPolicy()
	evaluation, err := EvaluateObservation(chain.Snapshot(), target.ID, testBase.Add(3*time.Minute), policy)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Decision != DecisionProposeSupport {
		t.Fatalf("decision=%s support=%d contradiction=%d", evaluation.Decision, evaluation.SupportScore, evaluation.ContradictionScore)
	}
	if evaluation.SupportScore != 225 || evaluation.ContradictionScore != 0 {
		t.Fatalf("unexpected scores: support=%d contradiction=%d", evaluation.SupportScore, evaluation.ContradictionScore)
	}
	proposal := evaluation.Proposal
	if proposal == nil || proposal.Contribution.Kind != chains.ContributionSupport || proposal.Contribution.Value != policy.SupportValue {
		t.Fatalf("unexpected support proposal: %#v", proposal)
	}
	if evaluation.ResolutionValues.SupportValue != policy.SupportValue || evaluation.ResolutionValues.ContradictionValue != policy.ContradictionValue || evaluation.ResolutionValues.NeutralValue != 0 {
		t.Fatalf("evaluation did not retain fixed resolution values: %+v", evaluation.ResolutionValues)
	}
	if proposal.Contribution.Source != "cge-evidence/evidence-v1" || len(proposal.Contribution.ObservationIDs) != 2 {
		t.Fatalf("unexpected proposal provenance: %#v", proposal.Contribution)
	}
	command, err := proposal.Command(chains.MutationContext{At: testBase.Add(4 * time.Minute), Actor: "test", Reason: "explicit evidence", CorrelationID: "evidence-1", ObservationIDs: proposal.Contribution.ObservationIDs})
	if err != nil {
		t.Fatal(err)
	}
	if command.ChainID != chain.Snapshot().ID || command.SourceRevision != chain.Snapshot().Revision {
		t.Fatalf("unexpected command: %#v", command)
	}

	low := chain.Snapshot()
	low.CurrentConfidence = 0
	high := chain.Snapshot()
	high.CurrentConfidence = 0.9
	high.MaxHistoricalConfidence = 0.9
	lowEvaluation, err := EvaluateObservation(low, target.ID, testBase.Add(3*time.Minute), policy)
	if err != nil {
		t.Fatal(err)
	}
	highEvaluation, err := EvaluateObservation(high, target.ID, testBase.Add(3*time.Minute), policy)
	if err != nil {
		t.Fatal(err)
	}
	if lowEvaluation.SupportScore != highEvaluation.SupportScore || lowEvaluation.ContradictionScore != highEvaluation.ContradictionScore {
		t.Fatal("current confidence influenced evidence score")
	}
}

func TestContradictionUncertainNeutralAndInsufficient(t *testing.T) {
	target := observation("target", "vision.identity", "person-b", "seq-1", testBase.Add(2*time.Minute))
	other := observation("other", "vision.identity", "person-c", "seq-1", testBase.Add(time.Minute))
	other.ActivationID = ""
	other.TrackID = ""
	other.DeviceID = ""
	other.NodeID = ""
	chain := testChain(t, "chain-contradiction", "person-a", chains.StatusActive, other, target)
	result, err := EvaluateObservation(chain.Snapshot(), target.ID, testBase.Add(3*time.Minute), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionProposeContradiction || result.ContradictionScore != 125 {
		t.Fatalf("unexpected contradiction: %#v", result)
	}
	if result.Proposal == nil || result.Proposal.Contribution.Kind != chains.ContributionContradiction || result.Proposal.Contribution.Value != 0.15 {
		t.Fatalf("unexpected contradiction proposal: %#v", result.Proposal)
	}

	unknownTarget := observation("unknown", "vision.unknown", "", "seq-2", testBase.Add(2*time.Minute))
	unknownContext := observation("unknown-context", "vision.unknown", "", "seq-2", testBase.Add(time.Minute))
	unknownTarget.ActivationID, unknownTarget.TrackID, unknownTarget.DeviceID, unknownTarget.NodeID = "", "", "", ""
	unknownContext.ActivationID, unknownContext.TrackID, unknownContext.DeviceID, unknownContext.NodeID = "", "", "", ""
	unknownChain := testChain(t, "chain-neutral", "", chains.StatusCandidate, unknownContext, unknownTarget)
	neutral, err := EvaluateObservation(unknownChain.Snapshot(), unknownTarget.ID, testBase.Add(3*time.Minute), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if neutral.Decision != DecisionProposeNeutral || neutral.Proposal == nil || neutral.Proposal.Contribution.Kind != chains.ContributionNeutral || neutral.Proposal.Contribution.Value != 0 {
		t.Fatalf("unexpected neutral result: %#v", neutral)
	}

	isolated := testChain(t, "chain-insufficient", "", chains.StatusCandidate, observation("isolated", "vision.identity", "", "", testBase.Add(time.Minute)))
	insufficient, err := EvaluateObservation(isolated.Snapshot(), "isolated", testBase.Add(2*time.Minute), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if insufficient.Decision != DecisionInsufficientEvidence || insufficient.Proposal != nil {
		t.Fatalf("unexpected insufficient result: %#v", insufficient)
	}

	uncertainTarget := observation("uncertain", "vision.uncertain", "person-b", "seq-3", testBase.Add(2*time.Minute))
	uncertainContext := observation("uncertain-context", "vision.identity", "person-b", "seq-3", testBase.Add(time.Minute))
	uncertainChain := testChain(t, "chain-uncertain", "person-a", chains.StatusActive, uncertainContext, uncertainTarget)
	uncertain, err := EvaluateObservation(uncertainChain.Snapshot(), uncertainTarget.ID, testBase.Add(3*time.Minute), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if uncertain.Decision == DecisionProposeContradiction {
		t.Fatal("uncertain evidence produced a strong contradiction")
	}
}

func TestAmbiguousMixedContext(t *testing.T) {
	target := observation("target", "vision.identity", "person-b", "seq-ambiguous", testBase.Add(3*time.Minute))
	positive1 := observation("positive-1", "vision.identity", "person-b", "seq-ambiguous", testBase.Add(2*time.Minute))
	positive2 := observation("positive-2", "vision.identity", "person-b", "seq-ambiguous", testBase.Add(4*time.Minute))
	negative1 := observation("negative-1", "vision.identity", "person-c", "seq-ambiguous", testBase.Add(time.Minute))
	negative2 := observation("negative-2", "vision.identity", "person-c", "seq-ambiguous", testBase.Add(5*time.Minute))
	chain := testChain(t, "chain-ambiguous", "", chains.StatusActive, negative1, positive1, target, positive2, negative2)
	result, err := EvaluateObservation(chain.Snapshot(), target.ID, testBase.Add(6*time.Minute), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionAmbiguous || result.Proposal != nil {
		t.Fatalf("expected ambiguity, got %#v", result)
	}
	if len(result.Facts) == 0 || result.SupportScore < DefaultPolicy().MinimumSupportScore || result.ContradictionScore < DefaultPolicy().MinimumContradictionScore {
		t.Fatalf("unexpected mixed scores/facts: support=%d contradiction=%d facts=%d", result.SupportScore, result.ContradictionScore, len(result.Facts))
	}
}

func TestIdempotenceCollisionAndDeterminism(t *testing.T) {
	target := observation("target", "vision.identity", "person-a", "seq-id", testBase.Add(2*time.Minute))
	context := observation("context", "vision.identity", "person-a", "seq-id", testBase.Add(time.Minute))
	chain := testChain(t, "chain-idempotent", "person-a", chains.StatusActive, context, target)
	policy := DefaultPolicy()
	first, err := EvaluateObservation(chain.Snapshot(), target.ID, testBase.Add(3*time.Minute), policy)
	if err != nil || first.Proposal == nil {
		t.Fatalf("first evaluation: %#v %v", first, err)
	}
	second, err := EvaluateObservation(chain.Snapshot(), target.ID, testBase.Add(9*time.Minute), policy)
	if err != nil || !reflect.DeepEqual(first, second) {
		if err != nil {
			t.Fatal(err)
		}
		// EvaluatedAt is intentionally part of the evaluation; all deterministic
		// evidence fields must still match even when the clock changes.
		if first.SupportScore != second.SupportScore || first.Proposal.Contribution.ID != second.Proposal.Contribution.ID {
			t.Fatalf("non-deterministic evaluation: first=%#v second=%#v", first, second)
		}
	}

	withContribution, err := chains.Restore(chain.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := withContribution.AddContribution(first.Proposal.Contribution, chains.MutationContext{At: testBase.Add(30 * time.Minute), Actor: "test", Reason: "explicit", CorrelationID: "explicit", ObservationIDs: first.Proposal.Contribution.ObservationIDs}); err != nil {
		t.Fatal(err)
	}
	idempotent, err := EvaluateObservation(withContribution.Snapshot(), target.ID, testBase.Add(5*time.Minute), policy)
	if err != nil {
		t.Fatal(err)
	}
	if idempotent.Decision != DecisionAlreadyEvaluated || idempotent.Proposal != nil {
		t.Fatalf("expected already evaluated: %#v", idempotent)
	}

	collisionChain, err := chains.Restore(chain.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	collision := first.Proposal.Contribution.Clone()
	collision.Kind = chains.ContributionNeutral
	collision.Value = 0
	if err := collisionChain.AddContribution(collision, chains.MutationContext{At: testBase.Add(30 * time.Minute), Actor: "test", Reason: "explicit", CorrelationID: "collision", ObservationIDs: collision.ObservationIDs}); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateObservation(collisionChain.Snapshot(), target.ID, testBase.Add(5*time.Minute), policy); !errors.Is(err, ErrEvidenceContributionCollision) {
		t.Fatalf("expected contribution collision, got %v", err)
	}
}

func TestDefensiveFactsAndConcurrentPureEvaluation(t *testing.T) {
	target := observation("target", "vision.identity", "person-a", "seq-def", testBase.Add(2*time.Minute))
	context := observation("context", "vision.identity", "person-a", "seq-def", testBase.Add(time.Minute))
	chain := testChain(t, "chain-defensive", "person-a", chains.StatusActive, context, target)
	snapshot := chain.Snapshot()
	baseline, err := EvaluateObservation(snapshot, target.ID, testBase.Add(3*time.Minute), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	baseline.ContextObservationIDs[0] = "mutated"
	for i := range baseline.Facts {
		if len(baseline.Facts[i].ObservationIDs) > 0 {
			baseline.Facts[i].ObservationIDs[0] = "mutated"
			break
		}
	}
	baseline.Proposal.Contribution.ObservationIDs[0] = "mutated"
	recheck, err := EvaluateObservation(snapshot, target.ID, testBase.Add(3*time.Minute), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if recheck.ContextObservationIDs[0] == "mutated" || recheck.Proposal.Contribution.ObservationIDs[0] == "mutated" {
		t.Fatal("evaluation returned shared mutable data")
	}
	for _, fact := range recheck.Facts {
		for _, id := range fact.ObservationIDs {
			if id == "mutated" {
				t.Fatal("evaluation returned shared fact data")
			}
		}
	}

	const workers = 16
	results := make([]EvidenceEvaluation, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wait.Done()
			results[index], _ = EvaluateObservation(snapshot, target.ID, testBase.Add(3*time.Minute), DefaultPolicy())
		}(i)
	}
	wait.Wait()
	for i := 1; i < workers; i++ {
		if !reflect.DeepEqual(results[0], results[i]) {
			t.Fatalf("concurrent evaluation differed at %d", i)
		}
	}
}

func TestExplicitProposalChangesConfidenceOnlyWhenCallerAppliesIt(t *testing.T) {
	target := observation("target", "vision.identity", "person-a", "seq-explicit", testBase.Add(2*time.Minute))
	context := observation("context", "vision.identity", "person-a", "seq-explicit", testBase.Add(time.Minute))
	chain := testChain(t, "chain-explicit", "person-a", chains.StatusActive, context, target)
	before := chain.Snapshot()
	evaluation, err := EvaluateObservation(before, target.ID, testBase.Add(3*time.Minute), DefaultPolicy())
	if err != nil || evaluation.Proposal == nil {
		t.Fatal(err)
	}
	if chain.Snapshot().Revision != before.Revision || chain.Snapshot().CurrentConfidence != before.CurrentConfidence {
		t.Fatal("pure evaluation mutated the chain")
	}
	if err := chain.AddContribution(evaluation.Proposal.Contribution, chains.MutationContext{At: testBase.Add(30 * time.Minute), Actor: "test", Reason: "explicit evidence", CorrelationID: "apply", ObservationIDs: evaluation.Proposal.Contribution.ObservationIDs}); err != nil {
		t.Fatal(err)
	}
	after := chain.Snapshot()
	if after.Status != before.Status || after.HistoricalReliability != before.HistoricalReliability || after.CurrentConfidence != before.CurrentConfidence+DefaultPolicy().SupportValue {
		t.Fatalf("unexpected explicit contribution effects: before=%#v after=%#v", before, after)
	}
}

func TestEvaluationVolume(t *testing.T) {
	for i := 0; i < 300; i++ {
		target := observation(fmt.Sprintf("target-%03d", i), "vision.unknown", "", fmt.Sprintf("sequence-%03d", i), testBase.Add(2*time.Minute))
		context := observation(fmt.Sprintf("context-%03d", i), "vision.unknown", "", fmt.Sprintf("sequence-%03d", i), testBase.Add(time.Minute))
		target.ActivationID, target.TrackID, target.DeviceID, target.NodeID = "", "", "", ""
		context.ActivationID, context.TrackID, context.DeviceID, context.NodeID = "", "", "", ""
		chain := testChain(t, chains.ChainID(fmt.Sprintf("chain-%03d", i)), "", chains.StatusCandidate, context, target)
		result, err := EvaluateObservation(chain.Snapshot(), target.ID, testBase.Add(3*time.Minute), DefaultPolicy())
		if err != nil || result.Decision != DecisionProposeNeutral {
			t.Fatalf("volume item %d: decision=%s err=%v", i, result.Decision, err)
		}
	}
}
