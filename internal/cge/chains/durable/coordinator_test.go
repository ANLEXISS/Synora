package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/generations"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/registry"
	"synora/internal/cge/chains/replay"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/routines"
)

var durableTestBase = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func durableMutation(at time.Time, actor, correlation, reason string) chains.MutationContext {
	return chains.MutationContext{At: at, Actor: actor, CorrelationID: correlation, Reason: reason}
}

func newDurableJournal(t *testing.T) (*journal.FileJournal, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cge.ndjson")
	fileJournal, err := journal.NewFileJournal(path, journal.FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		t.Fatalf("new journal: %v", err)
	}
	if _, err := fileJournal.Initialize(context.Background(), journal.GenesisInput{
		JournalID: "durable-test", CreatedAt: durableTestBase, RecordedAt: durableTestBase,
		Purpose: "durable coordinator test", Actor: "test", CorrelationID: "genesis",
	}); err != nil {
		t.Fatalf("initialize journal: %v", err)
	}
	return fileJournal, path
}

func newDurableChain(t *testing.T, id string) *chains.Chain {
	t.Helper()
	chain, err := chains.New(chains.ChainID(id), durableMutation(durableTestBase, "builder", "create-"+id, "create chain"))
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	return chain
}

func newDurableCoordinator(t *testing.T) (*Coordinator, *journal.FileJournal) {
	t.Helper()
	fileJournal, _ := newDurableJournal(t)
	coordinator, _, err := FromJournal(context.Background(), fileJournal)
	if err != nil {
		t.Fatalf("construct coordinator: %v", err)
	}
	return coordinator, fileJournal
}

func durableRoutineOccurrence(t *testing.T, coordinator *Coordinator, suffix string) routines.Occurrence {
	t.Helper()
	chainID := chains.ChainID("routine-wal-" + suffix)
	chain := newDurableChain(t, string(chainID))
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "add-"+suffix, durableTestBase); err != nil {
		t.Fatalf("add routine WAL chain: %v", err)
	}
	at := durableTestBase.Add(time.Minute)
	topology := cgecontext.TopologySnapshot{Revision: "routine-wal-topology", CapturedAt: at, Nodes: []cgecontext.Node{{ID: "room", ZoneID: "home", Kind: cgecontext.NodeRoom}}}
	frame, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: "routine-wal-observation-" + suffix, ObservedAt: at, NodeID: "room", Timezone: "UTC", Topology: topology})
	if err != nil {
		t.Fatalf("resolve routine WAL frame: %v", err)
	}
	observation := chains.ObservationRef{ID: "routine-wal-observation-" + suffix, EventType: "vision.identity", Timestamp: at, NodeID: "room", EntityID: "routine-wal-entity", Context: &frame}
	if _, err := coordinator.AddObservation(context.Background(), chains.AddObservationCommand{ChainID: chainID, SourceRevision: 1, Observation: observation, Mutation: durableMutation(at, "writer", "observation-"+suffix, "observation")}, at); err != nil {
		t.Fatalf("add routine WAL observation: %v", err)
	}
	snapshot, err := coordinator.Get(chainID)
	if err != nil {
		t.Fatalf("get routine WAL chain: %v", err)
	}
	occurrence, err := routines.ExtractPresenceOccurrence(snapshot, observation.ID, routines.DefaultExtractionPolicy())
	if err != nil {
		t.Fatalf("extract routine WAL occurrence: %v", err)
	}
	return occurrence
}

func readDurableJournal(t *testing.T, fileJournal *journal.FileJournal) journal.JournalSnapshot {
	t.Helper()
	snapshot, err := fileJournal.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	return snapshot
}

func TestCoordinatorConstructsReadyAndOwnsReplayedState(t *testing.T) {
	fileJournal, _ := newDurableJournal(t)
	chain := newDurableChain(t, "owned")
	if _, err := fileJournal.AppendChainAdded(context.Background(), journal.ChainAddedInput{
		Chain: chain.Snapshot(), RecordedAt: durableTestBase.Add(time.Second), Actor: "writer", CorrelationID: "add-owned",
	}); err != nil {
		t.Fatalf("append chain: %v", err)
	}
	coordinator, metadata, err := FromJournal(context.Background(), fileJournal)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if metadata.State != StateReady || coordinator.Status().State != StateReady || coordinator.Count() != 1 || metadata.JournalSequence != 2 {
		t.Fatalf("unexpected construction state: metadata=%#v status=%#v", metadata, coordinator.Status())
	}
	if err := chain.SetStatus(chains.StatusActive, durableMutation(durableTestBase.Add(2*time.Second), "external", "external", "must not leak")); err != nil {
		t.Fatalf("mutate source: %v", err)
	}
	stored, err := coordinator.Get(chains.ChainID("owned"))
	if err != nil {
		t.Fatalf("get owned chain: %v", err)
	}
	if stored.Status != chains.StatusCandidate || stored.Revision != 1 {
		t.Fatalf("coordinator shares source state: %#v", stored)
	}
}

func TestRoutineDurabilityReplayIdempotenceAndExplicitStatus(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	chain := newDurableChain(t, "routine-chain")
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "add-routine-chain", durableTestBase); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	at := durableTestBase.Add(time.Minute)
	topology := cgecontext.TopologySnapshot{Revision: "topology-1", CapturedAt: at, Nodes: []cgecontext.Node{{ID: "room", ZoneID: "home", Kind: cgecontext.NodeRoom}}}
	frame, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: "routine-observation", ObservedAt: at, NodeID: "room", Timezone: "UTC", Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome, Topology: topology})
	if err != nil {
		t.Fatalf("resolve frame: %v", err)
	}
	observation := chains.ObservationRef{ID: "routine-observation", EventType: "vision.identity", Timestamp: at, NodeID: "room", EntityID: "entity-1", Context: &frame}
	if _, err := coordinator.AddObservation(context.Background(), chains.AddObservationCommand{ChainID: chain.Snapshot().ID, SourceRevision: 1, Observation: observation, Mutation: durableMutation(at, "writer", "observation-routine", "observation")}, at); err != nil {
		t.Fatalf("add observation: %v", err)
	}
	chainSnapshot, err := coordinator.Get(chain.Snapshot().ID)
	if err != nil {
		t.Fatalf("get chain: %v", err)
	}
	occurrence, err := routines.ExtractPresenceOccurrence(chainSnapshot, observation.ID, routines.DefaultExtractionPolicy())
	if err != nil {
		t.Fatalf("extract presence: %v", err)
	}
	mutation := durableMutation(occurrence.ObservedAt, "synora-core/cge-shadow", "routine-occurrence", "routine occurrence extracted")
	first, err := coordinator.ApplyRoutineOccurrence(context.Background(), occurrence, mutation, at)
	if err != nil || !first.Created || !first.Applied || !first.Published || first.Idempotent {
		t.Fatalf("routine create result: %#v err=%v", first, err)
	}
	second, err := coordinator.ApplyRoutineOccurrence(context.Background(), occurrence, mutation, at)
	if err != nil || !second.Idempotent || second.Applied {
		t.Fatalf("routine duplicate result: %#v err=%v", second, err)
	}
	statusAt := at.Add(time.Minute)
	status, err := coordinator.SetRoutineStatus(context.Background(), routines.SetStatusCommand{RoutineID: occurrence.RoutineID, SourceRevision: 1, Target: routines.StatusActive, Mutation: durableMutation(statusAt, "operator", "routine-status", "explicit status")}, statusAt)
	if err != nil || !status.Applied || !status.Published || status.After.Status != routines.StatusActive {
		t.Fatalf("routine status result: %#v err=%v", status, err)
	}
	read := readDurableJournal(t, fileJournal)
	recovered, metadata, err := FromJournal(context.Background(), fileJournal)
	if err != nil {
		t.Fatalf("recover routines: %v", err)
	}
	if metadata.RoutineReplay.RoutinesCreated != 1 || metadata.RoutineReplay.StatusChangesApplied != 1 || recovered.RoutineCount() != 1 {
		t.Fatalf("routine replay metadata/status: %#v count=%d", metadata.RoutineReplay, recovered.RoutineCount())
	}
	snapshot, err := recovered.GetRoutine(occurrence.RoutineID)
	if err != nil || snapshot.Status != routines.StatusActive || snapshot.OccurrenceCount != 1 {
		t.Fatalf("recovered routine: %#v err=%v", snapshot, err)
	}
	if read.HeadSequence != 5 || recovered.Status().JournalSequence != read.HeadSequence {
		t.Fatalf("global head mismatch: read=%d status=%d", read.HeadSequence, recovered.Status().JournalSequence)
	}
}

func TestRoutineWALRejectedAndUncertainPublicationRules(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	occurrence := durableRoutineOccurrence(t, coordinator, "rejected")
	fileJournal.SetQualificationHook(func(stage string) error {
		if stage == "before_append" {
			return errors.New("routine rejected")
		}
		return nil
	})
	result, err := coordinator.ApplyRoutineOccurrence(context.Background(), occurrence, durableMutation(occurrence.ObservedAt, "writer", "routine-rejected", "routine"), occurrence.ObservedAt)
	if err == nil || !errors.Is(err, ErrRoutineAppendFailed) || result.Published || coordinator.RoutineCount() != 0 || coordinator.Status().State != StateReady {
		t.Fatalf("routine rejected append changed state: result=%#v status=%#v err=%v", result, coordinator.Status(), err)
	}
	fileJournal.SetQualificationHook(nil)
	uncertain := durableRoutineOccurrence(t, coordinator, "uncertain")
	fileJournal.SetQualificationHook(func(stage string) error {
		if stage == "after_sync" {
			return errors.New("routine uncertain")
		}
		return nil
	})
	result, err = coordinator.ApplyRoutineOccurrence(context.Background(), uncertain, durableMutation(uncertain.ObservedAt, "writer", "routine-uncertain", "routine"), uncertain.ObservedAt)
	if err == nil || !errors.Is(err, ErrJournalAppendAmbiguous) || result.Published || coordinator.Status().State != StateDegraded || coordinator.RoutineCount() != 0 {
		t.Fatalf("uncertain routine append was published: result=%#v status=%#v err=%v", result, coordinator.Status(), err)
	}
	fileJournal.SetQualificationHook(nil)
	recovered, _, recoveryErr := FromJournal(context.Background(), fileJournal)
	if recoveryErr != nil || recovered.RoutineCount() != 1 {
		t.Fatalf("uncertain durable routine was not recoverable: count=%d err=%v", func() int {
			if recovered == nil {
				return 0
			}
			return recovered.RoutineCount()
		}(), recoveryErr)
	}
}

func TestRoutineReplayIsFullJournalAcrossChainGenerationCheckpoint(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	first := durableRoutineOccurrence(t, coordinator, "before-checkpoint")
	if _, err := coordinator.ApplyRoutineOccurrence(context.Background(), first, durableMutation(first.ObservedAt, "writer", "routine-before", "routine"), first.ObservedAt); err != nil {
		t.Fatalf("apply routine before checkpoint: %v", err)
	}
	store, err := generations.NewStore(t.TempDir(), generations.StoreOptions{})
	if err != nil {
		t.Fatalf("new generation store: %v", err)
	}
	if _, err := coordinator.CreateSnapshotGeneration(context.Background(), store, durableTestBase.Add(10*time.Minute), "writer", "checkpoint"); err != nil {
		t.Fatalf("create generation: %v", err)
	}
	second := durableRoutineOccurrence(t, coordinator, "after-checkpoint")
	if _, err := coordinator.ApplyRoutineOccurrence(context.Background(), second, durableMutation(second.ObservedAt, "writer", "routine-after", "routine"), second.ObservedAt); err != nil {
		t.Fatalf("apply routine after checkpoint: %v", err)
	}
	recovered, _, err := FromGenerationManifest(context.Background(), store, fileJournal)
	if err != nil {
		t.Fatalf("recover manifest with routines: %v", err)
	}
	if recovered.RoutineCount() != 1 || recovered.Count() != 2 {
		t.Fatalf("checkpoint recovery lost routine or chain: routines=%d chains=%d", recovered.RoutineCount(), recovered.Count())
	}
	snapshot, err := recovered.GetRoutine(first.RoutineID)
	if err != nil || snapshot.OccurrenceCount != 2 {
		t.Fatalf("checkpoint recovery lost post-checkpoint occurrence: %#v err=%v", snapshot, err)
	}
}

func TestCoordinatorRejectsJournalWithoutGenesis(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.ndjson")
	fileJournal, err := journal.NewFileJournal(path, journal.FileJournalOptions{})
	if err != nil {
		t.Fatalf("new journal: %v", err)
	}
	if coordinator, _, err := FromJournal(context.Background(), fileJournal); err == nil || coordinator != nil || !errors.Is(err, ErrRecoveryFailed) || !errors.Is(err, journal.ErrJournalNotFound) {
		t.Fatalf("expected invalid journal rejection, coordinator=%v err=%v", coordinator, err)
	}
}

func TestCoordinatorAddUsesWALBeforePublication(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	chain := newDurableChain(t, "added")
	result, err := coordinator.AddChain(context.Background(), chain, "writer", "add-added", durableTestBase.Add(time.Second))
	if err != nil {
		t.Fatalf("add chain: %v", err)
	}
	if !result.Published || result.Kind != MutationChainAdded || result.JournalSequence != 2 || coordinator.Count() != 1 {
		t.Fatalf("unexpected add result/status: result=%#v status=%#v", result, coordinator.Status())
	}
	journalSnapshot := readDurableJournal(t, fileJournal)
	if journalSnapshot.HeadSequence != result.JournalSequence || journalSnapshot.HeadHash != result.JournalRecordHash {
		t.Fatalf("journal head does not match result: %#v", journalSnapshot)
	}
	var payload journal.ChainAddedPayload
	if err := json.Unmarshal(journalSnapshot.Records[1].Payload, &payload); err != nil {
		t.Fatalf("decode chain.added: %v", err)
	}
	if !reflect.DeepEqual(payload.Chain, result.After) {
		t.Fatalf("journal payload differs from published state: payload=%#v after=%#v", payload.Chain, result.After)
	}
	chain.SetStatus(chains.StatusActive, durableMutation(durableTestBase.Add(2*time.Second), "external", "external", "source mutation"))
	stored, err := coordinator.Get(chains.ChainID("added"))
	if err != nil || stored.Status != chains.StatusCandidate {
		t.Fatalf("source mutation leaked into coordinator: stored=%#v err=%v", stored, err)
	}
}

func TestCoordinatorAddsObservationWithSeparateDomainAndRecordTimes(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "observation"), "writer", "add-observation-chain", durableTestBase.Add(time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	before, err := coordinator.Get("observation")
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	evaluation, err := chains.EvaluateLifecycle(before, durableTestBase.Add(365*24*time.Hour), chains.DefaultLifecyclePolicy())
	if err != nil || evaluation.Proposal == nil {
		t.Fatalf("evaluate old proposal: evaluation=%#v err=%v", evaluation, err)
	}
	oldProposal := *evaluation.Proposal
	command := chains.AddObservationCommand{
		ChainID: "observation", SourceRevision: before.Revision,
		Observation: chains.ObservationRef{ID: "observation-1", EventType: "vision.motion", Timestamp: durableTestBase.Add(2 * time.Hour), NodeID: "entry"},
		Mutation:    chains.MutationContext{At: durableTestBase.Add(3 * time.Hour), Actor: "observer", Reason: "explicit observation", CorrelationID: "observation-1"},
	}
	result, err := coordinator.AddObservation(context.Background(), command, durableTestBase.Add(4*time.Hour))
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}
	if !result.Published || result.Kind != MutationObservationAdded || result.Before == nil || result.After.Revision != before.Revision+1 || result.Revision.Operation != chains.OperationObservationAdded || result.JournalSequence != 3 {
		t.Fatalf("unexpected observation result: %#v", result)
	}
	if result.After.Status != before.Status || result.After.CurrentConfidence != before.CurrentConfidence || result.After.HistoricalReliability != before.HistoricalReliability || result.After.ConfirmationCount != before.ConfirmationCount || result.After.ContradictionCount != before.ContradictionCount {
		t.Fatalf("observation changed unrelated state: before=%#v after=%#v", before, result.After)
	}
	read := readDurableJournal(t, fileJournal)
	var payload journal.ObservationAddedPayload
	if err := json.Unmarshal(read.Records[2].Payload, &payload); err != nil {
		t.Fatalf("decode observation record: %v", err)
	}
	if payload.Observation != command.Observation || !reflect.DeepEqual(payload.Revision, result.Revision) || read.Records[2].RecordedAt.Equal(command.Mutation.At) {
		t.Fatalf("observation payload/timestamps incorrect: payload=%#v revision=%#v record=%s mutation=%s", payload, result.Revision, read.Records[2].RecordedAt, command.Mutation.At)
	}
	replayed, _, err := replay.FromJournal(context.Background(), read)
	if err != nil {
		t.Fatalf("journal replay failed: %v", err)
	}
	if !reflect.DeepEqual(replayed.List(), coordinator.List()) {
		t.Fatalf("journal replay differs: replay=%#v coordinator=%#v", replayed.List(), coordinator.List())
	}
	if _, err := coordinator.ApplyLifecycleProposal(context.Background(), oldProposal, "lifecycle", "obsolete", oldProposal.EvaluatedAt); err == nil || !errors.Is(err, registry.ErrStaleProposal) {
		t.Fatalf("expected old lifecycle proposal to become stale: %v", err)
	}
	result.After.Observations[0].ID = "changed-result"
	if fresh, err := coordinator.Get("observation"); err != nil || fresh.Observations[0].ID == "changed-result" {
		t.Fatalf("mutation result exposed coordinator state: fresh=%#v err=%v", fresh, err)
	}
}

func TestCoordinatorPlansAndAppliesAssociationWithoutReplanning(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	chain := newDurableChain(t, "association-existing")
	existing := chains.ObservationRef{ID: "association-existing-observation", EventType: "vision.motion", Timestamp: durableTestBase, EntityID: "entity-a", NodeID: "entry", DeviceID: "device-1", ActivationID: "activation-1", TrackID: "track-1", SequenceKey: "sequence-1"}
	if err := chain.AddObservation(existing, durableMutation(durableTestBase.Add(time.Second), "builder", "association-existing-observation", "seed association")); err != nil {
		t.Fatalf("seed chain observation: %v", err)
	}
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "association-chain", durableTestBase.Add(2*time.Second)); err != nil {
		t.Fatalf("add association chain: %v", err)
	}
	input := association.Input{Observation: chains.ObservationRef{ID: "association-new-observation", EventType: "vision.motion", Timestamp: durableTestBase.Add(10 * time.Second), EntityID: "entity-a", NodeID: "entry", DeviceID: "device-1", ActivationID: "activation-1", TrackID: "track-1", SequenceKey: "sequence-1"}}
	planned, err := coordinator.PlanAssociation(input, durableTestBase.Add(time.Minute), association.DefaultPolicy())
	if err != nil || planned.Decision != association.DecisionAttachExisting {
		t.Fatalf("unexpected association plan: plan=%#v err=%v", planned, err)
	}
	beforeStatus := coordinator.Status()
	result, err := coordinator.ApplyAssociationPlan(context.Background(), planned, "association", "association-apply", durableTestBase.Add(2*time.Minute), durableTestBase.Add(2*time.Minute+time.Second))
	if err != nil {
		t.Fatalf("apply association plan: %v", err)
	}
	if !result.Applied || result.Idempotent || result.Decision != association.DecisionAttachExisting || result.ChainID != "association-existing" || result.JournalSequence != 3 || result.After == nil || result.After.Status != chains.StatusCandidate {
		t.Fatalf("unexpected association application: %#v", result)
	}
	if coordinator.Status().JournalSequence != beforeStatus.JournalSequence+1 || coordinator.Status().State != StateReady {
		t.Fatalf("unexpected coordinator state: before=%#v after=%#v", beforeStatus, coordinator.Status())
	}
	read := readDurableJournal(t, fileJournal)
	if read.Records[2].Kind != journal.RecordKindObservationAdded {
		t.Fatalf("association did not use observation delta: %#v", read.Records[2])
	}
	replayed, _, err := replay.FromJournal(context.Background(), read)
	if err != nil || !reflect.DeepEqual(replayed.List(), coordinator.List()) {
		t.Fatalf("association replay differs: replay=%#v current=%#v err=%v", replayed.List(), coordinator.List(), err)
	}
}

func TestCoordinatorAssociationCreateIsDeterministicAndIdempotent(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	input := association.Input{Observation: chains.ObservationRef{ID: "new-candidate-observation", EventType: "vision.motion", Timestamp: durableTestBase.Add(time.Minute), EntityID: "entity-new", NodeID: "entry"}}
	firstPlan, err := coordinator.PlanAssociation(input, durableTestBase.Add(2*time.Minute), association.DefaultPolicy())
	if err != nil || firstPlan.Decision != association.DecisionCreateCandidate {
		t.Fatalf("unexpected create plan: plan=%#v err=%v", firstPlan, err)
	}
	secondPlan, err := coordinator.PlanAssociation(input, durableTestBase.Add(2*time.Minute), association.DefaultPolicy())
	if err != nil || firstPlan.NewChainID != secondPlan.NewChainID {
		t.Fatalf("candidate ID is not stable: first=%#v second=%#v err=%v", firstPlan, secondPlan, err)
	}
	first, err := coordinator.ApplyAssociationPlan(context.Background(), firstPlan, "association", "create-apply", durableTestBase.Add(3*time.Minute), durableTestBase.Add(3*time.Minute+time.Second))
	if err != nil || !first.Applied || first.ChainID != firstPlan.NewChainID {
		t.Fatalf("apply candidate creation: result=%#v err=%v", first, err)
	}
	second, err := coordinator.ApplyAssociationPlan(context.Background(), secondPlan, "association", "create-apply-again", durableTestBase.Add(3*time.Minute), durableTestBase.Add(3*time.Minute+time.Second))
	if err != nil || second.Applied || !second.Idempotent || second.ChainID != first.ChainID {
		t.Fatalf("second candidate creation was not idempotent: result=%#v err=%v", second, err)
	}
	read := readDurableJournal(t, fileJournal)
	if read.RecordCount != 2 || read.Records[1].Kind != journal.RecordKindChainAdded {
		t.Fatalf("idempotent creation appended an extra record: %#v", read)
	}
	created, err := coordinator.Get(first.ChainID)
	if err != nil || created.Status != chains.StatusCandidate || len(created.Observations) != 1 || created.CurrentConfidence != 0 {
		t.Fatalf("candidate creation changed unexpected state: snapshot=%#v err=%v", created, err)
	}
}

func TestCoordinatorRejectsStaleAssociationPlan(t *testing.T) {
	coordinator, _ := newDurableCoordinator(t)
	chain := newDurableChain(t, "association-stale")
	seed := chains.ObservationRef{ID: "stale-seed", EventType: "vision.motion", Timestamp: durableTestBase, EntityID: "entity-a", NodeID: "entry"}
	if err := chain.AddObservation(seed, durableMutation(durableTestBase.Add(time.Second), "builder", "stale-seed", "seed")); err != nil {
		t.Fatalf("seed chain: %v", err)
	}
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "stale-chain", durableTestBase.Add(2*time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	input := association.Input{Observation: chains.ObservationRef{ID: "stale-new", EventType: "vision.motion", Timestamp: durableTestBase.Add(10 * time.Second), EntityID: "entity-a", NodeID: "entry"}}
	plan, err := coordinator.PlanAssociation(input, durableTestBase.Add(time.Minute), association.DefaultPolicy())
	if err != nil || plan.Decision != association.DecisionAttachExisting {
		t.Fatalf("unexpected stale plan: plan=%#v err=%v", plan, err)
	}
	if _, err := coordinator.AddObservation(context.Background(), chains.AddObservationCommand{ChainID: chain.Snapshot().ID, SourceRevision: plan.SelectedSourceRevision, Observation: chains.ObservationRef{ID: "intervening", EventType: "vision.motion", Timestamp: durableTestBase.Add(11 * time.Second), EntityID: "entity-a"}, Mutation: durableMutation(durableTestBase.Add(time.Minute+time.Second), "other", "intervening", "intervening observation")}, durableTestBase.Add(time.Minute+2*time.Second)); err != nil {
		t.Fatalf("intervening observation: %v", err)
	}
	if _, err := coordinator.ApplyAssociationPlan(context.Background(), plan, "association", "stale-apply", durableTestBase.Add(2*time.Minute), durableTestBase.Add(2*time.Minute+time.Second)); err == nil || !errors.Is(err, ErrStaleAssociationPlan) {
		t.Fatalf("stale association plan was accepted: %v", err)
	}
}

func TestCoordinatorSerializesConcurrentAssociationPlans(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	chain := newDurableChain(t, "association-concurrent")
	seed := chains.ObservationRef{ID: "concurrent-seed", EventType: "vision.motion", Timestamp: durableTestBase, EntityID: "entity-a", NodeID: "entry", DeviceID: "device-1", SequenceKey: "sequence-1"}
	if err := chain.AddObservation(seed, durableMutation(durableTestBase.Add(time.Second), "builder", "concurrent-seed", "seed")); err != nil {
		t.Fatalf("seed chain: %v", err)
	}
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "concurrent-chain", durableTestBase.Add(2*time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	plan, err := coordinator.PlanAssociation(association.Input{Observation: chains.ObservationRef{ID: "concurrent-new", EventType: "vision.motion", Timestamp: durableTestBase.Add(10 * time.Second), EntityID: "entity-a", NodeID: "entry", DeviceID: "device-1", SequenceKey: "sequence-1"}}, durableTestBase.Add(time.Minute), association.DefaultPolicy())
	if err != nil || plan.Decision != association.DecisionAttachExisting {
		t.Fatalf("unexpected concurrent plan: plan=%#v err=%v", plan, err)
	}
	type outcome struct {
		result AssociationApplyResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			outcomes <- func() outcome {
				result, applyErr := coordinator.ApplyAssociationPlan(context.Background(), plan, "association", fmt.Sprintf("concurrent-%d", index), durableTestBase.Add(2*time.Minute), durableTestBase.Add(2*time.Minute+time.Duration(index)*time.Second))
				return outcome{result: result, err: applyErr}
			}()
		}(i)
	}
	wait.Wait()
	close(outcomes)
	applied := 0
	stale := 0
	for result := range outcomes {
		if result.err == nil && result.result.Applied {
			applied++
		}
		if errors.Is(result.err, ErrStaleAssociationPlan) {
			stale++
		}
	}
	if applied != 1 || stale != 1 || readDurableJournal(t, fileJournal).RecordCount != 3 {
		t.Fatalf("concurrent plans were not serialized: applied=%d stale=%d journal=%#v", applied, stale, readDurableJournal(t, fileJournal))
	}
}

func TestCoordinatorSerializesAssociationWithExplicitGeneration(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	chain := newDurableChain(t, "association-generation")
	seed := chains.ObservationRef{ID: "generation-seed", EventType: "vision.motion", Timestamp: durableTestBase, EntityID: "entity-a", NodeID: "entry"}
	if err := chain.AddObservation(seed, durableMutation(durableTestBase.Add(time.Second), "builder", "generation-seed", "seed")); err != nil {
		t.Fatalf("seed chain: %v", err)
	}
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "generation-chain", durableTestBase.Add(2*time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	plan, err := coordinator.PlanAssociation(association.Input{Observation: chains.ObservationRef{ID: "generation-observation", EventType: "vision.motion", Timestamp: durableTestBase.Add(10 * time.Second), EntityID: "entity-a", NodeID: "entry"}}, durableTestBase.Add(time.Minute), association.DefaultPolicy())
	if err != nil || plan.Decision != association.DecisionAttachExisting {
		t.Fatalf("unexpected generation plan: plan=%#v err=%v", plan, err)
	}
	store, err := generations.NewStore(t.TempDir(), generations.StoreOptions{})
	if err != nil {
		t.Fatalf("new generation store: %v", err)
	}
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, applyErr := coordinator.ApplyAssociationPlan(context.Background(), plan, "association", "generation-association", durableTestBase.Add(2*time.Hour), durableTestBase.Add(2*time.Hour+time.Second))
		errs <- applyErr
	}()
	go func() {
		defer wait.Done()
		_, snapshotErr := coordinator.CreateSnapshotGeneration(context.Background(), store, durableTestBase.Add(3*time.Hour), "snapshotter", "association-generation-snapshot")
		errs <- snapshotErr
	}()
	wait.Wait()
	close(errs)
	for applyErr := range errs {
		if applyErr != nil {
			t.Fatalf("concurrent association/generation operation failed: %v", applyErr)
		}
	}
	recovered, _, err := FromGenerationManifest(context.Background(), store, fileJournal)
	if err != nil || !reflect.DeepEqual(recovered.List(), coordinator.List()) {
		t.Fatalf("generation recovery differs after concurrent association: recovered=%#v current=%#v err=%v", recovered.List(), coordinator.List(), err)
	}
}

func TestCoordinatorRejectsPreparationAndCancelledAppendWithoutMutation(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	before := coordinator.Status()
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "bad-actor"), "", "bad", durableTestBase); err == nil || !errors.Is(err, ErrInvalidActor) {
		t.Fatalf("expected invalid actor, got %v", err)
	}
	if status := coordinator.Status(); !reflect.DeepEqual(status, before) {
		t.Fatalf("invalid input changed status: before=%#v after=%#v", before, status)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := coordinator.AddChain(cancelled, newDurableChain(t, "cancelled"), "writer", "cancelled", durableTestBase); err == nil || !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("expected cancelled context, got %v", err)
	}
	if coordinator.Count() != 0 || coordinator.Status().JournalSequence != 1 {
		t.Fatalf("cancelled append changed coordinator: status=%#v", coordinator.Status())
	}
	if snapshot := readDurableJournal(t, fileJournal); snapshot.RecordCount != 1 {
		t.Fatalf("cancelled append wrote a record: %#v", snapshot)
	}
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "bad-time"), "writer", "bad-time", time.Time{}); err == nil || !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("expected invalid timestamp, got %v", err)
	}
}

func TestCoordinatorCleanAppendFailureLeavesReadyState(t *testing.T) {
	fileJournal, path := newDurableJournal(t)
	if _, err := fileJournal.ReadAll(context.Background()); err != nil {
		t.Fatalf("read initial journal: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat journal: %v", err)
	}
	limited, err := journal.NewFileJournal(path, journal.FileJournalOptions{
		MaxJournalSize: info.Size(), MaxRecordSize: int(info.Size()), CreateParentDirs: true,
	})
	if err != nil {
		t.Fatalf("new limited journal: %v", err)
	}
	coordinator, _, err := FromJournal(context.Background(), limited)
	if err != nil {
		t.Fatalf("construct limited coordinator: %v", err)
	}
	result, err := coordinator.AddChain(context.Background(), newDurableChain(t, "limited"), "writer", "limited", durableTestBase.Add(time.Second))
	var appendFailure AppendFailure
	if err == nil || !errors.As(err, &appendFailure) || appendFailure.Outcome != AppendRejected || !errors.Is(err, ErrJournalAppendFailed) {
		t.Fatalf("expected clean rejected append, result=%#v err=%v failure=%#v", result, err, appendFailure)
	}
	if coordinator.Status().State != StateReady || coordinator.Count() != 0 || readDurableJournal(t, limited).RecordCount != 1 {
		t.Fatalf("clean append failure changed state: status=%#v", coordinator.Status())
	}
}

func TestCoordinatorCloseRejectsFurtherOperations(t *testing.T) {
	coordinator, _ := newDurableCoordinator(t)
	if err := coordinator.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := coordinator.Get("missing"); err == nil || !errors.Is(err, ErrCoordinatorClosed) {
		t.Fatalf("expected closed get rejection, got %v", err)
	}
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "closed"), "writer", "closed", durableTestBase); err == nil || !errors.Is(err, ErrCoordinatorClosed) {
		t.Fatalf("expected closed add rejection, got %v", err)
	}
}

func TestCoordinatorSnapshotCheckpointManifestOrderAndRecovery(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	store, err := generations.NewStore(t.TempDir(), generations.StoreOptions{})
	if err != nil {
		t.Fatalf("new generation store: %v", err)
	}
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "snapshotted"), "writer", "add-snapshot", durableTestBase.Add(time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	result, err := coordinator.CreateSnapshotGeneration(context.Background(), store, durableTestBase.Add(time.Hour), "snapshotter", "snapshot-1")
	if err != nil {
		t.Fatalf("create generation: %v", err)
	}
	if !result.SnapshotWritten || !result.CheckpointAppended || !result.ManifestPublished || result.Pending != nil || result.Generation.CheckpointRecordSequence != 3 {
		t.Fatalf("unexpected generation result: %#v", result)
	}
	status := coordinator.Status()
	if status.State != StateReady || status.JournalSequence != 3 || coordinator.Count() != 1 {
		t.Fatalf("unexpected status after checkpoint: %#v", status)
	}
	manifest, err := store.LoadManifest(context.Background())
	if err != nil || !reflect.DeepEqual(manifest.Active, result.Generation) {
		t.Fatalf("active manifest=%#v err=%v", manifest, err)
	}
	recovered, _, err := FromGenerationManifest(context.Background(), store, fileJournal)
	if err != nil {
		t.Fatalf("recover from manifest: %v", err)
	}
	if !reflect.DeepEqual(recovered.List(), coordinator.List()) {
		t.Fatal("manifest recovery differs from coordinator")
	}
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "after-snapshot"), "writer", "add-after-snapshot", durableTestBase.Add(2*time.Hour)); err != nil {
		t.Fatalf("add after snapshot: %v", err)
	}
	recoveredWithDelta, _, err := FromGenerationManifest(context.Background(), store, fileJournal)
	if err != nil {
		t.Fatalf("recover old generation with post-checkpoint delta: %v", err)
	}
	if !reflect.DeepEqual(recoveredWithDelta.List(), coordinator.List()) {
		t.Fatal("old active generation plus journal delta differs from coordinator")
	}
	oldGeneration := result.Generation
	second, err := coordinator.CreateSnapshotGeneration(context.Background(), store, durableTestBase.Add(3*time.Hour), "snapshotter", "snapshot-2")
	if err != nil {
		t.Fatalf("create second generation: %v", err)
	}
	if second.Generation.GenerationID == oldGeneration.GenerationID || !second.ManifestPublished {
		t.Fatalf("second generation was not distinct and published: %#v", second)
	}
	if _, _, err := store.LoadGeneration(context.Background(), oldGeneration); err != nil {
		t.Fatalf("old generation was removed: %v", err)
	}
	if read := readDurableJournal(t, fileJournal); read.HeadSequence != 5 {
		t.Fatalf("unexpected journal head after two checkpoints: %#v", read)
	}
}

func TestCoordinatorSnapshotOrphanAfterCleanCheckpointFailure(t *testing.T) {
	_, path := newDurableJournal(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat journal: %v", err)
	}
	limit := info.Size() + 480
	limited, err := journal.NewFileJournal(path, journal.FileJournalOptions{MaxJournalSize: limit, MaxRecordSize: int(limit), CreateParentDirs: true})
	if err != nil {
		t.Fatalf("new limited journal: %v", err)
	}
	coordinator, _, err := FromJournal(context.Background(), limited)
	if err != nil {
		t.Fatalf("construct limited coordinator: %v", err)
	}
	store, err := generations.NewStore(t.TempDir(), generations.StoreOptions{})
	if err != nil {
		t.Fatalf("new generation store: %v", err)
	}
	result, err := coordinator.CreateSnapshotGeneration(context.Background(), store, durableTestBase.Add(time.Hour), "snapshotter", "orphan")
	if err == nil || !errors.Is(err, journal.ErrJournalTooLarge) || !result.SnapshotWritten || result.CheckpointAppended || result.ManifestPublished {
		t.Fatalf("expected orphan snapshot after rejected checkpoint: result=%#v err=%v", result, err)
	}
	if coordinator.Status().State != StateReady || coordinator.Status().JournalSequence != 1 {
		t.Fatalf("clean checkpoint failure degraded coordinator: %#v", coordinator.Status())
	}
	if _, err := store.LoadManifest(context.Background()); !errors.Is(err, generations.ErrManifestNotFound) {
		t.Fatalf("orphan snapshot unexpectedly became active: %v", err)
	}
	if readDurableJournal(t, limited).RecordCount != 1 {
		t.Fatalf("rejected checkpoint changed journal: %#v", readDurableJournal(t, limited))
	}
}

func TestObservationAfterGenerationCheckpointReplaysFromManifest(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "generation-observation"), "writer", "generation-chain", durableTestBase.Add(time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	store, err := generations.NewStore(t.TempDir(), generations.StoreOptions{})
	if err != nil {
		t.Fatalf("new generation store: %v", err)
	}
	if result, err := coordinator.CreateSnapshotGeneration(context.Background(), store, durableTestBase.Add(time.Hour), "snapshotter", "generation-snapshot"); err != nil || !result.ManifestPublished {
		t.Fatalf("create generation: result=%#v err=%v", result, err)
	}
	before, err := coordinator.Get("generation-observation")
	if err != nil {
		t.Fatalf("get generation chain: %v", err)
	}
	command := chains.AddObservationCommand{
		ChainID: "generation-observation", SourceRevision: before.Revision,
		Observation: chains.ObservationRef{ID: "post-checkpoint-observation", EventType: "vision.motion", Timestamp: durableTestBase.Add(2 * time.Hour), NodeID: "hall"},
		Mutation:    chains.MutationContext{At: durableTestBase.Add(2*time.Hour + time.Minute), Actor: "observer", Reason: "post-checkpoint observation", CorrelationID: "post-checkpoint-observation"},
	}
	if _, err := coordinator.AddObservation(context.Background(), command, durableTestBase.Add(2*time.Hour+2*time.Minute)); err != nil {
		t.Fatalf("add post-checkpoint observation: %v", err)
	}
	recovered, _, err := FromGenerationManifest(context.Background(), store, fileJournal)
	if err != nil {
		t.Fatalf("recover generation with observation delta: %v", err)
	}
	journalSnapshot := readDurableJournal(t, fileJournal)
	journalOnly, _, err := replay.FromJournal(context.Background(), journalSnapshot)
	if err != nil {
		t.Fatalf("journal-only replay: %v", err)
	}
	if !reflect.DeepEqual(recovered.List(), coordinator.List()) || !reflect.DeepEqual(recovered.List(), journalOnly.List()) {
		t.Fatalf("generation replay differs: recovered=%#v coordinator=%#v journal=%#v", recovered.List(), coordinator.List(), journalOnly.List())
	}
}

func TestCoordinatorManifestFailurePreservesOldManifestAndReadyState(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "manifest-old"), "writer", "manifest-old", durableTestBase.Add(time.Second)); err != nil {
		t.Fatalf("add initial chain: %v", err)
	}
	root := t.TempDir()
	store, err := generations.NewStore(root, generations.StoreOptions{})
	if err != nil {
		t.Fatalf("new generation store: %v", err)
	}
	first, err := coordinator.CreateSnapshotGeneration(context.Background(), store, durableTestBase.Add(time.Hour), "snapshotter", "manifest-first")
	if err != nil {
		t.Fatalf("create first generation: %v", err)
	}
	oldManifest, err := store.LoadManifest(context.Background())
	if err != nil {
		t.Fatalf("load old manifest: %v", err)
	}
	if !reflect.DeepEqual(oldManifest.Active, first.Generation) {
		t.Fatalf("unexpected old manifest: %#v", oldManifest)
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("make manifest directory read-only: %v", err)
	}
	result, err := coordinator.CreateSnapshotGeneration(context.Background(), store, durableTestBase.Add(2*time.Hour), "snapshotter", "manifest-second")
	_ = os.Chmod(root, 0o700)
	if err == nil || !errors.Is(err, generations.ErrManifestWriteFailed) || !result.SnapshotWritten || !result.CheckpointAppended || result.ManifestPublished {
		t.Fatalf("expected clean manifest failure: result=%#v err=%v", result, err)
	}
	if status := coordinator.Status(); status.State != StateReady || status.JournalSequence != first.Generation.CheckpointRecordSequence+1 {
		t.Fatalf("manifest failure changed coordinator readiness: %#v", status)
	}
	currentManifest, err := store.LoadManifest(context.Background())
	if err != nil || !reflect.DeepEqual(currentManifest, oldManifest) {
		t.Fatalf("old manifest was not preserved: manifest=%#v err=%v old=%#v", currentManifest, err, oldManifest)
	}
	recovered, _, err := FromGenerationManifest(context.Background(), store, fileJournal)
	if err != nil {
		t.Fatalf("old manifest recovery after checkpoint: %v", err)
	}
	if !reflect.DeepEqual(recovered.List(), coordinator.List()) {
		t.Fatal("old manifest plus replay did not recover the current registry")
	}
}

func TestCoordinatorLifecycleUsesExactDomainRevisionAndWAL(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "lifecycle"), "writer", "add-life", durableTestBase.Add(time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	before, err := coordinator.Get(chains.ChainID("lifecycle"))
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	evaluation, err := chains.EvaluateLifecycle(before, durableTestBase.Add(2*time.Hour), chains.DefaultLifecyclePolicy())
	if err != nil || evaluation.Proposal == nil {
		t.Fatalf("evaluate lifecycle: evaluation=%#v err=%v", evaluation, err)
	}
	proposal := *evaluation.Proposal
	result, err := coordinator.ApplyLifecycleProposal(context.Background(), proposal, "lifecycle", "life-transition", proposal.EvaluatedAt)
	if err != nil {
		t.Fatalf("apply lifecycle: %v", err)
	}
	if !result.Published || result.Before == nil || result.After.Status != chains.StatusInvalidated || result.After.Revision != before.Revision+1 {
		t.Fatalf("unexpected lifecycle result: %#v", result)
	}
	read := readDurableJournal(t, fileJournal)
	if read.RecordCount != 3 {
		t.Fatalf("record count=%d want 3", read.RecordCount)
	}
	var payload journal.LifecycleTransitionPayload
	if err := json.Unmarshal(read.Records[2].Payload, &payload); err != nil {
		t.Fatalf("decode transition: %v", err)
	}
	if payload.PreviousRevision != before.Revision || payload.NewRevision != result.After.Revision || payload.From != before.Status || payload.To != result.After.Status || !reflect.DeepEqual(payload.Revision, result.After.History[len(result.After.History)-1]) {
		t.Fatalf("journal transition differs from domain result: payload=%#v result=%#v", payload, result)
	}
	replayed, _, err := replay.FromJournal(context.Background(), read)
	if err != nil {
		t.Fatalf("replay durable journal: %v", err)
	}
	if !reflect.DeepEqual(replayed.List(), coordinator.List()) {
		t.Fatalf("coordinator differs from replayed journal")
	}
}

func TestCoordinatorRejectsStaleLifecycleWithoutAppend(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "stale"), "writer", "add-stale", durableTestBase.Add(time.Second)); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	snapshot, _ := coordinator.Get(chains.ChainID("stale"))
	evaluation, err := chains.EvaluateLifecycle(snapshot, durableTestBase.Add(2*time.Hour), chains.DefaultLifecyclePolicy())
	if err != nil || evaluation.Proposal == nil {
		t.Fatalf("evaluate: %#v %v", evaluation, err)
	}
	proposal := *evaluation.Proposal
	if _, err := coordinator.ApplyLifecycleProposal(context.Background(), proposal, "writer", "first", proposal.EvaluatedAt); err != nil {
		t.Fatalf("first transition: %v", err)
	}
	before := coordinator.Status()
	if _, err := coordinator.ApplyLifecycleProposal(context.Background(), proposal, "writer", "second", proposal.EvaluatedAt); err == nil || !errors.Is(err, ErrMutationPrepareFailed) {
		t.Fatalf("expected stale proposal rejection, got %v", err)
	}
	if !reflect.DeepEqual(before, coordinator.Status()) || readDurableJournal(t, fileJournal).RecordCount != 3 {
		t.Fatalf("stale proposal mutated state: status=%#v journal=%#v", coordinator.Status(), readDurableJournal(t, fileJournal))
	}
}

func TestCoordinatorDegradesAfterPublicationFailureAndRecovers(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	coordinator.publishHook = func() error { return errors.New("injected publication failure") }
	result, err := coordinator.AddChain(context.Background(), newDurableChain(t, "after-crash"), "writer", "add-crash", durableTestBase.Add(time.Second))
	if err == nil || !errors.Is(err, ErrPublicationFailed) || result.Published || coordinator.Count() != 0 {
		t.Fatalf("expected degraded publication failure: result=%#v status=%#v err=%v", result, coordinator.Status(), err)
	}
	status := coordinator.Status()
	if status.State != StateDegraded || status.JournalSequence != 2 || status.DegradedReason != "publication_failed" {
		t.Fatalf("unexpected degraded status: %#v", status)
	}
	if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "blocked"), "writer", "blocked", durableTestBase.Add(2*time.Second)); err == nil || !errors.Is(err, ErrCoordinatorDegraded) {
		t.Fatalf("expected mutation refusal while degraded, got %v", err)
	}
	coordinator.publishHook = nil
	recovery, err := coordinator.RecoverFromJournal(context.Background())
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovery.State != StateReady || coordinator.Status().State != StateReady || coordinator.Count() != 1 {
		t.Fatalf("unexpected recovery result: metadata=%#v status=%#v", recovery, coordinator.Status())
	}
	if snapshot, err := coordinator.Get(chains.ChainID("after-crash")); err != nil || snapshot.Revision != result.After.Revision {
		t.Fatalf("recovered mutation missing: snapshot=%#v err=%v", snapshot, err)
	}
	if readDurableJournal(t, fileJournal).RecordCount != 2 {
		t.Fatalf("recovery changed journal: %#v", readDurableJournal(t, fileJournal))
	}
}

func TestCoordinatorConcurrentConflictsAndReplay(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	const workers = 12
	var wait sync.WaitGroup
	results := make(chan error, workers)
	for index := 0; index < workers; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := coordinator.AddChain(context.Background(), newDurableChain(t, "concurrent-"+strconv.Itoa(index)), "writer", "concurrent-"+strconv.Itoa(index), durableTestBase.Add(time.Duration(index+1)*time.Second))
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent distinct add: %v", err)
		}
	}
	if coordinator.Count() != workers {
		t.Fatalf("count=%d want %d", coordinator.Count(), workers)
	}

	duplicateResults := make(chan error, 2)
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := coordinator.AddChain(context.Background(), newDurableChain(t, "same"), "writer", "same", durableTestBase.Add(time.Minute))
			duplicateResults <- err
		}()
	}
	wait.Wait()
	close(duplicateResults)
	successes := 0
	for err := range duplicateResults {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrMutationPrepareFailed) {
			t.Fatalf("unexpected duplicate result: %v", err)
		}
	}
	if successes != 1 || coordinator.Count() != workers+1 {
		t.Fatalf("duplicate coordination successes=%d count=%d", successes, coordinator.Count())
	}
	read := readDurableJournal(t, fileJournal)
	if read.RecordCount != uint64(1+workers+1) {
		t.Fatalf("journal count=%d want %d", read.RecordCount, 1+workers+1)
	}
	replayed, _, err := replay.FromJournal(context.Background(), read)
	if err != nil {
		t.Fatalf("replay concurrent journal: %v", err)
	}
	if !reflect.DeepEqual(replayed.List(), coordinator.List()) {
		t.Fatal("concurrent coordinator differs from replay")
	}
}

func TestCoordinatorReadersDuringWritesSeeOnlyPublishedSnapshots(t *testing.T) {
	coordinator, _ := newDurableCoordinator(t)
	const writers = 40
	var wait sync.WaitGroup
	for reader := 0; reader < 6; reader++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < writers*3; iteration++ {
				list := coordinator.List()
				for _, snapshot := range list {
					if _, err := chains.Restore(snapshot); err != nil {
						t.Errorf("reader observed invalid snapshot: %v", err)
					}
				}
				_ = coordinator.Status()
			}
		}()
	}
	for index := 0; index < writers; index++ {
		if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, "reader-writer-"+strconv.Itoa(index)), "writer", "reader-writer-"+strconv.Itoa(index), durableTestBase.Add(time.Duration(index+1)*time.Second)); err != nil {
			t.Fatalf("write %d: %v", index, err)
		}
	}
	wait.Wait()
	if coordinator.Count() != writers {
		t.Fatalf("final count=%d want %d", coordinator.Count(), writers)
	}
}

func TestCoordinatorVolumeAndFinalReplay(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	const chainCount = 500
	for index := 0; index < chainCount; index++ {
		id := "volume-" + strconv.Itoa(index)
		if _, err := coordinator.AddChain(context.Background(), newDurableChain(t, id), "volume", "add-"+id, durableTestBase.Add(time.Duration(index+1)*time.Second)); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	read := readDurableJournal(t, fileJournal)
	replayed, _, err := replay.FromJournal(context.Background(), read)
	if err != nil {
		t.Fatalf("replay volume journal: %v", err)
	}
	if coordinator.Count() != chainCount || replayed.Count() != chainCount || !reflect.DeepEqual(replayed.List(), coordinator.List()) {
		t.Fatalf("volume replay mismatch: coordinated=%d replayed=%d", coordinator.Count(), replayed.Count())
	}
}
