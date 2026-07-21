package shadowworkflow

import (
	"context"
	"testing"
	"time"
)

func TestQualificationDuplicateAfterRecoveryKeepsDigest(t *testing.T) {
	directory := t.TempDir()
	cfg := fileQualificationConfig(directory)
	r := commitFileQualificationEvent(t, cfg, "duplicate-after-recovery")
	before := r.Status()
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	if restarted.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "duplicate-after-recovery")).Status != SubmitAccepted {
		t.Fatal("duplicate not accepted for idempotent evaluation")
	}
	status := waitForQualification(t, restarted, func(status StatusSnapshot) bool { return status.Duplicates == 1 })
	if status.WorkflowRevision != before.WorkflowRevision || status.LastSequence != before.LastSequence || status.WorkflowDigest != before.WorkflowDigest {
		t.Fatalf("duplicate changed durable state before=%+v after=%+v", before, status)
	}
}

func TestQualificationPeriodicCheckpointAtConfiguredCount(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.CheckpointEveryTransactions = 3
	cfg.MaxProcessingDuration = 2 * time.Second
	clock := newQualificationClock()
	r, err := NewRuntime(context.Background(), cfg, clock, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	for index := 0; index < 3; index++ {
		if r.TrySubmit(testInput(clock.Now(), "periodic-checkpoint-"+string(rune('a'+index)))).Status != SubmitAccepted {
			t.Fatal("event not accepted")
		}
		waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CommitsSucceeded >= uint64(index+1) })
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CheckpointsSucceeded >= 1 })
	if status.CheckpointsSucceeded == 0 || status.WorkflowRevision != 3 {
		t.Fatalf("periodic checkpoint status=%+v", status)
	}
}
