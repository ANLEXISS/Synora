package evidence

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

func TestDefaultBatchOptionsAndValidation(t *testing.T) {
	options := DefaultBatchOptions()
	if err := options.Validate(); err != nil {
		t.Fatal(err)
	}
	if options.MaxChains != 1000 || options.MaxObservationsPerChain != 256 || !options.IncludeActive || !options.IncludeHistorical {
		t.Fatalf("unexpected defaults: %#v", options)
	}
	for _, invalid := range []BatchOptions{
		{MaxChains: 0, MaxObservationsPerChain: 1, IncludeActive: true},
		{MaxChains: 1, MaxObservationsPerChain: 0, IncludeActive: true},
		{MaxChains: 1, MaxObservationsPerChain: 1},
	} {
		if !errors.Is(invalid.Validate(), ErrInvalidEvidenceBatchOptions) {
			t.Fatalf("expected invalid options: %#v", invalid)
		}
	}
}

func TestEvaluateBatchDeterministicSelectionAndDeferral(t *testing.T) {
	first := observation("first", "vision.identity", "person-a", "sequence-a", testBase.Add(time.Minute))
	second := observation("second", "vision.identity", "person-a", "sequence-a", testBase.Add(2*time.Minute))
	third := observation("third", "vision.identity", "person-a", "sequence-a", testBase.Add(3*time.Minute))
	chainA := testChain(t, "chain-a", "person-a", chains.StatusActive, first, second, third)
	unsupported := observation("unsupported", "vision.weapon", "", "", testBase.Add(time.Minute))
	chainB := testChain(t, "chain-b", "", chains.StatusCandidate, unsupported)

	options := DefaultBatchOptions()
	batch, err := EvaluateBatch([]chains.Snapshot{chainB.Snapshot(), chainA.Snapshot()}, testBase.Add(10*time.Minute), DefaultPolicy(), options)
	if err != nil {
		t.Fatal(err)
	}
	if batch.CapturedChainCount != 2 || batch.SelectedChainCount != 2 || len(batch.ChainResults) != 2 || len(batch.Proposals) != 1 {
		t.Fatalf("unexpected batch shape: %#v", batch)
	}
	if batch.ChainResults[0].ChainID != "chain-a" || batch.ChainResults[1].ChainID != "chain-b" {
		t.Fatalf("chains are not ordered: %#v", batch.ChainResults)
	}
	chainResult := batch.ChainResults[0]
	if chainResult.SelectedProposal == nil || chainResult.ObservationsConsidered != 3 || chainResult.ObservationsEvaluated != 1 || chainResult.ObservationsDeferred != 2 {
		t.Fatalf("unexpected deferral: %#v", chainResult)
	}
	if chainResult.Results[0].TargetObservationID != "first" || !chainResult.Results[1].Deferred || chainResult.Results[1].ErrorCode != "deferred_after_selected_proposal" {
		t.Fatalf("observations are not ordered/deferred: %#v", chainResult.Results)
	}
	if batch.ChainResults[1].Results[0].ErrorCode != "target_evaluation_failed" || batch.EvaluationErrors != 1 {
		t.Fatalf("isolated evaluation error missing: %#v", batch)
	}

	copyBatch, err := EvaluateBatch([]chains.Snapshot{chainB.Snapshot(), chainA.Snapshot()}, testBase.Add(10*time.Minute), DefaultPolicy(), options)
	if err != nil || !reflect.DeepEqual(batch, copyBatch) {
		t.Fatalf("batch is not deterministic: first=%#v second=%#v err=%v", batch, copyBatch, err)
	}
	batch.Proposals[0].Contribution.ObservationIDs[0] = "mutated"
	again, err := EvaluateBatch([]chains.Snapshot{chainA.Snapshot()}, testBase.Add(10*time.Minute), DefaultPolicy(), options)
	if err != nil || again.Proposals[0].Contribution.ObservationIDs[0] == "mutated" {
		t.Fatal("batch returned shared proposal data")
	}
}

func TestEvaluateBatchLimitsHistoryAndEmpty(t *testing.T) {
	active := testChain(t, "active", "", chains.StatusActive, observation("active-observation", "vision.unknown", "", "sequence-active", testBase.Add(time.Minute)))
	historical := testChain(t, "historical", "", chains.StatusActive, observation("historical-observation", "vision.unknown", "", "sequence-historical", testBase.Add(time.Minute)))
	if err := historical.SetStatus(chains.StatusDeclining, chains.MutationContext{At: testBase.Add(22 * time.Minute), Actor: "test", Reason: "decline", CorrelationID: "decline"}); err != nil {
		t.Fatal(err)
	}
	if err := historical.SetStatus(chains.StatusArchived, chains.MutationContext{At: testBase.Add(23 * time.Minute), Actor: "test", Reason: "archive", CorrelationID: "archive"}); err != nil {
		t.Fatal(err)
	}
	activeOnly := DefaultBatchOptions()
	activeOnly.IncludeHistorical = false
	batch, err := EvaluateBatch([]chains.Snapshot{historical.Snapshot(), active.Snapshot()}, testBase.Add(10*time.Minute), DefaultPolicy(), activeOnly)
	if err != nil || batch.SelectedChainCount != 1 || batch.ChainResults[0].ChainID != "active" {
		t.Fatalf("historical filtering failed: %#v err=%v", batch, err)
	}
	empty, err := EvaluateBatch(nil, testBase.Add(time.Minute), DefaultPolicy(), DefaultBatchOptions())
	if err != nil || empty.CapturedChainCount != 0 || empty.SelectedChainCount != 0 || len(empty.Proposals) != 0 {
		t.Fatalf("unexpected empty batch: %#v err=%v", empty, err)
	}
	bad := active.Snapshot()
	bad.ID = "bad"
	bad.OccurrenceCount++
	withBad, err := EvaluateBatch([]chains.Snapshot{bad, active.Snapshot()}, testBase.Add(10*time.Minute), DefaultPolicy(), DefaultBatchOptions())
	if err != nil || withBad.EvaluationErrors != 1 || withBad.ChainResults[1].ErrorCode != "invalid_chain_snapshot" {
		t.Fatalf("invalid chain was not isolated: %#v err=%v", withBad, err)
	}
	if _, err := EvaluateBatch([]chains.Snapshot{active.Snapshot(), active.Snapshot()}, testBase.Add(10*time.Minute), DefaultPolicy(), DefaultBatchOptions()); !errors.Is(err, ErrInvalidEvidenceBatch) {
		t.Fatalf("duplicate chain snapshots did not fail globally: %v", err)
	}
}

func TestEvaluateBatchAlreadyEvaluatedAndGlobalValidation(t *testing.T) {
	target := observation("batch-target", "vision.identity", "person-a", "batch-sequence", testBase.Add(2*time.Minute))
	context := observation("batch-context", "vision.identity", "person-a", "batch-sequence", testBase.Add(time.Minute))
	chain := testChain(t, "batch-idempotent", "person-a", chains.StatusActive, context, target)
	first, err := EvaluateBatch([]chains.Snapshot{chain.Snapshot()}, testBase.Add(10*time.Minute), DefaultPolicy(), DefaultBatchOptions())
	if err != nil || len(first.Proposals) != 1 {
		t.Fatalf("first batch proposal missing: %#v err=%v", first, err)
	}
	proposal := first.Proposals[0]
	if proposal.Contribution.ID == "" {
		t.Fatalf("batch proposal lost contribution: %#v", proposal)
	}
	if err := chain.AddContribution(proposal.Contribution, chains.MutationContext{At: testBase.Add(30 * time.Minute), Actor: "test", Reason: "explicit contribution", CorrelationID: "explicit", ObservationIDs: proposal.Contribution.ObservationIDs}); err != nil {
		t.Fatal(err)
	}
	second, err := EvaluateBatch([]chains.Snapshot{chain.Snapshot()}, testBase.Add(31*time.Minute), DefaultPolicy(), DefaultBatchOptions())
	if err != nil || second.AlreadyEvaluated == 0 || len(second.Proposals) == 0 {
		// The next observation may still yield the next explicit proposal; the
		// first observation must nevertheless be reported as already evaluated.
		if err != nil || second.AlreadyEvaluated == 0 {
			t.Fatalf("already evaluated result missing: %#v err=%v", second, err)
		}
	}

	invalidPolicy := DefaultPolicy()
	invalidPolicy.Namespace = ""
	if _, err := EvaluateBatch([]chains.Snapshot{chain.Snapshot()}, testBase, invalidPolicy, DefaultBatchOptions()); !errors.Is(err, ErrInvalidEvidencePolicy) {
		t.Fatalf("invalid policy did not fail globally: %v", err)
	}
}

func TestEvaluateBatchVolume(t *testing.T) {
	snapshots := make([]chains.Snapshot, 0, 500)
	for index := 0; index < 500; index++ {
		id := chains.ChainID(fmt.Sprintf("volume-%03d", index))
		first := observation(fmt.Sprintf("volume-first-%03d", index), "vision.identity", "person-a", string(id), testBase.Add(time.Minute))
		second := observation(fmt.Sprintf("volume-second-%03d", index), "vision.identity", "person-a", string(id), testBase.Add(2*time.Minute))
		chain := testChain(t, id, "person-a", chains.StatusActive, first, second)
		snapshots = append(snapshots, chain.Snapshot())
	}
	batch, err := EvaluateBatch(snapshots, testBase.Add(10*time.Minute), DefaultPolicy(), DefaultBatchOptions())
	if err != nil || batch.SelectedChainCount != 500 || len(batch.Proposals) != 500 {
		t.Fatalf("unexpected volume batch: selected=%d proposals=%d err=%v", batch.SelectedChainCount, len(batch.Proposals), err)
	}
	for index := 1; index < len(batch.Proposals); index++ {
		if batch.Proposals[index-1].ChainID >= batch.Proposals[index].ChainID {
			t.Fatalf("volume proposals are not ordered at %d", index)
		}
	}
}
