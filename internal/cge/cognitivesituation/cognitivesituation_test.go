package cognitivesituation

import (
	"errors"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/durableworkflow"
	"synora/internal/cge/episodes"
	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

func qualificationEpisode() *episodes.EpisodeSnapshot {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	return &episodes.EpisodeSnapshot{
		ID: "episode-cognitive-test", Status: episodes.StatusOpen,
		CreatedAt: now, StartedAt: now, LastObservedAt: now, StatusChangedAt: now,
		Revision: 1,
		Observations: []episodes.ObservationRef{{
			EventID: "event-cognitive-test", ObservedAt: now, ReceivedAt: now,
			EventType: "vision.unknown",
			Subject:   episodes.SubjectRef{Kind: episodes.SubjectUnknown},
			NodeID:    "entry",
		}},
	}
}

func initialQualificationState(policy durableworkflow.Policy) durableworkflow.WorkflowState {
	state := durableworkflow.WorkflowState{
		SchemaFingerprint: durableworkflow.SchemaFingerprint(),
		PolicyFingerprint: policy.Fingerprint(),
	}
	state.Digest = durableworkflow.WorkflowStateFingerprint(state)
	return state
}

func applyQualificationMutation(t *testing.T, state durableworkflow.WorkflowState, mutation durableworkflow.WorkflowMutation, sequence uint64) durableworkflow.WorkflowState {
	t.Helper()
	policy := durableworkflow.DefaultPolicy()
	mutation.SourceWorkflowRevision = state.Revision
	mutation.SourceWorkflowDigest = state.Digest
	tx, result, err := durableworkflow.PlanTransaction(state, mutation, durableworkflow.WorkflowTransactionID("qualification-tx-"+string(rune(sequence+'0'))), sequence, time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), policy)
	if err != nil {
		t.Fatal(err)
	}
	if tx.ResultWorkflowDigest != result.Digest {
		t.Fatal("transaction/result digest mismatch")
	}
	return result
}

func stateWithEpisode(t *testing.T) durableworkflow.WorkflowState {
	t.Helper()
	state := initialQualificationState(durableworkflow.DefaultPolicy())
	episode := qualificationEpisode()
	return applyQualificationMutation(t, state, durableworkflow.WorkflowMutation{EpisodeID: string(episode.ID), Episode: episode}, 1)
}

func stateWithFacts(t *testing.T) durableworkflow.WorkflowState {
	t.Helper()
	state := stateWithEpisode(t)
	episode := *state.Episodes[0].Episode
	facts, err := situationfacts.Extract(situationfacts.ExtractionInput{
		Episode: episode, ExtractedAt: time.Date(2026, 7, 21, 10, 1, 0, 0, time.UTC),
	}, situationfacts.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return applyQualificationMutation(t, state, durableworkflow.WorkflowMutation{
		EpisodeID: string(episode.ID), Facts: &facts,
	}, 2)
}

func TestBuildDepthAndConfiguredLayers(t *testing.T) {
	state := stateWithEpisode(t)
	situation, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthEpisode}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if situation.Phase != PhaseObserving || situation.RecommendationReadiness.Status != ReadinessNotReady {
		t.Fatalf("unexpected episode phase/readiness: %+v", situation)
	}
	if situation.Knowledge.ExpectedLayers != 1 || situation.Knowledge.FreshLayers != 1 {
		t.Fatalf("unexpected knowledge summary: %+v", situation.Knowledge)
	}
	if situation.Knowledge.LayerStates[1].ReasonCodes[0] != "layer.not_configured" {
		t.Fatalf("unexpected unconfigured layer: %+v", situation.Knowledge.LayerStates[1])
	}
}

func TestFactsPresentAndHypothesesNotExpected(t *testing.T) {
	state := stateWithFacts(t)
	situation, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthSituationFacts}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if situation.Knowledge.FreshLayers != 2 || situation.Knowledge.AbsentExpectedLayers != 0 {
		t.Fatalf("unexpected facts knowledge: %+v", situation.Knowledge)
	}
	if situation.Knowledge.LayerStates[2].ReasonCodes[0] != "layer.not_configured" {
		t.Fatalf("hypotheses should be not configured: %+v", situation.Knowledge.LayerStates[2])
	}
}

func TestStaleInvalidatedAndIncompletePhases(t *testing.T) {
	state := stateWithEpisode(t)
	stale, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthSituationFacts}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if stale.Phase != PhaseStale || stale.RecommendationReadiness.Status != ReadinessBlockedStaleness {
		t.Fatalf("expected stale phase: %+v", stale)
	}
	invalidState := applyQualificationMutation(t, state, durableworkflow.WorkflowMutation{
		EpisodeID:             "episode-cognitive-test",
		ExplicitInvalidations: []durableworkflow.LayerKind{durableworkflow.LayerSituationFacts},
	}, 2)
	invalid, err := Build(BuildInput{Workflow: invalidState, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthSituationFacts}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if invalid.Phase != PhaseInvalidated {
		t.Fatalf("expected invalidated phase: %+v", invalid)
	}
	if _, err := Build(BuildInput{Workflow: state, EpisodeID: "missing", ExpectedDepth: DepthEpisode}, DefaultPolicy()); !errors.Is(err, ErrEpisodeNotFound) {
		t.Fatalf("expected missing episode, got %v", err)
	}
}

func TestHypothesisSummaryPreservesUpstreamLeaderAndAmbiguity(t *testing.T) {
	state := stateWithFacts(t)
	episodeState := state.Episodes[0]
	evaluation, err := situationhypotheses.Evaluate(situationhypotheses.EvaluationInput{
		FactSet: *episodeState.Facts,
	}, situationhypotheses.Schema(), situationhypotheses.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	next := applyQualificationMutation(t, state, durableworkflow.WorkflowMutation{
		EpisodeID: "episode-cognitive-test", Hypotheses: &evaluation.Set,
	}, 3)
	situation, err := Build(BuildInput{Workflow: next, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthSituationHypotheses}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !situation.Hypotheses.Available {
		t.Fatal("hypothesis summary unavailable")
	}
	if situation.Hypotheses.LeadingHypothesisID != string(evaluation.Set.LeadingHypothesisID) {
		t.Fatalf("leader changed: got %q want %q", situation.Hypotheses.LeadingHypothesisID, evaluation.Set.LeadingHypothesisID)
	}
	if situation.Hypotheses.LeadingPlausibilityPermille > 1000 || situation.Hypotheses.LeadingCoveragePermille > 1000 {
		t.Fatalf("out of bounds hypothesis summary: %+v", situation.Hypotheses)
	}
}

func TestReadinessAndNoAuthorityMarkers(t *testing.T) {
	state := stateWithFacts(t)
	situation, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthSituationFacts}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !situation.Markers.NotADecision || !situation.Markers.NotAuthorization || !situation.Markers.NotACommand || !situation.Markers.NotAnAction || !situation.Markers.NoSecurityMeaning {
		t.Fatalf("authority markers not enforced: %+v", situation.Markers)
	}
	if situation.RecommendationReadiness.Ready {
		t.Fatalf("facts-only state should not be recommendation-ready: %+v", situation.RecommendationReadiness)
	}
	if _, err := Explain(situation, DefaultPolicy()); err != nil {
		t.Fatal(err)
	}
}

func TestDeterministicFingerprintAndCompare(t *testing.T) {
	state := stateWithEpisode(t)
	first, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthEpisode}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthEpisode}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint || first.Revision != second.Revision {
		t.Fatalf("non-deterministic build: %s/%s revisions %d/%d", first.Fingerprint, second.Fingerprint, first.Revision, second.Revision)
	}
	diff, err := Compare(first, second)
	if err != nil {
		t.Fatal(err)
	}
	if diff.PhaseChanged || diff.LeadingHypothesisChanged || diff.ReadinessChanged {
		t.Fatalf("identical situations changed: %+v", diff)
	}
}

func TestSnapshotDefensiveAndConcurrentBuild(t *testing.T) {
	state := stateWithEpisode(t)
	value, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthEpisode}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := CognitiveSituationSnapshot{WorkflowRevision: state.Revision, Situations: []CognitiveSituation{value}, EpisodeIndex: map[string]int{value.EpisodeID: 0}}
	snapshot.Digest = SnapshotFingerprint(snapshot)
	copy := snapshot.Clone()
	copy.Situations[0].SourceFingerprints.AdvisoryRequests = append(copy.Situations[0].SourceFingerprints.AdvisoryRequests, "external")
	copy.EpisodeIndex["other"] = 1
	if len(snapshot.Situations[0].SourceFingerprints.AdvisoryRequests) != 0 || len(snapshot.EpisodeIndex) != 1 {
		t.Fatal("snapshot leaked mutable state")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthEpisode}, DefaultPolicy()); err != nil {
				t.Errorf("concurrent build: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestSnapshotFingerprintCanonicalizesEpisodeOrder(t *testing.T) {
	state := stateWithEpisode(t)
	secondEpisode := qualificationEpisode()
	secondEpisode.ID = "episode-cognitive-test-second"
	secondEpisode.Observations[0].EventID = "event-cognitive-test-second"
	state = applyQualificationMutation(t, state, durableworkflow.WorkflowMutation{EpisodeID: string(secondEpisode.ID), Episode: secondEpisode}, 2)

	first, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthEpisode}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test-second", ExpectedDepth: DepthEpisode}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	forward := CognitiveSituationSnapshot{
		WorkflowRevision: state.Revision,
		Situations:       []CognitiveSituation{first, second},
		EpisodeIndex:     map[string]int{first.EpisodeID: 0, second.EpisodeID: 1},
	}
	reverse := CognitiveSituationSnapshot{
		WorkflowRevision: state.Revision,
		Situations:       []CognitiveSituation{second, first},
		EpisodeIndex:     map[string]int{first.EpisodeID: 1, second.EpisodeID: 0},
	}
	if SnapshotFingerprint(forward) != SnapshotFingerprint(reverse) {
		t.Fatal("snapshot fingerprint depends on episode order")
	}
}

func TestReadiness(t *testing.T) {
	value := Readiness()
	if !value.ConsolidationImplemented || !value.Deterministic || !value.ReadyForRecommendationPlanning ||
		value.ProductionDecisionIntegrated || value.RecommendationEngineImplemented || value.ActionExecutionImplemented || value.SecurityAuthority {
		t.Fatalf("unexpected readiness: %+v", value)
	}
}
