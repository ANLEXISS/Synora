package runtime

import (
	"context"
	"testing"
	"time"
)

func TestStartLoopContextStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	registry := NewRegistry()
	done := make(chan struct{})
	go func() {
		StartLoopContext(ctx, registry, nil)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime loop did not stop after cancellation")
	}
}
