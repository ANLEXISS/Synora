package registry

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

func TestRegistryEvaluateLifecycleReturnsDeterministicDefensiveBatch(t *testing.T) {
	r := New()
	for _, id := range []string{"zeta", "alpha", "middle"} {
		if err := r.Add(newActiveRegistryChain(t, id)); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	evaluatedAt := registryTestBase.Add(2*time.Hour + time.Second)
	policy := chains.DefaultLifecyclePolicy()
	before := r.List()

	batch, err := r.EvaluateLifecycle(evaluatedAt, policy)
	if err != nil {
		t.Fatalf("evaluate registry: %v", err)
	}
	if batch.ChainCount != 3 || batch.EvaluationCount != 3 || batch.ProposalCount != 3 || batch.HealthyCount != 0 || batch.EvaluationErrors != 0 {
		t.Fatalf("unexpected batch counts: %#v", batch)
	}
	if got := batch.EvaluatedAt; !got.Equal(evaluatedAt) || batch.Policy != policy {
		t.Fatalf("batch metadata = %#v", batch)
	}
	assertBatchIDsSorted(t, batch)
	for _, result := range batch.Results {
		if result.Evaluation == nil || result.Evaluation.Proposal == nil {
			t.Fatalf("missing evaluation proposal: %#v", result)
		}
		if result.Evaluation.Proposal.SourceRevision != result.Revision {
			t.Fatalf("source revision = %d, captured revision = %d", result.Evaluation.Proposal.SourceRevision, result.Revision)
		}
	}

	// Evaluation is pure: it neither changes revisions nor applies proposals.
	if after := r.List(); !reflect.DeepEqual(before, after) {
		t.Fatalf("evaluation mutated registry: before=%#v after=%#v", before, after)
	}

	clone := batch.Clone()
	clone.Proposals[0].SupportingFacts[0].Value = "changed"
	clone.Evaluations[0].Proposal.SupportingFacts[0].Value = "changed again"
	clone.Results[0].Evaluation.Proposal.SupportingFacts[0].Value = "changed third time"
	if batch.Proposals[0].SupportingFacts[0].Value == "changed" ||
		batch.Evaluations[0].Proposal.SupportingFacts[0].Value == "changed again" ||
		batch.Results[0].Evaluation.Proposal.SupportingFacts[0].Value == "changed third time" {
		t.Fatal("batch clone shares mutable proposal facts")
	}
}

func TestRegistryEvaluateLifecycleRejectsInvalidGlobalInput(t *testing.T) {
	r := New()
	if _, err := r.EvaluateLifecycle(time.Time{}, chains.DefaultLifecyclePolicy()); err == nil {
		t.Fatal("zero evaluation timestamp must be rejected")
	}
	invalid := chains.DefaultLifecyclePolicy()
	invalid.ActiveDeclineAfter = 0
	if _, err := r.EvaluateLifecycle(registryTestBase.Add(time.Hour), invalid); err == nil || !errors.Is(err, chains.ErrInvalidLifecyclePolicy) {
		t.Fatalf("expected invalid policy error, got %v", err)
	}
}

func TestRegistryEvaluateLifecycleKeepsPerChainErrors(t *testing.T) {
	r := New()
	if err := r.Add(newRegistryChain(t, "valid")); err != nil {
		t.Fatalf("add valid chain: %v", err)
	}
	// This deliberately corrupts only the test registry's private fixture. It
	// verifies that one malformed stored value does not discard other results.
	r.chains[chains.ChainID("invalid")] = &chains.Chain{}

	batch, err := r.EvaluateLifecycle(registryTestBase.Add(time.Minute), chains.DefaultLifecyclePolicy())
	if err != nil {
		t.Fatalf("evaluate mixed registry: %v", err)
	}
	if batch.ChainCount != 2 || batch.EvaluationCount != 2 || batch.EvaluationErrors != 1 || len(batch.Evaluations) != 1 {
		t.Fatalf("unexpected mixed batch counts: %#v", batch)
	}
	var foundError, foundSuccess bool
	for _, result := range batch.Results {
		if result.ErrorCode == CodeEvaluationFailed {
			foundError = true
			if result.Error == "" {
				t.Fatal("evaluation error must be described")
			}
		}
		if result.ChainID == chains.ChainID("valid") && result.Evaluation != nil {
			foundSuccess = true
		}
	}
	if !foundError || !foundSuccess {
		t.Fatalf("per-chain result errors were not retained: %#v", batch.Results)
	}
}

func TestRegistryEvaluateLifecycleVolumeAndStableOrder(t *testing.T) {
	r := New()
	const count = 500
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("candidate-%03d", i)
		if err := r.Add(newRegistryChain(t, id)); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	batch, err := r.EvaluateLifecycle(registryTestBase.Add(chains.DefaultLifecyclePolicy().CandidateTTL), chains.DefaultLifecyclePolicy())
	if err != nil {
		t.Fatalf("evaluate volume: %v", err)
	}
	if batch.ChainCount != count || batch.EvaluationCount != count || batch.ProposalCount != count || batch.EvaluationErrors != 0 {
		t.Fatalf("volume counts = %#v", batch)
	}
	assertBatchIDsSorted(t, batch)
}

func TestRegistryApplyLifecycleBatchIsPartialAndDeterministic(t *testing.T) {
	r := New()
	for _, id := range []string{"beta", "alpha"} {
		if err := r.Add(newActiveRegistryChain(t, id)); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	policy := chains.DefaultLifecyclePolicy()
	batch, err := r.EvaluateLifecycle(registryTestBase.Add(2*time.Hour+time.Second), policy)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(batch.Proposals) != 2 || batch.Proposals[0].ChainID != chains.ChainID("alpha") || batch.Proposals[1].ChainID != chains.ChainID("beta") {
		t.Fatalf("proposals are not deterministic: %#v", batch.Proposals)
	}

	selected := r.ApplyLifecycleProposals([]chains.TransitionProposal{batch.Proposals[1]}, "cge.lifecycle", "batch")
	if selected.Applied != 1 || len(selected.Results) != 1 || !selected.Results[0].Applied {
		t.Fatalf("selected application = %#v", selected)
	}

	result := r.ApplyLifecycleBatch(batch, "cge.lifecycle", "batch")
	if result.ProposalsReceived != 2 || result.Applied != 1 || result.Stale != 1 || result.Invalid != 0 || result.NotFound != 0 {
		t.Fatalf("partial application counters = %#v", result)
	}
	if result.Results[0].ChainID != chains.ChainID("alpha") || !result.Results[0].Applied ||
		result.Results[1].ChainID != chains.ChainID("beta") || result.Results[1].ErrorCode != CodeStaleProposal {
		t.Fatalf("application order/results = %#v", result.Results)
	}
	for _, id := range []chains.ChainID{"alpha", "beta"} {
		snapshot, getErr := r.Get(id)
		if getErr != nil {
			t.Fatalf("get %s: %v", id, getErr)
		}
		if snapshot.Status != chains.StatusDeclining || snapshot.Revision != batch.Proposals[0].SourceRevision+1 {
			t.Fatalf("final %s snapshot = %#v", id, snapshot)
		}
		last := snapshot.History[len(snapshot.History)-1]
		if last.CorrelationID != "batch/"+string(id) || last.Actor != "cge.lifecycle" {
			t.Fatalf("correlation for %s = %#v", id, last)
		}
	}
}

func TestRegistryApplyLifecycleBatchRejectsDuplicateChainProposals(t *testing.T) {
	r := New()
	if err := r.Add(newActiveRegistryChain(t, "duplicate-batch")); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	proposal := activeProposal(t, r, chains.ChainID("duplicate-batch"))
	before, err := r.Get(proposal.ChainID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	result := r.ApplyLifecycleProposals([]chains.TransitionProposal{proposal, proposal}, "test", "duplicate")
	if result.Applied != 0 || result.Invalid != 2 || len(result.Results) != 2 {
		t.Fatalf("duplicate result = %#v", result)
	}
	after, err := r.Get(proposal.ChainID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("duplicate batch mutated chain: before=%#v after=%#v", before, after)
	}
}

func TestRegistryApplyLifecycleBatchMapsErrorsPerProposal(t *testing.T) {
	r := New()
	if err := r.Add(newActiveRegistryChain(t, "errors")); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	proposal := activeProposal(t, r, chains.ChainID("errors"))
	missing := proposal
	missing.ChainID = chains.ChainID("missing")
	badTransition := proposal
	badTransition.To = chains.StatusMerged
	result := r.ApplyLifecycleProposals([]chains.TransitionProposal{missing, badTransition}, "test", "batch")
	if result.ProposalsReceived != 2 || len(result.Results) != 2 || result.NotFound != 1 || result.Invalid != 1 {
		t.Fatalf("mapped errors = %#v", result)
	}
	if result.Results[0].ErrorCode != CodeInvalidTransition {
		t.Fatalf("unexpected first error code: %#v", result.Results)
	}
	if result.Results[1].ErrorCode != CodeChainNotFound {
		t.Fatalf("unexpected second error code: %#v", result.Results)
	}
}

func TestRegistryBatchConcurrentEvaluationAndApplication(t *testing.T) {
	r := New()
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		if err := r.Add(newActiveRegistryChain(t, id)); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	batch, err := r.EvaluateLifecycle(registryTestBase.Add(2*time.Hour+time.Second), chains.DefaultLifecyclePolicy())
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	var wait sync.WaitGroup
	results := make(chan ApplyBatchResult, 2)
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- r.ApplyLifecycleBatch(batch, "test", "concurrent")
		}()
	}
	wait.Wait()
	close(results)
	successes, stale := 0, 0
	for result := range results {
		successes += result.Applied
		stale += result.Stale
	}
	if successes != 2 || stale != 2 {
		t.Fatalf("concurrent batch successes=%d stale=%d, want 2/2", successes, stale)
	}
}

func TestRegistryConcurrentEvaluationAndAddsUseCompleteSnapshots(t *testing.T) {
	r := New()
	for _, id := range []string{"initial-a", "initial-b"} {
		if err := r.Add(newRegistryChain(t, id)); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}

	var wait sync.WaitGroup
	results := make(chan EvaluationBatch, 4)
	for i := 0; i < 4; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			batch, err := r.EvaluateLifecycle(registryTestBase.Add(time.Minute), chains.DefaultLifecyclePolicy())
			if err != nil {
				t.Errorf("concurrent evaluation %d: %v", index, err)
				return
			}
			results <- batch
		}(i)
	}
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			if err := r.Add(newRegistryChain(t, fmt.Sprintf("added-%02d", index))); err != nil {
				t.Errorf("concurrent add %d: %v", index, err)
			}
		}(i)
	}
	wait.Wait()
	close(results)
	for batch := range results {
		if batch.EvaluationCount != batch.ChainCount || batch.EvaluationErrors != 0 {
			t.Fatalf("incoherent captured batch: %#v", batch)
		}
		assertBatchIDsSorted(t, batch)
	}
}

func assertBatchIDsSorted(t *testing.T, batch EvaluationBatch) {
	t.Helper()
	ids := make([]string, 0, len(batch.Results))
	for _, result := range batch.Results {
		ids = append(ids, string(result.ChainID))
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("batch IDs are not sorted: %#v", ids)
	}
}
