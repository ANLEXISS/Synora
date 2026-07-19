package chains

import (
	"testing"
	"time"
)

func TestCreationAndMutationRevisionsAreContinuousAndAuditable(t *testing.T) {
	chain := mustNewChain(t)
	if err := chain.AddObservation(observation("event-1", "vision.identity", chainTestBase, "entry"), mutation(1, "observe identity", "event-1")); err != nil {
		t.Fatalf("add observation: %v", err)
	}
	if err := chain.AddContribution(ConfidenceContribution{
		ID: "contribution-1", Source: "matcher", Kind: ContributionSupport, Value: 0.6,
		ObservationIDs: []string{"event-1"}, Reason: "identity matched", CreatedAt: chainTestBase,
	}, mutation(2, "record match", "event-1")); err != nil {
		t.Fatalf("add contribution: %v", err)
	}
	if err := chain.SetStatus(StatusActive, mutation(3, "activate coherent chain", "event-1")); err != nil {
		t.Fatalf("activate status: %v", err)
	}
	if err := chain.SetStatus(StatusConfirmed, mutation(4, "confirm coherent chain", "event-1")); err != nil {
		t.Fatalf("confirm status: %v", err)
	}
	if err := chain.AssignEntity("resident-1", mutation(5, "assign candidate entity", "event-1")); err != nil {
		t.Fatalf("assign entity: %v", err)
	}
	if err := chain.SetHistoricalReliability(0.8, mutation(6, "record historical reliability")); err != nil {
		t.Fatalf("set reliability: %v", err)
	}

	history := chain.History()
	if len(history) != 7 {
		t.Fatalf("history length = %d, want 7", len(history))
	}
	if history[0].Operation != OperationChainCreated || history[0].PreviousRevision != 0 || history[0].NewRevision != 1 {
		t.Fatalf("creation revision = %#v", history[0])
	}
	wantOperations := []RevisionOperation{
		OperationChainCreated,
		OperationObservationAdded,
		OperationContributionAdded,
		OperationStatusChanged,
		OperationStatusChanged,
		OperationEntityAssigned,
		OperationHistoricalReliabilityUpdated,
	}
	for i, record := range history {
		if record.Operation != wantOperations[i] || record.PreviousRevision != uint64(i) || record.NewRevision != uint64(i+1) {
			t.Fatalf("history[%d] is not continuous: %#v", i, record)
		}
		if record.Actor != "test" || record.At.IsZero() || record.Reason == "" {
			t.Fatalf("history[%d] lacks provenance: %#v", i, record)
		}
	}
	if history[1].ObservationIDs[0] != "event-1" || history[2].ContributionIDs[0] != "contribution-1" {
		t.Fatalf("references not recorded: observation=%#v contribution=%#v", history[1], history[2])
	}
	if history[3].PreviousStatus != StatusCandidate || history[3].NewStatus != StatusActive || history[4].PreviousStatus != StatusActive || history[4].NewStatus != StatusConfirmed {
		t.Fatalf("status provenance = %#v / %#v", history[3], history[4])
	}
	if history[5].PreviousEntityID == nil || *history[5].PreviousEntityID != "" || history[5].NewEntityID == nil || *history[5].NewEntityID != "resident-1" {
		t.Fatalf("entity provenance = %#v", history[5])
	}
	if history[6].PreviousHistoricalReliability == nil || *history[6].PreviousHistoricalReliability != 0 || history[6].NewHistoricalReliability == nil || *history[6].NewHistoricalReliability != 0.8 {
		t.Fatalf("reliability provenance = %#v", history[6])
	}
	if snapshot := chain.Snapshot(); snapshot.Revision != history[len(history)-1].NewRevision {
		t.Fatalf("snapshot revision = %d, last history revision = %d", snapshot.Revision, history[len(history)-1].NewRevision)
	}
	if err := chain.Validate(); err != nil {
		t.Fatalf("valid chain rejected: %v", err)
	}
}

func TestMutationContextAndContributionIdentityAreRequired(t *testing.T) {
	if _, err := New(ChainID("chain-1"), MutationContext{At: chainTestBase, Reason: "create"}); err == nil {
		t.Fatal("expected empty actor to be rejected")
	}
	if _, err := New(ChainID("chain-1"), MutationContext{Actor: "test", Reason: "create"}); err == nil {
		t.Fatal("expected zero timestamp to be rejected")
	}
	if _, err := New(ChainID("chain-1"), MutationContext{At: chainTestBase, Actor: "test"}); err == nil {
		t.Fatal("expected empty reason to be rejected")
	}

	chain := mustNewChain(t)
	before := chain.Snapshot()
	invalid := ConfidenceContribution{
		Source: "matcher", Kind: ContributionSupport, Value: 0.5,
		Reason: "missing id", CreatedAt: chainTestBase,
	}
	if err := chain.AddContribution(invalid, mutation(1, "reject missing contribution id")); err == nil {
		t.Fatal("expected empty contribution ID to be rejected")
	}
	if after := chain.Snapshot(); after.Revision != before.Revision || len(after.History) != len(before.History) {
		t.Fatalf("rejected contribution changed audit state: before=%#v after=%#v", before, after)
	}

	valid := invalid
	valid.ID = "contribution-1"
	if err := chain.AddContribution(valid, mutation(1, "record contribution")); err != nil {
		t.Fatalf("valid contribution: %v", err)
	}
	if err := chain.AddContribution(valid, mutation(2, "reject duplicate contribution")); err == nil {
		t.Fatal("expected duplicate contribution ID to be rejected")
	}
	if got := chain.Snapshot(); got.Revision != 2 || len(got.Contributions) != 1 || len(got.History) != 2 {
		t.Fatalf("duplicate changed chain: %#v", got)
	}
}

func TestRejectedMutationDoesNotAdvanceOnInvalidTimestampOrOperation(t *testing.T) {
	chain := mustNewChain(t)
	if err := chain.SetStatus(StatusActive, mutation(1, "activate")); err != nil {
		t.Fatalf("set status: %v", err)
	}
	before := chain.Snapshot()
	if err := chain.SetStatus(StatusConfirmed, mutation(-1, "out of order")); err == nil {
		t.Fatal("expected mutation timestamp before latest revision to be rejected")
	}
	if err := RevisionOperation("unknown").Validate(); err == nil {
		t.Fatal("expected unknown operation to be rejected")
	}
	after := chain.Snapshot()
	if after.Revision != before.Revision || len(after.History) != len(before.History) || after.Status != before.Status {
		t.Fatalf("rejected timestamp changed chain: before=%#v after=%#v", before, after)
	}
}

func TestHistoryAndReferencedIDsAreDefensive(t *testing.T) {
	chain := mustNewChain(t)
	context := mutation(1, "observe", "event-1")
	if err := chain.AddObservation(observation("event-1", "vision.motion", chainTestBase, "entry"), context); err != nil {
		t.Fatalf("add observation: %v", err)
	}
	contributionIDs := []string{"event-1"}
	if err := chain.AddContribution(ConfidenceContribution{
		ID: "contribution-1", Source: "matcher", Kind: ContributionSupport, Value: 0.4,
		ObservationIDs: contributionIDs, Reason: "support", CreatedAt: chainTestBase,
	}, mutation(2, "record support", "event-1")); err != nil {
		t.Fatalf("add contribution: %v", err)
	}
	context.ObservationIDs[0] = "changed-after-call"
	contributionIDs[0] = "changed-after-call"

	history := chain.History()
	history[1].ObservationIDs[0] = "changed-history"
	history[2].ObservationIDs[0] = "changed-history"
	history[2].ContributionIDs[0] = "changed-history"
	if history[2].NewHistoricalReliability != nil {
		*history[2].NewHistoricalReliability = 0
	}
	history = append(history, RevisionRecord{})

	fresh := chain.Snapshot()
	if fresh.History[1].ObservationIDs[0] != "event-1" || fresh.History[2].ObservationIDs[0] != "event-1" || fresh.History[2].ContributionIDs[0] != "contribution-1" {
		t.Fatalf("history references were not defensive: %#v", fresh.History)
	}
	if fresh.Observations[0].ID != "event-1" || fresh.Contributions[0].ObservationIDs[0] != "event-1" {
		t.Fatalf("source references were not defensive: %#v", fresh)
	}
}

func TestValidateDetectsCorruptedHistory(t *testing.T) {
	chain := mustNewChain(t)
	if err := chain.AddObservation(observation("event-1", "vision.motion", chainTestBase, "entry"), mutation(1, "observe", "event-1")); err != nil {
		t.Fatalf("add observation: %v", err)
	}
	chain.history[1].PreviousRevision = 99
	if err := chain.Validate(); err == nil {
		t.Fatal("expected Validate to detect a discontinuous history")
	}
}

func TestRevisionTimestampsAreMonotonic(t *testing.T) {
	chain := mustNewChain(t)
	if err := chain.AddObservation(observation("event-1", "vision.motion", chainTestBase, "entry"), mutation(1, "observe")); err != nil {
		t.Fatalf("add observation: %v", err)
	}
	history := chain.History()
	if !history[0].At.Before(history[1].At) {
		t.Fatalf("expected monotonic timestamps: %#v", history)
	}
	if !history[1].At.Equal(chainTestBase.Add(time.Second)) {
		t.Fatalf("unexpected mutation timestamp: %s", history[1].At)
	}
}
