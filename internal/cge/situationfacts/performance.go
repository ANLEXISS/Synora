package situationfacts

type PerformanceReport struct {
	ExtractionMode string

	ReusedFactCount     int
	RecomputedFactCount int

	FullFallback   bool
	FallbackReason string

	FactCount     int
	ConflictCount int
}

func BuildPerformanceReport(result IncrementalExtractionResult) PerformanceReport {
	return PerformanceReport{
		ExtractionMode:      string(result.Mode),
		ReusedFactCount:     result.ReusedFactCount,
		RecomputedFactCount: result.RecomputedFactCount,
		FullFallback:        result.Mode == IncrementalModeFullFallback,
		FallbackReason:      result.FallbackReason,
		FactCount:           len(result.FactSet.Facts),
		ConflictCount:       len(result.FactSet.Conflicts),
	}
}
