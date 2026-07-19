package registry

import (
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

func TestRegistryAddContributionIsTransactional(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	chain, err := chains.New("registry-contribution", chains.MutationContext{At: base, Actor: "test", Reason: "create", CorrelationID: "create"})
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	observation := chains.ObservationRef{ID: "observation-1", EventType: "vision.identity", Timestamp: base.Add(time.Second)}
	if err := chain.AddObservation(observation, chains.MutationContext{At: base.Add(time.Second), Actor: "test", Reason: "observe", CorrelationID: "observe"}); err != nil {
		t.Fatalf("observation: %v", err)
	}
	registry := New()
	if err := registry.Add(chain); err != nil {
		t.Fatalf("registry add: %v", err)
	}
	command := chains.AddContributionCommand{
		ChainID: "registry-contribution", SourceRevision: chain.Snapshot().Revision,
		Contribution: chains.ConfidenceContribution{ID: "contribution-1", Source: "review", Kind: chains.ContributionSupport, Value: 0.4, ObservationIDs: []string{"observation-1"}, Reason: "support", CreatedAt: base.Add(2 * time.Second)},
		Mutation:     chains.MutationContext{At: base.Add(2 * time.Second), Actor: "reviewer", Reason: "support", CorrelationID: "contribution-1"},
	}
	result, err := registry.AddContribution(command)
	if err != nil {
		t.Fatalf("add contribution: %v", err)
	}
	if result.Before.Revision+1 != result.After.Revision || result.Revision.Operation != chains.OperationContributionAdded || result.After.CurrentConfidence != 0.4 || result.After.ConfirmationCount != 1 || result.After.Status != chains.StatusCandidate {
		t.Fatalf("unexpected result: %#v", result)
	}
	result.After.Contributions[0].ObservationIDs[0] = "mutated"
	stored, err := registry.Get(command.ChainID)
	if err != nil || stored.Contributions[0].ObservationIDs[0] != "observation-1" {
		t.Fatalf("result exposed mutable registry state: %#v err=%v", stored, err)
	}

	before := registry.List()
	if _, err := registry.AddContribution(command); err == nil || !errors.Is(err, ErrStaleContributionCommand) {
		t.Fatalf("stale contribution error = %v", err)
	}
	if got := registry.List(); len(got) != 1 || got[0].Revision != before[0].Revision || len(got[0].Contributions) != len(before[0].Contributions) {
		t.Fatalf("stale contribution changed registry: before=%#v after=%#v", before, got)
	}
}

func TestRegistryRejectsUnknownContributionReferenceWithoutMutation(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	chain, err := chains.New("unknown-reference", chains.MutationContext{At: base, Actor: "test", Reason: "create", CorrelationID: "create"})
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	registry := New()
	if err := registry.Add(chain); err != nil {
		t.Fatalf("registry add: %v", err)
	}
	command := chains.AddContributionCommand{ChainID: chain.Snapshot().ID, SourceRevision: 1, Contribution: chains.ConfidenceContribution{ID: "unknown", Source: "test", Kind: chains.ContributionNeutral, Value: 0, ObservationIDs: []string{"missing"}, Reason: "unknown", CreatedAt: base.Add(time.Second)}, Mutation: chains.MutationContext{At: base.Add(time.Second), Actor: "test", Reason: "unknown", CorrelationID: "unknown"}}
	if _, err := registry.AddContribution(command); err == nil || !errors.Is(err, chains.ErrUnknownObservationReference) {
		t.Fatalf("unknown reference error = %v", err)
	}
	if got := registry.List()[0]; got.Revision != 1 || len(got.Contributions) != 0 {
		t.Fatalf("unknown reference changed registry: %#v", got)
	}
}
