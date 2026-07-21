package shadowworkflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fileQualificationConfig(directory string) Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.StoreMode = StoreFile
	cfg.StoreDirectory = directory
	cfg.MaxProcessingDuration = 2 * time.Second
	return cfg
}

func commitFileQualificationEvent(t *testing.T, cfg Config, eventID string) *Runtime {
	t.Helper()
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result := r.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), eventID)); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 && status.CommitsSucceeded == 1 })
	if status.CommitsSucceeded != 1 {
		t.Fatalf("status=%+v metrics=%v", status, r.Metrics())
	}
	return r
}

func TestQualificationCorruptMiddleWALFailsClosed(t *testing.T) {
	directory := t.TempDir()
	cfg := fileQualificationConfig(directory)
	r := commitFileQualificationEvent(t, cfg, "corrupt-middle")
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "workflow.wal")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(content, []byte("\n"))
	if len(lines) < 3 {
		t.Fatalf("wal records=%d", len(lines))
	}
	lines[1][len(lines[1])/2] ^= 0x7f
	if err := os.WriteFile(path, bytes.Join(lines, []byte("\n")), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, openErr := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if openErr == nil || recovered.Status().State != StateRecoveryFailed || recovered.Status().LastErrorCode != "recovery_failed" {
		t.Fatalf("recovery=%+v err=%v", recovered.Status(), openErr)
	}
	if result := recovered.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "rejected-after-corruption")); result.Status != SubmitStopped {
		t.Fatalf("submit after corruption=%+v", result)
	}
}

func TestQualificationTruncatedFinalWALIsPolicyControlled(t *testing.T) {
	directory := t.TempDir()
	cfg := fileQualificationConfig(directory)
	r := commitFileQualificationEvent(t, cfg, "truncated-tail")
	before := r.Status()
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "workflow.wal")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("truncated-final-record")
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	recovered, openErr := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer recovered.Close(context.Background())
	status := recovered.Status()
	if status.State != StateRunning || status.WorkflowRevision != before.WorkflowRevision || len(status.RecoveryWarnings) == 0 {
		t.Fatalf("truncated recovery=%+v", status)
	}
	strict := cfg
	strict.AllowTruncatedFinalRecord = false
	failed, strictErr := NewRuntime(context.Background(), strict, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if strictErr == nil || failed.Status().State != StateRecoveryFailed {
		t.Fatalf("strict recovery=%+v err=%v", failed.Status(), strictErr)
	}
}

func TestQualificationCheckpointFailurePreservesCommittedState(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.CheckpointEveryTransactions = 1
	cfg.MaxProcessingDuration = 2 * time.Second
	store := &qualificationStore{base: newMemoryStore()}
	r := newInjectedRuntime(t, cfg, newQualificationClock(), store, nil, nil)
	store.setCheckpointFailure(true)
	if result := r.TrySubmit(testInput(newQualificationClock().Now(), "checkpoint-failure")); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
	if status.CommitsSucceeded != 1 || status.CheckpointsFailed == 0 || status.State != StateDegraded {
		t.Fatalf("status=%+v metrics=%v", status, r.Metrics())
	}
	if r.CoordinatorSnapshot().Revision != 1 {
		t.Fatalf("committed state lost: %+v", r.CoordinatorSnapshot())
	}
}

func TestQualificationAppendBeforePublicationReplaysAfterRestart(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.MaxProcessingDuration = 2 * time.Second
	store := &qualificationStore{base: newMemoryStore()}
	clock := newQualificationClock()
	r := newInjectedRuntime(t, cfg, clock, store, nil, nil)
	store.setAppendPanic(true)
	if r.TrySubmit(testInput(clock.Now(), "append-before-publication")).Status != SubmitAccepted {
		t.Fatal("event not accepted")
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool {
		return status.CyclesFailed == 1 && status.LastErrorCode == "panic.recovered"
	})
	if status.WorkflowRevision != 0 || status.LastErrorCode != "panic.recovered" {
		t.Fatalf("pre-publication state=%+v", status)
	}
	_ = r.Close(context.Background())
	restarted := newInjectedRuntime(t, cfg, clock, store, nil, nil)
	defer restarted.Close(context.Background())
	if status := restarted.Status(); status.WorkflowRevision != 1 || status.LastSequence != 1 {
		t.Fatalf("replayed state=%+v", status)
	}
}

func TestQualificationCommitFsyncFailureDoesNotPublishMemoryState(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.MaxProcessingDuration = 2 * time.Second
	store := &qualificationStore{base: newMemoryStore()}
	clock := newQualificationClock()
	r := newInjectedRuntime(t, cfg, clock, store, nil, nil)
	store.setSyncFailure(true)
	if r.TrySubmit(testInput(clock.Now(), "fsync-failure")).Status != SubmitAccepted {
		t.Fatal("event not accepted")
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool {
		return status.CyclesFailed == 1 && status.LastErrorCode == "transaction.durability_failure"
	})
	if status.WorkflowRevision != 0 || status.CommitsFailed == 0 || status.LastErrorCode != "transaction.durability_failure" {
		t.Fatalf("fsync failure state=%+v", status)
	}
	store.setSyncFailure(false)
	_ = r.Close(context.Background())
	restarted := newInjectedRuntime(t, cfg, clock, store, nil, nil)
	defer restarted.Close(context.Background())
	if restarted.Status().WorkflowRevision != 1 {
		t.Fatalf("durable tail was not replayed: %+v", restarted.Status())
	}
}

func TestQualificationWALLimitStopsOnlyWorkflow(t *testing.T) {
	directory := t.TempDir()
	cfg := fileQualificationConfig(directory)
	cfg.MaxWALBytes = 1
	workflow, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer workflow.Close(context.Background())
	if workflow.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "wal-limit")).Status != SubmitAccepted {
		t.Fatal("event not accepted")
	}
	status := waitForQualification(t, workflow, func(status StatusSnapshot) bool {
		return status.State == StateStorageLimitReached && status.CyclesFailed == 1
	})
	if status.State != StateStorageLimitReached || status.LastErrorCode != "quota.wal_size_limit" {
		t.Fatalf("wal limit status=%+v", status)
	}
	if result := workflow.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "wal-limit-rejected")); result.Status != SubmitStorageLimit {
		t.Fatalf("wal limit submit=%+v", result)
	}
}

func TestQualificationErrorCodesRemainTyped(t *testing.T) {
	if !errors.Is(fmt.Errorf("wrapped: %w", ErrPipelineTimeout), ErrPipelineTimeout) {
		t.Fatal("typed timeout error lost")
	}
}
