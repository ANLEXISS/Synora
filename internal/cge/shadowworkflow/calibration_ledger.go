package shadowworkflow

import (
	"context"
	"errors"
	"fmt"

	"synora/internal/cge/calibrationledger"
	"synora/internal/cge/cognitiverecommendation"
	"synora/internal/cge/cognitivesituation"
	"synora/internal/cge/decisioncomparison"
)

func (r *Runtime) appendCalibrationComparison(episodeID string) error {
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
	record, err := calibrationledger.BuildRecord(calibrationledger.BuildRecordInput{
		Comparison:                      comparison,
		ComparisonPolicyFingerprint:     decisioncomparison.DefaultPolicy().Fingerprint(),
		RecommendationPolicyFingerprint: cognitiverecommendation.DefaultPolicy().Fingerprint(),
		SituationPolicyFingerprint:      cognitivesituation.DefaultPolicy().Fingerprint(),
		Previous:                        previous,
	}, r.cfg.CalibrationLedger.effectivePolicy())
	if err != nil {
		return r.markCalibrationLedgerFailure(fmt.Errorf("%w: build", err))
	}
	result, err := r.calibrationLedger.Append(context.Background(), record)
	if err != nil {
		return r.markCalibrationLedgerFailure(err)
	}
	r.mu.Lock()
	status := r.calibrationLedgerStatus
	status.Enabled = true
	status.Available = true
	status.RecordCount += boolUint64(!result.Duplicate)
	status.LastSequence = result.Sequence
	status.LastRecordFingerprint = result.RecordFingerprint
	status.DuplicateRecords += boolUint64(result.Duplicate)
	status.Degraded = false
	status.LastErrorCode = ""
	r.calibrationLedgerStatus = status
	r.mu.Unlock()
	if result.Duplicate {
		r.metrics.add("calibration_ledger_duplicates")
	} else {
		r.metrics.add("calibration_ledger_records_appended")
	}
	r.metrics.add("calibration_ledger_records_total")
	r.metrics.addN("calibration_ledger_bytes", uint64(r.CalibrationSnapshot().LedgerBytes))
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

func boolUint64(value bool) uint64 {
	if value {
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
	if errors.Is(err, calibrationledger.ErrRecordTooLarge) || errors.Is(err, calibrationledger.ErrLedgerLimitReached) {
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
	if reader, ok := r.calibrationLedger.(interface {
		Snapshot() calibrationledger.Snapshot
	}); ok {
		return reader.Snapshot()
	}
	return calibrationledger.Snapshot{}
}

func (r *Runtime) CalibrationSummary() calibrationledger.CalibrationSummary {
	if r == nil || r.calibrationLedger == nil {
		return calibrationledger.CalibrationSummary{}
	}
	if reader, ok := r.calibrationLedger.(interface {
		Summary() calibrationledger.CalibrationSummary
	}); ok {
		return reader.Summary()
	}
	return calibrationledger.CalibrationSummary{}
}

func (r *Runtime) CalibrationRecords(query calibrationledger.Query) (calibrationledger.QueryResult, error) {
	if r == nil || r.calibrationLedger == nil {
		return calibrationledger.QueryResult{}, calibrationledger.ErrSnapshotUnavailable
	}
	if reader, ok := r.calibrationLedger.(interface {
		Query(calibrationledger.Query) (calibrationledger.QueryResult, error)
	}); ok {
		return reader.Query(query)
	}
	return calibrationledger.QueryResult{}, calibrationledger.ErrSnapshotUnavailable
}
