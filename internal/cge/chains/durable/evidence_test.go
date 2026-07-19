package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/replay"
)

func durableEvidenceChain(t *testing.T, id string) *chains.Chain {
	t.Helper()
	chain := newDurableChain(t, id)
	base := durableTestBase
	first := chains.ObservationRef{ID: id + "-first", EventType: "vision.identity", Timestamp: base.Add(time.Minute), EntityID: "person-a", SequenceKey: id + "-sequence", ActivationID: id + "-activation", TrackID: id + "-track", DeviceID: "device-1", NodeID: "node-1"}
	second := chains.ObservationRef{ID: id + "-second", EventType: "vision.identity", Timestamp: base.Add(2 * time.Minute), EntityID: "person-a", SequenceKey: id + "-sequence", ActivationID: id + "-activation", TrackID: id + "-track", DeviceID: "device-1", NodeID: "node-2"}
	if err := chain.AddObservation(first, durableMutation(base.Add(time.Minute+time.Second), "builder", "first", "first")); err != nil {
		t.Fatal(err)
	}
	if err := chain.AddObservation(second, durableMutation(base.Add(2*time.Minute+time.Second), "builder", "second", "second")); err != nil {
		t.Fatal(err)
	}
	if err := chain.AssignEntity("person-a", durableMutation(base.Add(3*time.Minute), "builder", "assign", "assign")); err != nil {
		t.Fatal(err)
	}
	return chain
}

func TestCoordinatorEvaluateAndApplyEvidenceBatch(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	chain := durableEvidenceChain(t, "batch-durable")
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "chain", durableTestBase.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	batch, err := coordinator.EvaluateEvidenceBatch(durableTestBase.Add(20*time.Minute), evidence.DefaultPolicy(), evidence.DefaultBatchOptions())
	if err != nil || len(batch.Proposals) != 1 || batch.ChainResults[0].ObservationsDeferred != 1 {
		t.Fatalf("unexpected durable evidence batch: %#v err=%v", batch, err)
	}
	proposal := batch.Proposals[0]
	result, err := coordinator.ApplyEvidenceProposals(context.Background(), []evidence.ContributionProposal{proposal}, "evidence-writer", "batch-correlation", durableTestBase.Add(21*time.Minute), durableTestBase.Add(22*time.Minute))
	if err != nil || result.Applied != 1 || result.Results[0].JournalSequence != 3 || !result.Results[0].Applied {
		t.Fatalf("unexpected application: %#v err=%v", result, err)
	}
	stored, err := coordinator.Get(chain.Snapshot().ID)
	if err != nil || stored.Revision != chain.Snapshot().Revision+1 || len(stored.Contributions) != 1 {
		t.Fatalf("contribution was not published: %#v err=%v", stored, err)
	}
	read := readDurableJournal(t, fileJournal)
	if read.HeadSequence != 3 || string(read.Records[2].Kind) != "chain.contribution_added" {
		t.Fatalf("unexpected journal after batch application: %#v", read)
	}
	replayed, _, err := replay.FromJournal(context.Background(), read)
	if err != nil || !reflect.DeepEqual(replayed.List(), coordinator.List()) {
		t.Fatalf("replay differs after batch application: replay=%#v current=%#v err=%v", replayed.List(), coordinator.List(), err)
	}

	idempotent, err := coordinator.ApplyEvidenceProposals(context.Background(), []evidence.ContributionProposal{proposal}, "evidence-writer", "batch-correlation", durableTestBase.Add(23*time.Minute), durableTestBase.Add(24*time.Minute))
	if err != nil || idempotent.Idempotent != 1 || idempotent.Applied != 0 || readDurableJournal(t, fileJournal).HeadSequence != 3 {
		t.Fatalf("repeat application was not idempotent: %#v err=%v", idempotent, err)
	}
}

func TestApplyEvidenceProposalsRejectsGlobalDuplicatesBeforeMutation(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	chain := durableEvidenceChain(t, "duplicate-batch")
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "chain", durableTestBase.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	batch, err := coordinator.EvaluateEvidenceBatch(durableTestBase.Add(20*time.Minute), evidence.DefaultPolicy(), evidence.DefaultBatchOptions())
	if err != nil || len(batch.Proposals) != 1 {
		t.Fatalf("proposal missing: %#v err=%v", batch, err)
	}
	before := readDurableJournal(t, fileJournal)
	result, err := coordinator.ApplyEvidenceProposals(context.Background(), []evidence.ContributionProposal{batch.Proposals[0], batch.Proposals[0]}, "writer", "duplicate", durableTestBase.Add(21*time.Minute), durableTestBase.Add(22*time.Minute))
	if err == nil || !errors.Is(err, ErrDuplicateChainProposal) || result.Invalid != 2 || readDurableJournal(t, fileJournal).HeadSequence != before.HeadSequence {
		t.Fatalf("duplicate chain proposal was not rejected before mutation: result=%#v err=%v", result, err)
	}
}

func TestApplyEvidenceProposalsSortsByChainIDAndKeepsPartialSuccesses(t *testing.T) {
	coordinator, fileJournal := newDurableCoordinator(t)
	chainB := durableEvidenceChain(t, "b-chain")
	chainA := durableEvidenceChain(t, "a-chain")
	if _, err := coordinator.AddChain(context.Background(), chainB, "writer", "chain-b", durableTestBase.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.AddChain(context.Background(), chainA, "writer", "chain-a", durableTestBase.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	batch, err := coordinator.EvaluateEvidenceBatch(durableTestBase.Add(20*time.Minute), evidence.DefaultPolicy(), evidence.DefaultBatchOptions())
	if err != nil || len(batch.Proposals) != 2 {
		t.Fatalf("expected two proposals: %#v err=%v", batch, err)
	}
	proposals := []evidence.ContributionProposal{batch.Proposals[1], batch.Proposals[0]}
	result, err := coordinator.ApplyEvidenceProposals(context.Background(), proposals, "writer", "ordered-batch", durableTestBase.Add(21*time.Minute), durableTestBase.Add(22*time.Minute))
	if err != nil || result.Applied != 2 || len(result.Results) != 2 || result.Results[0].ChainID != "a-chain" || result.Results[1].ChainID != "b-chain" {
		t.Fatalf("proposals were not applied in ChainID order: %#v err=%v", result, err)
	}
	read := readDurableJournal(t, fileJournal)
	var firstPayload, secondPayload journal.ContributionAddedPayload
	if err := json.Unmarshal(read.Records[3].Payload, &firstPayload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(read.Records[4].Payload, &secondPayload); err != nil {
		t.Fatal(err)
	}
	if firstPayload.ChainID != "a-chain" || secondPayload.ChainID != "b-chain" || read.Records[3].CorrelationID != "ordered-batch:0001" || read.Records[4].CorrelationID != "ordered-batch:0002" {
		t.Fatalf("journal order/correlation is not deterministic: first=%#v second=%#v records=%#v", firstPayload, secondPayload, read.Records[3:5])
	}
}

func TestEvidenceBatchStaleAndConcurrentApplication(t *testing.T) {
	coordinator, _ := newDurableCoordinator(t)
	chain := durableEvidenceChain(t, "stale-batch")
	if _, err := coordinator.AddChain(context.Background(), chain, "writer", "chain", durableTestBase.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	batch, err := coordinator.EvaluateEvidenceBatch(durableTestBase.Add(20*time.Minute), evidence.DefaultPolicy(), evidence.DefaultBatchOptions())
	if err != nil || len(batch.Proposals) != 1 {
		t.Fatalf("proposal missing: %#v err=%v", batch, err)
	}
	proposal := batch.Proposals[0]
	unrelated := chains.ConfidenceContribution{ID: "unrelated", Source: "review", Kind: chains.ContributionNeutral, Value: 0, ObservationIDs: proposal.Contribution.ObservationIDs, Reason: "unrelated", CreatedAt: durableTestBase.Add(21 * time.Minute)}
	command := chains.AddContributionCommand{ChainID: proposal.ChainID, SourceRevision: proposal.SourceRevision, Contribution: unrelated, Mutation: durableMutation(durableTestBase.Add(21*time.Minute), "reviewer", "unrelated", "unrelated")}
	if _, err := coordinator.AddContribution(context.Background(), command, durableTestBase.Add(22*time.Minute)); err != nil {
		t.Fatal(err)
	}
	stale, err := coordinator.ApplyEvidenceProposals(context.Background(), []evidence.ContributionProposal{proposal}, "writer", "stale", durableTestBase.Add(23*time.Minute), durableTestBase.Add(24*time.Minute))
	if err != nil || stale.Stale != 1 || stale.Results[0].ErrorCode != "evidence_proposal_stale" {
		t.Fatalf("stale proposal was not rejected: %#v err=%v", stale, err)
	}

	secondChain := durableEvidenceChain(t, "concurrent-batch")
	if _, err := coordinator.AddChain(context.Background(), secondChain, "writer", "second-chain", durableTestBase.Add(25*time.Minute)); err != nil {
		t.Fatal(err)
	}
	secondBatch, err := coordinator.EvaluateEvidenceBatch(durableTestBase.Add(30*time.Minute), evidence.DefaultPolicy(), evidence.DefaultBatchOptions())
	if err != nil || len(secondBatch.Proposals) == 0 {
		t.Fatalf("second proposal missing: %#v err=%v", secondBatch, err)
	}
	var wait sync.WaitGroup
	results := make([]EvidenceApplyBatchResult, 2)
	wait.Add(2)
	for index := range results {
		go func(index int) {
			defer wait.Done()
			results[index], _ = coordinator.ApplyEvidenceProposals(context.Background(), []evidence.ContributionProposal{secondBatch.Proposals[0]}, "writer", "concurrent", durableTestBase.Add(31*time.Minute), durableTestBase.Add(32*time.Minute))
		}(index)
	}
	wait.Wait()
	if results[0].Applied+results[1].Applied != 1 || results[0].Idempotent+results[1].Idempotent+results[0].Stale+results[1].Stale != 1 {
		t.Fatalf("concurrent application did not produce one durable effect and one idempotent/stale result: %#v", results)
	}
}

func TestEvaluateEvidenceBatchAllowedInDegradedButApplicationBlocked(t *testing.T) {
	coordinator, _ := newDurableCoordinator(t)
	coordinator.state = StateDegraded
	coordinator.degradedReason = "test"
	if _, err := coordinator.EvaluateEvidenceBatch(durableTestBase.Add(time.Minute), evidence.DefaultPolicy(), evidence.DefaultBatchOptions()); err != nil {
		t.Fatalf("degraded coordinator should remain readable: %v", err)
	}
	result, err := coordinator.ApplyEvidenceProposals(context.Background(), nil, "writer", "empty", durableTestBase.Add(time.Minute), durableTestBase.Add(2*time.Minute))
	if err == nil || !errors.Is(err, ErrCoordinatorNotReady) || result.ErrorCode != "coordinator_not_ready" {
		t.Fatalf("degraded application was not blocked: result=%#v err=%v", result, err)
	}
}
