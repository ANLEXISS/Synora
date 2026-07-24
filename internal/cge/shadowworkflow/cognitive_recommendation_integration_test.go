package shadowworkflow

import (
	"context"
	"testing"
	"time"

	"synora/internal/cge/cognitiverecommendation"
)

func TestCognitiveRecommendationPublishedWithSituationAfterCommit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if r.TrySubmit(testInput(at, "recommendation-projection-event")).Status != SubmitAccepted {
		t.Fatal("submit rejected")
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CommitsSucceeded == 1 })

	situations := r.CognitiveSituations()
	recommendations := r.CognitiveRecommendations()
	if len(situations.Situations) != 1 || len(recommendations.RecommendationSets) != 1 {
		t.Fatalf("situations=%+v recommendations=%+v", situations, recommendations)
	}
	if situations.WorkflowRevision != recommendations.WorkflowRevision || situations.Digest != recommendations.SituationSnapshotDigest {
		t.Fatalf("projection revisions/digests not aligned: situations=%+v recommendations=%+v", situations, recommendations)
	}
	set := recommendations.RecommendationSets[0]
	if set.SourceSituationFingerprint != situations.Situations[0].Fingerprint || set.SourceSituationRevision != situations.Situations[0].Revision {
		t.Fatalf("recommendation lineage set=%+v situation=%+v", set, situations.Situations[0])
	}
	if err := set.Validate(cognitiverecommendation.DefaultPolicy()); err != nil {
		t.Fatal(err)
	}
	if set.Markers.NotADecision != true || set.Markers.NotAnAction != true || !set.Markers.NoSecurityMeaning {
		t.Fatalf("markers=%+v", set.Markers)
	}
}

func TestCognitiveRecommendationSnapshotIsDefensive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if r.TrySubmit(testInput(at, "recommendation-defensive-event")).Status != SubmitAccepted {
		t.Fatal("submit rejected")
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CommitsSucceeded == 1 })

	snapshot := r.CognitiveRecommendations()
	if len(snapshot.RecommendationSets) != 1 || len(snapshot.RecommendationSets[0].Recommendations) == 0 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	snapshot.RecommendationSets[0].Recommendations[0].Kind = cognitiverecommendation.RecommendationNone
	snapshot.EpisodeIndex["injected"] = 3
	current, ok := r.CognitiveRecommendation(snapshot.RecommendationSets[0].EpisodeID)
	if !ok || current.Recommendations[0].Kind == cognitiverecommendation.RecommendationNone {
		t.Fatalf("mutable recommendation escaped: %+v", current)
	}
	if _, ok := r.CognitiveRecommendation("injected"); ok {
		t.Fatal("mutable recommendation index escaped")
	}
}

func TestCognitiveRecommendationRecoveryRebuild(t *testing.T) {
	directory := t.TempDir()
	cfg := fileQualificationConfig(directory)
	r := commitFileQualificationEvent(t, cfg, "recommendation-recovery-event")
	before := r.CognitiveRecommendations()
	if len(before.RecommendationSets) != 1 {
		t.Fatalf("before=%+v", before)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	after := restarted.CognitiveRecommendations()
	if before.Digest != after.Digest || before.RecommendationSets[0].Fingerprint != after.RecommendationSets[0].Fingerprint {
		t.Fatalf("recovery changed recommendation projection before=%+v after=%+v", before, after)
	}
}
