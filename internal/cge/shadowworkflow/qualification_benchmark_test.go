package shadowworkflow

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkQualificationTrySubmitWhileWorkerBlocked(b *testing.B) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.QueueCapacity = 1
	cfg.PipelineDepth = DepthAuthorizationBoundary
	provider := &qualificationAuthorizationProvider{available: true, block: true, entered: make(chan struct{})}
	clock := newQualificationClock()
	r, err := NewRuntime(context.Background(), cfg, clock, nil, newQualificationCapabilityProvider(), provider)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close(context.Background())
	if r.TrySubmit(testInput(clock.Now(), "qualification-benchmark-blocked")).Status != SubmitAccepted {
		b.Fatal("worker input was not accepted")
	}
	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		b.Fatal("worker did not enter blocked provider")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input := testInput(clock.Now(), fmt.Sprintf("qualification-benchmark-queue-%d", i))
		_ = r.TrySubmit(input)
	}
	b.StopTimer()
}

func BenchmarkQualificationFullPipelineAuthorization(b *testing.B) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAuthorizationBoundary
	cfg.CheckpointEveryTransactions = uint64(b.N + 1)
	clock := newQualificationClock()
	r, err := NewRuntime(context.Background(), cfg, clock, nil, newQualificationCapabilityProvider(), &qualificationAuthorizationProvider{available: true, allow: true})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close(context.Background())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input := testInput(clock.Now(), fmt.Sprintf("qualification-benchmark-full-%d", i))
		if err := r.process(context.Background(), input); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

func BenchmarkQualificationStatusSnapshotUnderLoad(b *testing.B) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MaxProcessingDuration = 2 * time.Second
	clock := newQualificationClock()
	r, err := NewRuntime(context.Background(), cfg, clock, nil, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close(context.Background())
	if r.TrySubmit(testInput(clock.Now(), "qualification-benchmark-status")).Status != SubmitAccepted {
		b.Fatal("input was not accepted")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && r.Status().CyclesSucceeded == 0 {
		time.Sleep(time.Millisecond)
	}
	if r.Status().CyclesSucceeded == 0 {
		b.Fatal("status benchmark input was not processed")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Status()
	}
}
