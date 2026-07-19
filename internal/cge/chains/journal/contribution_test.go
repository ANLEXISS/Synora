package journal

import (
	"context"
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

func TestAppendContributionAddedUsesCompactValidatedDelta(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	j, _ := initializeTestJournal(t, "contribution")
	chain, err := chains.New("journal-contribution", chains.MutationContext{At: base, Actor: "test", Reason: "create", CorrelationID: "create"})
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	observation := chains.ObservationRef{ID: "observation-1", EventType: "vision.identity", Timestamp: base.Add(time.Second)}
	if err := chain.AddObservation(observation, chains.MutationContext{At: base.Add(time.Second), Actor: "test", Reason: "observe", CorrelationID: "observe"}); err != nil {
		t.Fatalf("observation: %v", err)
	}
	if _, err := j.AppendChainAdded(context.Background(), ChainAddedInput{Chain: chain.Snapshot(), RecordedAt: base.Add(time.Second), Actor: "test", CorrelationID: "chain-added"}); err != nil {
		t.Fatalf("chain append: %v", err)
	}
	contribution := chains.ConfidenceContribution{ID: "contribution-1", Source: "review", Kind: chains.ContributionContradiction, Value: 0.2, ObservationIDs: []string{"observation-1"}, Reason: "contradiction", CreatedAt: base.Add(2 * time.Second)}
	if err := chain.AddContribution(contribution, chains.MutationContext{At: base.Add(2 * time.Second), Actor: "reviewer", Reason: "contradiction", CorrelationID: "contribution-1"}); err != nil {
		t.Fatalf("domain contribution: %v", err)
	}
	after := chain.Snapshot()
	record, err := j.AppendContributionAdded(context.Background(), ContributionAddedInput{
		ChainID: after.ID, PreviousRevision: 2, NewRevision: 3, Contribution: contribution,
		PreviousConfidence: 0, NewConfidence: after.CurrentConfidence,
		PreviousSupportCount: 0, NewSupportCount: after.ConfirmationCount,
		PreviousContradictionCount: 0, NewContradictionCount: after.ContradictionCount,
		Revision: after.History[len(after.History)-1], RecordedAt: base.Add(2 * time.Second), Actor: "reviewer", CorrelationID: "contribution-1",
	})
	if err != nil {
		t.Fatalf("contribution append: %v", err)
	}
	if record.Kind != RecordKindContributionAdded || record.Sequence != 3 || record.RecordHash == "" || record.PayloadSHA256 == "" {
		t.Fatalf("invalid contribution record: %#v", record)
	}
	read, err := j.ReadAll(context.Background())
	if err != nil || read.HeadSequence != 3 {
		t.Fatalf("read journal: %#v err=%v", read, err)
	}
	var payload ContributionAddedPayload
	if err := decodeStrictJSON(record.Payload, &payload); err != nil || payload.NewConfidence != after.CurrentConfidence || payload.Contribution.ID != contribution.ID {
		t.Fatalf("invalid contribution payload: %#v err=%v", payload, err)
	}
	invalid := ContributionAddedInput{
		ChainID: after.ID, PreviousRevision: 2, NewRevision: 4, Contribution: contribution,
		PreviousConfidence: 0, NewConfidence: after.CurrentConfidence, PreviousSupportCount: 0,
		NewSupportCount: after.ConfirmationCount, PreviousContradictionCount: 0,
		NewContradictionCount: after.ContradictionCount, Revision: after.History[len(after.History)-1],
		RecordedAt: base.Add(3 * time.Second), Actor: "reviewer", CorrelationID: "invalid",
	}
	if _, err := j.AppendContributionAdded(context.Background(), invalid); err == nil || !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("invalid contribution payload accepted: %v", err)
	}
}

func TestContributionRecordKindIsSupportedWithoutChangingSchema(t *testing.T) {
	if err := RecordKindContributionAdded.Validate(); err != nil {
		t.Fatalf("contribution record kind rejected: %v", err)
	}
	if CurrentSchemaVersion != 1 {
		t.Fatalf("schema version changed unexpectedly: %d", CurrentSchemaVersion)
	}
}
