package routines

import (
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

func TestRoutineCreationAggregationLateObservationAndRestore(t *testing.T) {
	first := routineObservation(t, "model-first", routineTestBase, "room", "entity-a", false)
	second := routineObservation(t, "model-second", routineTestBase.Add(2*time.Hour), "room", "entity-a", false)
	third := routineObservation(t, "model-third", routineTestBase.Add(time.Hour), "room", "entity-a", false)
	policy := DefaultExtractionPolicy()
	firstOccurrence, err := ExtractPresenceOccurrence(routineChain(t, "model-chain", first), first.ID, policy)
	if err != nil {
		t.Fatal(err)
	}
	routine, err := NewFromOccurrence(firstOccurrence, chains.MutationContext{At: firstOccurrence.ObservedAt, Actor: "test", Reason: "create routine", CorrelationID: "routine-create"})
	if err != nil {
		t.Fatal(err)
	}
	secondOccurrence, err := ExtractPresenceOccurrence(routineChain(t, "model-chain", second), second.ID, policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := routine.AddOccurrence(AddOccurrenceCommand{RoutineID: routine.id, SourceRevision: routine.revision, Occurrence: secondOccurrence, Mutation: chains.MutationContext{At: secondOccurrence.ObservedAt, Actor: "test", Reason: "add routine occurrence", CorrelationID: "routine-second"}}); err != nil {
		t.Fatal(err)
	}
	thirdOccurrence, err := ExtractPresenceOccurrence(routineChain(t, "model-chain", third), third.ID, policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := routine.AddOccurrence(AddOccurrenceCommand{RoutineID: routine.id, SourceRevision: routine.revision, Occurrence: thirdOccurrence, Mutation: chains.MutationContext{At: secondOccurrence.ObservedAt.Add(time.Second), Actor: "test", Reason: "add late routine occurrence", CorrelationID: "routine-third"}}); err != nil {
		t.Fatal(err)
	}
	if routine.revision != 3 || routine.Snapshot().OccurrenceCount != 3 || routine.intervalStatistics.Minimum != time.Hour || routine.intervalStatistics.Maximum != time.Hour {
		t.Fatalf("wrong aggregation: %#v", routine.Snapshot())
	}
	clone, err := routine.Clone()
	if err != nil {
		t.Fatal(err)
	}
	clone.occurrences[0].ObservationIDs[0] = "changed"
	if routine.occurrences[0].ObservationIDs[0] == "changed" {
		t.Fatal("clone aliases occurrence IDs")
	}
	restored, err := Restore(routine.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatal(err)
	}
	if restored.revision != routine.revision {
		t.Fatal("restore changed revision")
	}
}

func TestRoutineStatusAndDuplicateCollision(t *testing.T) {
	observation := routineObservation(t, "status-observation", routineTestBase, "room", "entity-a", false)
	occurrence, err := ExtractPresenceOccurrence(routineChain(t, "status-chain", observation), observation.ID, DefaultExtractionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	routine, err := NewFromOccurrence(occurrence, chains.MutationContext{At: occurrence.ObservedAt, Actor: "test", Reason: "create", CorrelationID: "create"})
	if err != nil {
		t.Fatal(err)
	}
	duplicate := routine.AddOccurrence(AddOccurrenceCommand{RoutineID: routine.id, SourceRevision: routine.revision, Occurrence: occurrence, Mutation: chains.MutationContext{At: occurrence.ObservedAt.Add(time.Second), Actor: "test", Reason: "duplicate", CorrelationID: "duplicate"}})
	if !errors.Is(duplicate, ErrDuplicateRoutineOccurrence) {
		t.Fatalf("duplicate error=%v", duplicate)
	}
	if err := routine.SetStatus(SetStatusCommand{RoutineID: routine.id, SourceRevision: routine.revision, Target: StatusActive, Mutation: chains.MutationContext{At: occurrence.ObservedAt.Add(time.Second), Actor: "test", Reason: "activate", CorrelationID: "activate"}}); err != nil {
		t.Fatal(err)
	}
	if err := routine.SetStatus(SetStatusCommand{RoutineID: routine.id, SourceRevision: routine.revision, Target: StatusInvalidated, Mutation: chains.MutationContext{At: occurrence.ObservedAt.Add(2 * time.Second), Actor: "test", Reason: "invalidate", CorrelationID: "invalidate"}}); err != nil {
		t.Fatal(err)
	}
	if err := routine.SetStatus(SetStatusCommand{RoutineID: routine.id, SourceRevision: routine.revision, Target: StatusActive, Mutation: chains.MutationContext{At: occurrence.ObservedAt.Add(3 * time.Second), Actor: "test", Reason: "reactivate", CorrelationID: "reactivate"}}); !errors.Is(err, ErrRoutineStatusTransition) {
		t.Fatalf("terminal transition error=%v", err)
	}
}
