package calibrationanalytics

import "testing"

func TestPolicyCohortsAreBoundedAndComparativeOnly(t *testing.T) {
	snapshot, records := analyticsFixture(t, 12, 500)
	policy := DefaultAnalyticsPolicy()
	policy.MinimumRecords = 1
	policy.MinimumComparableRecords = 1
	policy.MinimumRecordsPerCohort = 1
	policy.MinimumWindowsForTrend = 2
	policy.WindowSizeRecords = 3
	policy.DriftMinimumSampleSize = 1
	report, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.PolicyCohorts) != 2 || len(report.PolicyEvaluation.Comparisons) != 1 || report.PolicyEvaluation.ReferenceCohortFingerprint == "" {
		t.Fatalf("cohorts=%d comparisons=%d evaluation=%+v", len(report.PolicyCohorts), len(report.PolicyEvaluation.Comparisons), report.PolicyEvaluation)
	}
	if !report.PolicyEvaluation.Markers.NoPolicyRecommended || !report.PolicyEvaluation.Markers.NoPolicyActivated {
		t.Fatalf("markers=%+v", report.PolicyEvaluation.Markers)
	}
}
