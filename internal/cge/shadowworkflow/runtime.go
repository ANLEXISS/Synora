package shadowworkflow

import (
	"context"
	"fmt"
	"sync"
	"time"

	"synora/internal/cge/durableworkflow"
)

type Clock interface{ Now() time.Time }
type Logger interface{ Printf(string, ...any) }

type Runtime struct {
	mu                          sync.RWMutex
	cfg                         Config
	clock                       Clock
	logger                      Logger
	coordinator                 *durableworkflow.Coordinator
	store                       durableworkflow.Store
	queue                       chan ShadowWorkflowInput
	cancel                      context.CancelFunc
	done                        chan struct{}
	accepting                   bool
	state                       RuntimeState
	lastErrorCode               string
	lastWarnings                []string
	breaker                     breaker
	counters                    counters
	metrics                     *metricCounter
	capabilityProvider          CapabilityInputProvider
	authorizationProvider       AuthorizationInputProvider
	transactionsSinceCheckpoint uint64
	lastCheckpointAt            time.Time
}

type memoryStore struct {
	mu         sync.Mutex
	records    []durableworkflow.Record
	checkpoint *durableworkflow.Checkpoint
	closed     bool
}

func newMemoryStore() *memoryStore { return &memoryStore{} }
func (s *memoryStore) Append(record durableworkflow.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return durableworkflow.ErrStoreClosed
	}
	s.records = append(s.records, record.Clone())
	return nil
}
func (s *memoryStore) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return durableworkflow.ErrStoreClosed
	}
	return nil
}
func (s *memoryStore) Load() (durableworkflow.RecoveryInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return durableworkflow.RecoveryInput{}, durableworkflow.ErrStoreClosed
	}
	out := durableworkflow.RecoveryInput{}
	for _, v := range s.records {
		out.Records = append(out.Records, v.Clone())
	}
	if s.checkpoint != nil {
		c := s.checkpoint.Clone()
		out.Checkpoint = &c
	}
	return out, nil
}
func (s *memoryStore) WriteCheckpoint(c durableworkflow.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return durableworkflow.ErrStoreClosed
	}
	copy := c.Clone()
	s.checkpoint = &copy
	return nil
}
func (s *memoryStore) Close() error { s.mu.Lock(); s.closed = true; s.mu.Unlock(); return nil }

func NewRuntime(ctx context.Context, cfg Config, clock Clock, logger Logger, capabilityProvider CapabilityInputProvider, authorizationProvider AuthorizationInputProvider) (*Runtime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	r := &Runtime{cfg: cfg, clock: clock, logger: logger, state: StateDisabled, metrics: newMetricCounter(), capabilityProvider: capabilityProvider, authorizationProvider: authorizationProvider, done: make(chan struct{})}
	r.breaker.state = circuitClosed
	if !cfg.Enabled {
		return r, nil
	}
	if clock == nil {
		return nil, fmt.Errorf("%w: clock", ErrInvalidConfig)
	}
	r.queue = make(chan ShadowWorkflowInput, cfg.QueueCapacity)
	r.state = StateStarting
	var store durableworkflow.Store
	if cfg.StoreMode == StoreFile {
		var err error
		store, err = durableworkflow.OpenFileStore(cfg.StoreDirectory, r.durablePolicy())
		if err != nil {
			return nil, err
		}
	} else {
		store = newMemoryStore()
	}
	coordinator, err := durableworkflow.Open(store, r.durablePolicy())
	if err != nil {
		_ = store.Close()
		r.state = StateRecoveryFailed
		r.lastErrorCode = ErrorCode(err)
		close(r.done)
		return r, fmt.Errorf("%w: %v", ErrRecoveryFailed, err)
	}
	r.store, r.coordinator, r.accepting = store, coordinator, true
	r.state = StateRunning
	workerCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.worker(workerCtx)
	return r, nil
}

func (r *Runtime) durablePolicy() durableworkflow.Policy {
	return durableworkflow.Policy{MaxRecordBytes: 8 * 1024 * 1024, MaxCheckpointBytes: int(r.cfg.MaxCheckpointBytes), MaxEpisodes: r.cfg.MaxEpisodes, MaxAdvisoryRequestsPerEpisode: r.cfg.MaxAdvisoryRequests, MaxMappingsPerEpisode: r.cfg.MaxMappingsPerCycle, MaxAuthorizationAssessmentsPerEpisode: r.cfg.MaxAuthorizationsPerCycle, SyncOnCommit: r.cfg.SyncOnCommit, AllowTruncatedFinalRecord: true, FileMode: 0600, DirectoryMode: 0700}
}

func (r *Runtime) TrySubmit(input ShadowWorkflowInput) (result SubmitResult) {
	if r == nil {
		return SubmitResult{Status: SubmitDisabled, ReasonCode: "disabled"}
	}
	r.mu.RLock()
	enabled := r.cfg.Enabled && r.state != StateDisabled
	r.mu.RUnlock()
	if !enabled {
		return SubmitResult{Status: SubmitDisabled, ReasonCode: "disabled"}
	}
	r.counters.received.Add(1)
	if err := input.Validate(); err != nil {
		r.counters.rejected.Add(1)
		r.metrics.add("input.rejected")
		return SubmitResult{Status: SubmitRejected, ReasonCode: "input.rejected"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.cfg.Enabled || r.state == StateDisabled {
		r.counters.rejected.Add(1)
		return SubmitResult{Status: SubmitDisabled, ReasonCode: "disabled"}
	}
	if !r.accepting || r.state == StateStopping || r.state == StateStopped {
		r.counters.rejected.Add(1)
		return SubmitResult{Status: SubmitStopped, ReasonCode: "stopped"}
	}
	now := r.clock.Now().UTC()
	if now.Sub(input.ObservedAt) > r.cfg.MaxInputAge {
		r.counters.rejected.Add(1)
		r.metrics.add("quota.input_too_old")
		return SubmitResult{Status: SubmitRejected, ReasonCode: "quota.input_too_old"}
	}
	if !r.breaker.permit(now, r.cfg.CircuitResetAfter) {
		r.counters.rejected.Add(1)
		r.state = StateCircuitOpen
		r.metrics.add("circuit_open")
		return SubmitResult{Status: SubmitCircuitOpen, ReasonCode: "circuit_open"}
	}
	select {
	case r.queue <- input:
		r.counters.accepted.Add(1)
		r.metrics.add("input.accepted")
		return SubmitResult{Status: SubmitAccepted}
	default:
		r.counters.rejected.Add(1)
		r.counters.dropped.Add(1)
		r.breaker.halfOpenBusy = false
		r.metrics.add("quota.queue_full")
		return SubmitResult{Status: SubmitQueueFull, ReasonCode: "quota.queue_full"}
	}
}

func (r *Runtime) worker(ctx context.Context) {
	defer close(r.done)
	for {
		select {
		case <-ctx.Done():
			return
		case input := <-r.queue:
			if err := r.processSafe(ctx, input); err != nil {
				r.recordFailure(err)
			} else {
				r.recordSuccess()
			}
		}
	}
}

func (r *Runtime) processSafe(parent context.Context, input ShadowWorkflowInput) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.metrics.add("panic.recovered")
			err = ErrPanicRecovered
		}
	}()
	ctx, cancel := context.WithTimeout(parent, r.cfg.MaxProcessingDuration)
	defer cancel()
	err = r.process(ctx, input)
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		r.counters.timeout.Add(1)
		r.metrics.add("cycle.timeout")
		return fmt.Errorf("%w: %v", ErrPipelineTimeout, err)
	}
	return err
}

func (r *Runtime) recordSuccess() {
	r.mu.Lock()
	r.breaker.success()
	if (r.state == StateCircuitOpen || r.state == StateDegraded) && r.lastErrorCode != "checkpoint_failed" {
		r.state = StateRunning
	}
	r.mu.Unlock()
	r.counters.success.Add(1)
}
func (r *Runtime) recordFailure(err error) {
	code := ErrorCode(err)
	r.mu.Lock()
	r.lastErrorCode = code
	r.breaker.failure(r.clock.Now().UTC(), r.cfg.ConsecutiveFailureLimit)
	if r.breaker.state == circuitOpen {
		r.state = StateCircuitOpen
	} else {
		r.state = StateDegraded
	}
	r.mu.Unlock()
	r.counters.failed.Add(1)
	r.metrics.add("cycle.failed")
}

func (r *Runtime) Status() StatusSnapshot {
	if r == nil {
		return StatusSnapshot{State: StateDisabled}
	}
	r.mu.RLock()
	state, enabled, depth, qcap, circuit, lastErr, warnings, failures := r.state, r.cfg.Enabled, r.cfg.PipelineDepth, cap(r.queue), string(r.breaker.state), r.lastErrorCode, append([]string(nil), r.lastWarnings...), r.breaker.failures
	r.mu.RUnlock()
	workflowRev, seq, digest, episodes := uint64(0), uint64(0), "", 0
	fresh, stale := map[durableworkflow.LayerKind]int{}, map[durableworkflow.LayerKind]int{}
	if r.coordinator != nil {
		s := r.coordinator.Snapshot()
		workflowRev, seq, episodes = s.Revision, s.LastSequence, len(s.Episodes)
		digest = s.Digest
		fresh, stale = layerCounts(s)
	}
	return StatusSnapshot{State: state, Enabled: enabled, PipelineDepth: depth, QueueDepth: len(r.queue), QueueCapacity: qcap, CircuitState: circuit, WorkflowRevision: workflowRev, LastSequence: seq, WorkflowDigest: digest, EpisodeCount: episodes, FreshLayerCounts: cloneCounts(fresh), StaleLayerCounts: cloneCounts(stale), Received: r.counters.received.Load(), Accepted: r.counters.accepted.Load(), Rejected: r.counters.rejected.Load(), DroppedQueueFull: r.counters.dropped.Load(), Duplicates: r.counters.duplicates.Load(), CyclesSucceeded: r.counters.success.Load(), CyclesFailed: r.counters.failed.Load(), CyclesTimedOut: r.counters.timeout.Load(), CommitsSucceeded: r.counters.commits.Load(), CommitsFailed: r.counters.commitFailed.Load(), CheckpointsSucceeded: r.counters.checkpoints.Load(), CheckpointsFailed: r.counters.checkpointFailed.Load(), RecoveryPerformed: r.coordinator != nil, RecoveryWarnings: warnings, ConsecutiveFailures: failures, LastErrorCode: lastErr}
}

func (r *Runtime) Metrics() map[string]uint64 {
	if r == nil {
		return nil
	}
	return r.metrics.snapshot()
}
func (r *Runtime) CoordinatorSnapshot() durableworkflow.WorkflowState {
	if r == nil || r.coordinator == nil {
		return durableworkflow.WorkflowState{}
	}
	return r.coordinator.Snapshot()
}

func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if !r.cfg.Enabled {
		r.state = StateStopped
		r.mu.Unlock()
		return nil
	}
	if r.state == StateStopped {
		r.mu.Unlock()
		return nil
	}
	if r.coordinator == nil {
		r.state = StateStopped
		r.mu.Unlock()
		return nil
	}
	r.accepting = false
	r.state = StateStopping
	r.mu.Unlock()
	r.mu.RLock()
	transactionsSinceCheckpoint := r.transactionsSinceCheckpoint
	r.mu.RUnlock()
	if r.coordinator != nil && transactionsSinceCheckpoint > 0 {
		if _, err := r.coordinator.CheckpointAt(r.clock.Now().UTC()); err != nil {
			r.counters.checkpointFailed.Add(1)
			r.metrics.add("checkpoint.failed")
		}
	}
	if r.cancel != nil {
		r.cancel()
	}
	select {
	case <-r.done:
	case <-ctx.Done():
		r.mu.Lock()
		r.state = StateStopped
		r.mu.Unlock()
		return ErrShutdownTimeout
	}
	if r.coordinator != nil {
		if err := r.coordinator.Close(); err != nil {
			return err
		}
	}
	r.mu.Lock()
	r.state = StateStopped
	r.mu.Unlock()
	return nil
}

func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case err == ErrInputTooOld:
		return "quota.input_too_old"
	case err == ErrQueueFull:
		return "quota.queue_full"
	case err == ErrPipelineTimeout:
		return "quota.processing_timeout"
	case err == ErrPanicRecovered:
		return "panic.recovered"
	case err == ErrRecoveryFailed:
		return "recovery_failed"
	default:
		return "workflow_error"
	}
}
