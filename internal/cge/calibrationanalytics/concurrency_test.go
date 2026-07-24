package calibrationanalytics

import (
	"sync"
	"testing"
)

func TestAnalyzeConcurrentCallsDoNotShareState(t *testing.T) {
	snapshot, records := analyticsFixture(t, 20, 400)
	policy := DefaultAnalyticsPolicy()
	policy.MinimumRecords = 1
	policy.MinimumComparableRecords = 1
	policy.MinimumRecordsPerCohort = 1
	policy.MinimumWindowsForTrend = 2
	policy.WindowSizeRecords = 5
	policy.DriftMinimumSampleSize = 1
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Analyze(AnalyticsInput{LedgerSnapshot: snapshot, Records: records, GeneratedFromSequence: snapshot.LastSequence}, policy); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
