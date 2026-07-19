package replay

import (
	"context"
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/hypotheses"
)

var replayBase = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

func replaySet(t *testing.T, id string) *hypotheses.HypothesisSet {
	t.Helper()
	plan := association.Plan{
		PolicyVersion: "association-v1", PlannedAt: replayBase, Decision: association.DecisionAmbiguous,
		Observation: chains.ObservationRef{ID: id, EventType: "vision.identity", Timestamp: replayBase},
		BestScore:   60, ScoreMargin: 0, ReasonCode: "association.ambiguous", Reason: "competing candidates",
		RankedCandidates: []association.CandidateScore{
			{ChainID: "chain-a", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: 60, Facts: []association.ScoreFact{{Code: "sequence.same", Score: 60}}},
			{ChainID: "chain-b", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: 60, Facts: []association.ScoreFact{{Code: "track.same", Score: 60}}},
		},
	}
	set, err := hypotheses.FromAmbiguousAssociation(plan, replayBase, chains.MutationContext{At: replayBase, Actor: "planner", Reason: "open hypothesis", CorrelationID: "open-" + id})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func replayJournal(t *testing.T) (*journal.FileJournal, *hypotheses.HypothesisSet) {
	t.Helper()
	j, err := journal.NewFileJournal(t.TempDir()+"/journal.ndjson", journal.FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Initialize(context.Background(), journal.GenesisInput{JournalID: "hyp-replay", CreatedAt: replayBase, RecordedAt: replayBase, Purpose: "test", Actor: "test", CorrelationID: "genesis"}); err != nil {
		t.Fatal(err)
	}
	set := replaySet(t, "replay-observation")
	opening := set.Snapshot()
	if _, err := j.AppendHypothesisOpened(context.Background(), journal.HypothesisOpenedInput{Hypothesis: opening, RecordedAt: replayBase.Add(time.Second), Actor: "planner", CorrelationID: "open-replay-observation"}); err != nil {
		t.Fatal(err)
	}
	if err := set.SetStatus(hypotheses.StatusUnderReview, chains.MutationContext{At: replayBase.Add(2 * time.Second), Actor: "reviewer", Reason: "inspect", CorrelationID: "status-replay"}); err != nil {
		t.Fatal(err)
	}
	after := set.Snapshot()
	if _, err := j.AppendHypothesisStatusChanged(context.Background(), journal.HypothesisStatusChangedInput{SetID: after.ID, PreviousRevision: 1, NewRevision: 2, PreviousStatus: hypotheses.StatusOpen, NewStatus: hypotheses.StatusUnderReview, Revision: after.History[1], RecordedAt: replayBase.Add(3 * time.Second), Actor: "reviewer", CorrelationID: "status-replay"}); err != nil {
		t.Fatal(err)
	}
	return j, set
}

func TestFromJournalReplaysHypothesesAndSkipsOtherRecords(t *testing.T) {
	j, expected := replayJournal(t)
	source, err := j.ReadAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registry, metadata, err := FromJournal(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if registry.Count() != 1 || metadata.SetsOpened != 1 || metadata.StatusChangesApplied != 1 || metadata.RecordsApplied != 2 || metadata.RecordsSkipped != 1 || metadata.FinalHeadSequence != source.HeadSequence || metadata.FinalHeadHash != source.HeadHash {
		t.Fatalf("unexpected replay metadata: %+v", metadata)
	}
	actual, err := registry.Get(expected.ID())
	if err != nil {
		t.Fatal(err)
	}
	if actual.Status != hypotheses.StatusUnderReview || actual.Revision != 2 || len(actual.History) != 2 {
		t.Fatalf("unexpected restored set: %+v", actual)
	}
}

func TestFromJournalReturnsNoPartialRegistry(t *testing.T) {
	j, expected := replayJournal(t)
	if _, err := j.AppendHypothesisStatusChanged(context.Background(), journal.HypothesisStatusChangedInput{SetID: expected.ID(), PreviousRevision: 99, NewRevision: 100, PreviousStatus: hypotheses.StatusOpen, NewStatus: hypotheses.StatusUnderReview, Revision: hypotheses.RevisionRecord{SetID: expected.ID(), Operation: hypotheses.OperationHypothesisStatusChanged, PreviousRevision: 99, NewRevision: 100, At: replayBase.Add(4 * time.Second), Actor: "reviewer", Reason: "bad", CorrelationID: "bad", PreviousStatus: hypotheses.StatusOpen, NewStatus: hypotheses.StatusUnderReview}, RecordedAt: replayBase.Add(5 * time.Second), Actor: "reviewer", CorrelationID: "bad"}); err != nil {
		t.Fatal(err)
	}
	source, err := j.ReadAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registry, _, err := FromJournal(context.Background(), source)
	if registry != nil || err == nil || !errors.Is(err, ErrHypothesisReplayFailed) {
		t.Fatalf("expected atomic replay failure, registry=%v err=%v", registry, err)
	}
}
