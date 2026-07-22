package calibrationanalytics

import (
	"synora/internal/cge/calibrationledger"
)

func windowAnalytics(records []calibrationledger.CalibrationRecord, index int, policy AnalyticsPolicy) WindowAnalytics {
	s := statsFor(records)
	a := s.aggregate
	value := WindowAnalytics{Index: index, RecordCount: a.TotalRecords, ComparableRatePermille: ratePermille(s.comparable, a.TotalRecords), SignificantDivergenceRatePermille: ratePermille(s.significant, a.TotalRecords), AlignmentMeanPermille: a.AlignmentMeanPermille, DivergenceMeanPermille: a.DivergenceMeanPermille, CoverageMeanPermille: a.CoverageMeanPermille, AlignmentP95Permille: a.AlignmentP95Permille, DivergenceP95Permille: a.DivergenceP95Permille, CoverageP95Permille: a.CoverageP95Permille}
	if len(records) > 0 {
		value.FirstSequence = records[0].Sequence
		value.LastSequence = records[len(records)-1].Sequence
	}
	value.WindowFingerprint = windowFingerprint(value)
	return value
}

func buildWindows(records []calibrationledger.CalibrationRecord, policy AnalyticsPolicy) ([]WindowAnalytics, [][]calibrationledger.CalibrationRecord, error) {
	if len(records) == 0 {
		return []WindowAnalytics{}, [][]calibrationledger.CalibrationRecord{}, nil
	}
	size := int(policy.WindowSizeRecords)
	all := make([][]calibrationledger.CalibrationRecord, 0, (len(records)+size-1)/size)
	for start := 0; start < len(records); start += size {
		end := start + size
		if end > len(records) {
			end = len(records)
		}
		chunk := make([]calibrationledger.CalibrationRecord, end-start)
		copy(chunk, records[start:end])
		all = append(all, chunk)
	}
	if len(all) > policy.MaximumWindows {
		all = all[len(all)-policy.MaximumWindows:]
	}
	windows := make([]WindowAnalytics, len(all))
	for i, chunk := range all {
		windows[i] = windowAnalytics(chunk, i, policy)
	}
	return windows, all, nil
}

func windowFingerprint(value WindowAnalytics) string {
	value.WindowFingerprint = ""
	return digest("calibration-window-analytics-v1:", value)
}
