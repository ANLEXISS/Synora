package calibrationanalytics

import "testing"

func TestAnalyticsOutputsStayBounded(t *testing.T) {
	snapshot, records := analyticsFixture(t, 40, 350)
	policy := DefaultAnalyticsPolicy()
	policy.MinimumRecords = 1
	policy.MinimumComparableRecords = 1
	policy.MinimumRecordsPerCohort = 1
	policy.MinimumWindowsForTrend = 2
	policy.WindowSizeRecords = 4
	policy.DriftMinimumSampleSize = 1
	policy.MaximumWindows = 3
	report, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Windows) > policy.MaximumWindows || len(report.Categories) > policy.MaximumCategories || len(report.PolicyCohorts) > policy.MaximumCohorts {
		t.Fatalf("unbounded report windows=%d categories=%d cohorts=%d", len(report.Windows), len(report.Categories), len(report.PolicyCohorts))
	}
}
