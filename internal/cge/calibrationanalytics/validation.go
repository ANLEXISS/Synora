package calibrationanalytics

func allMarkersTrue(m AnalyticsMarkers) bool {
	return m.DescriptiveOnly && m.NotModelAccuracy && m.NotGroundTruth && m.NotAutomaticCalibration && m.NotAProductionDecision && m.NotARecommendation && m.NotAuthorization && m.NotACommand && m.NotAnAction && m.NotAnAlert && m.DoesNotChangeThresholds && m.DoesNotChangeWeights && m.DoesNotSelectPolicy && m.DoesNotDeployPolicy && m.HistoricalProductionAuthorityUnchanged && m.NoSecurityMeaning
}

func allEvaluationMarkersTrue(m PolicyEvaluationMarkers) bool {
	return m.ComparativeOnly && m.NoWinnerSelected && m.NoPolicyRecommended && m.NoPolicyActivated && m.NoProductionFeedback && m.NoAutomaticCalibration
}

func validMetric(value string) bool {
	switch value {
	case "alignment", "divergence", "coverage", "comparable_rate", "significant_divergence_rate":
		return true
	default:
		return false
	}
}

func validDirection(value string) bool {
	switch value {
	case directionIncreasing, directionDecreasing, directionStable, directionInsufficient:
		return true
	default:
		return false
	}
}

func (r CalibrationAnalyticsReport) Validate(policy AnalyticsPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if r.SchemaVersion != SchemaVersion {
		return ErrUnsupportedSchema
	}
	if r.AnalyticsPolicyFingerprint != policy.Fingerprint() {
		return ErrInvalidReport
	}
	var categoryRecords, cohortRecords uint64
	if !allMarkersTrue(r.Markers) {
		return ErrInvalidAnalyticsMarkers
	}
	if !allEvaluationMarkersTrue(r.PolicyEvaluation.Markers) {
		return ErrInvalidPolicyEvaluationMarkers
	}
	if r.ReportFingerprint == "" || r.DataSufficiency.Fingerprint == "" || r.Global.Fingerprint == "" || r.Trend.Fingerprint == "" || r.Drift.Fingerprint == "" || r.PolicyEvaluation.Fingerprint == "" {
		return ErrInvalidReport
	}
	if r.DataSufficiency.Fingerprint != sufficiencyFingerprint(r.DataSufficiency) || r.Global.Fingerprint != globalFingerprint(r.Global) || r.Trend.Fingerprint != trendFingerprint(r.Trend) || r.Drift.Fingerprint != driftFingerprint(r.Drift) || r.PolicyEvaluation.Fingerprint != policyEvaluationFingerprint(r.PolicyEvaluation) {
		return ErrInvalidReport
	}
	for _, value := range r.Categories {
		if value.Fingerprint != categoryFingerprint(value) {
			return ErrInvalidReport
		}
	}
	for _, value := range r.PolicyCohorts {
		if value.CohortFingerprint != cohortFingerprint(value) || !validPermille(value.ComparableRatePermille) || !validPermille(value.SignificantDivergenceRatePermille) || !validPermille(value.AlignmentMeanPermille) || !validPermille(value.DivergenceMeanPermille) || !validPermille(value.CoverageMeanPermille) || !validPermille(value.AlignmentP95Permille) || !validPermille(value.DivergenceP95Permille) || !validPermille(value.CoverageP95Permille) {
			return ErrInvalidReport
		}
		if value.ComparableCount > value.RecordCount {
			return ErrInvalidReport
		}
		cohortRecords += value.RecordCount
	}
	for _, value := range r.Windows {
		if value.WindowFingerprint != windowFingerprint(value) {
			return ErrInvalidReport
		}
	}
	for _, value := range r.PolicyEvaluation.Comparisons {
		if value.Fingerprint != cohortComparisonFingerprint(value) {
			return ErrInvalidReport
		}
	}
	if r.ReportFingerprint != reportFingerprint(r) {
		return ErrReportFingerprintMismatch
	}
	if r.FirstSequence != 0 && r.FirstSequence > r.LastSequence || r.GeneratedFromSequence != 0 && r.LastSequence > r.GeneratedFromSequence || r.RecordCount != r.Global.RecordCount {
		return ErrInvalidReport
	}
	for _, value := range r.Categories {
		if !validCategory(value.Category) || value.Fingerprint == "" || !validPermille(value.ComparableRatePermille) || !validPermille(value.SignificantDivergenceRatePermille) || !validPermille(value.AlignmentMeanPermille) || !validPermille(value.DivergenceMeanPermille) || !validPermille(value.CoverageMeanPermille) || !validPermille(value.AlignmentP95Permille) || !validPermille(value.DivergenceP95Permille) || !validPermille(value.CoverageP95Permille) {
			return ErrInvalidReport
		}
		categoryRecords += value.RecordCount
	}
	if !validGlobal(r.Global) || !validSufficiency(r.DataSufficiency) {
		return ErrInvalidReport
	}
	if r.Global.ComparableCount > r.Global.RecordCount || r.Global.SignificantDivergenceCount > r.Global.RecordCount || r.Global.IncomparableCount > r.Global.RecordCount || r.DataSufficiency.ComparableRecordCount != r.Global.ComparableCount || r.PolicyEvaluation.CohortCount != len(r.PolicyCohorts) || r.PolicyEvaluation.EligibleCohortCount > r.PolicyEvaluation.CohortCount {
		return ErrInvalidReport
	}
	trends := []MetricTrend{r.Trend.Alignment, r.Trend.Divergence, r.Trend.Coverage, r.Trend.ComparableRate, r.Trend.SignificantDivergenceRate}
	for _, value := range trends {
		if !validMetric(value.Metric) || !validDirection(value.Direction) || !validPermille(value.FirstValuePermille) || !validPermille(value.LastValuePermille) || value.DeltaPermille < -1000 || value.DeltaPermille > 1000 {
			return ErrInvalidReport
		}
	}
	drifts := []MetricDrift{r.Drift.Alignment, r.Drift.Divergence, r.Drift.Coverage, r.Drift.ComparableRate, r.Drift.SignificantDivergenceRate}
	for _, value := range drifts {
		if !validMetric(value.Metric) || !validPermille(value.BaselineMeanPermille) || !validPermille(value.RecentMeanPermille) || !validPermille(value.BaselineP95Permille) || !validPermille(value.RecentP95Permille) || value.MeanDeltaPermille < -1000 || value.MeanDeltaPermille > 1000 || value.P95DeltaPermille < -1000 || value.P95DeltaPermille > 1000 {
			return ErrInvalidReport
		}
	}
	for i, value := range r.Windows {
		if value.Index != i || value.RecordCount == 0 || value.FirstSequence == 0 || value.LastSequence < value.FirstSequence || !validPermille(value.ComparableRatePermille) || !validPermille(value.SignificantDivergenceRatePermille) || !validPermille(value.AlignmentMeanPermille) || !validPermille(value.DivergenceMeanPermille) || !validPermille(value.CoverageMeanPermille) || !validPermille(value.AlignmentP95Permille) || !validPermille(value.DivergenceP95Permille) || !validPermille(value.CoverageP95Permille) {
			return ErrInvalidReport
		}
		if i > 0 && r.Windows[i-1].LastSequence >= value.FirstSequence {
			return ErrInvalidReport
		}
	}
	for i := 1; i < len(r.Categories); i++ {
		if r.Categories[i-1].Category >= r.Categories[i].Category {
			return ErrInvalidReport
		}
	}
	for i := 1; i < len(r.PolicyCohorts); i++ {
		if !cohortKeyLess(r.PolicyCohorts[i-1].Key, r.PolicyCohorts[i].Key) {
			return ErrInvalidReport
		}
	}
	if categoryRecords != r.RecordCount || cohortRecords != r.RecordCount {
		return ErrInvalidReport
	}
	return nil
}

func validPermille(value int) bool { return value >= 0 && value <= 1000 }

func validGlobal(value GlobalAnalytics) bool {
	return validPermille(value.ComparableRatePermille) && validPermille(value.SignificantDivergenceRatePermille) && validPermille(value.AlignmentMeanPermille) && validPermille(value.DivergenceMeanPermille) && validPermille(value.CoverageMeanPermille) && validPermille(value.AlignmentP50Permille) && validPermille(value.AlignmentP95Permille) && validPermille(value.AlignmentP99Permille) && validPermille(value.DivergenceP50Permille) && validPermille(value.DivergenceP95Permille) && validPermille(value.DivergenceP99Permille) && validPermille(value.CoverageP50Permille) && validPermille(value.CoverageP95Permille) && validPermille(value.CoverageP99Permille)
}

func validSufficiency(value DataSufficiency) bool {
	return value.RecordCount >= value.ComparableRecordCount && value.MissingWindows >= 0 && value.WindowCount >= 0 && value.EligibleCohortCount >= 0
}
