package vision

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerManagerStopIsBoundedWhenKillDoesNotEndProcess(t *testing.T) {
	process := &stuckProcess{released: make(chan struct{})}
	manager := NewWorkerManager(nil, WorkerManagerConfig{
		Executor:    stuckExecutor{process: process},
		StopTimeout: 10 * time.Millisecond,
	})
	if err := manager.Start("cam-1"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err := manager.Stop("cam-1")
	if !errors.Is(err, ErrWorkerStopTimeout) {
		t.Fatalf("Stop error=%v, want timeout", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Stop was not bounded: %s", elapsed)
	}
	if got := manager.Snapshot(); got.Status == WorkerStatusRunning {
		t.Fatalf("timed out stop still claimed running: %#v", got)
	}
	close(process.released)
}

func TestWorkerManagerClearsExpiredBackoffOnRestart(t *testing.T) {
	executor := &fakeExecutor{}
	current := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	manager := NewWorkerManager(nil, WorkerManagerConfig{
		Executor:         executor,
		Now:              func() time.Time { return current },
		QuickCrashWindow: time.Minute,
		BaseBackoff:      time.Second,
	})
	if err := manager.Start("cam-1"); err != nil {
		t.Fatal(err)
	}
	executor.lastProcess().waitCh <- errors.New("crashed")
	waitForStatus(t, manager, WorkerStatusBackoff)
	current = current.Add(2 * time.Second)
	if err := manager.Start("cam-1"); err != nil {
		t.Fatal(err)
	}
	if got := manager.Snapshot(); !got.BackoffUntil.IsZero() {
		t.Fatalf("expired backoff remained observable after restart: %s", got.BackoffUntil)
	}
	executor.lastProcess().waitCh <- nil
}

type stuckExecutor struct{ process *stuckProcess }

func (e stuckExecutor) Start(string, ...string) (ManagedProcess, error) { return e.process, nil }

type stuckProcess struct {
	released chan struct{}
	killed   atomic.Bool
}

func (p *stuckProcess) PID() int { return 4242 }

func (p *stuckProcess) Wait() error {
	<-p.released
	if p.killed.Load() {
		return errors.New("killed")
	}
	return nil
}

func (p *stuckProcess) Signal(os.Signal) error { return nil }

func (p *stuckProcess) Kill() error {
	p.killed.Store(true)
	return nil
}

func TestWorkerPoolValidatesJobsAndNormalizesWorkerCount(t *testing.T) {
	var calls atomic.Int32
	done := make(chan struct{})
	pool := NewWorkerPool(0, func(*ClipJob) error {
		if calls.Add(1) == 1 {
			close(done)
		}
		return nil
	})
	defer pool.Close()
	if err := pool.Enqueue(nil); !errors.Is(err, ErrInvalidClipJob) {
		t.Fatalf("nil job error=%v", err)
	}
	if err := pool.Enqueue(&ClipJob{ID: "missing-camera"}); !errors.Is(err, ErrInvalidClipJob) {
		t.Fatalf("incomplete job error=%v", err)
	}
	if err := pool.Enqueue(&ClipJob{ID: "clip-1", CameraID: "cam-1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("normalized worker did not process job")
	}
}

func TestWorkerPoolCloseRejectsNewJobs(t *testing.T) {
	pool := NewWorkerPool(1, func(*ClipJob) error { return nil })
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pool.Enqueue(&ClipJob{ID: "clip-1", CameraID: "cam-1"}); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("enqueue after close error=%v", err)
	}
}
