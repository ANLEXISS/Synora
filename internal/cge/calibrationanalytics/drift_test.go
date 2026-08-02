package calibrationanalytics

import "testing"

func TestDriftUsesDeterministicBaselineAndRecentHalves(t *testing.T) {
	snapshot, records := analyticsFixture(t, 200, 500)
	policy := DefaultAnalyticsPolicy()
	policy.MinimumRecords = 1
	policy.MinimumComparableRecords = 1
	policy.MinimumRecordsPerCohort = 1
	policy.MinimumWindowsForTrend = 2
	policy.WindowSizeRecords = 10
	policy.DriftMinimumSampleSize = 10
	policy.DriftMeanDeltaPermille = 50
	policy.DriftP95DeltaPermille = 50
	report, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Drift.Sufficient || !report.Drift.AnyDriftDetected || !report.Drift.Alignment.Detected {
		t.Fatalf("drift=%+v", report.Drift)
	}
}
