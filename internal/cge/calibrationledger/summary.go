package calibrationledger

func makeSummary(s Snapshot) CalibrationSummary {
	total := s.RecordCount
	rate := func(n uint64) int {
		if total == 0 {
			return 0
		}
		return int((n*1000 + total/2) / total)
	}
	return CalibrationSummary{LedgerFingerprint: summaryFingerprint(s), RecordCount: total, ComparableRatePermille: rate(s.ComparableCount), SignificantDivergenceRatePermille: rate(s.SignificantDivergenceCount), AlignmentMeanPermille: s.Aggregate.AlignmentMeanPermille, DivergenceMeanPermille: s.Aggregate.DivergenceMeanPermille, CoverageMeanPermille: s.Aggregate.CoverageMeanPermille, HistoricalTransitionOnlyCount: s.CategoryCounts["historical_transition_only"], CognitiveTransitionOnlyCount: s.CategoryCounts["cognitive_transition_only"], CognitiveMoreConservativeCount: s.Aggregate.CognitiveMoreConservative, HistoricalMoreDecisiveCount: s.Aggregate.HistoricalMoreDecisive, Markers: CalibrationSummaryMarkers{DescriptiveOnly: true, NotModelAccuracy: true, NotAutomaticCalibration: true, NotAProductionDecision: true, NotAnAlert: true, NoSecurityMeaning: true}}
}

// SummaryFromSnapshot derives the same descriptive summary without exposing
// the store implementation to callers using only the Store interface.
func SummaryFromSnapshot(s Snapshot) CalibrationSummary { return makeSummary(cloneSnapshot(s)) }
