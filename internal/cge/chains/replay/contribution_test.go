package replay

import (
	"context"
	"reflect"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/registry"
)

func TestFromJournalReplaysContributionThroughDomainOperation(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	fileJournal := newReplayJournal(t)
	chain, err := chains.New("replay-contribution", chains.MutationContext{At: base, Actor: "builder", Reason: "create", CorrelationID: "create"})
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	observation := chains.ObservationRef{ID: "observation-1", EventType: "vision.identity", Timestamp: base.Add(time.Second)}
	if err := chain.AddObservation(observation, chains.MutationContext{At: base.Add(time.Second), Actor: "builder", Reason: "observe", CorrelationID: "observe"}); err != nil {
		t.Fatalf("observation: %v", err)
	}
	appendChainAdded(t, fileJournal, chain, base.Add(time.Second))
	contribution := chains.ConfidenceContribution{ID: "contribution-1", Source: "review", Kind: chains.ContributionSupport, Value: 0.6, ObservationIDs: []string{"observation-1"}, Reason: "support", CreatedAt: base.Add(2 * time.Second)}
	if err := chain.AddContribution(contribution, chains.MutationContext{At: base.Add(2 * time.Second), Actor: "reviewer", Reason: "support", CorrelationID: "contribution-1"}); err != nil {
		t.Fatalf("domain contribution: %v", err)
	}
	after := chain.Snapshot()
	if _, err := fileJournal.AppendContributionAdded(context.Background(), journal.ContributionAddedInput{
		ChainID: after.ID, PreviousRevision: 2, NewRevision: 3, Contribution: contribution,
		PreviousConfidence: 0, NewConfidence: after.CurrentConfidence, PreviousSupportCount: 0,
		NewSupportCount: after.ConfirmationCount, PreviousContradictionCount: 0,
		NewContradictionCount: after.ContradictionCount, Revision: after.History[len(after.History)-1],
		RecordedAt: base.Add(2 * time.Second), Actor: "reviewer", CorrelationID: "contribution-1",
	}); err != nil {
		t.Fatalf("append contribution: %v", err)
	}
	replayed, metadata, err := FromJournal(context.Background(), readReplayJournal(t, fileJournal))
	if err != nil {
		t.Fatalf("replay contribution: %v", err)
	}
	want := registry.New()
	if err := want.Add(chain); err != nil {
		t.Fatalf("expected registry: %v", err)
	}
	if !reflect.DeepEqual(replayed.List(), want.List()) || metadata.ContributionsAdded != 1 || metadata.RecordsApplied != 2 {
		t.Fatalf("contribution replay mismatch: got=%#v want=%#v metadata=%#v", replayed.List(), want.List(), metadata)
	}
}
