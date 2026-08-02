package calibrationanalytics

func buildSufficiency(global GlobalAnalytics, windowCount, eligibleCohortCount int, policy AnalyticsPolicy) DataSufficiency {
	missingRecords := uint64(0)
	if global.RecordCount < policy.MinimumRecords {
		missingRecords = policy.MinimumRecords - global.RecordCount
	}
	missingComparable := uint64(0)
	if global.ComparableCount < policy.MinimumComparableRecords {
		missingComparable = policy.MinimumComparableRecords - global.ComparableCount
	}
	missingWindows := 0
	if windowCount < policy.MinimumWindowsForTrend {
		missingWindows = policy.MinimumWindowsForTrend - windowCount
	}
	limitations := make([]string, 0, 4)
	if missingRecords > 0 {
		limitations = append(limitations, "minimum_records_not_reached")
	}
	if missingComparable > 0 {
		limitations = append(limitations, "minimum_comparable_records_not_reached")
	}
	if missingWindows > 0 {
		limitations = append(limitations, "minimum_windows_not_reached")
	}
	if eligibleCohortCount < 2 {
		limitations = append(limitations, "minimum_policy_cohorts_not_reached")
	}
	value := DataSufficiency{SufficientForGlobalAnalysis: missingRecords == 0 && missingComparable == 0, SufficientForTrendAnalysis: missingWindows == 0, SufficientForDriftAnalysis: missingWindows == 0, SufficientForPolicyComparison: eligibleCohortCount >= 2, RecordCount: global.RecordCount, ComparableRecordCount: global.ComparableCount, WindowCount: windowCount, EligibleCohortCount: eligibleCohortCount, MissingRecords: missingRecords, MissingComparableRecords: missingComparable, MissingWindows: missingWindows, Limitations: limitations}
	value.Fingerprint = sufficiencyFingerprint(value)
	return value
}

func sufficiencyFingerprint(value DataSufficiency) string {
	value.Fingerprint = ""
	return digest("calibration-data-sufficiency-v1:", value)
}
