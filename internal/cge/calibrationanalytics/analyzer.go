package calibrationanalytics

import (
	"sort"

	"synora/internal/cge/calibrationledger"
)

type metricStats struct {
	aggregate    calibrationledger.AggregateSnapshot
	comparable   uint64
	significant  uint64
	incomparable uint64
}

func statsFor(records []calibrationledger.CalibrationRecord) metricStats {
	stats := metricStats{aggregate: calibrationledger.AggregateRecords(records)}
	for _, record := range records {
		if record.Comparable {
			stats.comparable++
		}
		if record.SignificantDivergence {
			stats.significant++
		}
		if record.Category == "incomparable" {
			stats.incomparable++
		}
	}
	return stats
}

func ratePermille(value, total uint64) int {
	if total == 0 {
		return 0
	}
	return int((value*1000 + total/2) / total)
}

func globalAnalytics(records []calibrationledger.CalibrationRecord) GlobalAnalytics {
	s := statsFor(records)
	a := s.aggregate
	return GlobalAnalytics{RecordCount: a.TotalRecords, ComparableCount: s.comparable, IncomparableCount: s.incomparable, SignificantDivergenceCount: s.significant, ComparableRatePermille: ratePermille(s.comparable, a.TotalRecords), SignificantDivergenceRatePermille: ratePermille(s.significant, a.TotalRecords), AlignmentMeanPermille: a.AlignmentMeanPermille, DivergenceMeanPermille: a.DivergenceMeanPermille, CoverageMeanPermille: a.CoverageMeanPermille, AlignmentP50Permille: a.AlignmentP50Permille, AlignmentP95Permille: a.AlignmentP95Permille, AlignmentP99Permille: a.AlignmentP99Permille, DivergenceP50Permille: a.DivergenceP50Permille, DivergenceP95Permille: a.DivergenceP95Permille, DivergenceP99Permille: a.DivergenceP99Permille, CoverageP50Permille: a.CoverageP50Permille, CoverageP95Permille: a.CoverageP95Permille, CoverageP99Permille: a.CoverageP99Permille, HistoricalTransitions: a.HistoricalTransitions, CognitiveTransitions: a.CognitiveTransitions, AlignedTransitions: a.AlignedTransitions, CognitiveMoreConservative: a.CognitiveMoreConservative, HistoricalMoreDecisive: a.HistoricalMoreDecisive}
}

func categoryAnalytics(records []calibrationledger.CalibrationRecord, policy AnalyticsPolicy) CategoryAnalytics {
	stats := statsFor(records)
	a := stats.aggregate
	return CategoryAnalytics{Category: records[0].Category, RecordCount: a.TotalRecords, ComparableRatePermille: ratePermille(stats.comparable, a.TotalRecords), SignificantDivergenceRatePermille: ratePermille(stats.significant, a.TotalRecords), AlignmentMeanPermille: a.AlignmentMeanPermille, DivergenceMeanPermille: a.DivergenceMeanPermille, CoverageMeanPermille: a.CoverageMeanPermille, AlignmentP95Permille: a.AlignmentP95Permille, DivergenceP95Permille: a.DivergenceP95Permille, CoverageP95Permille: a.CoverageP95Permille, Sufficient: a.TotalRecords >= policy.MinimumRecordsPerCohort}
}

func globalFingerprint(value GlobalAnalytics) string {
	value.Fingerprint = ""
	return digest("calibration-global-analytics-v1:", value)
}

func categoryFingerprint(value CategoryAnalytics) string {
	value.Fingerprint = ""
	return digest("calibration-category-analytics-v1:", value)
}

func filterRecords(records []calibrationledger.CalibrationRecord, policy AnalyticsPolicy) []calibrationledger.CalibrationRecord {
	out := make([]calibrationledger.CalibrationRecord, 0, len(records))
	for _, record := range records {
		switch record.Category {
		case "incomparable":
			if !policy.IncludeIncomparable {
				continue
			}
		case "stale":
			if !policy.IncludeStale {
				continue
			}
		case "invalidated":
			if !policy.IncludeInvalidated {
				continue
			}
		}
		out = append(out, record.Clone())
	}
	return out
}

func Analyze(input AnalyticsInput, policy AnalyticsPolicy) (CalibrationAnalyticsReport, error) {
	records, ledgerFingerprint, err := input.normalized(policy)
	if err != nil {
		return CalibrationAnalyticsReport{}, err
	}
	records = filterRecords(records, policy)
	global := globalAnalytics(records)
	global.Fingerprint = globalFingerprint(global)

	byCategory := map[string][]calibrationledger.CalibrationRecord{}
	for _, record := range records {
		byCategory[record.Category] = append(byCategory[record.Category], record)
	}
	if len(byCategory) > policy.MaximumCategories {
		return CalibrationAnalyticsReport{}, ErrTooManyCategories
	}
	categories := make([]CategoryAnalytics, 0, len(byCategory))
	categoryKeys := make([]string, 0, len(byCategory))
	for category := range byCategory {
		categoryKeys = append(categoryKeys, category)
	}
	sort.Strings(categoryKeys)
	for _, category := range categoryKeys {
		value := categoryAnalytics(byCategory[category], policy)
		value.Fingerprint = categoryFingerprint(value)
		categories = append(categories, value)
	}

	cohorts, cohortRecords, err := buildCohorts(records, policy)
	if err != nil {
		return CalibrationAnalyticsReport{}, err
	}
	windows, windowRecords := buildWindows(records, policy)
	trend := buildTrend(windows, policy)
	drift := buildDrift(windowRecords, policy)
	evaluation := buildPolicyEvaluation(cohorts, cohortRecords)
	sufficiency := buildSufficiency(global, len(windows), evaluation.EligibleCohortCount, policy)
	sufficiency.SufficientForTrendAnalysis = trend.Sufficient
	sufficiency.SufficientForDriftAnalysis = drift.Sufficient
	sufficiency.SufficientForPolicyComparison = evaluation.Sufficient
	if !drift.Sufficient && len(windows) >= policy.MinimumWindowsForTrend {
		sufficiency.Limitations = append(sufficiency.Limitations, "drift_minimum_sample_not_reached")
	}
	sufficiency.Fingerprint = sufficiencyFingerprint(sufficiency)

	first, last := uint64(0), uint64(0)
	if len(records) > 0 {
		first, last = records[0].Sequence, records[len(records)-1].Sequence
	}
	report := CalibrationAnalyticsReport{SchemaVersion: SchemaVersion, LedgerFingerprint: ledgerFingerprint, AnalyticsPolicyFingerprint: policy.Fingerprint(), FirstSequence: first, LastSequence: last, GeneratedFromSequence: input.GeneratedFromSequence, RecordCount: uint64(len(records)), DataSufficiency: sufficiency, Global: global, Categories: canonicalCategories(categories), PolicyCohorts: cohorts, Windows: windows, Trend: trend, Drift: drift, PolicyEvaluation: evaluation, Markers: analyticsMarkers()}
	report.ReportFingerprint = reportFingerprint(report)
	if err := report.Validate(policy); err != nil {
		return CalibrationAnalyticsReport{}, err
	}
	return report.Clone(), nil
}
