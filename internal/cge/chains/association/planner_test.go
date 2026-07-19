package association

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

var associationTestBase = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func associationMutation(at time.Time, id string) chains.MutationContext {
	return chains.MutationContext{At: at, Actor: "association-test", Reason: "association test mutation", CorrelationID: id}
}

func associationChain(t *testing.T, id string, status chains.Status, observation chains.ObservationRef) chains.Snapshot {
	t.Helper()
	chain, err := chains.New(chains.ChainID(id), associationMutation(associationTestBase, "create-"+id))
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	if observation.ID != "" {
		if err := chain.AddObservation(observation, associationMutation(observation.Timestamp.Add(time.Second), "observation-"+id)); err != nil {
			t.Fatalf("add observation: %v", err)
		}
	}
	if status != chains.StatusCandidate {
		at := associationTestBase.Add(10 * time.Minute)
		if observation.ID != "" && !observation.Timestamp.Add(time.Second).Before(at) {
			at = observation.Timestamp.Add(2 * time.Second)
		}
		if err := chain.SetStatus(status, associationMutation(at, "status-"+id)); err != nil {
			t.Fatalf("set status %s: %v", status, err)
		}
	}
	return chain.Snapshot()
}

func associationObservation(id, entity, node string, at time.Time) chains.ObservationRef {
	return chains.ObservationRef{ID: id, EventType: "vision.motion", Timestamp: at, EntityID: entity, NodeID: node, DeviceID: "device-1", ActivationID: "activation-1", TrackID: "track-1", SequenceKey: "sequence-1"}
}

func TestPolicyValidationRejectsUnsafeConfigurations(t *testing.T) {
	policy := DefaultPolicy()
	policy.Version = ""
	if err := policy.Validate(); err == nil || !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("empty policy version accepted: %v", err)
	}
	policy = DefaultPolicy()
	policy.MaxForwardGap = 0
	if err := policy.Validate(); err == nil || !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("zero forward window accepted: %v", err)
	}
	policy = DefaultPolicy()
	policy.SameEntityScore = policy.MinimumAttachScore
	if err := policy.Validate(); err == nil || !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("single criterion crossing threshold accepted: %v", err)
	}
	policy = DefaultPolicy()
	policy.MinimumAttachScore = 1000
	if err := policy.Validate(); err == nil || !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("unreachable threshold accepted: %v", err)
	}
}

func TestAssociationEligibilityMatchesExplicitLifecyclePolicy(t *testing.T) {
	for _, status := range []chains.Status{chains.StatusCandidate, chains.StatusActive, chains.StatusConfirmed, chains.StatusDeclining, chains.StatusReactivated} {
		if !IsAssociationEligible(status) {
			t.Fatalf("status %s was unexpectedly excluded", status)
		}
	}
	for _, status := range []chains.Status{chains.StatusDormant, chains.StatusArchived, chains.StatusMerged, chains.StatusSplit, chains.StatusInvalidated} {
		if IsAssociationEligible(status) {
			t.Fatalf("status %s was unexpectedly eligible", status)
		}
	}
}

func TestPlanAssociationAttachesDeterministicallyWithExplainableScore(t *testing.T) {
	observation := associationObservation("existing", "entity-a", "entry", associationTestBase)
	snapshot := associationChain(t, "chain-a", chains.StatusActive, observation)
	input := Input{Observation: associationObservation("new", "entity-a", "entry", associationTestBase.Add(10*time.Second))}
	policy := DefaultPolicy()
	before := snapshot
	first, err := PlanAssociation([]chains.Snapshot{snapshot}, input, associationTestBase.Add(time.Hour), policy)
	if err != nil {
		t.Fatalf("plan association: %v", err)
	}
	second, err := PlanAssociation([]chains.Snapshot{snapshot}, input, associationTestBase.Add(time.Hour), policy)
	if err != nil {
		t.Fatalf("repeat plan association: %v", err)
	}
	if first.Decision != DecisionAttachExisting || first.SelectedChainID != "chain-a" || first.ScoreMargin < policy.MinimumScoreMargin || !reflect.DeepEqual(first, second) {
		t.Fatalf("non-deterministic or unexpected plan: first=%#v second=%#v", first, second)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("invalid plan: %v", err)
	}
	if !reflect.DeepEqual(snapshot, before) {
		t.Fatalf("planner mutated input snapshot")
	}
	if len(first.RankedCandidates[0].Facts) < 4 || first.RankedCandidates[0].Score <= 0 {
		t.Fatalf("score explanation is incomplete: %#v", first.RankedCandidates[0])
	}
	first.RankedCandidates[0].Facts[0].Detail = "changed"
	third, err := PlanAssociation([]chains.Snapshot{snapshot}, input, associationTestBase.Add(time.Hour), policy)
	if err != nil || third.RankedCandidates[0].Facts[0].Detail == "changed" {
		t.Fatalf("planner result was not defensive: plan=%#v err=%v", third, err)
	}
}

func TestPlanAssociationRejectsStrongConflictsAndAcceptsLateWithinWindow(t *testing.T) {
	snapshot := associationChain(t, "chain-a", chains.StatusActive, associationObservation("existing", "entity-a", "entry", associationTestBase.Add(time.Minute)))
	policy := DefaultPolicy()
	conflict, err := PlanAssociation([]chains.Snapshot{snapshot}, Input{Observation: associationObservation("conflict", "entity-b", "entry", associationTestBase.Add(2*time.Minute))}, associationTestBase.Add(time.Hour), policy)
	if err != nil || conflict.Decision != DecisionCreateCandidate || len(conflict.RankedCandidates) != 1 || conflict.RankedCandidates[0].RejectionCode != "entity.conflict" {
		t.Fatalf("entity conflict was not excluded: plan=%#v err=%v", conflict, err)
	}
	late, err := PlanAssociation([]chains.Snapshot{snapshot}, Input{Observation: associationObservation("late", "entity-a", "entry", associationTestBase.Add(30*time.Second))}, associationTestBase.Add(time.Hour), policy)
	if err != nil || !late.RankedCandidates[0].Eligible {
		t.Fatalf("valid late observation was excluded: plan=%#v err=%v", late, err)
	}
	tooLate, err := PlanAssociation([]chains.Snapshot{snapshot}, Input{Observation: associationObservation("too-late", "entity-a", "entry", associationTestBase.Add(-3*time.Minute))}, associationTestBase.Add(time.Hour), policy)
	if err != nil || tooLate.RankedCandidates[0].Eligible || tooLate.RankedCandidates[0].RejectionCode != "time.out_of_window" {
		t.Fatalf("late observation outside window was accepted: plan=%#v err=%v", tooLate, err)
	}
}

func TestPlanAssociationAmbiguityAndIdempotence(t *testing.T) {
	observation := associationObservation("existing-a", "entity-a", "entry", associationTestBase)
	left := associationChain(t, "chain-a", chains.StatusActive, observation)
	right := associationChain(t, "chain-b", chains.StatusActive, observation)
	// Use a new ID for scoring so the duplicate-attachment check does not win.
	input := Input{Observation: associationObservation("new", "entity-a", "entry", associationTestBase.Add(10*time.Second))}
	plan, err := PlanAssociation([]chains.Snapshot{left, right}, input, associationTestBase.Add(time.Hour), DefaultPolicy())
	if err != nil || plan.Decision != DecisionAmbiguous || len(plan.RankedCandidates) != 2 {
		t.Fatalf("equal candidates were not left ambiguous: plan=%#v err=%v", plan, err)
	}
	attached, err := PlanAssociation([]chains.Snapshot{left}, Input{Observation: observation}, associationTestBase.Add(time.Hour), DefaultPolicy())
	if err != nil || attached.Decision != DecisionAlreadyAttached || attached.SelectedChainID != left.ID {
		t.Fatalf("already attached observation was not idempotent: plan=%#v err=%v", attached, err)
	}
	if _, err := PlanAssociation([]chains.Snapshot{left, right}, Input{Observation: observation}, associationTestBase.Add(time.Hour), DefaultPolicy()); err == nil || !errors.Is(err, ErrObservationMultipleAttachments) {
		t.Fatalf("multiple attachment was not rejected: %v", err)
	}
}

func TestCandidateChainIDIsDeterministicAndOpaque(t *testing.T) {
	input := Input{Observation: associationObservation("observation-id", "entity-a", "entry", associationTestBase)}
	policy := DefaultPolicy()
	left, err := DeriveCandidateChainID(input, policy)
	if err != nil {
		t.Fatalf("derive candidate id: %v", err)
	}
	right, err := DeriveCandidateChainID(input, policy)
	if err != nil || left != right || left == "" || len(left) != len("cge-")+64 || string(left) == "observation-id" {
		t.Fatalf("candidate id is not deterministic and opaque: left=%q right=%q err=%v", left, right, err)
	}
}
