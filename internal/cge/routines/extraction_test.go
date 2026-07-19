package routines

import (
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
	cgecontext "synora/internal/cge/context"
)

var routineTestBase = time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)

func routineTopology() cgecontext.TopologySnapshot {
	return cgecontext.TopologySnapshot{Revision: "routine-topology-1", CapturedAt: routineTestBase, Nodes: []cgecontext.Node{{ID: "corridor", ZoneID: "ground", Kind: cgecontext.NodeCorridor}, {ID: "entry", Kind: cgecontext.NodeEntrance, EntryPoint: true}, {ID: "room", ZoneID: "ground", Kind: cgecontext.NodeRoom}}, Edges: []cgecontext.Edge{{From: "corridor", To: "entry", TraversalKind: cgecontext.TraversalDoor}, {From: "corridor", To: "room", TraversalKind: cgecontext.TraversalWalk}}}
}

func routineObservation(t testing.TB, id string, at time.Time, node, entity string, allowPartial bool) chains.ObservationRef {
	t.Helper()
	frame, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: id, ObservedAt: at, NodeID: node, Timezone: "UTC", Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome, Topology: routineTopology(), AllowPartial: allowPartial})
	if err != nil {
		t.Fatal(err)
	}
	return chains.ObservationRef{ID: id, EventType: "vision.identity", Timestamp: at, NodeID: node, EntityID: entity, Context: &frame}
}

func routineChain(t testing.TB, id string, observations ...chains.ObservationRef) chains.Snapshot {
	t.Helper()
	chain, err := chains.New(chains.ChainID(id), chains.MutationContext{At: routineTestBase, Actor: "test", Reason: "create chain", CorrelationID: "create-" + id})
	if err != nil {
		t.Fatal(err)
	}
	for _, observation := range observations {
		if err := chain.AddObservation(observation, chains.MutationContext{At: observation.Timestamp, Actor: "test", Reason: "add observation", CorrelationID: "add-" + observation.ID}); err != nil {
			t.Fatal(err)
		}
	}
	return chain.Snapshot()
}

func TestExtractPresenceKeepsTemporalDimensionsOutOfIdentity(t *testing.T) {
	policy := DefaultExtractionPolicy()
	first := routineObservation(t, "presence-1", routineTestBase, "room", "entity-a", false)
	second := routineObservation(t, "presence-2", routineTestBase.Add(24*time.Hour), "room", "entity-a", false)
	firstOccurrence, err := ExtractPresenceOccurrence(routineChain(t, "chain-presence-1", first), first.ID, policy)
	if err != nil {
		t.Fatal(err)
	}
	secondOccurrence, err := ExtractPresenceOccurrence(routineChain(t, "chain-presence-2", second), second.ID, policy)
	if err != nil {
		t.Fatal(err)
	}
	if firstOccurrence.RoutineID != secondOccurrence.RoutineID {
		t.Fatalf("temporal dimensions fragmented routine: %s != %s", firstOccurrence.RoutineID, secondOccurrence.RoutineID)
	}
	if firstOccurrence.ID == secondOccurrence.ID {
		t.Fatal("different observations share occurrence ID")
	}
	if firstOccurrence.Subject.Kind != SubjectEntity || firstOccurrence.Subject.EntityID != "entity-a" || firstOccurrence.Subject.ChainID != "" {
		t.Fatalf("unexpected entity subject: %#v", firstOccurrence.Subject)
	}
}

func TestExtractUnknownPresenceUsesChainSubject(t *testing.T) {
	observation := routineObservation(t, "unknown-1", routineTestBase, "room", "", false)
	occurrence, err := ExtractPresenceOccurrence(routineChain(t, "chain-unknown", observation), observation.ID, DefaultExtractionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if occurrence.Subject.Kind != SubjectChain || occurrence.Subject.ChainID != "chain-unknown" || occurrence.Subject.EntityID != "" {
		t.Fatalf("unexpected unknown subject: %#v", occurrence.Subject)
	}
}

func TestExtractTransitionAndDeterministicPreviousSelection(t *testing.T) {
	previous := routineObservation(t, "transition-previous", routineTestBase, "entry", "entity-a", false)
	tie := routineObservation(t, "transition-tie", routineTestBase.Add(30*time.Second), "corridor", "entity-a", false)
	target := routineObservation(t, "transition-target", routineTestBase.Add(time.Minute), "room", "entity-a", false)
	occurrence, err := ExtractTransitionOccurrence(routineChain(t, "chain-transition", previous, tie, target), target.ID, routineTopology(), DefaultExtractionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if occurrence.Kind != KindTransition || len(occurrence.ObservationIDs) != 2 || occurrence.ObservationIDs[0] != tie.ID || occurrence.ObservationIDs[1] != target.ID {
		t.Fatalf("wrong causal transition: %#v", occurrence)
	}
	if occurrence.Pattern.Transition == nil || !occurrence.Pattern.Transition.GraphDistanceKnown || occurrence.Pattern.Transition.GraphDistance != 1 {
		t.Fatalf("wrong transition pattern: %#v", occurrence.Pattern)
	}
}

func TestExtractionSkipsWithoutFalseRoutine(t *testing.T) {
	partial := routineObservation(t, "partial", routineTestBase, "missing", "entity-a", true)
	policy := DefaultExtractionPolicy()
	policy.AllowPartialContext = false
	_, err := ExtractPresenceOccurrence(routineChain(t, "chain-partial", partial), partial.ID, policy)
	var notApplicable NotApplicableError
	if !errors.As(err, &notApplicable) || notApplicable.Code != SkipPartialDisallowed {
		t.Fatalf("partial context error = %v", err)
	}
	plan, err := PlanLearning(routineChain(t, "chain-partial-plan", partial), partial.ID, routineTopology(), routineTestBase.Add(time.Hour), policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Occurrences) != 0 || len(plan.Skipped) != 2 {
		t.Fatalf("unexpected skipped plan: %#v", plan)
	}
}

func TestTransitionPolicySkipsTopologyAndGap(t *testing.T) {
	previous := routineObservation(t, "gap-previous", routineTestBase, "entry", "entity-a", false)
	target := routineObservation(t, "gap-target", routineTestBase.Add(20*time.Minute), "room", "entity-a", false)
	policy := DefaultExtractionPolicy()
	_, err := ExtractTransitionOccurrence(routineChain(t, "chain-gap", previous, target), target.ID, routineTopology(), policy)
	var notApplicable NotApplicableError
	if !errors.As(err, &notApplicable) || notApplicable.Code != SkipTransitionGapExceeded {
		t.Fatalf("gap error = %v", err)
	}
	shortTarget := routineObservation(t, "no-topology-target", routineTestBase.Add(time.Minute), "room", "entity-a", false)
	_, err = ExtractTransitionOccurrence(routineChain(t, "chain-no-topology", previous, shortTarget), shortTarget.ID, cgecontext.TopologySnapshot{}, DefaultExtractionPolicy())
	if !errors.As(err, &notApplicable) || notApplicable.Code != SkipTopologyMissing {
		t.Fatalf("topology error = %v", err)
	}
}

func TestTransitionPolicySkipsTopologyRevisionMismatch(t *testing.T) {
	previous := routineObservation(t, "revision-previous", routineTestBase, "entry", "entity-a", false)
	target := routineObservation(t, "revision-target", routineTestBase.Add(time.Minute), "room", "entity-a", false)
	target.Context.TopologyRevision = "routine-topology-2"
	// Recompute through the public resolver so the changed frame remains valid.
	resolved, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: target.ID, ObservedAt: target.Timestamp, NodeID: target.NodeID, Timezone: "UTC", Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome, Topology: cgecontext.TopologySnapshot{Revision: "routine-topology-2", CapturedAt: routineTestBase, Nodes: routineTopology().Nodes, Edges: routineTopology().Edges}})
	if err != nil {
		t.Fatal(err)
	}
	target.Context = &resolved
	_, err = ExtractTransitionOccurrence(routineChain(t, "chain-revision-mismatch", previous, target), target.ID, routineTopology(), DefaultExtractionPolicy())
	var notApplicable NotApplicableError
	if !errors.As(err, &notApplicable) || notApplicable.Code != SkipTopologyRevisionMismatch {
		t.Fatalf("revision mismatch error = %v", err)
	}
}
