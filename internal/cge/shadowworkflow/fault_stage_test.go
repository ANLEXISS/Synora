package shadowworkflow

import (
	"context"
	"testing"
	"time"
)

func TestQualificationPanicInProviderStageIsContained(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAuthorizationBoundary
	cfg.MaxProcessingDuration = 2 * time.Second
	provider := &qualificationAuthorizationProvider{available: true, panicNow: true}
	r, err := NewRuntime(context.Background(), cfg, newQualificationClock(), nil, newQualificationCapabilityProvider(), provider)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if r.TrySubmit(testInput(newQualificationClock().Now(), "panic-stage")).Status != SubmitAccepted {
		t.Fatal("panic event not accepted")
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesFailed == 1 })
	if status.LastErrorCode != "panic.recovered" || status.CyclesSucceeded != 0 || status.WorkflowRevision != 0 {
		t.Fatalf("panic containment=%+v metrics=%v", status, r.Metrics())
	}
}

func TestQualificationProviderTimeoutDoesNotCommitAndNextEventRuns(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAuthorizationBoundary
	cfg.MaxProcessingDuration = 500 * time.Millisecond
	provider := &qualificationAuthorizationProvider{available: true, block: true}
	clock := newQualificationClock()
	r, err := NewRuntime(context.Background(), cfg, clock, nil, newQualificationCapabilityProvider(), provider)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if r.TrySubmit(testInput(clock.Now(), "timeout-stage")).Status != SubmitAccepted {
		t.Fatal("timeout event not accepted")
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool {
		return status.CyclesTimedOut == 1 && status.CyclesFailed == 1 && status.LastErrorCode == "quota.processing_timeout"
	})
	if status.WorkflowRevision != 0 || status.CyclesSucceeded != 0 || status.LastErrorCode != "quota.processing_timeout" {
		t.Fatalf("timeout state=%+v", status)
	}
	provider.setBlock(false)
	provider.setAllow(true)
	if r.TrySubmit(testInput(clock.Now(), "after-timeout")).Status != SubmitAccepted {
		t.Fatal("post-timeout event not accepted")
	}
	status = waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
	if status.WorkflowRevision != 1 {
		t.Fatalf("post-timeout state=%+v", status)
	}
}
