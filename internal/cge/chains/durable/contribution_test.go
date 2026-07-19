package durable

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/generations"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/replay"
)

func TestCoordinatorAddContributionWALAndReplay(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	base := durableTestBase
	chain := newDurableChain(t, "durable-contribution")
	observation := chains.ObservationRef{ID: "observation-1", EventType: "vision.identity", Timestamp: base.Add(time.Second)}
	if err := chain.AddObservation(observation, durableMutation(base.Add(time.Second), "builder", "observe", "observe")); err != nil {
		t.Fatalf("observation: %v", err)
	}
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "chain-added", base.Add(time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	command := chains.AddContributionCommand{
		ChainID: "durable-contribution", SourceRevision: 2,
		Contribution: chains.ConfidenceContribution{ID: "contribution-1", Source: "review", Kind: chains.ContributionContradiction, Value: 0.25, ObservationIDs: []string{"observation-1"}, Reason: "contradiction", CreatedAt: base.Add(2 * time.Second)},
		Mutation:     chains.MutationContext{At: base.Add(2 * time.Second), Actor: "reviewer", Reason: "contradiction", CorrelationID: "contribution-1"},
	}
	result, err := coordinator.AddContribution(context.Background(), command, base.Add(2*time.Second))
	if err != nil {
		t.Fatalf("durable contribution: %v", err)
	}
	if !result.Published || result.Kind != MutationContributionAdded || result.Before == nil || result.After.Revision != 3 || result.Revision.Operation != chains.OperationContributionAdded || result.ContributionID != "contribution-1" || result.PreviousConfidence != 0 || result.NewConfidence != 0 || result.JournalSequence != 3 {
		t.Fatalf("unexpected durable result: %#v", result)
	}
	if coordinator.Status().State != StateReady {
		t.Fatalf("coordinator not ready after contribution: %#v", coordinator.Status())
	}
	read := readDurableJournal(t, fileJournal)
	if read.Records[2].Kind != journal.RecordKindContributionAdded {
		t.Fatalf("unexpected journal kind: %#v", read.Records[2])
	}
	replayed, metadata, err := replay.FromJournal(context.Background(), read)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !reflect.DeepEqual(replayed.List(), coordinator.List()) || metadata.ContributionsAdded != 1 {
		t.Fatalf("replayed registry differs: replay=%#v coordinator=%#v metadata=%#v", replayed.List(), coordinator.List(), metadata)
	}
	generationRoot := t.TempDir()
	store, err := generations.NewStore(generationRoot, generations.StoreOptions{})
	if err != nil {
		t.Fatalf("generation store: %v", err)
	}
	if _, err := coordinator.CreateSnapshotGeneration(context.Background(), store, base.Add(3*time.Second), "snapshotter", "snapshot-contribution"); err != nil {
		t.Fatalf("snapshot contribution: %v", err)
	}
	postCheckpoint := chains.AddContributionCommand{
		ChainID: "durable-contribution", SourceRevision: 3,
		Contribution: chains.ConfidenceContribution{ID: "neutral-1", Source: "context", Kind: chains.ContributionNeutral, Value: 1, ObservationIDs: []string{"observation-1"}, Reason: "neutral", CreatedAt: base.Add(4 * time.Second)},
		Mutation:     chains.MutationContext{At: base.Add(4 * time.Second), Actor: "reviewer", Reason: "neutral", CorrelationID: "neutral-1"},
	}
	if _, err := coordinator.AddContribution(context.Background(), postCheckpoint, base.Add(4*time.Second)); err != nil {
		t.Fatalf("post-checkpoint contribution: %v", err)
	}
	manifestStore, err := generations.NewStore(generationRoot, generations.StoreOptions{})
	if err != nil {
		t.Fatalf("reload generation store: %v", err)
	}
	recovered, _, err := FromGenerationManifest(context.Background(), manifestStore, fileJournal)
	if err != nil || !reflect.DeepEqual(recovered.List(), coordinator.List()) {
		t.Fatalf("manifest replay differs: recovered=%#v coordinator=%#v err=%v", recovered.List(), coordinator.List(), err)
	}
	before := coordinator.Status()
	if _, err := coordinator.AddContribution(context.Background(), command, base.Add(3*time.Second)); err == nil || !errors.Is(err, ErrStaleContributionCommand) {
		t.Fatalf("stale contribution error = %v", err)
	}
	if !reflect.DeepEqual(before, coordinator.Status()) || readDurableJournal(t, fileJournal).HeadSequence != 5 {
		t.Fatalf("stale contribution changed durable state")
	}
}

func TestCoordinatorContributionPublicationFailureRecoversFromJournal(t *testing.T) {
	coordinator, _ := newDurableCoordinator(t)
	base := durableTestBase
	chain := newDurableChain(t, "contribution-crash")
	observation := chains.ObservationRef{ID: "observation-1", EventType: "vision.identity", Timestamp: base.Add(time.Second)}
	if err := chain.AddObservation(observation, durableMutation(base.Add(time.Second), "builder", "observe", "observe")); err != nil {
		t.Fatalf("observation: %v", err)
	}
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "chain-added", base.Add(time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	coordinator.publishHook = func() error { return errors.New("injected publication failure") }
	command := chains.AddContributionCommand{ChainID: chain.Snapshot().ID, SourceRevision: 2, Contribution: chains.ConfidenceContribution{ID: "support-1", Source: "review", Kind: chains.ContributionSupport, Value: 0.5, ObservationIDs: []string{"observation-1"}, Reason: "support", CreatedAt: base.Add(2 * time.Second)}, Mutation: chains.MutationContext{At: base.Add(2 * time.Second), Actor: "reviewer", Reason: "support", CorrelationID: "support-1"}}
	result, err := coordinator.AddContribution(context.Background(), command, base.Add(2*time.Second))
	if err == nil || !errors.Is(err, ErrPublicationFailed) || result.Published || coordinator.Status().State != StateDegraded {
		t.Fatalf("expected degraded contribution publication failure: result=%#v status=%#v err=%v", result, coordinator.Status(), err)
	}
	coordinator.publishHook = nil
	if _, err := coordinator.RecoverFromJournal(context.Background()); err != nil {
		t.Fatalf("recover contribution: %v", err)
	}
	if got, err := coordinator.Get(chain.Snapshot().ID); err != nil || len(got.Contributions) != 1 || got.CurrentConfidence != 0.5 {
		t.Fatalf("recovered contribution missing: %#v err=%v", got, err)
	}
}

func TestContributionMakesExistingAssociationPlanStale(t *testing.T) {
	coordinator, _ := newDurableCoordinator(t)
	base := durableTestBase
	chain := newDurableChain(t, "association-after-contribution")
	seed := chains.ObservationRef{ID: "seed", EventType: "vision.identity", Timestamp: base, EntityID: "entity-a", NodeID: "entry"}
	if err := chain.AddObservation(seed, durableMutation(base.Add(time.Second), "builder", "seed", "seed")); err != nil {
		t.Fatalf("seed observation: %v", err)
	}
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "chain", base.Add(2*time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	plan, err := coordinator.PlanAssociation(association.Input{Observation: chains.ObservationRef{ID: "planned", EventType: "vision.identity", Timestamp: base.Add(10 * time.Second), EntityID: "entity-a", NodeID: "entry"}}, base.Add(time.Minute), association.DefaultPolicy())
	if err != nil || plan.Decision != association.DecisionAttachExisting {
		t.Fatalf("unexpected association plan: %#v err=%v", plan, err)
	}
	command := chains.AddContributionCommand{ChainID: chain.Snapshot().ID, SourceRevision: plan.SelectedSourceRevision, Contribution: chains.ConfidenceContribution{ID: "intervening-contribution", Source: "review", Kind: chains.ContributionNeutral, Value: 0, ObservationIDs: []string{"seed"}, Reason: "intervening", CreatedAt: base.Add(time.Minute + time.Second)}, Mutation: durableMutation(base.Add(time.Minute+time.Second), "reviewer", "intervening", "intervening")}
	if _, err := coordinator.AddContribution(context.Background(), command, base.Add(time.Minute+2*time.Second)); err != nil {
		t.Fatalf("intervening contribution: %v", err)
	}
	if _, err := coordinator.ApplyAssociationPlan(context.Background(), plan, "association", "stale-plan", base.Add(2*time.Minute), base.Add(2*time.Minute+time.Second)); err == nil || !errors.Is(err, ErrStaleAssociationPlan) {
		t.Fatalf("stale association plan was accepted: %v", err)
	}
}
