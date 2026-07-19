package durable

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/chains/generations"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/hypotheses"
)

var durableHypothesisBase = time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)

func durableHypothesisSet(t *testing.T, observationID string) *hypotheses.HypothesisSet {
	t.Helper()
	plan := association.Plan{
		PolicyVersion: "association-v1", PlannedAt: durableHypothesisBase, Decision: association.DecisionAmbiguous,
		Observation: chains.ObservationRef{ID: observationID, EventType: "vision.identity", Timestamp: durableHypothesisBase},
		BestScore:   70, ScoreMargin: 0, ReasonCode: "association.ambiguous", Reason: "competing candidates",
		RankedCandidates: []association.CandidateScore{
			{ChainID: "chain-a", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: 70, Facts: []association.ScoreFact{{Code: "same.entity", Score: 70}}},
			{ChainID: "chain-b", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: 70, Facts: []association.ScoreFact{{Code: "same.sequence", Score: 70}}},
		},
	}
	set, err := hypotheses.FromAmbiguousAssociation(plan, durableHypothesisBase, chains.MutationContext{At: durableHypothesisBase, Actor: "planner", Reason: "open hypothesis", CorrelationID: "open-" + observationID})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func durableCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	j, err := journal.NewFileJournal(t.TempDir()+"/journal.ndjson", journal.FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Initialize(context.Background(), journal.GenesisInput{JournalID: "durable-hypothesis", CreatedAt: durableHypothesisBase, RecordedAt: durableHypothesisBase, Purpose: "test", Actor: "test", CorrelationID: "genesis"}); err != nil {
		t.Fatal(err)
	}
	coordinator, _, err := FromJournal(context.Background(), j)
	if err != nil {
		t.Fatal(err)
	}
	return coordinator
}

func TestCoordinatorOwnsAndPersistsHypotheses(t *testing.T) {
	c := durableCoordinator(t)
	set := durableHypothesisSet(t, "durable-open")
	result, err := c.AddHypothesis(context.Background(), set, durableHypothesisBase.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !result.Published || result.Idempotent || result.Kind != HypothesisMutationOpened || result.JournalSequence != 2 || c.CountHypotheses() != 1 {
		t.Fatalf("unexpected opening result: %+v", result)
	}
	idempotent, err := c.AddHypothesis(context.Background(), set, durableHypothesisBase.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if idempotent.Applied || !idempotent.Idempotent || idempotent.JournalSequence != 0 {
		t.Fatalf("unexpected idempotent result: %+v", idempotent)
	}
	command := hypotheses.SetStatusCommand{SetID: set.ID(), SourceRevision: 1, Target: hypotheses.StatusUnderReview, Mutation: chains.MutationContext{At: durableHypothesisBase.Add(3 * time.Second), Actor: "reviewer", Reason: "review", CorrelationID: "status-1"}}
	status, err := c.SetHypothesisStatus(context.Background(), command, durableHypothesisBase.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !status.Applied || !status.Published || status.After.Status != hypotheses.StatusUnderReview || status.After.Revision != 2 || status.JournalSequence != 3 {
		t.Fatalf("unexpected status result: %+v", status)
	}
	recovered, metadata, err := FromJournal(context.Background(), c.journal)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.HypothesisCount != 1 || recovered.CountHypotheses() != 1 {
		t.Fatalf("unexpected recovery metadata: %+v", metadata)
	}
	restored, err := recovered.GetHypothesis(set.ID())
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != hypotheses.StatusUnderReview || restored.Revision != 2 {
		t.Fatalf("unexpected restored hypothesis: %+v", restored)
	}
}

func TestCoordinatorRejectsStaleHypothesisStatus(t *testing.T) {
	c := durableCoordinator(t)
	set := durableHypothesisSet(t, "stale-status")
	if _, err := c.AddHypothesis(context.Background(), set, durableHypothesisBase.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	command := hypotheses.SetStatusCommand{SetID: set.ID(), SourceRevision: 1, Target: hypotheses.StatusUnderReview, Mutation: chains.MutationContext{At: durableHypothesisBase.Add(2 * time.Second), Actor: "reviewer", Reason: "review", CorrelationID: "status-1"}}
	if _, err := c.SetHypothesisStatus(context.Background(), command, durableHypothesisBase.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err := c.SetHypothesisStatus(context.Background(), command, durableHypothesisBase.Add(4*time.Second))
	if err == nil || !errors.Is(err, hypotheses.ErrStaleHypothesisCommand) {
		t.Fatalf("expected stale hypothesis command, got %v", err)
	}
	if c.Status().State != StateReady || c.CountHypotheses() != 1 {
		t.Fatal("stale command changed coordinator state")
	}
}

func TestCoordinatorRebasesHypothesisAppendOnlyAndReplays(t *testing.T) {
	c := durableCoordinator(t)
	set := durableHypothesisSet(t, "durable-rebase")
	if _, err := c.AddHypothesis(context.Background(), set, durableHypothesisBase.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	current, err := c.GetHypothesis(set.ID())
	if err != nil {
		t.Fatal(err)
	}
	plan := association.Plan{PolicyVersion: "association-v2", PlannedAt: durableHypothesisBase.Add(2 * time.Second), Decision: association.DecisionAmbiguous, Observation: chains.ObservationRef{ID: "durable-rebase", EventType: "vision.identity", Timestamp: durableHypothesisBase}, BestScore: 80, ReasonCode: association.ReasonAmbiguous, Reason: "new candidates", RankedCandidates: []association.CandidateScore{
		{ChainID: "chain-a", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: 80, Facts: []association.ScoreFact{{Code: "entity.same", Score: 80}}},
		{ChainID: "chain-c", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: 80, Facts: []association.ScoreFact{{Code: "node.same", Score: 80}}},
	}}
	proposal, err := hypotheses.ProposeAssociationRebase(current, plan, durableHypothesisBase.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	command, err := proposal.Command(chains.MutationContext{At: durableHypothesisBase.Add(3 * time.Second), Actor: "reviewer", Reason: "re-evaluate", CorrelationID: "rebase-durable"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.RebaseHypothesis(context.Background(), command, durableHypothesisBase.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !result.Published || result.NewAssessmentVersion != 2 || result.JournalSequence != 3 {
		t.Fatalf("unexpected rebase result: %+v", result)
	}
	idempotent, err := c.RebaseHypothesis(context.Background(), command, durableHypothesisBase.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !idempotent.Idempotent || idempotent.Applied {
		t.Fatalf("expected idempotent rebase: %+v", idempotent)
	}
	recovered, _, err := FromJournal(context.Background(), c.journal)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := recovered.GetHypothesis(set.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, result.After) || len(restored.Assessments) != 2 {
		t.Fatal("replayed rebase differs from published state")
	}
}

func TestCoordinatorSupersedesEvidenceWithOneWALRecord(t *testing.T) {
	c := durableCoordinator(t)
	evaluation := evidence.EvidenceEvaluation{ChainID: "chain-evidence", SourceRevision: 4, TargetObservationID: "obs-supersede", EvaluatedAt: durableHypothesisBase, PolicyNamespace: "synora.cge.evidence", PolicyVersion: "evidence-v1", EvidenceFingerprint: "fingerprint-one", ResolutionValues: evidence.ResolutionValues{SupportValue: 0.10, ContradictionValue: 0.15, NeutralValue: 0}, Decision: evidence.DecisionAmbiguous, SupportScore: 70, ContradictionScore: 70, DecisionMargin: 0, Facts: []evidence.EvidenceFact{{Code: "entity.same", Side: evidence.EvidenceSupport, Score: 70, ObservationIDs: []string{"obs-supersede"}}, {Code: "entity.conflict", Side: evidence.EvidenceContradiction, Score: 70, ObservationIDs: []string{"obs-supersede"}}}, ReasonCode: "evidence.ambiguous", Reason: "competing evidence"}
	set, err := hypotheses.FromAmbiguousEvidence(evaluation, durableHypothesisBase, chains.MutationContext{At: durableHypothesisBase, Actor: "planner", Reason: "open evidence", CorrelationID: "open-evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddHypothesis(context.Background(), set, durableHypothesisBase.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	evaluation.EvidenceFingerprint = "fingerprint-two"
	evaluation.PolicyVersion = "evidence-v2"
	proposal, err := hypotheses.ProposeEvidenceSupersession(set.Snapshot(), evaluation, durableHypothesisBase.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	command, err := proposal.Command(chains.MutationContext{At: durableHypothesisBase.Add(3 * time.Second), Actor: "reviewer", Reason: "new evidence subject", CorrelationID: "supersede-evidence"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.SupersedeHypothesis(context.Background(), command, durableHypothesisBase.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !result.Published || result.JournalSequence != 3 || result.PreviousAfter.Status != hypotheses.StatusSuperseded || result.NewAfter.Status != hypotheses.StatusOpen {
		t.Fatalf("unexpected supersession result: %+v", result)
	}
	lineage, err := c.HypothesisLineage(result.NewSetID)
	if err != nil || len(lineage) != 2 || lineage[0].ID != result.PreviousSetID || lineage[1].ID != result.NewSetID {
		t.Fatalf("unexpected lineage: %v %+v", err, lineage)
	}
	recovered, metadata, err := FromJournal(context.Background(), c.journal)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.HypothesisReplay.SupersessionsApplied != 1 || recovered.CountHypotheses() != 2 {
		t.Fatalf("unexpected replay metadata: %+v", metadata)
	}
	restored, err := recovered.GetHypothesis(result.PreviousSetID)
	if err != nil || restored.Status != hypotheses.StatusSuperseded {
		t.Fatalf("predecessor not restored: %v %+v", err, restored)
	}
}

func TestGenerationRecoveryReplaysHypothesesFromFullJournal(t *testing.T) {
	root := t.TempDir()
	j, err := journal.NewFileJournal(root+"/journal.ndjson", journal.FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Initialize(context.Background(), journal.GenesisInput{JournalID: "generation-hypothesis", CreatedAt: durableHypothesisBase, RecordedAt: durableHypothesisBase, Purpose: "test", Actor: "test", CorrelationID: "genesis"}); err != nil {
		t.Fatal(err)
	}
	c, _, err := FromJournal(context.Background(), j)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := chains.New("chain-generation", chains.MutationContext{At: durableHypothesisBase, Actor: "test", Reason: "create chain", CorrelationID: "chain-create"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddChain(context.Background(), chain, "test", "chain-create", durableHypothesisBase.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	set := durableHypothesisSet(t, "before-checkpoint")
	if _, err := c.AddHypothesis(context.Background(), set, durableHypothesisBase.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	store, err := generations.NewStore(root+"/generations", generations.StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := c.CreateSnapshotGeneration(context.Background(), store, durableHypothesisBase.Add(3*time.Second), "snapshot", "snapshot-1"); err != nil || !result.ManifestPublished {
		t.Fatalf("snapshot failed: result=%+v err=%v", result, err)
	}
	post := durableHypothesisSet(t, "after-checkpoint")
	if _, err := c.AddHypothesis(context.Background(), post, durableHypothesisBase.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	recovered, _, err := FromGenerationManifest(context.Background(), store, j)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Count() != c.Count() || recovered.CountHypotheses() != 2 {
		t.Fatalf("manifest recovery lost state: chains=%d/%d hypotheses=%d", recovered.Count(), c.Count(), recovered.CountHypotheses())
	}
}

func TestHypothesisPublicationFailureKeepsPublishedStateUntilRecovery(t *testing.T) {
	c := durableCoordinator(t)
	set := durableHypothesisSet(t, "publication-failure")
	c.publishHook = func() error { return errors.New("injected publication failure") }
	result, err := c.AddHypothesis(context.Background(), set, durableHypothesisBase.Add(time.Second))
	if err == nil || !errors.Is(err, ErrPublicationFailed) || result.Published || c.CountHypotheses() != 0 || c.Status().State != StateDegraded {
		t.Fatalf("unexpected publication failure: result=%+v err=%v status=%+v", result, err, c.Status())
	}
	c.publishHook = nil
	if _, err := c.RecoverFromJournal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.CountHypotheses() != 1 || c.Status().State != StateReady {
		t.Fatal("explicit recovery did not publish the durable hypothesis")
	}
}

func TestHypothesisReplayFailureDoesNotReplaceCurrentRegistries(t *testing.T) {
	c := durableCoordinator(t)
	set := durableHypothesisSet(t, "atomic-recovery")
	if _, err := c.AddHypothesis(context.Background(), set, durableHypothesisBase.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	chain, err := chains.New("atomic-chain", chains.MutationContext{At: durableHypothesisBase, Actor: "test", Reason: "create chain", CorrelationID: "atomic-chain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddChain(context.Background(), chain, "test", "atomic-chain", durableHypothesisBase.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	beforeHypotheses := c.ListHypotheses()
	beforeChains := c.List()
	if _, err := c.journal.AppendHypothesisStatusChanged(context.Background(), journal.HypothesisStatusChangedInput{
		SetID: set.ID(), PreviousRevision: 99, NewRevision: 100, PreviousStatus: hypotheses.StatusOpen, NewStatus: hypotheses.StatusUnderReview,
		Revision:   hypotheses.RevisionRecord{SetID: set.ID(), Operation: hypotheses.OperationHypothesisStatusChanged, PreviousRevision: 99, NewRevision: 100, At: durableHypothesisBase.Add(3 * time.Second), Actor: "reviewer", Reason: "bad", CorrelationID: "bad", PreviousStatus: hypotheses.StatusOpen, NewStatus: hypotheses.StatusUnderReview},
		RecordedAt: durableHypothesisBase.Add(4 * time.Second), Actor: "reviewer", CorrelationID: "bad",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RecoverFromJournal(context.Background()); err == nil || !errors.Is(err, ErrHypothesisReplayFailed) {
		t.Fatalf("expected hypothesis replay failure, got %v", err)
	}
	if !reflect.DeepEqual(beforeHypotheses, c.ListHypotheses()) || !reflect.DeepEqual(beforeChains, c.List()) {
		t.Fatal("failed recovery replaced a current registry")
	}
}
