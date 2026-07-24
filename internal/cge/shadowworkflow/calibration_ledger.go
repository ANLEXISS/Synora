package shadowworkflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"synora/internal/cge/calibrationledger"
	"synora/internal/cge/cognitiverecommendation"
	"synora/internal/cge/cognitivesituation"
	"synora/internal/cge/decisioncomparison"
)

func (r *Runtime) appendCalibrationComparison(ctx context.Context, episodeID string) error {
	if r == nil || r.calibrationLedger == nil {
		return nil
	}
	comparison, ok := r.HistoricalDecisionComparison(episodeID)
	if !ok {
		return nil
	}
	var previous *calibrationledger.CalibrationRecord
	if reader, ok := r.calibrationLedger.(interface {
		LastRecord() (calibrationledger.CalibrationRecord, bool)
	}); ok {
		if value, found := reader.LastRecord(); found {
			previous = &value
		}
	}
	record, err := calibrationledger.BuildRecord(calibrationledger.BuildRecordInput{Comparison: comparison, SituationPolicyFingerprint: cognitivesituation.DefaultPolicy().Fingerprint(), RecommendationPolicyFingerprint: cognitiverecommendation.DefaultPolicy().Fingerprint(), ComparisonPolicyFingerprint: decisioncomparison.DefaultPolicy().Fingerprint(), Previous: previous}, r.cfg.CalibrationLedger.effectivePolicy())
	if err != nil {
		return r.markCalibrationLedgerFailure(fmt.Errorf("%w: build", err))
	}
	started := time.Now()
	result, err := r.calibrationLedger.Append(ctx, record)
	r.metrics.addN("calibration_ledger_append_duration_ns", uint64(time.Since(started).Nanoseconds()))
	if err != nil {
		return r.markCalibrationLedgerFailure(err)
	}
	snapshot := r.calibrationLedger.Snapshot()
	r.mu.Lock()
	r.calibrationLedgerStatus.Enabled = true
	r.calibrationLedgerStatus.Available = true
	r.calibrationLedgerStatus.Degraded = false
	r.calibrationLedgerStatus.RecordCount = snapshot.RecordCount
	r.calibrationLedgerStatus.LastSequence = snapshot.LastSequence
	r.calibrationLedgerStatus.LastRecordFingerprint = snapshot.LastRecordFingerprint
	r.calibrationLedgerStatus.DuplicateRecords += boolUint64(result.Duplicate)
	r.calibrationLedgerStatus.LastErrorCode = ""
	r.mu.Unlock()
	if result.Duplicate {
		r.metrics.add("calibration_ledger_duplicates")
	} else {
		r.metrics.add("calibration_ledger_records_appended")
	}
	if !result.Duplicate {
		r.metrics.add("calibration_ledger_records_total")
		r.maybeRecomputeCalibrationAnalytics(result.Sequence)
	}
	r.metrics.addN("calibration_ledger_bytes", uint64(snapshot.LedgerBytes))
	if comparison.Comparable {
		r.metrics.add("calibration_comparable_total")
	} else {
		r.metrics.add("calibration_incomparable_total")
	}
	if comparison.SignificantDivergence {
		r.metrics.add("calibration_significant_divergence_total")
	}
	switch comparison.Category {
	case decisioncomparison.CategoryAligned:
		r.metrics.add("calibration_aligned_total")
	case decisioncomparison.CategoryPartiallyAligned:
		r.metrics.add("calibration_partially_aligned_total")
	case decisioncomparison.CategoryDivergent:
		r.metrics.add("calibration_divergent_total")
	}
	return nil
}

func boolUint64(v bool) uint64 {
	if v {
		return 1
	}
	return 0
}

func (r *Runtime) markCalibrationLedgerFailure(err error) error {
	r.mu.Lock()
	r.calibrationLedgerStatus.Enabled = true
	r.calibrationLedgerStatus.Available = false
	r.calibrationLedgerStatus.Degraded = true
	r.calibrationLedgerStatus.AppendFailures++
	r.calibrationLedgerStatus.LastErrorCode = calibrationledger.ErrorCode(err)
	r.mu.Unlock()
	r.metrics.add("calibration_ledger_append_failures")
	if errors.Is(err, calibrationledger.ErrRecordTooLarge) || errors.Is(err, calibrationledger.ErrLedgerLimitReached) || errors.Is(err, calibrationledger.ErrHashChainMismatch) || errors.Is(err, calibrationledger.ErrRecordFingerprintMismatch) || errors.Is(err, calibrationledger.ErrEnvelopeFingerprintMismatch) {
		r.metrics.add("calibration_ledger_integrity_failures")
	}
	return fmt.Errorf("%w: %v", ErrCalibrationLedgerAppendFailed, err)
}

func (r *Runtime) CalibrationLedgerStatus() CalibrationLedgerStatus {
	if r == nil {
		return CalibrationLedgerStatus{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.calibrationLedgerStatus
}
func (r *Runtime) CalibrationSnapshot() calibrationledger.Snapshot {
	if r == nil || r.calibrationLedger == nil {
		return calibrationledger.Snapshot{}
	}
	return r.calibrationLedger.Snapshot()
}
func (r *Runtime) CalibrationSummary() calibrationledger.CalibrationSummary {
	if r == nil || r.calibrationLedger == nil {
		return calibrationledger.CalibrationSummary{}
	}
	if v, ok := r.calibrationLedger.(interface {
		Summary() calibrationledger.CalibrationSummary
	}); ok {
		return v.Summary()
	}
	return calibrationledger.SummaryFromSnapshot(r.calibrationLedger.Snapshot())
}
func (r *Runtime) CalibrationRecords(q calibrationledger.Query) (calibrationledger.QueryResult, error) {
	if r == nil || r.calibrationLedger == nil {
		return calibrationledger.QueryResult{}, calibrationledger.ErrSnapshotUnavailable
	}
	return r.calibrationLedger.Query(q)
}
