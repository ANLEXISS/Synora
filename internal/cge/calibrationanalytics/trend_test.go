package calibrationanalytics

import "testing"

func TestTrendDirectionsAreClosedAndDescriptive(t *testing.T) {
	snapshot, records := analyticsFixture(t, 12, 400)
	policy := DefaultAnalyticsPolicy()
	policy.MinimumRecords = 1
	policy.MinimumComparableRecords = 1
	policy.MinimumRecordsPerCohort = 1
	policy.MinimumWindowsForTrend = 3
	policy.WindowSizeRecords = 2
	policy.DriftMinimumSampleSize = 1
	report, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Trend.Sufficient || report.Trend.Alignment.Direction != directionDecreasing || report.Trend.Alignment.Metric != "alignment" {
		t.Fatalf("trend=%+v", report.Trend)
	}
}
