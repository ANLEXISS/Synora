package shadowworkflow

import (
	"context"
	"testing"
	"time"

	"synora/internal/cge/cognitivesituation"
)

func TestCognitiveSituationPublishedOnlyAfterDurableCommit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAdvisoryRequests
	cfg.MaxProcessingDuration = 2 * time.Second
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())

	if snapshot := r.CognitiveSituations(); len(snapshot.Situations) != 0 {
		t.Fatalf("situation published before commit: %+v", snapshot)
	}
	if result := r.TrySubmit(testInput(at, "cognitive-situation-event")); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CommitsSucceeded == 1 })

	state := r.CoordinatorSnapshot()
	if len(state.Episodes) != 1 {
		t.Fatalf("state=%+v", state)
	}
	situation, ok := r.CognitiveSituation(state.Episodes[0].EpisodeID)
	if !ok {
		t.Fatalf("situation not published after commit: %+v", r.CognitiveSituations())
	}
	if err := situation.Validate(cognitivesituation.DefaultPolicy()); err != nil {
		t.Fatalf("invalid situation: %v", err)
	}
	if situation.WorkflowRevision != r.Status().WorkflowRevision || !situation.Markers.DerivedFromCommittedState {
		t.Fatalf("situation=%+v status=%+v", situation, r.Status())
	}
}

func TestCognitiveSituationSnapshotIsDefensive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if r.TrySubmit(testInput(at, "defensive-cognitive-situation")).Status != SubmitAccepted {
		t.Fatal("submit rejected")
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CommitsSucceeded == 1 })

	snapshot := r.CognitiveSituations()
	if len(snapshot.Situations) != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	snapshot.Situations[0].Phase = cognitivesituation.PhaseInvalidated
	snapshot.Situations[0].SourceFingerprints.Episode = "mutated"
	snapshot.EpisodeIndex["injected"] = 99

	current, ok := r.CognitiveSituation(snapshot.Situations[0].EpisodeID)
	if !ok || current.Phase == cognitivesituation.PhaseInvalidated || current.SourceFingerprints.Episode == "mutated" {
		t.Fatalf("mutable situation escaped: %+v", current)
	}
	if _, ok := r.CognitiveSituation("injected"); ok {
		t.Fatal("mutable episode index escaped")
	}
}

func TestCognitiveSituationRebuiltAfterRecovery(t *testing.T) {
	directory := t.TempDir()
	cfg := fileQualificationConfig(directory)
	r := commitFileQualificationEvent(t, cfg, "cognitive-situation-recovery")
	beforeSnapshot := r.CognitiveSituations()
	if len(beforeSnapshot.Situations) != 1 {
		t.Fatalf("snapshot before recovery=%+v", beforeSnapshot)
	}
	episodeID := beforeSnapshot.Situations[0].EpisodeID
	before, ok := r.CognitiveSituation(episodeID)
	if !ok {
		t.Fatal("situation missing before recovery")
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	after, ok := restarted.CognitiveSituation(episodeID)
	if !ok {
		t.Fatal("situation missing after recovery")
	}
	if before.Fingerprint != after.Fingerprint || before.WorkflowRevision != after.WorkflowRevision {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	if afterSnapshot := restarted.CognitiveSituations(); beforeSnapshot.Digest != afterSnapshot.Digest {
		t.Fatalf("snapshot digest changed before=%s after=%s", beforeSnapshot.Digest, afterSnapshot.Digest)
	}
}
