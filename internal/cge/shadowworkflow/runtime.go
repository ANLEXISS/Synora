package shadowworkflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"synora/internal/cge/calibrationledger"
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
	qualification               *QualificationRecorder
	transactionsSinceCheckpoint uint64
	lastCheckpointAt            time.Time
	checkpointFailure           bool
	projection                  cognitiveProjectionCache
	calibrationLedger           calibrationledger.Store
	calibrationLedgerStatus     CalibrationLedgerStatus
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
	var qualificationStarted time.Time
	var recoveryStarted time.Time
	if cfg.Qualification.Enabled {
		qualificationStarted = clock.Now().UTC()
		recoveryStarted = time.Now()
	}
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
		r.lastErrorCode = ErrorCode(fmt.Errorf("%w: %v", ErrRecoveryFailed, err))
		close(r.done)
		return r, fmt.Errorf("%w: %v", ErrRecoveryFailed, err)
	}
	r.store, r.coordinator, r.accepting = store, coordinator, true
	if recovered, loadErr := store.Load(); loadErr == nil {
		r.lastWarnings = append([]string(nil), recovered.Warnings...)
	}
	r.lastCheckpointAt = clock.Now().UTC()
	if err := r.rebuildCognitiveSituations(coordinator.Snapshot()); err != nil {
		r.state = StateDegraded
		r.lastErrorCode = "cognitive_situation_recovery_failed"
	} else {
		r.state = StateRunning
	}
	if cfg.CalibrationLedger.Enabled {
		r.metrics.add("calibration_ledger_enabled")
		started := time.Now()
		ledger := cfg.CalibrationLedger.Store
		var ledgerErr error
		if ledger == nil {
			ledger, ledgerErr = calibrationledger.OpenFileStore(cfg.CalibrationLedger.Path, cfg.CalibrationLedger.effectivePolicy())
		}
		var recovery calibrationledger.RecoveryResult
		if ledgerErr == nil {
			if fileLedger, ok := ledger.(*calibrationledger.FileStore); ok {
				recovery = fileLedger.LastRecovery()
			} else {
				recovery, ledgerErr = ledger.Recover(ctx)
			}
		}
		r.metrics.addN("calibration_ledger_recovery_duration_ns", uint64(time.Since(started).Nanoseconds()))
		if ledgerErr != nil {
			if ledger != nil {
				_ = ledger.Close()
			}
			r.calibrationLedgerStatus = CalibrationLedgerStatus{Enabled: true, Available: false, Degraded: true, IntegrityFailures: 1, LastErrorCode: calibrationledger.ErrorCode(ledgerErr)}
			r.metrics.add("calibration_ledger_recovery_failures")
			r.metrics.add("calibration_ledger_integrity_failures")
		} else {
			r.calibrationLedger = ledger
			snapshot := ledger.Snapshot()
			r.calibrationLedgerStatus = calibrationStatusFromSnapshot(snapshot)
			r.calibrationLedgerStatus.RecoveryCompleted = recovery.Completed
			r.calibrationLedgerStatus.RecoveryRepairedTrailingRecord = recovery.RepairedTrailingRecord
			r.metrics.addN("calibration_ledger_records_total", snapshot.RecordCount)
			if recovery.RepairedTrailingRecord {
				r.metrics.add("calibration_ledger_trailing_repairs")
			}
		}
	}
	if cfg.Qualification.Enabled {
		qualificationConfig := cfg.Qualification
		qualificationConfig.MaxWALBytes = cfg.MaxWALBytes
		recorder, recorderErr := OpenQualificationRecorder(qualificationConfig, qualificationStarted, r.qualificationSample)
		if recorderErr != nil {
			r.metrics.add("qualification.recorder_failed")
		} else {
			r.qualification = recorder
			r.qualification.SetRecoveryDuration(time.Since(recoveryStarted))
			r.qualificationStageEnd(qualificationStageRecovery, recoveryStarted, nil)
		}
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.worker(workerCtx)
	return r, nil
}

func (r *Runtime) durablePolicy() durableworkflow.Policy {
	return durableworkflow.Policy{MaxRecordBytes: 8 * 1024 * 1024, MaxCheckpointBytes: int(r.cfg.MaxCheckpointBytes), MaxEpisodes: r.cfg.MaxEpisodes, MaxAdvisoryRequestsPerEpisode: r.cfg.MaxAdvisoryRequests, MaxMappingsPerEpisode: r.cfg.MaxMappingsPerCycle, MaxAuthorizationAssessmentsPerEpisode: r.cfg.MaxAuthorizationsPerCycle, SyncOnCommit: r.cfg.SyncOnCommit, AllowTruncatedFinalRecord: r.cfg.AllowTruncatedFinalRecord, FileMode: 0600, DirectoryMode: 0700}
}

func (r *Runtime) TrySubmit(input ShadowWorkflowInput) (result SubmitResult) {
	if r != nil && r.qualification != nil {
		started := r.qualificationStageBegin()
		defer func() {
			r.qualificationStageEnd(qualificationStageTrySubmit, started, qualificationSubmitError(result))
			if result.Status != SubmitAccepted {
				r.qualification.RecordTrySubmitFailure()
			}
		}()
	}
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
	if input.HistoricalDecision != nil {
		copy := input.HistoricalDecision.Clone()
		input.HistoricalDecision = &copy
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.cfg.Enabled || r.state == StateDisabled {
		r.counters.rejected.Add(1)
		return SubmitResult{Status: SubmitDisabled, ReasonCode: "disabled"}
	}
	if r.state == StateStorageLimitReached {
		r.counters.rejected.Add(1)
		return SubmitResult{Status: SubmitStorageLimit, ReasonCode: "quota.wal_size_limit"}
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
		if r.qualification != nil {
			r.qualification.ObserveQueue(len(r.queue))
		}
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
	started := r.qualificationStageBegin()
	defer func() {
		if recovered := recover(); recovered != nil {
			r.metrics.add("panic.recovered")
			err = ErrPanicRecovered
		}
		r.qualificationStageEnd(qualificationStageFullCycle, started, err)
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
	if (r.state == StateCircuitOpen || r.state == StateDegraded) && !r.checkpointFailure && r.lastErrorCode != "checkpoint_failed" && r.lastErrorCode != "comparison_build_failed" {
		r.state = StateRunning
	}
	r.mu.Unlock()
	r.counters.success.Add(1)
}
func (r *Runtime) recordFailure(err error) {
	code := ErrorCode(err)
	r.mu.Lock()
	r.lastErrorCode = code
	if errors.Is(err, ErrWALSizeLimit) {
		r.accepting = false
		r.state = StateStorageLimitReached
		r.mu.Unlock()
		r.counters.failed.Add(1)
		r.metrics.add("quota.wal_size_limit")
		return
	}
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
	cycleSuccesses := r.counters.success.Load()
	cycleFailures := r.counters.failed.Load()
	checkpointFailures := r.counters.checkpointFailed.Load()
	checkpointSuccesses := r.counters.checkpoints.Load()
	r.mu.RLock()
	state, lastErr = r.state, r.lastErrorCode
	r.mu.RUnlock()
	if state == StateRunning && checkpointFailures > checkpointSuccesses {
		state = StateDegraded
		if lastErr == "" {
			lastErr = "checkpoint_failed"
		}
	}
	if state == StateRunning && cycleFailures > cycleSuccesses && failures > 0 {
		state = StateDegraded
		if lastErr == "" {
			lastErr = "cycle_failed"
		}
	}
	workflowRev, seq, digest, episodes := uint64(0), uint64(0), "", 0
	fresh, stale := map[durableworkflow.LayerKind]int{}, map[durableworkflow.LayerKind]int{}
	if r.coordinator != nil {
		s := r.coordinator.Snapshot()
		workflowRev, seq, episodes = s.Revision, s.LastSequence, len(s.Episodes)
		digest = s.Digest
		fresh, stale = layerCounts(s)
	}
	r.mu.RLock()
	calibrationStatus := r.calibrationLedgerStatus
	r.mu.RUnlock()
	return StatusSnapshot{State: state, Enabled: enabled, PipelineDepth: depth, QueueDepth: len(r.queue), QueueCapacity: qcap, CircuitState: circuit, WorkflowRevision: workflowRev, LastSequence: seq, WorkflowDigest: digest, EpisodeCount: episodes, FreshLayerCounts: cloneCounts(fresh), StaleLayerCounts: cloneCounts(stale), Received: r.counters.received.Load(), Accepted: r.counters.accepted.Load(), Rejected: r.counters.rejected.Load(), DroppedQueueFull: r.counters.dropped.Load(), Duplicates: r.counters.duplicates.Load(), CyclesSucceeded: cycleSuccesses, CyclesFailed: cycleFailures, CyclesTimedOut: r.counters.timeout.Load(), CommitsSucceeded: r.counters.commits.Load(), CommitsFailed: r.counters.commitFailed.Load(), CheckpointsSucceeded: checkpointSuccesses, CheckpointsFailed: checkpointFailures, RecoveryPerformed: r.coordinator != nil, RecoveryWarnings: warnings, ConsecutiveFailures: failures, LastErrorCode: lastErr, CalibrationLedger: calibrationStatus}
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
		// Keep the stopping state while a blocked worker is still alive. A
		// later Close call may complete with a longer context.
		r.state = StateStopping
		r.mu.Unlock()
		return ErrShutdownTimeout
	}
	if r.coordinator != nil {
		if r.qualification != nil {
			if err := r.qualification.Close(r.qualificationSample); err != nil {
				r.metrics.add("qualification.close_failed")
			}
		}
		if err := r.coordinator.Close(); err != nil {
			return err
		}
	}
	if r.calibrationLedger != nil {
		_ = r.calibrationLedger.Close()
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
	case errors.Is(err, ErrInputTooOld):
		return "quota.input_too_old"
	case errors.Is(err, ErrQueueFull):
		return "quota.queue_full"
	case errors.Is(err, ErrPipelineTimeout):
		return "quota.processing_timeout"
	case errors.Is(err, ErrPanicRecovered):
		return "panic.recovered"
	case errors.Is(err, ErrRecoveryFailed):
		return "recovery_failed"
	case errors.Is(err, ErrWALSizeLimit):
		return "quota.wal_size_limit"
	case errors.Is(err, ErrCheckpointFailed):
		return "checkpoint.failed"
	case errors.Is(err, ErrDurableCommitFailed):
		return "transaction.durability_failure"
	case errors.Is(err, ErrCalibrationLedgerAppendFailed):
		return "calibration_ledger.append_failure"
	case errors.Is(err, ErrProviderUnavailable):
		return "provider.unavailable"
	case errors.Is(err, ErrProviderInvalid):
		return "provider.invalid"
	default:
		return "workflow_error"
	}
}
