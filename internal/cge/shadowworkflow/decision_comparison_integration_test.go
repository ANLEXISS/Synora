package shadowworkflow

import (
	"context"
	"testing"
	"time"

	"synora/internal/cge/decisioncomparison"
)

func TestHistoricalDecisionComparisonPublishedWithCognitiveProjection(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	ref := decisioncomparison.HistoricalDecisionRef{ID: "historical-comparison-integration", SourceEventRef: "comparison-integration-event", PreviousStateCode: "activity", CurrentStateCode: "activity", HistoricalDecisionHasProductionAuthority: true, DecidedAtUnixNano: at.UnixNano()}
	ref.Fingerprint = decisioncomparison.HistoricalDecisionFingerprint(ref)
	input := testInput(at, "comparison-integration-event")
	input.HistoricalDecision = &ref
	if result := r.TrySubmit(input); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CommitsSucceeded == 1 })
	snapshot := r.HistoricalDecisionComparisons()
	if len(snapshot.Comparisons) != 1 {
		t.Fatalf("comparison snapshot=%+v", snapshot)
	}
	comparison, ok := r.HistoricalDecisionComparison(snapshot.Comparisons[0].EpisodeID)
	if !ok || comparison.Category != decisioncomparison.CategoryAligned || !comparison.Markers.HistoricalDecisionRetainsAuthority || !comparison.Markers.CognitiveRecommendationHasNoAuthority {
		t.Fatalf("comparison=%+v ok=%v", comparison, ok)
	}
	if r.Metrics()["comparisons_total"] != 1 {
		t.Fatalf("comparison metrics=%v", r.Metrics())
	}
}

func TestHistoricalDecisionComparisonAbsentWithoutReference(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	input := testInput(at, "comparison-no-ref-event")
	if result := r.TrySubmit(input); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CommitsSucceeded == 1 })
	if len(r.HistoricalDecisionComparisons().Comparisons) != 0 {
		t.Fatal("comparison was fabricated without historical reference")
	}
	if r.Metrics()["comparison_skipped_no_historical_ref"] == 0 {
		t.Fatalf("missing skip metric=%v", r.Metrics())
	}
}
