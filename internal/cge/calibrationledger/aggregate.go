package calibrationledger

type AggregateSnapshot struct {
	TotalRecords              uint64 `json:"total_records"`
	AlignmentMeanPermille     int    `json:"alignment_mean_permille"`
	DivergenceMeanPermille    int    `json:"divergence_mean_permille"`
	CoverageMeanPermille      int    `json:"coverage_mean_permille"`
	AlignmentP50Permille      int    `json:"alignment_p50_permille"`
	AlignmentP95Permille      int    `json:"alignment_p95_permille"`
	AlignmentP99Permille      int    `json:"alignment_p99_permille"`
	DivergenceP50Permille     int    `json:"divergence_p50_permille"`
	DivergenceP95Permille     int    `json:"divergence_p95_permille"`
	DivergenceP99Permille     int    `json:"divergence_p99_permille"`
	CoverageP50Permille       int    `json:"coverage_p50_permille"`
	CoverageP95Permille       int    `json:"coverage_p95_permille"`
	CoverageP99Permille       int    `json:"coverage_p99_permille"`
	HistoricalTransitions     uint64 `json:"historical_transitions"`
	CognitiveTransitions      uint64 `json:"cognitive_transitions"`
	AlignedTransitions        uint64 `json:"aligned_transitions"`
	CognitiveMoreConservative uint64 `json:"cognitive_more_conservative"`
	HistoricalMoreDecisive    uint64 `json:"historical_more_decisive"`
}

type aggregateState struct {
	alignment, divergence, coverage                        PermilleHistogram
	historical, cognitive, aligned, conservative, decisive uint64
}

// AggregateRecords applies the ledger's canonical descriptive aggregation to
// a caller-owned record subset. It never mutates the input and does not retain
// references to it.
func AggregateRecords(records []CalibrationRecord) AggregateSnapshot {
	var state aggregateState
	for _, record := range records {
		state.add(record)
	}
	return state.snapshot()
}

func (a *aggregateState) add(r CalibrationRecord) {
	a.alignment.Add(r.AlignmentPermille)
	a.divergence.Add(r.DivergencePermille)
	a.coverage.Add(r.CoveragePermille)
	if r.HistoricalStateChanged {
		a.historical++
	}
	if r.CognitiveTransitionFound {
		a.cognitive++
	}
	if r.HistoricalStateChanged && r.CognitiveTransitionFound {
		a.aligned++
	}
	if r.CognitiveMoreConservative {
		a.conservative++
	}
	if r.HistoricalMoreDecisive {
		a.decisive++
	}
}
func (a aggregateState) snapshot() AggregateSnapshot {
	return AggregateSnapshot{TotalRecords: a.alignment.count, AlignmentMeanPermille: a.alignment.Mean(), DivergenceMeanPermille: a.divergence.Mean(), CoverageMeanPermille: a.coverage.Mean(), AlignmentP50Permille: a.alignment.Percentile(500), AlignmentP95Permille: a.alignment.Percentile(950), AlignmentP99Permille: a.alignment.Percentile(990), DivergenceP50Permille: a.divergence.Percentile(500), DivergenceP95Permille: a.divergence.Percentile(950), DivergenceP99Permille: a.divergence.Percentile(990), CoverageP50Permille: a.coverage.Percentile(500), CoverageP95Permille: a.coverage.Percentile(950), CoverageP99Permille: a.coverage.Percentile(990), HistoricalTransitions: a.historical, CognitiveTransitions: a.cognitive, AlignedTransitions: a.aligned, CognitiveMoreConservative: a.conservative, HistoricalMoreDecisive: a.decisive}
}
