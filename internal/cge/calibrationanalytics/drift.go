package calibrationanalytics

import (
	"math"

	"synora/internal/cge/calibrationledger"
)

func buildDrift(windowRecords [][]calibrationledger.CalibrationRecord, policy AnalyticsPolicy) DriftAnalytics {
	value := DriftAnalytics{Alignment: MetricDrift{Metric: "alignment"}, Divergence: MetricDrift{Metric: "divergence"}, Coverage: MetricDrift{Metric: "coverage"}, ComparableRate: MetricDrift{Metric: "comparable_rate"}, SignificantDivergenceRate: MetricDrift{Metric: "significant_divergence_rate"}}
	if len(windowRecords) >= 2 {
		mid := len(windowRecords) / 2
		baseline := flattenWindows(windowRecords[:mid])
		recent := flattenWindows(windowRecords[mid:])
		value.BaselineRecordCount = uint64(len(baseline))
		value.RecentRecordCount = uint64(len(recent))
		value.Alignment = driftMetric("alignment", baseline, recent, policy)
		value.Divergence = driftMetric("divergence", baseline, recent, policy)
		value.Coverage = driftMetric("coverage", baseline, recent, policy)
		value.ComparableRate = driftMetric("comparable_rate", baseline, recent, policy)
		value.SignificantDivergenceRate = driftMetric("significant_divergence_rate", baseline, recent, policy)
		value.Sufficient = value.Alignment.Sufficient && value.Divergence.Sufficient && value.Coverage.Sufficient && value.ComparableRate.Sufficient && value.SignificantDivergenceRate.Sufficient
		value.AnyDriftDetected = value.Alignment.Detected || value.Divergence.Detected || value.Coverage.Detected || value.ComparableRate.Detected || value.SignificantDivergenceRate.Detected
	}
	value.Fingerprint = driftFingerprint(value)
	return value
}

func flattenWindows(windows [][]calibrationledger.CalibrationRecord) []calibrationledger.CalibrationRecord {
	var out []calibrationledger.CalibrationRecord
	for _, values := range windows {
		out = append(out, values...)
	}
	return out
}

func driftMetric(metric string, baseline, recent []calibrationledger.CalibrationRecord, policy AnalyticsPolicy) MetricDrift {
	left := statsFor(baseline)
	right := statsFor(recent)
	value := MetricDrift{Metric: metric, Sufficient: uint64(len(baseline)) >= policy.DriftMinimumSampleSize && uint64(len(recent)) >= policy.DriftMinimumSampleSize}
	value.BaselineMeanPermille, value.RecentMeanPermille = recordMetricMean(metric, left), recordMetricMean(metric, right)
	value.BaselineP95Permille, value.RecentP95Permille = recordMetricP95(metric, left), recordMetricP95(metric, right)
	value.MeanDeltaPermille = value.RecentMeanPermille - value.BaselineMeanPermille
	value.P95DeltaPermille = value.RecentP95Permille - value.BaselineP95Permille
	threshold := policy.DriftMeanDeltaPermille
	if metric == "comparable_rate" || metric == "significant_divergence_rate" {
		threshold = policy.DriftRateDeltaPermille
	}
	value.Detected = value.Sufficient && (int(math.Abs(float64(value.MeanDeltaPermille))) >= threshold || int(math.Abs(float64(value.P95DeltaPermille))) >= policy.DriftP95DeltaPermille)
	return value
}

func recordMetricMean(metric string, value metricStats) int {
	switch metric {
	case "alignment":
		return value.aggregate.AlignmentMeanPermille
	case "divergence":
		return value.aggregate.DivergenceMeanPermille
	case "coverage":
		return value.aggregate.CoverageMeanPermille
	case "comparable_rate":
		return ratePermille(value.comparable, value.aggregate.TotalRecords)
	case "significant_divergence_rate":
		return ratePermille(value.significant, value.aggregate.TotalRecords)
	default:
		return 0
	}
}

func recordMetricP95(metric string, value metricStats) int {
	switch metric {
	case "alignment":
		return value.aggregate.AlignmentP95Permille
	case "divergence":
		return value.aggregate.DivergenceP95Permille
	case "coverage":
		return value.aggregate.CoverageP95Permille
	case "comparable_rate":
		return ratePermille(value.comparable, value.aggregate.TotalRecords)
	case "significant_divergence_rate":
		return ratePermille(value.significant, value.aggregate.TotalRecords)
	default:
		return 0
	}
}

func driftFingerprint(value DriftAnalytics) string {
	value.Fingerprint = ""
	return digest("calibration-drift-v1:", value)
}
