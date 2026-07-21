package shadowworkflow

import (
	"time"
)

const (
	qualificationStageTrySubmit           = "try_submit"
	qualificationStageFullCycle           = "full_cycle"
	qualificationStageEpisode             = "episode"
	qualificationStageFacts               = "situation_facts"
	qualificationStageHypotheses          = "situation_hypotheses"
	qualificationStageDiscrimination      = "evidence_discrimination"
	qualificationStageAdvisory            = "advisory_requests"
	qualificationStageMapping             = "capability_mapping"
	qualificationStageAuthorization       = "authorization_boundary"
	qualificationStageTransactionPlanning = "transaction_planning"
	qualificationStageDurableCommit       = "durable_commit"
	qualificationStageCheckpoint          = "checkpoint"
	qualificationStageRecovery            = "recovery"
)

func (r *Runtime) qualificationStageBegin() time.Time {
	if r == nil || r.qualification == nil {
		return time.Time{}
	}
	return time.Now()
}

func (r *Runtime) qualificationStageEnd(stage string, started time.Time, err error) {
	if r != nil && r.qualification != nil {
		r.qualification.RecordStage(stage, started, err)
	}
}

func qualificationSubmitError(result SubmitResult) error {
	if result.Status == SubmitAccepted {
		return nil
	}
	switch result.Status {
	case SubmitQueueFull:
		return ErrQueueFull
	case SubmitCircuitOpen:
		return ErrCircuitOpen
	case SubmitStorageLimit:
		return ErrWALSizeLimit
	case SubmitDisabled:
		return ErrDisabled
	case SubmitStopped:
		return ErrStopped
	default:
		return ErrInputRejected
	}
}

func (r *Runtime) qualificationSample() QualificationSample {
	status := r.Status()
	sample := QualificationSample{SampledAt: r.clock.Now().UTC(), RuntimeState: string(status.State), PipelineDepth: string(status.PipelineDepth), CircuitState: status.CircuitState, WorkflowRevision: status.WorkflowRevision, LastSequence: status.LastSequence, QueueDepth: status.QueueDepth, QueueCapacity: status.QueueCapacity, EpisodeCount: status.EpisodeCount, Received: status.Received, Accepted: status.Accepted, Rejected: status.Rejected, DroppedQueueFull: status.DroppedQueueFull, Duplicates: status.Duplicates, CyclesSucceeded: status.CyclesSucceeded, CyclesFailed: status.CyclesFailed, CyclesTimedOut: status.CyclesTimedOut, CommitsSucceeded: status.CommitsSucceeded, CommitsFailed: status.CommitsFailed, CheckpointsSucceeded: status.CheckpointsSucceeded, CheckpointsFailed: status.CheckpointsFailed, LastErrorCode: status.LastErrorCode}
	metrics := r.Metrics()
	sample.InvalidLineage = metrics["lineage.invalid"]
	sample.RecoveryDigestMismatches = metrics["recovery.digest_mismatch"]
	if r.cfg.Qualification.IncludeProcessMetrics {
		sample.Process = readProcessSample(true)
	}
	if r.cfg.Qualification.IncludeStageMetrics && r.qualification != nil {
		sample.StageCounters = r.qualification.StageSnapshot()
	}
	if r.qualification != nil {
		sample.HistoricalIsolation.TrySubmitFailuresObserved = r.qualification.TrySubmitFailures()
	}
	if r.cfg.Qualification.IncludeStorageMetrics {
		sample.Storage = r.qualificationStorageSample()
	}
	if r.cfg.CalibrationLedger.Enabled {
		snapshot := r.CalibrationSnapshot()
		summary := r.CalibrationSummary()
		status := r.CalibrationLedgerStatus()
		sample.CalibrationLedgerRecords = snapshot.RecordCount
		sample.CalibrationLedgerBytes = snapshot.LedgerBytes
		sample.CalibrationLedgerAppendFailures = status.AppendFailures
		sample.CalibrationLedgerIntegrityFailures = status.IntegrityFailures
		sample.CalibrationAlignmentMeanPermille = summary.AlignmentMeanPermille
		sample.CalibrationDivergenceMeanPermille = summary.DivergenceMeanPermille
		sample.CalibrationCoverageMeanPermille = summary.CoverageMeanPermille
		sample.CalibrationComparableRatePermille = summary.ComparableRatePermille
		sample.CalibrationSignificantDivergenceRatePermille = summary.SignificantDivergenceRatePermille
	}
	return sample
}

func (r *Runtime) qualificationStorageSample() StorageSample {
	sample := StorageSample{StoreMode: string(r.cfg.StoreMode), StorageLimitReached: r.Status().State == StateStorageLimitReached}
	r.mu.RLock()
	sample.TransactionsSinceCheckpoint = r.transactionsSinceCheckpoint
	r.mu.RUnlock()
	if sized, ok := r.store.(interface{ WALSize() (int64, error) }); ok {
		if value, err := sized.WALSize(); err == nil {
			sample.WALBytes = value
		}
	}
	if sized, ok := r.store.(interface{ CheckpointSize() (int64, error) }); ok {
		if value, err := sized.CheckpointSize(); err == nil {
			sample.CheckpointBytes = value
		}
	}
	return sample
}
