package calibrationanalytics

import (
	"sort"

	"synora/internal/cge/calibrationledger"
)

type cohortBuild struct {
	analytics PolicyCohortAnalytics
	records   []calibrationledger.CalibrationRecord
	first     uint64
}

func buildCohorts(records []calibrationledger.CalibrationRecord, policy AnalyticsPolicy) ([]PolicyCohortAnalytics, []cohortBuild, error) {
	groups := map[PolicyCohortKey][]calibrationledger.CalibrationRecord{}
	for _, record := range records {
		key := PolicyCohortKey{SituationPolicyFingerprint: record.SituationPolicyFingerprint, RecommendationPolicyFingerprint: record.RecommendationPolicyFingerprint, ComparisonPolicyFingerprint: record.ComparisonPolicyFingerprint}
		groups[key] = append(groups[key], record)
	}
	if len(groups) > policy.MaximumCohorts {
		return nil, nil, ErrTooManyCohorts
	}
	keys := make([]PolicyCohortKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return cohortKeyLess(keys[i], keys[j]) })
	result := make([]PolicyCohortAnalytics, 0, len(keys))
	builds := make([]cohortBuild, 0, len(keys))
	for _, key := range keys {
		values := groups[key]
		stats := statsFor(values)
		a := stats.aggregate
		value := PolicyCohortAnalytics{Key: key, RecordCount: a.TotalRecords, ComparableCount: stats.comparable, ComparableRatePermille: ratePermille(stats.comparable, a.TotalRecords), SignificantDivergenceRatePermille: ratePermille(stats.significant, a.TotalRecords), AlignmentMeanPermille: a.AlignmentMeanPermille, DivergenceMeanPermille: a.DivergenceMeanPermille, CoverageMeanPermille: a.CoverageMeanPermille, AlignmentP95Permille: a.AlignmentP95Permille, DivergenceP95Permille: a.DivergenceP95Permille, CoverageP95Permille: a.CoverageP95Permille, HistoricalTransitions: a.HistoricalTransitions, CognitiveTransitions: a.CognitiveTransitions, CognitiveMoreConservative: a.CognitiveMoreConservative, HistoricalMoreDecisive: a.HistoricalMoreDecisive, Sufficient: a.TotalRecords >= policy.MinimumRecordsPerCohort}
		value.CohortFingerprint = cohortFingerprint(value)
		first := uint64(0)
		if len(values) > 0 {
			first = values[0].Sequence
		}
		result = append(result, value)
		builds = append(builds, cohortBuild{analytics: value, records: append([]calibrationledger.CalibrationRecord(nil), values...), first: first})
	}
	return result, builds, nil
}

func cohortKeyLess(a, b PolicyCohortKey) bool {
	if a.SituationPolicyFingerprint != b.SituationPolicyFingerprint {
		return a.SituationPolicyFingerprint < b.SituationPolicyFingerprint
	}
	if a.RecommendationPolicyFingerprint != b.RecommendationPolicyFingerprint {
		return a.RecommendationPolicyFingerprint < b.RecommendationPolicyFingerprint
	}
	return a.ComparisonPolicyFingerprint < b.ComparisonPolicyFingerprint
}

func cohortFingerprint(value PolicyCohortAnalytics) string {
	value.CohortFingerprint = ""
	return digest("calibration-policy-cohort-v1:", value)
}
