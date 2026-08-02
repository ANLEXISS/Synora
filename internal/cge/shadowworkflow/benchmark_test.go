package shadowworkflow

import (
	"context"
	"testing"
	"time"
)

func BenchmarkTrySubmitDisabled(b *testing.B) {
	r, err := NewRuntime(context.Background(), DefaultConfig(), fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	input := testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "benchmark-event")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.TrySubmit(input)
	}
}

func BenchmarkStatusSnapshot(b *testing.B) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MaxProcessingDuration = 2 * time.Second
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close(context.Background())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Status()
	}
}

func BenchmarkTrySubmitQueueBounded(b *testing.B) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.QueueCapacity = 1
	cfg.MaxProcessingDuration = 2 * time.Second
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close(context.Background())
	input := testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "benchmark-queue")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input.EventID = "benchmark-queue-" + string(rune('a'+i%26))
		input.Observation.EventID = input.EventID
		r.TrySubmit(input)
	}
}
