package hypotheses

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

var hypothesisTestBase = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func testAssociationSet(t *testing.T, observationID, policy string) *HypothesisSet {
	t.Helper()
	setID, err := DeriveAssociationSetID(observationID, policy)
	if err != nil {
		t.Fatal(err)
	}
	alternatives := []Alternative{
		{Kind: AlternativeAttachExisting, ChainID: "chain-a", SourceRevision: 3, Score: 80, Rank: 1, ReasonCode: "candidate.a", Facts: []FactReference{{Code: "same.entity", Side: "support", Score: 80, ObservationIDs: []string{observationID}}}},
		{Kind: AlternativeAttachExisting, ChainID: "chain-b", SourceRevision: 4, Score: 70, Rank: 2, ReasonCode: "candidate.b", Facts: []FactReference{{Code: "same.sequence", Side: "support", Score: 70, ObservationIDs: []string{observationID}}}},
	}
	for i := range alternatives {
		alternatives[i].ID = deriveAlternativeID(setID, alternatives[i])
	}
	return mustOpen(t, setID, FamilyAssociation, Subject{ObservationID: observationID}, alternatives, Provenance{Source: "association", PolicyNamespace: associationNamespace, PolicyVersion: policy, PlannedOrEvaluatedAt: hypothesisTestBase}, "association.ambiguous", "two plausible chains")
}

func mustOpen(t *testing.T, id SetID, family Family, subject Subject, alternatives []Alternative, provenance Provenance, reasonCode, reason string) *HypothesisSet {
	t.Helper()
	set, err := openHypothesis(id, family, subject, alternatives, provenance, reasonCode, reason, hypothesisTestBase, chains.MutationContext{At: hypothesisTestBase, Actor: "test", Reason: "open hypothesis", CorrelationID: "open-1"})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestHypothesisStatusMachineAndHistory(t *testing.T) {
	if CanTransition(StatusOpen, StatusResolved) || !CanTransition(StatusOpen, StatusSuperseded) || !CanTransition(StatusUnderReview, StatusSuperseded) || CanTransition(StatusInvalidated, StatusOpen) {
		t.Fatal("hypothesis status transition machine is invalid")
	}
	set := testAssociationSet(t, "obs-1", "association-v1")
	if set.Revision() != 1 || set.Status() != StatusOpen {
		t.Fatalf("unexpected initial state: rev=%d status=%s", set.Revision(), set.Status())
	}
	if err := set.SetStatus(StatusUnderReview, chains.MutationContext{At: hypothesisTestBase.Add(time.Minute), Actor: "reviewer", Reason: "inspect", CorrelationID: "review-1"}); err != nil {
		t.Fatal(err)
	}
	if err := set.SetStatus(StatusOpen, chains.MutationContext{At: hypothesisTestBase.Add(2 * time.Minute), Actor: "reviewer", Reason: "reopen", CorrelationID: "review-2"}); err != nil {
		t.Fatal(err)
	}
	if err := set.SetStatus(StatusInvalidated, chains.MutationContext{At: hypothesisTestBase.Add(3 * time.Minute), Actor: "reviewer", Reason: "invalidate", CorrelationID: "review-3"}); err != nil {
		t.Fatal(err)
	}
	if set.Revision() != 4 || set.Status() != StatusInvalidated {
		t.Fatalf("unexpected final state: rev=%d status=%s", set.Revision(), set.Status())
	}
	if err := set.SetStatus(StatusOpen, chains.MutationContext{At: hypothesisTestBase.Add(4 * time.Minute), Actor: "reviewer", Reason: "illegal", CorrelationID: "review-4"}); !errors.Is(err, ErrInvalidHypothesisTransition) {
		t.Fatalf("expected transition error, got %v", err)
	}
	if err := set.SetStatus(StatusSuperseded, chains.MutationContext{At: hypothesisTestBase.Add(4 * time.Minute), Actor: "reviewer", Reason: "illegal", CorrelationID: "review-5"}); !errors.Is(err, ErrSupersessionNotAllowed) {
		t.Fatalf("expected explicit supersession error, got %v", err)
	}
	if set.Revision() != 4 {
		t.Fatal("rejected transition changed revision")
	}
	if err := set.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestHypothesisCloneAndRestoreAreDefensive(t *testing.T) {
	set := testAssociationSet(t, "obs-clone", "association-v1")
	snapshot := set.Snapshot()
	snapshot.Alternatives[0].Facts[0].ObservationIDs[0] = "changed"
	snapshot.History[0].Reason = "changed"
	owned, err := set.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if owned.Snapshot().Alternatives[0].Facts[0].ObservationIDs[0] == "changed" || owned.Snapshot().History[0].Reason == "changed" {
		t.Fatal("snapshot mutation leaked into aggregate")
	}
	restored, err := Restore(set.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if restored.ID() != set.ID() || restored.Revision() != set.Revision() {
		t.Fatal("restore changed identity or revision")
	}
}

func TestHypothesisRegistryOwnsAndOrdersSets(t *testing.T) {
	registry := NewRegistry()
	first := testAssociationSet(t, "obs-z", "association-v1")
	second := testAssociationSet(t, "obs-a", "association-v1")
	if err := registry.Add(first); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(second); err != nil {
		t.Fatal(err)
	}
	if registry.Count() != 2 {
		t.Fatalf("count=%d", registry.Count())
	}
	if err := registry.Add(first); !errors.Is(err, ErrHypothesisAlreadyExists) {
		t.Fatalf("expected identical duplicate, got %v", err)
	}
	list := registry.List()
	if len(list) != 2 || list[0].ID > list[1].ID {
		t.Fatal("registry list is not sorted")
	}
	list[0].Alternatives[0].Facts[0].ObservationIDs[0] = "changed"
	stored, err := registry.Get(list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Alternatives[0].Facts[0].ObservationIDs[0] == "changed" {
		t.Fatal("registry leaked mutable nested data")
	}
	collision, err := first.Clone()
	if err != nil {
		t.Fatal(err)
	}
	collision.reason = "different explanation"
	if err := registry.Add(collision); !errors.Is(err, ErrHypothesisCollision) {
		t.Fatalf("expected semantic collision, got %v", err)
	}
}

func TestHypothesisRegistryOptimisticStatus(t *testing.T) {
	registry := NewRegistry()
	set := testAssociationSet(t, "obs-status", "association-v1")
	if err := registry.Add(set); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := registry.SetStatus(set.ID(), 1, StatusUnderReview, chains.MutationContext{At: hypothesisTestBase.Add(time.Minute), Actor: "test", Reason: "review", CorrelationID: "review-" + string(rune('a'+i))})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	successes := 0
	stale := 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrStaleHypothesisCommand) {
			stale++
		}
	}
	if successes != 1 || stale != 1 {
		t.Fatalf("successes=%d stale=%d", successes, stale)
	}
}

func TestHypothesisRegistryVolumeAndConcurrentReads(t *testing.T) {
	registry := NewRegistry()
	for i := 0; i < 500; i++ {
		set := testAssociationSet(t, "volume-"+strconv.Itoa(i), "association-v1")
		if err := registry.Add(set); err != nil {
			t.Fatal(err)
		}
	}
	if registry.Count() != 500 || len(registry.List()) != 500 {
		t.Fatalf("unexpected volume count: %d", registry.Count())
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = registry.List()
			}
		}()
	}
	wg.Wait()
}
