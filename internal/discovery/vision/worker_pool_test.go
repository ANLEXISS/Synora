package vision

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPoolRetriesOneTransientProcessingFailure(t *testing.T) {
	var calls atomic.Int32
	done := make(chan struct{})
	pool := NewWorkerPool(1, func(*ClipJob) error {
		if calls.Add(1) == 1 {
			return errors.New("transient worker failure")
		}
		close(done)
		return nil
	})
	if err := pool.Enqueue(&ClipJob{ID: "clip-retry", CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not retry transient failure")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("processing attempts=%d, want 2", got)
	}
}
