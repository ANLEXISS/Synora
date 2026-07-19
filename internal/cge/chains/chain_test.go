package chains

import (
	"testing"
	"time"
)

var chainTestBase = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func TestNewChainRequiresStableNonEmptyID(t *testing.T) {
	chain, err := New(ChainID("chain-1"), mutation(0, "create chain"))
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	snapshot := chain.Snapshot()
	if snapshot.ID != "chain-1" || snapshot.Status != StatusCandidate || snapshot.Revision != 1 {
		t.Fatalf("unexpected new chain snapshot: %#v", snapshot)
	}

	if _, err := New(ChainID("   "), mutation(0, "create chain")); err == nil {
		t.Fatal("expected empty chain ID to be rejected")
	}
	if _, err := NewChainID(""); err == nil {
		t.Fatal("expected empty NewChainID value to be rejected")
	}
}

func TestStatusValidationAndClassification(t *testing.T) {
	for _, status := range []Status{
		StatusCandidate, StatusActive, StatusConfirmed, StatusDeclining,
		StatusDormant, StatusArchived, StatusReactivated, StatusMerged,
		StatusSplit, StatusInvalidated,
	} {
		if err := status.Validate(); err != nil {
			t.Fatalf("status %q rejected: %v", status, err)
		}
	}
	if Status("unknown").Validate() == nil {
		t.Fatal("expected invalid status to be rejected")
	}
	if !StatusActive.IsActiveModelStatus() || !StatusActive.IsActive() || StatusCandidate.IsActiveModelStatus() || StatusDormant.IsActive() || !StatusArchived.IsHistoricalStatus() || StatusArchived.IsTerminal() || !StatusInvalidated.IsTerminal() {
		t.Fatal("unexpected status classification")
	}
}

func TestChainRejectsInvalidConfidenceWithoutMutation(t *testing.T) {
	chain := mustNewChain(t)
	if err := chain.SetConfidence(1.1, mutation(1, "reject confidence")); err == nil {
		t.Fatal("expected out-of-range confidence to be rejected")
	}
	if err := chain.SetConfidence(-0.1, mutation(1, "reject confidence")); err == nil {
		t.Fatal("expected negative confidence to be rejected")
	}
	if err := chain.SetConfidence(0.7, mutation(1, "set confidence")); err != nil {
		t.Fatalf("set confidence: %v", err)
	}
	before := chain.Snapshot()
	if err := chain.SetConfidence(2, mutation(2, "reject confidence")); err == nil {
		t.Fatal("expected second out-of-range confidence to be rejected")
	}
	after := chain.Snapshot()
	if after.Revision != before.Revision || after.CurrentConfidence != before.CurrentConfidence {
		t.Fatalf("rejected confidence changed chain: before=%#v after=%#v", before, after)
	}
}

func TestAddObservationOrdersAndProjectsPath(t *testing.T) {
	chain := mustNewChain(t)
	late := observation("event-b", "vision.identity", chainTestBase.Add(2*time.Second), "salon")
	early := observation("event-a", "vision.motion", chainTestBase, "entry")
	middle := observation("event-c", "vision.unknown", chainTestBase.Add(time.Second), "hall")

	if err := chain.AddObservation(late, mutation(1, "add late observation", "event-b")); err != nil {
		t.Fatalf("add late observation: %v", err)
	}
	if err := chain.AddObservation(early, mutation(2, "add early observation", "event-a")); err != nil {
		t.Fatalf("add early observation: %v", err)
	}
	if err := chain.AddObservation(middle, mutation(3, "add middle observation", "event-c")); err != nil {
		t.Fatalf("add middle observation: %v", err)
	}

	snapshot := chain.Snapshot()
	if snapshot.OccurrenceCount != 3 || len(snapshot.Observations) != 3 {
		t.Fatalf("unexpected observation counts: %#v", snapshot)
	}
	if got := []string{
		snapshot.Observations[0].ID,
		snapshot.Observations[1].ID,
		snapshot.Observations[2].ID,
	}; !equalStrings(got, []string{"event-a", "event-c", "event-b"}) {
		t.Fatalf("observation order = %#v", got)
	}
	if !snapshot.FirstSeenAt.Equal(chainTestBase) || !snapshot.LastSeenAt.Equal(chainTestBase.Add(2*time.Second)) {
		t.Fatalf("unexpected seen window: first=%s last=%s", snapshot.FirstSeenAt, snapshot.LastSeenAt)
	}
	if !equalStrings(snapshot.NodePath, []string{"entry", "hall", "salon"}) {
		t.Fatalf("node path = %#v", snapshot.NodePath)
	}
	if snapshot.Revision != 4 {
		t.Fatalf("revision = %d, want 4", snapshot.Revision)
	}
}

func TestDuplicateObservationIsRejectedWithoutRevision(t *testing.T) {
	chain := mustNewChain(t)
	ref := observation("event-1", "vision.motion", chainTestBase, "entry")
	if err := chain.AddObservation(ref, mutation(1, "add observation", "event-1")); err != nil {
		t.Fatalf("first observation: %v", err)
	}
	before := chain.Snapshot()
	if err := chain.AddObservation(ref, mutation(2, "duplicate observation", "event-1")); err == nil {
		t.Fatal("expected duplicate observation to be rejected")
	}
	after := chain.Snapshot()
	if after.Revision != before.Revision || after.OccurrenceCount != before.OccurrenceCount {
		t.Fatalf("duplicate changed chain: before=%#v after=%#v", before, after)
	}
}

func TestContributionExplainsConfidenceAndCounters(t *testing.T) {
	chain := mustNewChain(t)
	if err := chain.AddObservation(observation("event-1", "vision.identity", chainTestBase, "entry"), mutation(1, "add identity observation", "event-1")); err != nil {
		t.Fatalf("identity observation: %v", err)
	}
	if err := chain.AddObservation(observation("event-2", "vision.unknown", chainTestBase.Add(time.Second), "hall"), mutation(2, "add contradictory observation", "event-2")); err != nil {
		t.Fatalf("contradictory observation: %v", err)
	}
	if err := chain.AddContribution(ConfidenceContribution{
		ID: "contribution-1", Source: "vision", Kind: ContributionSupport, Value: 0.7,
		ObservationIDs: []string{"event-1"}, Reason: "consistent identity observation", CreatedAt: chainTestBase,
	}, mutation(3, "record identity support", "event-1")); err != nil {
		t.Fatalf("support contribution: %v", err)
	}
	if err := chain.AddContribution(ConfidenceContribution{
		ID: "contribution-2", Source: "review", Kind: ContributionContradiction, Value: 0.4,
		ObservationIDs: []string{"event-2"}, Reason: "inconsistent location", CreatedAt: chainTestBase.Add(time.Second),
	}, mutation(4, "record contradiction", "event-2")); err != nil {
		t.Fatalf("contradiction contribution: %v", err)
	}
	if err := chain.AddContribution(ConfidenceContribution{
		ID: "contribution-3", Source: "context", Kind: ContributionNeutral, Value: 1,
		Reason: "context retained without confidence effect", CreatedAt: chainTestBase.Add(2 * time.Second),
	}, mutation(5, "record neutral context")); err != nil {
		t.Fatalf("neutral contribution: %v", err)
	}

	snapshot := chain.Snapshot()
	if snapshot.CurrentConfidence != 0.3 || snapshot.MaxHistoricalConfidence != 0.7 {
		t.Fatalf("unexpected confidence values: current=%v max=%v", snapshot.CurrentConfidence, snapshot.MaxHistoricalConfidence)
	}
	if snapshot.ConfirmationCount != 1 || snapshot.ContradictionCount != 1 || len(snapshot.Contributions) != 3 {
		t.Fatalf("unexpected contribution counters: %#v", snapshot)
	}
	if snapshot.Revision != 6 {
		t.Fatalf("revision = %d, want 6", snapshot.Revision)
	}

	before := chain.Snapshot()
	if err := chain.AddContribution(ConfidenceContribution{
		ID: "contribution-invalid", Source: "review", Kind: ContributionSupport, Value: 1.2,
		Reason: "invalid", CreatedAt: chainTestBase,
	}, mutation(4, "reject contribution")); err == nil {
		t.Fatal("expected invalid contribution value to be rejected")
	}
	after := chain.Snapshot()
	if after.Revision != before.Revision || len(after.Contributions) != len(before.Contributions) {
		t.Fatalf("rejected contribution changed chain: before=%#v after=%#v", before, after)
	}
}

func TestChainMutationsAndHistoricalReliability(t *testing.T) {
	chain := mustNewChain(t)
	if err := chain.SetStatus(StatusActive, mutation(1, "activate chain")); err != nil {
		t.Fatalf("activate status: %v", err)
	}
	if err := chain.SetStatus(StatusConfirmed, mutation(2, "confirm chain")); err != nil {
		t.Fatalf("confirm status: %v", err)
	}
	if err := chain.AssignEntity("resident-1", mutation(3, "assign resident")); err != nil {
		t.Fatalf("assign entity: %v", err)
	}
	if err := chain.SetHistoricalReliability(0.8, mutation(4, "record reliability")); err != nil {
		t.Fatalf("set historical reliability: %v", err)
	}
	before := chain.Snapshot()
	if err := chain.SetStatus(Status("invalid"), mutation(4, "reject status")); err == nil {
		t.Fatal("expected invalid status to be rejected")
	}
	if err := chain.SetHistoricalReliability(1.2, mutation(4, "reject reliability")); err == nil {
		t.Fatal("expected invalid historical reliability to be rejected")
	}
	after := chain.Snapshot()
	if after.Revision != before.Revision || after.Status != StatusConfirmed || after.EntityID != "resident-1" || after.HistoricalReliability != 0.8 {
		t.Fatalf("rejected mutation changed chain: before=%#v after=%#v", before, after)
	}
}

func TestSnapshotUsesDefensiveCopies(t *testing.T) {
	chain := mustNewChain(t)
	if err := chain.AddObservation(observation("event-1", "vision.motion", chainTestBase, "entry"), mutation(1, "add observation", "event-1")); err != nil {
		t.Fatalf("add observation: %v", err)
	}
	if err := chain.AddContribution(ConfidenceContribution{
		ID: "contribution-1", Source: "vision", Kind: ContributionSupport, Value: 0.5,
		ObservationIDs: []string{"event-1"}, Reason: "support", CreatedAt: chainTestBase,
	}, mutation(2, "record support", "event-1")); err != nil {
		t.Fatalf("add contribution: %v", err)
	}

	copy := chain.Snapshot()
	copy.Observations[0].ID = "mutated-event"
	copy.Observations = append(copy.Observations, observation("event-2", "vision.unknown", chainTestBase.Add(time.Second), "hall"))
	copy.NodePath[0] = "mutated-node"
	copy.NodePath = append(copy.NodePath, "mutated-path")
	copy.Contributions[0].ObservationIDs[0] = "mutated-reference"
	copy.Contributions = append(copy.Contributions, ConfidenceContribution{})

	fresh := chain.Snapshot()
	if len(fresh.Observations) != 1 || fresh.Observations[0].ID != "event-1" {
		t.Fatalf("observation snapshot was not defensive: %#v", fresh.Observations)
	}
	if len(fresh.NodePath) != 1 || fresh.NodePath[0] != "entry" {
		t.Fatalf("node path snapshot was not defensive: %#v", fresh.NodePath)
	}
	if len(fresh.Contributions) != 1 || fresh.Contributions[0].ObservationIDs[0] != "event-1" {
		t.Fatalf("contribution snapshot was not defensive: %#v", fresh.Contributions)
	}
}

func mustNewChain(t *testing.T) *Chain {
	t.Helper()
	chain, err := New(ChainID("chain-test"), mutation(0, "create chain"))
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	return chain
}

func observation(id, eventType string, at time.Time, nodeID string) ObservationRef {
	return ObservationRef{ID: id, EventType: eventType, Timestamp: at, NodeID: nodeID}
}

func mutation(offset int, reason string, observationIDs ...string) MutationContext {
	return MutationContext{
		At:             chainTestBase.Add(time.Duration(offset) * time.Second),
		Actor:          "test",
		Reason:         reason,
		ObservationIDs: observationIDs,
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
