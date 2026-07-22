package calibrationanalytics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type AnalyticsPolicy struct {
	MinimumRecords           uint64 `json:"minimum_records"`
	MinimumComparableRecords uint64 `json:"minimum_comparable_records"`
	MinimumRecordsPerCohort  uint64 `json:"minimum_records_per_cohort"`
	MinimumWindowsForTrend   int    `json:"minimum_windows_for_trend"`
	WindowSizeRecords        uint64 `json:"window_size_records"`
	MaximumWindows           int    `json:"maximum_windows"`
	MaximumCohorts           int    `json:"maximum_cohorts"`
	MaximumCategories        int    `json:"maximum_categories"`
	DriftMinimumSampleSize   uint64 `json:"drift_minimum_sample_size"`
	DriftMeanDeltaPermille   int    `json:"drift_mean_delta_permille"`
	DriftP95DeltaPermille    int    `json:"drift_p95_delta_permille"`
	DriftRateDeltaPermille   int    `json:"drift_rate_delta_permille"`
	StabilityRangePermille   int    `json:"stability_range_permille"`
	SignificantTrendPermille int    `json:"significant_trend_permille"`
	IncludeIncomparable      bool   `json:"include_incomparable"`
	IncludeStale             bool   `json:"include_stale"`
	IncludeInvalidated       bool   `json:"include_invalidated"`
}

func DefaultAnalyticsPolicy() AnalyticsPolicy {
	return AnalyticsPolicy{MinimumRecords: 100, MinimumComparableRecords: 50, MinimumRecordsPerCohort: 50, MinimumWindowsForTrend: 3, WindowSizeRecords: 100, MaximumWindows: 100, MaximumCohorts: 32, MaximumCategories: 32, DriftMinimumSampleSize: 100, DriftMeanDeltaPermille: 100, DriftP95DeltaPermille: 150, DriftRateDeltaPermille: 100, StabilityRangePermille: 100, SignificantTrendPermille: 100, IncludeIncomparable: true, IncludeStale: true, IncludeInvalidated: true}
}

func (p AnalyticsPolicy) Validate() error {
	if p.MinimumRecords == 0 || p.MinimumComparableRecords > p.MinimumRecords || p.MinimumRecordsPerCohort == 0 || p.MinimumWindowsForTrend < 2 || p.WindowSizeRecords == 0 || p.WindowSizeRecords > uint64(^uint(0)>>1) || p.MaximumWindows <= 0 || p.MaximumWindows > 1000 || p.MaximumCohorts <= 0 || p.MaximumCohorts > 256 || p.MaximumCategories <= 0 || p.MaximumCategories > 256 || p.DriftMinimumSampleSize == 0 || p.DriftMeanDeltaPermille < 0 || p.DriftMeanDeltaPermille > 1000 || p.DriftP95DeltaPermille < 0 || p.DriftP95DeltaPermille > 1000 || p.DriftRateDeltaPermille < 0 || p.DriftRateDeltaPermille > 1000 || p.StabilityRangePermille < 0 || p.StabilityRangePermille > 1000 || p.SignificantTrendPermille < 0 || p.SignificantTrendPermille > 1000 {
		return ErrInvalidAnalyticsPolicy
	}
	return nil
}

func (p AnalyticsPolicy) Fingerprint() string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return "calibration-analytics-policy-v1:" + hex.EncodeToString(h[:])
}
