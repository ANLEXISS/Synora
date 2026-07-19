package registry

import (
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

var registryTestBase = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func registryMutation(at time.Time, reason string) chains.MutationContext {
	return chains.MutationContext{At: at, Actor: "test", Reason: reason}
}

func newRegistryChain(t *testing.T, id string) *chains.Chain {
	t.Helper()
	chain, err := chains.New(chains.ChainID(id), registryMutation(registryTestBase, "create chain"))
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	return chain
}

func newActiveRegistryChain(t *testing.T, id string) *chains.Chain {
	t.Helper()
	chain := newRegistryChain(t, id)
	if err := chain.AddObservation(chains.ObservationRef{
		ID: "observation-" + id, EventType: "vision.motion", Timestamp: registryTestBase.Add(time.Second), NodeID: "entry",
	}, registryMutation(registryTestBase.Add(time.Second), "add observation")); err != nil {
		t.Fatalf("add observation: %v", err)
	}
	if err := chain.SetStatus(chains.StatusActive, registryMutation(registryTestBase.Add(2*time.Second), "activate chain")); err != nil {
		t.Fatalf("activate chain: %v", err)
	}
	if err := chain.SetConfidence(0.8, registryMutation(registryTestBase.Add(3*time.Second), "support chain")); err != nil {
		t.Fatalf("support chain: %v", err)
	}
	return chain
}

func activeProposal(t *testing.T, registry *Registry, id chains.ChainID) chains.TransitionProposal {
	t.Helper()
	snapshot, err := registry.Get(id)
	if err != nil {
		t.Fatalf("get chain for proposal: %v", err)
	}
	evaluation, err := chains.EvaluateLifecycle(snapshot, registryTestBase.Add(2*time.Hour+time.Second), chains.DefaultLifecyclePolicy())
	if err != nil {
		t.Fatalf("evaluate lifecycle: %v", err)
	}
	if evaluation.Proposal == nil {
		t.Fatalf("expected lifecycle proposal: %#v", evaluation)
	}
	return *evaluation.Proposal
}

func TestRegistryAddOwnsDeepCloneAndPreservesHistory(t *testing.T) {
	registry := New()
	original := newActiveRegistryChain(t, "owned")
	expected := original.Snapshot()
	if err := registry.Add(original); err != nil {
		t.Fatalf("add chain: %v", err)
	}

	if err := original.SetStatus(chains.StatusDeclining, registryMutation(registryTestBase.Add(4*time.Second), "mutate original")); err != nil {
		t.Fatalf("mutate original: %v", err)
	}
	got, err := registry.Get(expected.ID)
	if err != nil {
		t.Fatalf("get owned chain: %v", err)
	}
	if !reflect.DeepEqual(got, expected) || got.Status != chains.StatusActive || got.Revision != expected.Revision {
		t.Fatalf("registry retained caller pointer: expected=%#v got=%#v", expected, got)
	}
	if len(got.History) != len(expected.History) || got.History[len(got.History)-1].Operation != chains.OperationConfidenceUpdated {
		t.Fatalf("owned history was not preserved: %#v", got.History)
	}

	got.History[0].Reason = "mutated snapshot"
	got.Contributions = append(got.Contributions, chains.ConfidenceContribution{})
	fresh, err := registry.Get(expected.ID)
	if err != nil {
		t.Fatalf("get fresh owned chain: %v", err)
	}
	if fresh.History[0].Reason == "mutated snapshot" || len(fresh.Contributions) != len(expected.Contributions) {
		t.Fatal("registry exposed mutable snapshot state")
	}
}

func TestRegistryAddValidationAndDuplicate(t *testing.T) {
	registry := New()
	if err := registry.Add(nil); err == nil {
		t.Fatal("expected nil chain rejection")
	}
	if err := registry.Add(&chains.Chain{}); err == nil {
		t.Fatal("expected invalid chain rejection")
	}
	chain := newRegistryChain(t, "duplicate")
	if err := registry.Add(chain); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := registry.Add(chain); err == nil || !errors.Is(err, ErrChainAlreadyExists) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if registry.Count() != 1 {
		t.Fatalf("count = %d, want 1", registry.Count())
	}
}

func TestRegistryGetListAndCount(t *testing.T) {
	registry := New()
	if list := registry.List(); list == nil || len(list) != 0 {
		t.Fatalf("empty list = %#v", list)
	}
	if _, err := registry.Get(chains.ChainID("missing")); err == nil || !errors.Is(err, ErrChainNotFound) {
		t.Fatalf("expected not-found error, got %v", err)
	}
	for _, id := range []string{"zeta", "alpha", "middle"} {
		if err := registry.Add(newRegistryChain(t, id)); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	list := registry.List()
	ids := make([]string, 0, len(list))
	for _, snapshot := range list {
		ids = append(ids, string(snapshot.ID))
	}
	if !sort.StringsAreSorted(ids) || !reflect.DeepEqual(ids, []string{"alpha", "middle", "zeta"}) {
		t.Fatalf("list order = %#v", ids)
	}
	if registry.Count() != 3 {
		t.Fatalf("count = %d, want 3", registry.Count())
	}
}

func TestRegistryAppliesProposalTransactionallyWithProvenance(t *testing.T) {
	registry := New()
	chain := newActiveRegistryChain(t, "apply")
	if err := registry.Add(chain); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	before, err := registry.Get(chains.ChainID("apply"))
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	proposal := activeProposal(t, registry, before.ID)
	if proposal.SourceRevision != before.Revision || proposal.From != before.Status {
		t.Fatalf("proposal source = revision %d/status %s, want %d/%s", proposal.SourceRevision, proposal.From, before.Revision, before.Status)
	}
	after, err := registry.ApplyLifecycleProposal(proposal, "cge.lifecycle", "apply-correlation")
	if err != nil {
		t.Fatalf("apply proposal: %v", err)
	}
	if after.Status != chains.StatusDeclining || after.Revision != before.Revision+1 {
		t.Fatalf("after = %#v", after)
	}
	if len(after.History) != len(before.History)+1 {
		t.Fatalf("history length after = %d, before = %d", len(after.History), len(before.History))
	}
	last := after.History[len(after.History)-1]
	if last.Operation != chains.OperationStatusChanged || last.Actor != "cge.lifecycle" || last.CorrelationID != "apply-correlation" || last.PreviousStatus != chains.StatusActive || last.NewStatus != chains.StatusDeclining {
		t.Fatalf("unexpected applied history record: %#v", last)
	}
}

func TestRegistryRejectsStaleProposalWithoutMutation(t *testing.T) {
	registry := New()
	missing := chains.TransitionProposal{ChainID: chains.ChainID("missing"), SourceRevision: 1, From: chains.StatusActive, To: chains.StatusDeclining}
	if _, err := registry.ApplyLifecycleProposal(missing, "test", "missing"); err == nil || !errors.Is(err, ErrChainNotFound) {
		t.Fatalf("expected apply not-found error, got %v", err)
	}
	if err := registry.Add(newActiveRegistryChain(t, "stale")); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	id := chains.ChainID("stale")
	proposal := activeProposal(t, registry, id)
	before, err := registry.Get(id)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	proposal.SourceRevision--
	if _, err := registry.ApplyLifecycleProposal(proposal, "test", "stale-revision"); err == nil || !errors.Is(err, ErrStaleProposal) {
		t.Fatalf("expected stale revision error, got %v", err)
	}
	proposal = activeProposal(t, registry, id)
	proposal.From = chains.StatusCandidate
	if _, err := registry.ApplyLifecycleProposal(proposal, "test", "stale-status"); err == nil || !errors.Is(err, ErrStaleProposal) {
		t.Fatalf("expected stale status error, got %v", err)
	}
	after, err := registry.Get(id)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("stale proposal mutated chain: before=%#v after=%#v", before, after)
	}
}

func TestRegistryAddsObservationTransactionallyWithOptimisticRevision(t *testing.T) {
	registry := New()
	chain := newActiveRegistryChain(t, "observation")
	if err := registry.Add(chain); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	before, err := registry.Get("observation")
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	command := chains.AddObservationCommand{
		ChainID: "observation", SourceRevision: before.Revision,
		Observation: chains.ObservationRef{ID: "observation-2", EventType: "vision.motion", Timestamp: registryTestBase.Add(5 * time.Second), NodeID: "hall"},
		Mutation:    chains.MutationContext{At: registryTestBase.Add(6 * time.Second), Actor: "observer", Reason: "explicit observation", CorrelationID: "observation-2"},
	}
	result, err := registry.AddObservation(command)
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}
	if result.Before.Revision != before.Revision || result.After.Revision != before.Revision+1 || result.Revision.Operation != chains.OperationObservationAdded || result.After.Status != before.Status || result.After.CurrentConfidence != before.CurrentConfidence || result.After.HistoricalReliability != before.HistoricalReliability {
		t.Fatalf("unexpected observation result: %#v", result)
	}
	if len(result.After.Observations) != len(before.Observations)+1 || result.Revision.ObservationIDs[0] != "observation-2" {
		t.Fatalf("observation delta is incomplete: %#v", result)
	}
	command.Mutation.ObservationIDs = []string{"changed-after-call"}
	result.After.Observations[0].ID = "changed-result"
	fresh, err := registry.Get("observation")
	if err != nil || fresh.Observations[0].ID == "changed-result" || fresh.Revision != result.After.Revision {
		t.Fatalf("registry state was not defensive: fresh=%#v err=%v", fresh, err)
	}
	if _, err := registry.AddObservation(command); err == nil || !errors.Is(err, ErrStaleObservationCommand) {
		t.Fatalf("expected stale observation command, got %v", err)
	}
}

func TestRegistryRejectsObservationOnForbiddenStateWithoutMutation(t *testing.T) {
	registry := New()
	chain := newActiveRegistryChain(t, "forbidden")
	if err := chain.SetStatus(chains.StatusDeclining, registryMutation(registryTestBase.Add(4*time.Second), "decline")); err != nil {
		t.Fatalf("decline chain: %v", err)
	}
	if err := chain.SetStatus(chains.StatusDormant, registryMutation(registryTestBase.Add(5*time.Second), "dormant")); err != nil {
		t.Fatalf("make dormant: %v", err)
	}
	if err := registry.Add(chain); err != nil {
		t.Fatalf("add dormant chain: %v", err)
	}
	before, err := registry.Get("forbidden")
	if err != nil {
		t.Fatalf("get dormant: %v", err)
	}
	command := chains.AddObservationCommand{ChainID: "forbidden", SourceRevision: before.Revision, Observation: chains.ObservationRef{ID: "new-observation", EventType: "vision.motion", Timestamp: registryTestBase.Add(6 * time.Second)}, Mutation: chains.MutationContext{At: registryTestBase.Add(6 * time.Second), Actor: "observer", Reason: "must reject", CorrelationID: "forbidden-observation"}}
	if _, err := registry.AddObservation(command); err == nil || !errors.Is(err, ErrObservationNotAllowed) {
		t.Fatalf("expected forbidden-state rejection, got %v", err)
	}
	after, err := registry.Get("forbidden")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("forbidden observation mutated registry: before=%#v after=%#v err=%v", before, after, err)
	}
}

func TestRegistryAppliesProposalOnlyOnce(t *testing.T) {
	registry := New()
	if err := registry.Add(newActiveRegistryChain(t, "once")); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	proposal := activeProposal(t, registry, chains.ChainID("once"))
	if _, err := registry.ApplyLifecycleProposal(proposal, "test", "once"); err != nil {
		t.Fatalf("first application: %v", err)
	}
	if _, err := registry.ApplyLifecycleProposal(proposal, "test", "once"); err == nil || !errors.Is(err, ErrStaleProposal) {
		t.Fatalf("expected second application to be stale, got %v", err)
	}
	snapshot, err := registry.Get(chains.ChainID("once"))
	if err != nil {
		t.Fatalf("get once chain: %v", err)
	}
	if snapshot.Revision != proposal.SourceRevision+1 || len(snapshot.History) != int(snapshot.Revision) {
		t.Fatalf("unexpected revision after duplicate application: %#v", snapshot)
	}
}

func TestRegistryRejectsInvalidApplicationWithoutMutation(t *testing.T) {
	registry := New()
	if err := registry.Add(newActiveRegistryChain(t, "invalid-application")); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	id := chains.ChainID("invalid-application")
	proposal := activeProposal(t, registry, id)
	before, err := registry.Get(id)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	if _, err := registry.ApplyLifecycleProposal(proposal, "", "invalid-actor"); err == nil || !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("expected invalid actor error, got %v", err)
	}
	proposal.EvaluatedAt = registryTestBase
	if _, err := registry.ApplyLifecycleProposal(proposal, "test", "invalid-time"); err == nil || !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("expected invalid timestamp error, got %v", err)
	}
	after, err := registry.Get(id)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("invalid application mutated chain: before=%#v after=%#v", before, after)
	}
}

func TestRegistryConcurrentDistinctAddsAndReads(t *testing.T) {
	registry := New()
	const count = 32
	var wait sync.WaitGroup
	errorsCh := make(chan error, count)
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			id := chains.ChainID("concurrent-" + time.Duration(index).String())
			errorsCh <- registry.Add(newRegistryChain(t, string(id)))
		}(i)
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Errorf("concurrent distinct add: %v", err)
		}
	}
	if registry.Count() != count {
		t.Fatalf("concurrent count = %d, want %d", registry.Count(), count)
	}

	for i := 0; i < count; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			id := chains.ChainID("concurrent-" + time.Duration(index).String())
			if _, err := registry.Get(id); err != nil {
				t.Errorf("concurrent get %s: %v", id, err)
			}
			_ = registry.List()
		}(i)
	}
	wait.Wait()
}

func TestRegistryConcurrentDuplicateAdds(t *testing.T) {
	registry := New()
	const count = 24
	var wait sync.WaitGroup
	errorsCh := make(chan error, count)
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsCh <- registry.Add(newRegistryChain(t, "same-id"))
		}()
	}
	wait.Wait()
	close(errorsCh)
	successes := 0
	for err := range errorsCh {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrChainAlreadyExists) {
			t.Errorf("unexpected duplicate add error: %v", err)
		}
	}
	if successes != 1 || registry.Count() != 1 {
		t.Fatalf("duplicate add successes=%d count=%d", successes, registry.Count())
	}
}

func TestRegistryConcurrentSameProposalOnlyOneWins(t *testing.T) {
	registry := New()
	if err := registry.Add(newActiveRegistryChain(t, "same-proposal")); err != nil {
		t.Fatalf("add chain: %v", err)
	}
	proposal := activeProposal(t, registry, chains.ChainID("same-proposal"))
	const count = 24
	var wait sync.WaitGroup
	results := make(chan error, count)
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := registry.ApplyLifecycleProposal(proposal, "test", "same-proposal")
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrStaleProposal) {
			t.Errorf("unexpected concurrent proposal error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent proposal successes=%d, want 1", successes)
	}
	snapshot, err := registry.Get(chains.ChainID("same-proposal"))
	if err != nil {
		t.Fatalf("get final chain: %v", err)
	}
	if snapshot.Revision != proposal.SourceRevision+1 || snapshot.Status != chains.StatusDeclining {
		t.Fatalf("unexpected final chain: %#v", snapshot)
	}
}
