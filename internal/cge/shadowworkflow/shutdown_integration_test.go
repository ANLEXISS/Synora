package shadowworkflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQualificationNormalShutdownStopsWorkerAndStore(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.MaxProcessingDuration = 2 * time.Second
	clock := newQualificationClock()
	r, err := NewRuntime(context.Background(), cfg, clock, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, eventID := range []string{"shutdown-one", "shutdown-two", "shutdown-three"} {
		if r.TrySubmit(testInput(clock.Now(), eventID)).Status != SubmitAccepted {
			t.Fatalf("event %s not accepted", eventID)
		}
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded > 0 })
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := r.Status(); status.State != StateStopped {
		t.Fatalf("shutdown status=%+v", status)
	}
	if result := r.TrySubmit(testInput(clock.Now(), "after-shutdown")); result.Status != SubmitStopped {
		t.Fatalf("post-shutdown submit=%+v", result)
	}
}

func TestQualificationShutdownTimeoutLeavesStoppingState(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAuthorizationBoundary
	cfg.MaxProcessingDuration = time.Minute
	blocked := make(chan struct{})
	provider := &qualificationAuthorizationProvider{available: true, hardBlock: blocked, entered: make(chan struct{})}
	clock := newQualificationClock()
	r, err := NewRuntime(context.Background(), cfg, clock, nil, newQualificationCapabilityProvider(), provider)
	if err != nil {
		t.Fatal(err)
	}
	if r.TrySubmit(testInput(clock.Now(), "shutdown-timeout")).Status != SubmitAccepted {
		t.Fatal("event not accepted")
	}
	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("blocked provider was not entered")
	}
	closeContext, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	err = r.Close(closeContext)
	cancel()
	if !errors.Is(err, ErrShutdownTimeout) || r.Status().State != StateStopping {
		t.Fatalf("timeout shutdown err=%v status=%+v", err, r.Status())
	}
	close(blocked)
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.Status().State != StateStopped {
		t.Fatalf("final shutdown status=%+v", r.Status())
	}
}
