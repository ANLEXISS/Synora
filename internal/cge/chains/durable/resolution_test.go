package durable

import (
	"context"
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/hypotheses"
)

func TestCoordinatorResolvesHypothesisWithOneGlobalRecord(t *testing.T) {
	c, fileJournal := newDurableCoordinator(t)
	for _, id := range []string{"chain-a", "chain-b"} {
		chain, err := chains.New(chains.ChainID(id), chains.MutationContext{At: durableHypothesisBase, Actor: "writer", Reason: "create chain", CorrelationID: "create-" + id})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.AddChain(context.Background(), chain, "writer", "add-"+id, durableTestBase.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	set := durableHypothesisSet(t, "resolve-observation")
	if _, err := c.AddHypothesis(context.Background(), set, durableTestBase.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	before, err := c.GetHypothesis(set.ID())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := hypotheses.PlanResolution(before, before.Alternatives[0].ID, durableTestBase.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	command, err := plan.Command(chains.MutationContext{At: durableTestBase.Add(3 * time.Second), Actor: "reviewer", Reason: "explicit selection", CorrelationID: "resolve-1"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.ResolveHypothesis(context.Background(), command, durableTestBase.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !result.Published || result.Idempotent || result.Outcome.Kind != hypotheses.ResolutionEffectAttachObservation || result.HypothesisAfter.Status != hypotheses.StatusResolved {
		t.Fatalf("unexpected result: %#v", result)
	}
	read, err := fileJournal.ReadAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Records) != 5 || read.Records[4].Kind != journal.RecordKindHypothesisResolved {
		t.Fatalf("unexpected journal kinds/count: %d %s", len(read.Records), read.Records[4].Kind)
	}
	for _, record := range read.Records {
		if record.Kind == journal.RecordKindObservationAdded {
			t.Fatal("resolution emitted a separate observation record")
		}
	}
	recovered, metadata, err := FromJournal(context.Background(), fileJournal)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Replay.ResolutionsApplied != 1 || metadata.HypothesisReplay.ResolutionsApplied != 1 {
		t.Fatalf("resolution replay metadata missing: %#v", metadata)
	}
	recoveredHypothesis, err := recovered.GetHypothesis(set.ID())
	if err != nil {
		t.Fatal(err)
	}
	if recoveredHypothesis.Status != hypotheses.StatusResolved || recoveredHypothesis.Resolution == nil {
		t.Fatalf("hypothesis was not restored resolved: %#v", recoveredHypothesis)
	}
	chain, err := recovered.Get("chain-a")
	if err != nil {
		t.Fatal(err)
	}
	if chain.Revision != 2 || len(chain.Observations) != 1 || chain.Observations[0].ID != "resolve-observation" {
		t.Fatalf("chain delta was not restored: %#v", chain)
	}
	idempotent, err := c.ResolveHypothesis(context.Background(), command, durableTestBase.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if idempotent.Applied || !idempotent.Idempotent {
		t.Fatalf("expected explicit idempotence: %#v", idempotent)
	}
	collision := command
	collision.AlternativeID = before.Alternatives[1].ID
	if _, err := c.ResolveHypothesis(context.Background(), collision, durableTestBase.Add(6*time.Second)); err == nil || !errors.Is(err, hypotheses.ErrHypothesisResolutionCollision) {
		t.Fatalf("expected resolution collision, got %v", err)
	}
}
