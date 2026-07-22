package calibrationanalytics

import "sort"

func buildPolicyEvaluation(cohorts []PolicyCohortAnalytics, builds []cohortBuild, policy AnalyticsPolicy) PolicyEvaluation {
	eligible := make([]cohortBuild, 0, len(builds))
	for _, value := range builds {
		if value.analytics.Sufficient {
			eligible = append(eligible, value)
		}
	}
	value := PolicyEvaluation{CohortCount: len(cohorts), EligibleCohortCount: len(eligible), Markers: policyEvaluationMarkers(), Sufficient: len(eligible) >= 2}
	if len(eligible) > 0 {
		reference := eligible[0]
		for _, candidate := range eligible[1:] {
			if candidate.first < reference.first || candidate.first == reference.first && candidate.analytics.CohortFingerprint < reference.analytics.CohortFingerprint {
				reference = candidate
			}
		}
		value.ReferenceCohortFingerprint = reference.analytics.CohortFingerprint
	}
	for i := 0; i < len(eligible); i++ {
		for j := i + 1; j < len(eligible); j++ {
			left, right := eligible[i].analytics, eligible[j].analytics
			comparison := PolicyCohortComparison{LeftCohortFingerprint: left.CohortFingerprint, RightCohortFingerprint: right.CohortFingerprint, AlignmentMeanDeltaPermille: right.AlignmentMeanPermille - left.AlignmentMeanPermille, DivergenceMeanDeltaPermille: right.DivergenceMeanPermille - left.DivergenceMeanPermille, CoverageMeanDeltaPermille: right.CoverageMeanPermille - left.CoverageMeanPermille, ComparableRateDeltaPermille: right.ComparableRatePermille - left.ComparableRatePermille, SignificantDivergenceRateDeltaPermille: right.SignificantDivergenceRatePermille - left.SignificantDivergenceRatePermille, LeftRecordCount: left.RecordCount, RightRecordCount: right.RecordCount, Sufficient: left.Sufficient && right.Sufficient}
			comparison.Fingerprint = cohortComparisonFingerprint(comparison)
			value.Comparisons = append(value.Comparisons, comparison)
		}
	}
	sort.Slice(value.Comparisons, func(i, j int) bool {
		if value.Comparisons[i].LeftCohortFingerprint != value.Comparisons[j].LeftCohortFingerprint {
			return value.Comparisons[i].LeftCohortFingerprint < value.Comparisons[j].LeftCohortFingerprint
		}
		return value.Comparisons[i].RightCohortFingerprint < value.Comparisons[j].RightCohortFingerprint
	})
	value.Fingerprint = policyEvaluationFingerprint(value)
	return value
}

func cohortComparisonFingerprint(value PolicyCohortComparison) string {
	value.Fingerprint = ""
	return digest("calibration-policy-cohort-comparison-v1:", value)
}

func policyEvaluationFingerprint(value PolicyEvaluation) string {
	value.Fingerprint = ""
	return digest("calibration-policy-evaluation-v1:", value)
}
