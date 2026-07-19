package routines

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

func occurrenceForRegistry(t testing.TB, id string, at time.Time, chainID, entity string) Occurrence {
	t.Helper()
	observation := routineObservation(t, id, at, "room", entity, false)
	occurrence, err := ExtractPresenceOccurrence(routineChain(t, chainID, observation), id, DefaultExtractionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return occurrence
}

func TestRegistryApplyOccurrenceIndexesAndIdempotence(t *testing.T) {
	registry := NewRegistry()
	first := occurrenceForRegistry(t, "registry-first", routineTestBase, "registry-chain", "entity-a")
	created, err := registry.ApplyOccurrence(first, chains.MutationContext{At: first.ObservedAt, Actor: "test", Reason: "learn first", CorrelationID: "learn-first"})
	if err != nil || !created.Applied || !created.Created {
		t.Fatalf("create result=%#v err=%v", created, err)
	}
	second := occurrenceForRegistry(t, "registry-second", routineTestBase.Add(time.Hour), "registry-chain-2", "entity-a")
	added, err := registry.ApplyOccurrence(second, chains.MutationContext{At: second.ObservedAt, Actor: "test", Reason: "learn second", CorrelationID: "learn-second"})
	if err != nil || !added.Applied || added.Created || added.Snapshot.OccurrenceCount != 2 {
		t.Fatalf("add result=%#v err=%v", added, err)
	}
	idempotent, err := registry.ApplyOccurrence(second, chains.MutationContext{At: second.ObservedAt.Add(time.Second), Actor: "test", Reason: "repeat second", CorrelationID: "repeat-second"})
	if err != nil || !idempotent.Idempotent || idempotent.Snapshot.Revision != added.Snapshot.Revision {
		t.Fatalf("idempotent result=%#v err=%v", idempotent, err)
	}
	bySubject, err := registry.ListBySubject(second.Subject)
	if err != nil || len(bySubject) != 1 {
		t.Fatalf("subject index=%#v err=%v", bySubject, err)
	}
	byKind, err := registry.ListBySubjectAndKind(second.Subject, KindPresence)
	if err != nil || len(byKind) != 1 {
		t.Fatalf("kind index=%#v err=%v", byKind, err)
	}
	if registry.Count() != 1 || registry.Validate() != nil {
		t.Fatalf("invalid registry: %#v", registry.List())
	}
	clone, err := registry.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if clone.Count() != registry.Count() {
		t.Fatal("clone count mismatch")
	}
}

func TestRegistryLearningPlanAndConcurrentSameOccurrence(t *testing.T) {
	observation := routineObservation(t, "plan-observation", routineTestBase, "room", "entity-a", false)
	chain := routineChain(t, "plan-chain", observation)
	plan, err := PlanLearning(chain, observation.ID, routineTopology(), routineTestBase.Add(time.Hour), DefaultExtractionPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Occurrences) != 1 || plan.Occurrences[0].Kind != KindPresence {
		t.Fatalf("plan=%#v", plan)
	}
	registry := NewRegistry()
	const workers = 8
	results := make(chan LearningApplyResult, workers)
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() { defer wait.Done(); results <- registry.ApplyLearningPlan(plan, "test", "parallel-plan") }()
	}
	wait.Wait()
	close(results)
	applied, idempotent := 0, 0
	for result := range results {
		applied += result.AppliedCount
		idempotent += result.IdempotentCount
		if result.ErrorCount != 0 {
			t.Fatalf("parallel apply error: %#v", result)
		}
	}
	if applied != 1 || idempotent != workers-1 {
		t.Fatalf("parallel counts applied=%d idempotent=%d", applied, idempotent)
	}
	if registry.Count() != 1 {
		t.Fatalf("routine count=%d", registry.Count())
	}
}

func TestRegistryDistinctUnknownChainsDoNotMerge(t *testing.T) {
	registry := NewRegistry()
	first := occurrenceForRegistry(t, "unknown-one", routineTestBase, "unknown-chain-one", "")
	second := occurrenceForRegistry(t, "unknown-two", routineTestBase.Add(time.Hour), "unknown-chain-two", "")
	if _, err := registry.ApplyOccurrence(first, chains.MutationContext{At: first.ObservedAt, Actor: "test", Reason: "unknown one", CorrelationID: "unknown-one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ApplyOccurrence(second, chains.MutationContext{At: second.ObservedAt, Actor: "test", Reason: "unknown two", CorrelationID: "unknown-two"}); err != nil {
		t.Fatal(err)
	}
	if registry.Count() != 2 {
		t.Fatalf("unknown subjects were merged: %d", registry.Count())
	}
}

func TestRegistryVolumeAndDeterministicClone(t *testing.T) {
	registry := NewRegistry()
	for i := 0; i < 1000; i++ {
		chainID := fmt.Sprintf("volume-chain-%04d", i)
		first := occurrenceForRegistry(t, fmt.Sprintf("volume-%04d-1", i), routineTestBase.Add(time.Duration(i)*time.Minute), chainID, "")
		second := occurrenceForRegistry(t, fmt.Sprintf("volume-%04d-2", i), routineTestBase.Add(time.Duration(i)*time.Minute+time.Minute), chainID, "")
		third := occurrenceForRegistry(t, fmt.Sprintf("volume-%04d-3", i), routineTestBase.Add(time.Duration(i)*time.Minute+2*time.Minute), chainID, "")
		for _, occurrence := range []Occurrence{first, second, third} {
			if _, err := registry.ApplyOccurrence(occurrence, chains.MutationContext{At: occurrence.ObservedAt, Actor: "volume", Reason: "volume occurrence", CorrelationID: string(occurrence.ID)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if registry.Count() != 1000 {
		t.Fatalf("routine volume=%d", registry.Count())
	}
	if err := registry.Validate(); err != nil {
		t.Fatal(err)
	}
	clone, err := registry.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if clone.Count() != registry.Count() || len(clone.List()) != 1000 {
		t.Fatalf("clone volume=%d", clone.Count())
	}
}
