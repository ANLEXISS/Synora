package vision

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestWorkerPoolRestoresAndAcknowledgesDurableJobs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "queue.json")
	seed := []*ClipJob{{ID: "clip-restored", CameraID: "cam-1", Path: "/clips/clip-restored.mp4"}}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	p := NewWorkerPoolWithConfig(1, func(job *ClipJob) error {
		if job.ID != "clip-restored" {
			t.Fatalf("unexpected restored job: %#v", job)
		}
		close(done)
		return nil
	}, WorkerPoolConfig{PersistencePath: path, ProcessTimeout: time.Second})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("durable job was not restored")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "[]" {
		t.Fatalf("acknowledged queue journal=%s, want []", data)
	}
}

func TestWorkerPoolDeduplicatesAndBoundsPendingJobs(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	p := NewWorkerPoolWithConfig(1, func(*ClipJob) error {
		close(started)
		<-release
		return nil
	}, WorkerPoolConfig{QueueSize: 1, ProcessTimeout: time.Second})
	if err := p.Enqueue(&ClipJob{ID: "clip-one", CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}
	if err := p.Enqueue(&ClipJob{ID: "clip-one", CameraID: "cam-1"}); err != nil {
		t.Fatalf("identical pending job should be idempotent: %v", err)
	}
	if err := p.Enqueue(&ClipJob{ID: "clip-two", CameraID: "cam-1"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("queue bound error=%v, want ErrQueueFull", err)
	}
	close(release)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerPoolTimeoutRetriesThenReportsPermanentFailure(t *testing.T) {
	var attempts atomic.Int32
	failed := make(chan error, 1)
	p := NewWorkerPoolWithConfig(1, func(*ClipJob) error {
		attempts.Add(1)
		time.Sleep(100 * time.Millisecond)
		return nil
	}, WorkerPoolConfig{
		MaxAttempts:        2,
		ProcessTimeout:     10 * time.Millisecond,
		RetryBackoff:       time.Millisecond,
		MaxRetryBackoff:    2 * time.Millisecond,
		OnPermanentFailure: func(_ *ClipJob, err error) { failed <- err },
	})
	if err := p.Enqueue(&ClipJob{ID: "clip-timeout", CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-failed:
		if !errors.Is(err, ErrProcessingTimeout) {
			t.Fatalf("permanent error=%v, want timeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout was not reported")
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("processing attempts=%d, want 2", got)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}
