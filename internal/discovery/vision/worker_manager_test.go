package vision

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"synora/pkg/contract"
)

type fakePublisher struct {
	mu       sync.Mutex
	messages []contract.Message
}

func (p *fakePublisher) Send(
	msg contract.Message,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.messages = append(
		p.messages,
		msg,
	)

	return nil
}

func (p *fakePublisher) types() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	types := make(
		[]string,
		0,
		len(p.messages),
	)

	for _, msg := range p.messages {
		types = append(
			types,
			msg.Type,
		)
	}

	return types
}

type fakeExecutor struct {
	mu        sync.Mutex
	starts    int
	processes []*fakeProcess
}

func (e *fakeExecutor) Start(
	command string,
	args ...string,
) (ManagedProcess, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.starts++

	process := &fakeProcess{
		pid:    1000 + e.starts,
		waitCh: make(chan error, 1),
	}

	e.processes = append(
		e.processes,
		process,
	)

	return process, nil
}

func (e *fakeExecutor) lastProcess() *fakeProcess {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.processes) == 0 {
		return nil
	}

	return e.processes[len(e.processes)-1]
}

func (e *fakeExecutor) startCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.starts
}

type fakeProcess struct {
	pid int

	waitCh chan error

	mu      sync.Mutex
	signals []os.Signal
	killed  bool
}

func (p *fakeProcess) PID() int {
	return p.pid
}

func (p *fakeProcess) Wait() error {
	return <-p.waitCh
}

func (p *fakeProcess) Signal(
	signal os.Signal,
) error {
	p.mu.Lock()
	p.signals = append(
		p.signals,
		signal,
	)
	p.mu.Unlock()

	p.waitCh <- nil

	return nil
}

func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()

	p.waitCh <- errors.New("killed")

	return nil
}

func TestWorkerManagerStartTracksStateAndPublishesEvent(t *testing.T) {
	publisher := &fakePublisher{}
	executor := &fakeExecutor{}
	now := fixedClock(
		time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC),
	)

	manager := NewWorkerManager(
		publisher,
		WorkerManagerConfig{
			Executor: executor,
			Now:      now,
		},
	)

	if err := manager.Start("cam_01"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	snapshot := manager.Snapshot()
	if snapshot.Status != WorkerStatusRunning {
		t.Fatalf("status=%s", snapshot.Status)
	}

	if snapshot.PID != 1001 {
		t.Fatalf("pid=%d", snapshot.PID)
	}

	if !snapshot.LastStart.Equal(now()) {
		t.Fatalf("last_start=%s", snapshot.LastStart)
	}

	assertPublished(
		t,
		publisher,
		contract.EventDiscoveryWorkerStarted,
	)

	executor.lastProcess().waitCh <- nil
}

func TestWorkerManagerStopTracksExitAndPublishesEvent(t *testing.T) {
	publisher := &fakePublisher{}
	executor := &fakeExecutor{}

	manager := NewWorkerManager(
		publisher,
		WorkerManagerConfig{
			Executor: executor,
		},
	)

	if err := manager.Start("cam_01"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := manager.Stop("cam_01"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	snapshot := manager.Snapshot()
	if snapshot.Status != WorkerStatusStopped {
		t.Fatalf("status=%s", snapshot.Status)
	}

	if snapshot.PID != 0 {
		t.Fatalf("pid=%d", snapshot.PID)
	}

	if snapshot.LastExit.IsZero() {
		t.Fatal("last_exit is zero")
	}

	assertPublished(
		t,
		publisher,
		contract.EventDiscoveryWorkerStopped,
	)
}

func TestWorkerManagerBacksOffAfterQuickCrash(t *testing.T) {
	publisher := &fakePublisher{}
	executor := &fakeExecutor{}
	current := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

	manager := NewWorkerManager(
		publisher,
		WorkerManagerConfig{
			Executor:         executor,
			Now:              func() time.Time { return current },
			QuickCrashWindow: time.Minute,
			BaseBackoff:      time.Second,
		},
	)

	if err := manager.Start("cam_01"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	executor.lastProcess().waitCh <- errors.New("boom")

	waitForStatus(
		t,
		manager,
		WorkerStatusBackoff,
	)

	if err := manager.Start("cam_01"); !errors.Is(err, ErrWorkerBackoff) {
		t.Fatalf("Start() error=%v, want ErrWorkerBackoff", err)
	}

	assertPublished(
		t,
		publisher,
		contract.EventDiscoveryWorkerCrashed,
	)
}

func TestWorkerManagerSerializesSameCamera(t *testing.T) {
	executor := &fakeExecutor{}

	manager := NewWorkerManager(
		nil,
		WorkerManagerConfig{
			Executor: executor,
		},
	)

	enteredFirst := make(chan struct{})
	releaseFirst := make(chan struct{})
	doneFirst := make(chan error, 1)

	go func() {
		doneFirst <- manager.WithCamera("cam_01", func() error {
			close(enteredFirst)
			<-releaseFirst
			return nil
		})
	}()

	<-enteredFirst

	enteredSecond := make(chan struct{})
	doneSecond := make(chan error, 1)

	go func() {
		doneSecond <- manager.WithCamera("cam_01", func() error {
			close(enteredSecond)
			return nil
		})
	}()

	select {
	case <-enteredSecond:
		t.Fatal("second worker entered while first camera job was active")
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseFirst)

	if err := <-doneFirst; err != nil {
		t.Fatalf("first WithCamera() error = %v", err)
	}

	select {
	case <-enteredSecond:
	case <-time.After(time.Second):
		t.Fatal("second worker did not enter after first camera job completed")
	}

	if err := <-doneSecond; err != nil {
		t.Fatalf("second WithCamera() error = %v", err)
	}

	if executor.startCount() != 1 {
		t.Fatalf("process starts=%d, want 1", executor.startCount())
	}

	executor.lastProcess().waitCh <- nil
}

func fixedClock(
	now time.Time,
) func() time.Time {
	return func() time.Time {
		return now
	}
}

func assertPublished(
	t *testing.T,
	publisher *fakePublisher,
	eventType string,
) {
	t.Helper()

	for _, current := range publisher.types() {
		if current == eventType {
			return
		}
	}

	t.Fatalf(
		"event %s not published, got %v",
		eventType,
		publisher.types(),
	)
}

func waitForStatus(
	t *testing.T,
	manager *WorkerManager,
	status string,
) {
	t.Helper()

	deadline := time.Now().Add(
		time.Second,
	)

	for time.Now().Before(deadline) {
		if manager.Snapshot().Status == status {
			return
		}

		time.Sleep(
			time.Millisecond,
		)
	}

	t.Fatalf(
		"status=%s, want %s",
		manager.Snapshot().Status,
		status,
	)
}
