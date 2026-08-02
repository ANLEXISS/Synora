package calibrationanalytics

import "math"

const (
	directionIncreasing   = "increasing"
	directionDecreasing   = "decreasing"
	directionStable       = "stable"
	directionInsufficient = "insufficient_data"
)

func trendMetric(metric string, values []WindowAnalytics, policy AnalyticsPolicy) MetricTrend {
	value := MetricTrend{Metric: metric, Direction: directionInsufficient}
	if len(values) < 2 {
		return value
	}
	value.FirstValuePermille = windowMetric(metric, values[0])
	value.LastValuePermille = windowMetric(metric, values[len(values)-1])
	value.DeltaPermille = value.LastValuePermille - value.FirstValuePermille
	value.Significant = int(math.Abs(float64(value.DeltaPermille))) >= policy.SignificantTrendPermille
	value.Stable = int(math.Abs(float64(value.DeltaPermille))) <= policy.StabilityRangePermille
	switch {
	case value.Stable:
		value.Direction = directionStable
	case value.DeltaPermille > 0:
		value.Direction = directionIncreasing
	default:
		value.Direction = directionDecreasing
	}
	return value
}

func windowMetric(metric string, value WindowAnalytics) int {
	switch metric {
	case "alignment":
		return value.AlignmentMeanPermille
	case "divergence":
		return value.DivergenceMeanPermille
	case "coverage":
		return value.CoverageMeanPermille
	case "comparable_rate":
		return value.ComparableRatePermille
	case "significant_divergence_rate":
		return value.SignificantDivergenceRatePermille
	default:
		return 0
	}
}

func buildTrend(windows []WindowAnalytics, policy AnalyticsPolicy) TrendAnalytics {
	value := TrendAnalytics{Alignment: trendMetric("alignment", windows, policy), Divergence: trendMetric("divergence", windows, policy), Coverage: trendMetric("coverage", windows, policy), ComparableRate: trendMetric("comparable_rate", windows, policy), SignificantDivergenceRate: trendMetric("significant_divergence_rate", windows, policy), WindowCount: len(windows), Sufficient: len(windows) >= policy.MinimumWindowsForTrend}
	value.Fingerprint = trendFingerprint(value)
	return value
}

func trendFingerprint(value TrendAnalytics) string {
	value.Fingerprint = ""
	return digest("calibration-trend-v1:", value)
}
