package journal

import (
	"context"
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/hypotheses"
)

var hypothesisJournalBase = time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)

func journalHypothesis(t *testing.T, observationID string) *hypotheses.HypothesisSet {
	t.Helper()
	plan := association.Plan{
		PolicyVersion: "association-v1", PlannedAt: hypothesisJournalBase, Decision: association.DecisionAmbiguous,
		Observation: chains.ObservationRef{ID: observationID, EventType: "vision.identity", Timestamp: hypothesisJournalBase},
		BestScore:   80, ScoreMargin: 0, ReasonCode: "association.ambiguous", Reason: "two candidates",
		RankedCandidates: []association.CandidateScore{
			{ChainID: "chain-a", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: 80, Facts: []association.ScoreFact{{Code: "same.entity", Score: 80}}},
			{ChainID: "chain-b", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: 80, Facts: []association.ScoreFact{{Code: "same.sequence", Score: 80}}},
		},
	}
	set, err := hypotheses.FromAmbiguousAssociation(plan, hypothesisJournalBase, chains.MutationContext{At: hypothesisJournalBase, Actor: "planner", Reason: "open hypothesis", CorrelationID: "hyp-open"})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func initializedHypothesisJournal(t *testing.T) *FileJournal {
	t.Helper()
	path := t.TempDir() + "/journal.ndjson"
	j, err := NewFileJournal(path, FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Initialize(context.Background(), GenesisInput{JournalID: "journal-hypothesis-test", CreatedAt: hypothesisJournalBase, RecordedAt: hypothesisJournalBase, Purpose: "test", Actor: "test", CorrelationID: "genesis"}); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestHypothesisRecordsAppendAndRestore(t *testing.T) {
	j := initializedHypothesisJournal(t)
	set := journalHypothesis(t, "observation-open")
	opening := set.Snapshot()
	openingRecord, err := j.AppendHypothesisOpened(context.Background(), HypothesisOpenedInput{Hypothesis: opening, RecordedAt: hypothesisJournalBase.Add(time.Second), Actor: "planner", CorrelationID: "hyp-open"})
	if err != nil {
		t.Fatal(err)
	}
	if openingRecord.Kind != RecordKindHypothesisOpened || openingRecord.Sequence != 2 {
		t.Fatalf("unexpected opening record: %+v", openingRecord)
	}
	if err := set.SetStatus(hypotheses.StatusUnderReview, chains.MutationContext{At: hypothesisJournalBase.Add(2 * time.Second), Actor: "reviewer", Reason: "review", CorrelationID: "hyp-review"}); err != nil {
		t.Fatal(err)
	}
	after := set.Snapshot()
	revision := after.History[len(after.History)-1]
	statusRecord, err := j.AppendHypothesisStatusChanged(context.Background(), HypothesisStatusChangedInput{SetID: after.ID, PreviousRevision: 1, NewRevision: 2, PreviousStatus: hypotheses.StatusOpen, NewStatus: hypotheses.StatusUnderReview, Revision: revision, RecordedAt: hypothesisJournalBase.Add(3 * time.Second), Actor: "reviewer", CorrelationID: "hyp-review"})
	if err != nil {
		t.Fatal(err)
	}
	if statusRecord.Kind != RecordKindHypothesisStatusChanged || statusRecord.Sequence != 3 {
		t.Fatalf("unexpected status record: %+v", statusRecord)
	}
	source, err := j.ReadAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if source.RecordCount != 3 || source.Records[1].Kind != RecordKindHypothesisOpened || source.Records[2].Kind != RecordKindHypothesisStatusChanged {
		t.Fatal("global sequence is incorrect")
	}
}

func TestHypothesisOpeningRequiresInitialSnapshot(t *testing.T) {
	j := initializedHypothesisJournal(t)
	set := journalHypothesis(t, "observation-noninitial")
	if err := set.SetStatus(hypotheses.StatusUnderReview, chains.MutationContext{At: hypothesisJournalBase.Add(time.Second), Actor: "reviewer", Reason: "review", CorrelationID: "review"}); err != nil {
		t.Fatal(err)
	}
	_, err := j.AppendHypothesisOpened(context.Background(), HypothesisOpenedInput{Hypothesis: set.Snapshot(), RecordedAt: hypothesisJournalBase.Add(2 * time.Second), Actor: "reviewer", CorrelationID: "review"})
	if err == nil || !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("expected invalid initial hypothesis, got %v", err)
	}
	source, readErr := j.ReadAll(context.Background())
	if readErr != nil || source.RecordCount != 1 {
		t.Fatalf("invalid append changed journal: %v", readErr)
	}
}
