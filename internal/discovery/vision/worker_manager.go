package vision

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

const (
	DefaultPythonPath = "/usr/bin/python3"
	DefaultWorkerPath = "/opt/synora/services/vision-worker/worker.py"

	WorkerStatusStopped  = "stopped"
	WorkerStatusStarting = "starting"
	WorkerStatusRunning  = "running"
	WorkerStatusBackoff  = "backoff"
	WorkerStatusCrashed  = "crashed"
)

var ErrWorkerBackoff = errors.New("worker in crash backoff")

type ManagedProcess interface {
	PID() int
	Wait() error
	Signal(signal os.Signal) error
	Kill() error
}

type ProcessExecutor interface {
	Start(command string, args ...string) (ManagedProcess, error)
}

type execProcess struct {
	cmd *exec.Cmd
}

func (p *execProcess) PID() int {
	if p.cmd.Process == nil {
		return 0
	}

	return p.cmd.Process.Pid
}

func (p *execProcess) Wait() error {
	return p.cmd.Wait()
}

func (p *execProcess) Signal(
	signal os.Signal,
) error {
	if p.cmd.Process == nil {
		return os.ErrProcessDone
	}

	return p.cmd.Process.Signal(
		signal,
	)
}

func (p *execProcess) Kill() error {
	if p.cmd.Process == nil {
		return os.ErrProcessDone
	}

	return p.cmd.Process.Kill()
}

type ExecProcessExecutor struct{}

func (ExecProcessExecutor) Start(
	command string,
	args ...string,
) (ManagedProcess, error) {
	cmd := exec.Command(
		command,
		args...,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &execProcess{
		cmd: cmd,
	}, nil
}

type WorkerManagerConfig struct {
	PythonPath string
	WorkerPath string

	QuickCrashWindow time.Duration
	BaseBackoff      time.Duration
	MaxBackoff       time.Duration
	CrashEventLimit  time.Duration
	StopTimeout      time.Duration

	Executor ProcessExecutor
	Now      func() time.Time
}

type WorkerSnapshot struct {
	PID int

	LastStart time.Time
	LastExit  time.Time

	Status string

	BackoffUntil time.Time
}

type WorkerManager struct {
	mu sync.Mutex

	publisher Publisher

	pythonPath string
	workerPath string

	executor ProcessExecutor
	now      func() time.Time

	quickCrashWindow time.Duration
	baseBackoff      time.Duration
	maxBackoff       time.Duration
	stopTimeout      time.Duration

	process ManagedProcess
	done    chan struct{}

	pid int

	lastStart time.Time
	lastExit  time.Time

	status string

	backoffUntil    time.Time
	nextBackoff     time.Duration
	lastCrashEvent  time.Time
	crashEventLimit time.Duration

	expectedStop bool

	cameraLocks map[string]*sync.Mutex
}

func NewWorkerManager(
	publisher Publisher,
	cfg WorkerManagerConfig,
) *WorkerManager {
	if cfg.PythonPath == "" {
		cfg.PythonPath = DefaultPythonPath
	}

	if cfg.WorkerPath == "" {
		cfg.WorkerPath = DefaultWorkerPath
	}

	if cfg.QuickCrashWindow == 0 {
		cfg.QuickCrashWindow = 10 * time.Second
	}

	if cfg.BaseBackoff == 0 {
		cfg.BaseBackoff = 2 * time.Second
	}

	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 2 * time.Minute
	}

	if cfg.CrashEventLimit == 0 {
		cfg.CrashEventLimit = 30 * time.Second
	}

	if cfg.StopTimeout == 0 {
		cfg.StopTimeout = 5 * time.Second
	}

	if cfg.Executor == nil {
		cfg.Executor = ExecProcessExecutor{}
	}

	if cfg.Now == nil {
		cfg.Now = func() time.Time {
			return time.Now().UTC()
		}
	}

	return &WorkerManager{
		publisher: publisher,

		pythonPath: cfg.PythonPath,
		workerPath: cfg.WorkerPath,

		executor: cfg.Executor,
		now:      cfg.Now,

		quickCrashWindow: cfg.QuickCrashWindow,
		baseBackoff:      cfg.BaseBackoff,
		maxBackoff:       cfg.MaxBackoff,
		stopTimeout:      cfg.StopTimeout,
		crashEventLimit:  cfg.CrashEventLimit,
		nextBackoff:      cfg.BaseBackoff,

		status: WorkerStatusStopped,

		cameraLocks: map[string]*sync.Mutex{},
	}
}

func (m *WorkerManager) Start(
	cameraID string,
) error {
	now := m.now()

	m.mu.Lock()
	if m.process != nil {
		m.mu.Unlock()
		return nil
	}

	if now.Before(m.backoffUntil) {
		m.status = WorkerStatusBackoff
		until := m.backoffUntil
		m.mu.Unlock()

		return fmt.Errorf(
			"%w until %s",
			ErrWorkerBackoff,
			until.Format(time.RFC3339Nano),
		)
	}

	m.status = WorkerStatusStarting
	m.lastStart = now
	m.expectedStop = false

	process, err := m.executor.Start(
		m.pythonPath,
		m.workerPath,
	)

	if err != nil {
		m.status = WorkerStatusStopped
		m.lastExit = m.now()
		m.mu.Unlock()

		return err
	}

	done := make(chan struct{})

	m.process = process
	m.done = done
	m.pid = process.PID()
	m.status = WorkerStatusRunning
	pid := m.pid
	startedAt := m.lastStart
	m.mu.Unlock()

	m.publish(
		contract.EventDiscoveryWorkerStarted,
		cameraID,
		map[string]any{
			"pid":        pid,
			"last_start": startedAt,
			"status":     WorkerStatusRunning,
		},
	)

	go m.monitor(
		process,
		done,
	)

	return nil
}

func (m *WorkerManager) Stop(
	cameraID string,
) error {
	m.mu.Lock()
	process := m.process
	done := m.done

	if process == nil {
		m.status = WorkerStatusStopped
		m.mu.Unlock()
		return nil
	}

	m.expectedStop = true
	m.mu.Unlock()

	if err := process.Signal(syscall.SIGTERM); err != nil {
		log.Printf(
			"vision worker signal failed pid=%d err=%v",
			process.PID(),
			err,
		)

		_ = process.Kill()
	}

	select {
	case <-done:
	case <-time.After(m.stopTimeout):
		_ = process.Kill()
		<-done
	}

	return nil
}

func (m *WorkerManager) Restart(
	cameraID string,
) error {
	if err := m.Stop(cameraID); err != nil {
		return err
	}

	return m.Start(cameraID)
}

func (m *WorkerManager) WithCamera(
	cameraID string,
	fn func() error,
) error {
	cameraLock := m.cameraLock(
		cameraID,
	)

	cameraLock.Lock()
	defer cameraLock.Unlock()

	if err := m.Start(cameraID); err != nil {
		return err
	}

	return fn()
}

func (m *WorkerManager) Snapshot() WorkerSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	return WorkerSnapshot{
		PID: m.pid,

		LastStart: m.lastStart,
		LastExit:  m.lastExit,

		Status: m.status,

		BackoffUntil: m.backoffUntil,
	}
}

func (m *WorkerManager) monitor(
	process ManagedProcess,
	done chan struct{},
) {
	err := process.Wait()
	now := m.now()

	m.mu.Lock()
	if m.process != process {
		m.mu.Unlock()
		close(done)
		return
	}

	expectedStop := m.expectedStop
	startedAt := m.lastStart
	pid := m.pid

	m.process = nil
	m.done = nil
	m.pid = 0
	m.lastExit = now
	m.expectedStop = false

	crashed := !expectedStop && err != nil

	if crashed {
		m.status = WorkerStatusCrashed

		if now.Sub(startedAt) < m.quickCrashWindow {
			m.backoffUntil = now.Add(
				m.nextBackoff,
			)

			m.status = WorkerStatusBackoff

			m.nextBackoff *= 2
			if m.nextBackoff > m.maxBackoff {
				m.nextBackoff = m.maxBackoff
			}
		} else {
			m.nextBackoff = m.baseBackoff
		}
	} else {
		m.status = WorkerStatusStopped
		m.nextBackoff = m.baseBackoff
	}

	status := m.status
	backoffUntil := m.backoffUntil
	lastExit := m.lastExit
	m.mu.Unlock()

	close(done)

	eventType := contract.EventDiscoveryWorkerStopped
	if crashed {
		eventType = contract.EventDiscoveryWorkerCrashed
	}

	payload := map[string]any{
		"pid":           pid,
		"last_exit":     lastExit,
		"status":        status,
		"backoff_until": backoffUntil,
	}

	if err != nil {
		payload["error"] = err.Error()
	}

	if eventType != contract.EventDiscoveryWorkerCrashed || m.shouldPublishCrashEvent(now) {
		m.publish(
			eventType,
			"",
			payload,
		)
	}
}

func (m *WorkerManager) shouldPublishCrashEvent(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.lastCrashEvent.IsZero() && now.Sub(m.lastCrashEvent) < m.crashEventLimit {
		return false
	}
	m.lastCrashEvent = now
	return true
}

func (m *WorkerManager) cameraLock(
	cameraID string,
) *sync.Mutex {
	if cameraID == "" {
		cameraID = "unknown"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	lock := m.cameraLocks[cameraID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.cameraLocks[cameraID] = lock
	}

	return lock
}

func (m *WorkerManager) publish(
	eventType string,
	cameraID string,
	fields map[string]any,
) {
	if m.publisher == nil {
		return
	}

	now := m.now()

	payload := map[string]any{
		"timestamp": now,
	}

	if cameraID != "" {
		payload["camera_id"] = cameraID
	}

	for key, value := range fields {
		payload[key] = value
	}

	body, err := json.Marshal(
		payload,
	)

	if err != nil {
		log.Printf(
			"worker event marshal failed type=%s err=%v",
			eventType,
			err,
		)

		return
	}

	err = m.publisher.Send(contract.Message{
		ID:        idgen.New("msg"),
		Type:      eventType,
		Kind:      contract.KindEvent,
		Source:    "discovery",
		Target:    "core",
		Timestamp: now,
		Payload:   body,
	})

	if err != nil {
		log.Printf(
			"worker event publish failed type=%s err=%v",
			eventType,
			err,
		)
	}
}
