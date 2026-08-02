package shadowworkflow

import (
	"context"
	"errors"
	"time"

	"synora/internal/cge/calibrationanalytics"
	"synora/internal/cge/calibrationledger"
)

func (r *Runtime) maybeRecomputeCalibrationAnalytics(sequence uint64) {
	if r == nil || !r.cfg.CalibrationAnalytics.Enabled || sequence == 0 || sequence%r.cfg.CalibrationAnalytics.RecomputeEveryRecords != 0 {
		return
	}
	_ = r.recomputeCalibrationAnalytics(context.Background())
}

func (r *Runtime) recomputeCalibrationAnalytics(ctx context.Context) error {
	if r == nil || !r.cfg.CalibrationAnalytics.Enabled {
		return calibrationanalytics.ErrAnalyticsDisabled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.calibrationAnalyticsMu.Lock()
	defer r.calibrationAnalyticsMu.Unlock()
	r.mu.RLock()
	ledger := r.calibrationLedger
	policy := r.cfg.CalibrationAnalytics.Policy
	r.mu.RUnlock()
	if ledger == nil {
		return r.markCalibrationAnalyticsFailure(calibrationanalytics.ErrLedgerUnavailable)
	}
	started := time.Now()
	defer func() {
		r.metrics.addN("calibration_analytics_recompute_duration_ns", uint64(time.Since(started).Nanoseconds()))
	}()
	snapshot := ledger.Snapshot()
	records := make([]calibrationledger.CalibrationRecord, 0)
	if snapshot.LastSequence > 0 {
		from := uint64(1)
		for from <= snapshot.LastSequence {
			if err := ctx.Err(); err != nil {
				return r.markCalibrationAnalyticsFailure(err)
			}
			result, err := ledger.Query(calibrationledger.Query{SequenceFrom: from, SequenceTo: snapshot.LastSequence, Limit: 1000})
			if err != nil {
				if errors.Is(err, calibrationledger.ErrSnapshotUnavailable) {
					return r.markCalibrationAnalyticsFailure(errors.Join(calibrationanalytics.ErrLedgerUnavailable, err))
				}
				return r.markCalibrationAnalyticsFailure(err)
			}
			if len(result.Records) == 0 {
				break
			}
			records = append(records, result.Records...)
			last := result.Records[len(result.Records)-1].Sequence
			if last < from {
				return r.markCalibrationAnalyticsFailure(calibrationanalytics.ErrInvalidInput)
			}
			from = last + 1
		}
	}
	input := calibrationanalytics.AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence, LedgerFingerprint: calibrationledger.SummaryFromSnapshot(snapshot).LedgerFingerprint}
	report, err := calibrationanalytics.Analyze(input, policy)
	if err != nil {
		return r.markCalibrationAnalyticsFailure(err)
	}
	r.mu.Lock()
	r.calibrationAnalyticsReport = report.Clone()
	r.calibrationAnalyticsStatus.Enabled = true
	r.calibrationAnalyticsStatus.Available = true
	r.calibrationAnalyticsStatus.Degraded = false
	r.calibrationAnalyticsStatus.LastAnalyzedSequence = report.LastSequence
	r.calibrationAnalyticsStatus.LastReportFingerprint = report.ReportFingerprint
	r.calibrationAnalyticsStatus.ReportsGenerated++
	r.calibrationAnalyticsStatus.LastRecordCount = report.RecordCount
	r.calibrationAnalyticsStatus.LastEligibleCohortCount = report.DataSufficiency.EligibleCohortCount
	r.calibrationAnalyticsStatus.LastWindowCount = report.DataSufficiency.WindowCount
	r.calibrationAnalyticsStatus.LastErrorCode = ""
	if !report.DataSufficiency.SufficientForGlobalAnalysis || !report.DataSufficiency.SufficientForTrendAnalysis || !report.DataSufficiency.SufficientForDriftAnalysis || !report.DataSufficiency.SufficientForPolicyComparison {
		r.calibrationAnalyticsStatus.InsufficientDataCount++
	}
	r.mu.Unlock()
	r.metrics.add("calibration_analytics_reports_total")
	if !report.DataSufficiency.SufficientForGlobalAnalysis || !report.DataSufficiency.SufficientForTrendAnalysis || !report.DataSufficiency.SufficientForDriftAnalysis || !report.DataSufficiency.SufficientForPolicyComparison {
		r.metrics.add("calibration_analytics_insufficient_data_total")
	}
	r.metrics.set("calibration_analytics_last_record_count", report.RecordCount)
	r.metrics.set("calibration_analytics_last_window_count", uint64(report.DataSufficiency.WindowCount))
	r.metrics.set("calibration_analytics_last_eligible_cohort_count", uint64(report.DataSufficiency.EligibleCohortCount))
	r.metrics.set("calibration_analytics_alignment_mean_permille", uint64(report.Global.AlignmentMeanPermille))
	r.metrics.set("calibration_analytics_divergence_mean_permille", uint64(report.Global.DivergenceMeanPermille))
	r.metrics.set("calibration_analytics_coverage_mean_permille", uint64(report.Global.CoverageMeanPermille))
	r.metrics.set("calibration_analytics_comparable_rate_permille", uint64(report.Global.ComparableRatePermille))
	r.metrics.set("calibration_analytics_significant_divergence_rate_permille", uint64(report.Global.SignificantDivergenceRatePermille))
	if report.Drift.AnyDriftDetected {
		r.metrics.set("calibration_analytics_drift_detected", 1)
	} else {
		r.metrics.set("calibration_analytics_drift_detected", 0)
	}
	return nil
}

func (r *Runtime) markCalibrationAnalyticsFailure(err error) error {
	r.mu.Lock()
	r.calibrationAnalyticsStatus.Enabled = true
	r.calibrationAnalyticsStatus.Available = false
	r.calibrationAnalyticsStatus.Degraded = true
	r.calibrationAnalyticsStatus.AnalysisFailures++
	r.calibrationAnalyticsStatus.LastErrorCode = calibrationanalytics.ErrorCode(err)
	r.mu.Unlock()
	r.metrics.add("calibration_analytics_failures_total")
	if errors.Is(err, calibrationanalytics.ErrLedgerUnavailable) {
		return err
	}
	return errors.Join(calibrationanalytics.ErrAnalysisFailed, err)
}

func (r *Runtime) CalibrationAnalyticsStatus() CalibrationAnalyticsStatus {
	if r == nil {
		return CalibrationAnalyticsStatus{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.calibrationAnalyticsStatus
}

func (r *Runtime) CalibrationAnalyticsReport() calibrationanalytics.CalibrationAnalyticsReport {
	if r == nil {
		return calibrationanalytics.CalibrationAnalyticsReport{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.calibrationAnalyticsReport.Clone()
}

func (r *Runtime) RecomputeCalibrationAnalytics() (calibrationanalytics.CalibrationAnalyticsReport, error) {
	if r == nil || !r.cfg.CalibrationAnalytics.Enabled {
		return calibrationanalytics.CalibrationAnalyticsReport{}, calibrationanalytics.ErrAnalyticsDisabled
	}
	if err := r.recomputeCalibrationAnalytics(context.Background()); err != nil {
		return calibrationanalytics.CalibrationAnalyticsReport{}, err
	}
	return r.CalibrationAnalyticsReport(), nil
}
