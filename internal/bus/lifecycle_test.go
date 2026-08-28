package bus

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConnectContextStopsRetryWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	_, err := ConnectContext(ctx, "/run/synora/nonexistent.sock", "test")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ConnectContext error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("canceled ConnectContext took %s", elapsed)
	}
}
