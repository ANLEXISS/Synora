package decisioncomparison

import (
	"errors"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/cognitiverecommendation"
	"synora/internal/cge/cognitivesituation"
	"synora/internal/cge/durableworkflow"
	"synora/internal/cge/episodes"
)

func comparisonState(t testing.TB) durableworkflow.WorkflowState {
	t.Helper()
	policy := durableworkflow.DefaultPolicy()
	state := durableworkflow.WorkflowState{SchemaFingerprint: durableworkflow.SchemaFingerprint(), PolicyFingerprint: policy.Fingerprint()}
	state.Digest = durableworkflow.WorkflowStateFingerprint(state)
	at := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	episode := &episodes.EpisodeSnapshot{ID: "episode-comparison-test", Status: episodes.StatusOpen, CreatedAt: at, StartedAt: at, LastObservedAt: at, StatusChangedAt: at, Revision: 1, Observations: []episodes.ObservationRef{{EventID: "event-comparison-test", ObservedAt: at, ReceivedAt: at, EventType: "vision.unknown", Subject: episodes.SubjectRef{Kind: episodes.SubjectUnknown}, NodeID: "entry"}}}
	mutation := durableworkflow.WorkflowMutation{EpisodeID: string(episode.ID), Episode: episode, SourceWorkflowRevision: state.Revision, SourceWorkflowDigest: state.Digest}
	tx, result, err := durableworkflow.PlanTransaction(state, mutation, "comparison-test-tx", 1, at, policy)
	if err != nil || tx.ResultWorkflowDigest != result.Digest {
		t.Fatalf("workflow fixture: tx=%v err=%v", tx, err)
	}
	return result
}

func comparisonSituation(t testing.TB, phase cognitivesituation.CognitivePhase) cognitivesituation.CognitiveSituation {
	t.Helper()
	state := comparisonState(t)
	value, err := cognitivesituation.Build(cognitivesituation.BuildInput{Workflow: state, EpisodeID: "episode-comparison-test", ExpectedDepth: cognitivesituation.DepthEpisode}, cognitivesituation.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	value.Phase = phase
	if phase == cognitivesituation.PhaseCoherent {
		value.Hypotheses.Available = true
		value.Hypotheses.LeadingHypothesisID = "hypothesis-leading"
		value.Hypotheses.LeadingHypothesisKind = "continuity"
		value.Hypotheses.LeadingCoveragePermille = 800
		value.Hypotheses.LeadingMarginPermille = 200
		value.Hypotheses.Alternatives = []cognitivesituation.HypothesisAlternative{{ID: "hypothesis-leading", Kind: "continuity", Status: "supported", CoveragePermille: 800, PlausibilityPermille: 700, Rank: 1}}
		value.RecommendationReadiness.Status = cognitivesituation.ReadinessCognitiveRecommendation
		value.RecommendationReadiness.Ready = true
		value.RecommendationReadiness.Fingerprint = cognitivesituation.ReadinessFingerprint(value.RecommendationReadiness)
	}
	value.Fingerprint = cognitivesituation.SituationFingerprint(value)
	return value
}

func comparisonRecommendations(t testing.TB, situation cognitivesituation.CognitiveSituation, diff *cognitivesituation.CognitiveSituationDiff) cognitiverecommendation.CognitiveRecommendationSet {
	t.Helper()
	value, err := cognitiverecommendation.Plan(cognitiverecommendation.PlanInput{Situation: situation, SituationDiff: diff}, cognitiverecommendation.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func historicalRef(stateChanged bool) HistoricalDecisionRef {
	value := HistoricalDecisionRef{ID: "historical-decision-test", SourceEventRef: "event-comparison-test", PreviousStateCode: "activity", CurrentStateCode: "activity", StateChanged: stateChanged, DecidedAtUnixNano: 100, HistoricalDecisionHasProductionAuthority: true}
	if stateChanged {
		value.PreviousStateCode, value.CurrentStateCode = "activity", "intrusion"
	}
	value.Fingerprint = HistoricalDecisionFingerprint(value)
	return value
}

func TestCompareContinuityTransitionAndConservativePosture(t *testing.T) {
	stableSituation := comparisonSituation(t, cognitivesituation.PhaseObserving)
	stableRecommendations := comparisonRecommendations(t, stableSituation, nil)
	stable, err := Compare(CompareInput{Historical: historicalRef(false), Situation: stableSituation, Recommendations: stableRecommendations}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if stable.Category != CategoryAligned || !stable.Comparable {
		t.Fatalf("stable comparison=%+v", stable)
	}

	changed, err := Compare(CompareInput{Historical: historicalRef(true), Situation: stableSituation, Recommendations: stableRecommendations}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if changed.Category != CategoryHistoricalTransitionOnly {
		t.Fatalf("historical-only transition=%+v", changed)
	}

	previous := comparisonSituation(t, cognitivesituation.PhaseObserving)
	current := comparisonSituation(t, cognitivesituation.PhaseCoherent)
	diff, err := cognitivesituation.Compare(previous, current)
	if err != nil {
		t.Fatal(err)
	}
	recommendations := comparisonRecommendations(t, current, &diff)
	cognitiveOnly, err := Compare(CompareInput{Historical: historicalRef(false), Situation: current, Recommendations: recommendations}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if cognitiveOnly.Category != CategoryCognitiveTransitionOnly || !cognitiveOnly.CognitiveTransitionFlagged {
		t.Fatalf("cognitive-only transition=%+v", cognitiveOnly)
	}

	ambiguous := comparisonSituation(t, cognitivesituation.PhaseAmbiguous)
	ambiguous.Hypotheses.Ambiguous = true
	ambiguous.Fingerprint = cognitivesituation.SituationFingerprint(ambiguous)
	ambiguousSet := comparisonRecommendations(t, ambiguous, nil)
	conservative, err := Compare(CompareInput{Historical: historicalRef(false), Situation: ambiguous, Recommendations: ambiguousSet}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if conservative.Category != CategoryCognitiveMoreConservative || conservative.SignificantDivergence {
		t.Fatalf("conservative comparison=%+v", conservative)
	}
}

func TestCompareEvidenceStaleInvalidatedAndIncomparable(t *testing.T) {
	awaiting := comparisonSituation(t, cognitivesituation.PhaseAwaitingEvidence)
	awaiting.Advisory.Active = 1
	awaiting.Advisory.PreferredRequestID = "advisory-existing"
	awaiting.Advisory.PreferredCandidateKind = "context_observation"
	awaiting.Fingerprint = cognitivesituation.SituationFingerprint(awaiting)
	set := comparisonRecommendations(t, awaiting, nil)
	value, err := Compare(CompareInput{Historical: historicalRef(false), Situation: awaiting, Recommendations: set}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	foundEvidence := false
	for _, dimension := range value.Dimensions {
		foundEvidence = foundEvidence || dimension.Kind == DimensionEvidencePosture
	}
	if !foundEvidence {
		t.Fatalf("evidence dimension missing: %+v", value)
	}

	stale := comparisonSituation(t, cognitivesituation.PhaseStale)
	staleSet := comparisonRecommendations(t, stale, nil)
	value, err = Compare(CompareInput{Historical: historicalRef(false), Situation: stale, Recommendations: staleSet}, DefaultPolicy())
	if err != nil || value.Category != CategoryStale || value.Comparable {
		t.Fatalf("stale value=%+v err=%v", value, err)
	}
	invalid := comparisonSituation(t, cognitivesituation.PhaseInvalidated)
	invalidSet := comparisonRecommendations(t, invalid, nil)
	value, err = Compare(CompareInput{Historical: historicalRef(false), Situation: invalid, Recommendations: invalidSet}, DefaultPolicy())
	if err != nil || value.Category != CategoryInvalidated || value.Comparable {
		t.Fatalf("invalid value=%+v err=%v", value, err)
	}
	unknown := historicalRef(false)
	unknown.CurrentStateCode = ""
	unknown.Fingerprint = HistoricalDecisionFingerprint(unknown)
	value, err = Compare(CompareInput{Historical: unknown, Situation: comparisonSituation(t, cognitivesituation.PhaseObserving), Recommendations: comparisonRecommendations(t, comparisonSituation(t, cognitivesituation.PhaseObserving), nil)}, DefaultPolicy())
	if err != nil || value.Category != CategoryIncomparable {
		t.Fatalf("unknown-state value=%+v err=%v", value, err)
	}
}

func TestCompareDescriptiveDivergenceCategories(t *testing.T) {
	observing := comparisonSituation(t, cognitivesituation.PhaseObserving)
	observingSet := comparisonRecommendations(t, observing, nil)
	moreDecisiveRef := historicalRef(false)
	moreDecisiveRef.Escalated = true
	moreDecisiveRef.Fingerprint = HistoricalDecisionFingerprint(moreDecisiveRef)
	moreDecisive, err := Compare(CompareInput{Historical: moreDecisiveRef, Situation: observing, Recommendations: observingSet}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if moreDecisive.Category != CategoryHistoricalMoreDecisive || !moreDecisive.Markers.NotAnAlert {
		t.Fatalf("historical-more-decisive comparison=%+v", moreDecisive)
	}

	changed, err := Compare(CompareInput{Historical: historicalRef(true), Situation: observing, Recommendations: observingSet}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !changed.SignificantDivergence || changed.Category != CategoryHistoricalTransitionOnly {
		t.Fatalf("significant descriptive divergence=%+v", changed)
	}
}

func TestCompareRejectsInvalidPreviousComparison(t *testing.T) {
	situation := comparisonSituation(t, cognitivesituation.PhaseObserving)
	set := comparisonRecommendations(t, situation, nil)
	previous := HistoricalDecisionComparison{ID: "previous", EpisodeID: situation.EpisodeID}
	if _, err := Compare(CompareInput{Historical: historicalRef(false), Situation: situation, Recommendations: set, Previous: &previous}, DefaultPolicy()); !errors.Is(err, ErrInvalidComparison) {
		t.Fatalf("expected invalid previous comparison error, got %v", err)
	}
}

func TestCompareValidationExplanationDeterminismAndLifecycle(t *testing.T) {
	situation := comparisonSituation(t, cognitivesituation.PhaseObserving)
	set := comparisonRecommendations(t, situation, nil)
	input := CompareInput{Historical: historicalRef(false), Situation: situation, Recommendations: set}
	first, err := Compare(input, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compare(input, DefaultPolicy())
	if err != nil || first.Fingerprint != second.Fingerprint {
		t.Fatalf("non-deterministic comparison first=%+v second=%+v err=%v", first, second, err)
	}
	repeated, err := Compare(CompareInput{Historical: input.Historical, Situation: situation, Recommendations: set, Previous: &first}, DefaultPolicy())
	if err != nil || repeated.Revision != first.Revision || repeated.Fingerprint != first.Fingerprint {
		t.Fatalf("recompare changed comparison first=%+v repeated=%+v err=%v", first, repeated, err)
	}
	explanation, err := Explain(first, DefaultPolicy())
	if err != nil || !explanation.HistoricalDecisionRetainsAuthority || !explanation.NotAnAlert || !explanation.CalibrationOnly {
		t.Fatalf("explanation=%+v err=%v", explanation, err)
	}
	forged := input.Historical
	forged.Fingerprint = "forged"
	if _, err := Compare(CompareInput{Historical: forged, Situation: situation, Recommendations: set}, DefaultPolicy()); !errors.Is(err, ErrInvalidHistoricalDecisionRef) {
		t.Fatalf("expected forged fingerprint error, got %v", err)
	}
	wrong := set
	wrong.SituationID = "other"
	wrong.Fingerprint = cognitiverecommendation.RecommendationSetFingerprint(wrong)
	if _, err := Compare(CompareInput{Historical: input.Historical, Situation: situation, Recommendations: wrong}, DefaultPolicy()); !errors.Is(err, ErrSourceFingerprintMismatch) {
		t.Fatalf("expected lineage error, got %v", err)
	}
}

func TestComparisonSnapshotDefensiveAndConcurrent(t *testing.T) {
	situation := comparisonSituation(t, cognitivesituation.PhaseObserving)
	set := comparisonRecommendations(t, situation, nil)
	value, err := Compare(CompareInput{Historical: historicalRef(false), Situation: situation, Recommendations: set}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := HistoricalDecisionComparisonSnapshot{WorkflowRevision: 1, ProjectionDigest: "projection", Comparisons: []HistoricalDecisionComparison{value}, EpisodeIndex: map[string]int{value.EpisodeID: 0}}
	snapshot.Digest = ComparisonSnapshotFingerprint(snapshot)
	copy := snapshot.Clone()
	copy.Comparisons[0].Category = CategoryDivergent
	copy.EpisodeIndex["mutated"] = 4
	if snapshot.Comparisons[0].Category == CategoryDivergent || len(snapshot.EpisodeIndex) != 1 {
		t.Fatal("comparison snapshot leaked mutable state")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Compare(CompareInput{Historical: historicalRef(false), Situation: situation, Recommendations: set}, DefaultPolicy()); err != nil {
				t.Errorf("concurrent compare: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestReadiness(t *testing.T) {
	value := Readiness()
	if !value.ReadyForCalibrationLedger || !value.HistoricalAuthorityPreserved ||
		!value.ComparisonRecoverySupported || !value.DurableCalibrationLedgerImplemented ||
		value.AutomaticCalibrationImplemented || value.ProductionDecisionFeedbackImplemented ||
		value.ProductionDecisionOverrideImplemented || value.ActionExecutionImplemented || value.SecurityAuthority {
		t.Fatalf("unexpected readiness=%+v", value)
	}
}
