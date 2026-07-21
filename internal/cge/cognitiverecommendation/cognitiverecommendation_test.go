package cognitiverecommendation

import (
	"errors"
	"testing"
	"time"

	"synora/internal/cge/cognitivesituation"
	"synora/internal/cge/durableworkflow"
	"synora/internal/cge/episodes"
)

func recommendationEpisode() *episodes.EpisodeSnapshot {
	at := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	return &episodes.EpisodeSnapshot{
		ID: "episode-recommendation-test", Status: episodes.StatusOpen, CreatedAt: at, StartedAt: at, LastObservedAt: at, StatusChangedAt: at, Revision: 1,
		Observations: []episodes.ObservationRef{{EventID: "event-recommendation-test", ObservedAt: at, ReceivedAt: at, EventType: "vision.unknown", Subject: episodes.SubjectRef{Kind: episodes.SubjectUnknown}, NodeID: "entry"}},
	}
}

func recommendationState(t testing.TB) durableworkflow.WorkflowState {
	t.Helper()
	policy := durableworkflow.DefaultPolicy()
	state := durableworkflow.WorkflowState{SchemaFingerprint: durableworkflow.SchemaFingerprint(), PolicyFingerprint: policy.Fingerprint()}
	state.Digest = durableworkflow.WorkflowStateFingerprint(state)
	episode := recommendationEpisode()
	mutation := durableworkflow.WorkflowMutation{EpisodeID: string(episode.ID), Episode: episode, SourceWorkflowRevision: state.Revision, SourceWorkflowDigest: state.Digest}
	_, state, err := durableworkflow.PlanTransaction(state, mutation, "recommendation-test-tx", 1, time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC), policy)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func observingSituation(t testing.TB) cognitivesituation.CognitiveSituation {
	t.Helper()
	state := recommendationState(t)
	situation, err := cognitivesituation.Build(cognitivesituation.BuildInput{Workflow: state, EpisodeID: "episode-recommendation-test", ExpectedDepth: cognitivesituation.DepthEpisode}, cognitivesituation.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return situation
}

func alterSituation(value cognitivesituation.CognitiveSituation, phase cognitivesituation.CognitivePhase) cognitivesituation.CognitiveSituation {
	value.Phase = phase
	value.Fingerprint = cognitivesituation.SituationFingerprint(value)
	return value
}

func coherentSituation(t testing.TB) cognitivesituation.CognitiveSituation {
	value := observingSituation(t)
	value.Phase = cognitivesituation.PhaseCoherent
	value.Hypotheses.Available = true
	value.Hypotheses.LeadingHypothesisID = "hypothesis-leading"
	value.Hypotheses.LeadingHypothesisKind = "identity_continuity"
	value.Hypotheses.LeadingCoveragePermille = 800
	value.Hypotheses.LeadingMarginPermille = 200
	value.Hypotheses.Alternatives = []cognitivesituation.HypothesisAlternative{{ID: "hypothesis-leading", Kind: "identity_continuity", Status: "supported", CoveragePermille: 800, PlausibilityPermille: 700, Rank: 1}}
	value.RecommendationReadiness.Status = cognitivesituation.ReadinessCognitiveRecommendation
	value.RecommendationReadiness.Ready = true
	value.RecommendationReadiness.Fingerprint = cognitivesituation.ReadinessFingerprint(value.RecommendationReadiness)
	value.Fingerprint = cognitivesituation.SituationFingerprint(value)
	return value
}

func TestPlanObservingAndCoherent(t *testing.T) {
	observing := observingSituation(t)
	set, err := Plan(PlanInput{Situation: observing}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Recommendations) != 1 || set.Recommendations[0].Kind != RecommendationContinueObservation || set.PrimaryRecommendationID != "" {
		t.Fatalf("observing set=%+v", set)
	}
	coherent := coherentSituation(t)
	set, err = Plan(PlanInput{Situation: coherent}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Recommendations) != 1 || set.Recommendations[0].Kind != RecommendationMaintainInterpretation {
		t.Fatalf("coherent set=%+v", set)
	}
	if err := set.Validate(DefaultPolicy()); err != nil {
		t.Fatal(err)
	}
}

func TestPlanAmbiguityAndNoPrimary(t *testing.T) {
	value := coherentSituation(t)
	value.Phase = cognitivesituation.PhaseAmbiguous
	value.Hypotheses.Ambiguous = true
	value.Hypotheses.Alternatives = append(value.Hypotheses.Alternatives, cognitivesituation.HypothesisAlternative{ID: "hypothesis-alternative", Kind: "context_state", Status: "supported", CoveragePermille: 800, PlausibilityPermille: 700, Rank: 2})
	value.Fingerprint = cognitivesituation.SituationFingerprint(value)
	set, err := Plan(PlanInput{Situation: value}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !set.Ambiguous || set.PrimaryRecommendationID != "" {
		t.Fatalf("ambiguous set=%+v", set)
	}
	if len(set.Recommendations) == 0 || set.Recommendations[0].Kind != RecommendationPreserveAmbiguity {
		t.Fatalf("ambiguity not preserved: %+v", set)
	}
}

func TestPlanAwaitingEvidenceRequiresExistingReference(t *testing.T) {
	value := coherentSituation(t)
	value.Phase = cognitivesituation.PhaseAwaitingEvidence
	value.Advisory.Active = 1
	value.Advisory.PreferredRequestID = ""
	value.Fingerprint = cognitivesituation.SituationFingerprint(value)
	if _, err := Plan(PlanInput{Situation: value}, DefaultPolicy()); !errors.Is(err, ErrMissingAdvisoryReference) {
		t.Fatalf("expected missing reference, got %v", err)
	}
	value.Advisory.PreferredRequestID = "advisory-existing"
	value.Advisory.PreferredCandidateKind = "identity_observation"
	value.Fingerprint = cognitivesituation.SituationFingerprint(value)
	set, err := Plan(PlanInput{Situation: value}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Recommendations) != 1 || set.Recommendations[0].Target.AdvisoryRequestID != "advisory-existing" {
		t.Fatalf("evidence set=%+v", set)
	}
}

func TestPlanStaleAndInvalidated(t *testing.T) {
	stale := alterSituation(observingSituation(t), cognitivesituation.PhaseStale)
	set, err := Plan(PlanInput{Situation: stale}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if set.HasApplicableRecommendation || set.Recommendations[0].Status != RecommendationBlocked {
		t.Fatalf("stale set=%+v", set)
	}
	invalid := alterSituation(observingSituation(t), cognitivesituation.PhaseInvalidated)
	set, err = Plan(PlanInput{Situation: invalid}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Recommendations) != 1 || set.Recommendations[0].Kind != RecommendationNone || set.Recommendations[0].Status != RecommendationInvalidated {
		t.Fatalf("invalid set=%+v", set)
	}
	previous := observingSituation(t)
	diff, err := cognitivesituation.Compare(previous, stale)
	if err != nil {
		t.Fatal(err)
	}
	set, err = Plan(PlanInput{Situation: stale, SituationDiff: &diff}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for _, recommendation := range set.Recommendations {
		if recommendation.Status == RecommendationApplicable {
			t.Fatalf("stale situation must not produce an applicable recommendation: %+v", set)
		}
	}
	invalidPrevious, err := Plan(PlanInput{Situation: previous}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	set, err = Plan(PlanInput{Situation: invalid, Previous: &invalidPrevious}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Recommendations) != 1 || set.Recommendations[0].Status != RecommendationInvalidated {
		t.Fatalf("invalidated situation must suppress derived prior recommendations: %+v", set)
	}
}

func TestPlanIncompleteCapabilityAndAuthorizationConstraints(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		phase cognitivesituation.CognitivePhase
		want  RecommendationKind
	}{
		{name: "incomplete", phase: cognitivesituation.PhaseIncomplete, want: RecommendationReassessContext},
		{name: "capability", phase: cognitivesituation.PhaseCapabilityUnavailable, want: RecommendationPreserveAmbiguity},
		{name: "authorization", phase: cognitivesituation.PhaseAuthorizationConstrained, want: RecommendationMaintainInterpretation},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			value := alterSituation(coherentSituation(t), testCase.phase)
			set, err := Plan(PlanInput{Situation: value}, DefaultPolicy())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, recommendation := range set.Recommendations {
				if recommendation.Kind == testCase.want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("phase=%s recommendations=%+v", testCase.phase, set.Recommendations)
			}
		})
	}
}

func TestTransitionCompareExplainAndDeterminism(t *testing.T) {
	previous := coherentSituation(t)
	current := previous
	current.Phase = cognitivesituation.PhaseAmbiguous
	current.Hypotheses.Ambiguous = true
	current.Fingerprint = cognitivesituation.SituationFingerprint(current)
	situationDiff, err := cognitivesituation.Compare(previous, current)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Plan(PlanInput{Situation: current, SituationDiff: &situationDiff}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Plan(PlanInput{Situation: current, SituationDiff: &situationDiff}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint || !first.HasCognitiveTransition {
		t.Fatalf("transition determinism first=%+v second=%+v", first, second)
	}
	explanation, err := Explain(first.Recommendations[0], DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !explanation.NotADecision || !explanation.NotAnAction || explanation.NoSecurityMeaning != true {
		t.Fatalf("explanation markers=%+v", explanation)
	}
}

func TestPreviousRecommendationsBecomeDerivedLifecycleStates(t *testing.T) {
	previousSituation := observingSituation(t)
	previousSet, err := Plan(PlanInput{Situation: previousSituation}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	currentSituation := coherentSituation(t)
	currentSet, err := Plan(PlanInput{Situation: currentSituation, Previous: &previousSet}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	foundReplacement := false
	for _, recommendation := range currentSet.Recommendations {
		if recommendation.ID == previousSet.Recommendations[0].ID && (recommendation.Status == RecommendationSuperseded || recommendation.Status == RecommendationWithdrawn) {
			foundReplacement = true
		}
	}
	if !foundReplacement {
		t.Fatalf("previous recommendation lifecycle not preserved: %+v", currentSet)
	}
}

func TestRecommendationSetCanonicalOrderAndDefensiveSnapshot(t *testing.T) {
	set, err := Plan(PlanInput{Situation: observingSituation(t)}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := CognitiveRecommendationSnapshot{WorkflowRevision: 1, SituationSnapshotDigest: "situation", RecommendationSets: []CognitiveRecommendationSet{set}, EpisodeIndex: map[string]int{set.EpisodeID: 0}}
	snapshot.Digest = RecommendationSnapshotFingerprint(snapshot)
	if err := snapshot.Validate(DefaultPolicy()); err != nil {
		t.Fatal(err)
	}
	copy := snapshot.Clone()
	copy.RecommendationSets[0].Recommendations[0].Kind = RecommendationNone
	copy.EpisodeIndex["mutated"] = 4
	if snapshot.RecommendationSets[0].Recommendations[0].Kind == RecommendationNone || len(snapshot.EpisodeIndex) != 1 {
		t.Fatal("snapshot mutable state escaped")
	}
}

func TestReadiness(t *testing.T) {
	value := Readiness()
	if !value.ReadyForHistoricalDecisionComparison || value.ProductionDecisionIntegrated || value.HistoricalDecisionComparisonImplemented || value.AutomationIntegrated || value.ActionExecutionImplemented || value.SecurityAuthority {
		t.Fatalf("unexpected readiness=%+v", value)
	}
}
