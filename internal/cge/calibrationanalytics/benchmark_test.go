package calibrationanalytics

import (
	"strconv"
	"testing"
)

func BenchmarkAnalyze(b *testing.B) {
	snapshot, records := analyticsFixture(b, 200, 500)
	policy := DefaultAnalyticsPolicy()
	policy.MinimumRecords = 1
	policy.MinimumComparableRecords = 1
	policy.MinimumRecordsPerCohort = 1
	policy.MinimumWindowsForTrend = 2
	policy.WindowSizeRecords = 10
	policy.DriftMinimumSampleSize = 10
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence}, policy); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAnalyzeSizes(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			snapshot, records := analyticsFixture(b, size, 500)
			policy := DefaultAnalyticsPolicy()
			policy.MinimumRecords = 1
			policy.MinimumComparableRecords = 1
			policy.MinimumRecordsPerCohort = 1
			policy.MinimumWindowsForTrend = 2
			policy.WindowSizeRecords = 100
			policy.DriftMinimumSampleSize = 50
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence}, policy); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
