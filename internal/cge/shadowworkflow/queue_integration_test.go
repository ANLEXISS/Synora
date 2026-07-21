package shadowworkflow

import (
	"context"
	"testing"
	"time"
)

func TestQualificationQueueFullRejectsNewestWithoutBlocking(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAuthorizationBoundary
	cfg.QueueCapacity = 1
	cfg.MaxProcessingDuration = time.Second
	provider := &qualificationAuthorizationProvider{available: true, block: true, entered: make(chan struct{})}
	clock := newQualificationClock()
	r, err := NewRuntime(context.Background(), cfg, clock, nil, newQualificationCapabilityProvider(), provider)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if r.TrySubmit(testInput(clock.Now(), "queue-worker-blocked")).Status != SubmitAccepted {
		t.Fatal("first event not accepted")
	}
	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("blocked provider was not entered")
	}
	if r.TrySubmit(testInput(clock.Now(), "queue-kept")).Status != SubmitAccepted {
		t.Fatal("queued event not accepted")
	}
	start := time.Now()
	result := r.TrySubmit(testInput(clock.Now(), "queue-newest-dropped"))
	if result.Status != SubmitQueueFull || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("queue result=%+v duration=%s", result, time.Since(start))
	}
	if r.Status().DroppedQueueFull != 1 {
		t.Fatalf("queue counters=%+v", r.Status())
	}
}
