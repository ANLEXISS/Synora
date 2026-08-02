package decisioncomparison

func Explain(value HistoricalDecisionComparison, policy Policy) (HistoricalDecisionComparisonExplanation, error) {
	if err := value.Validate(policy); err != nil {
		return HistoricalDecisionComparisonExplanation{}, ErrInvalidExplanation
	}
	out := HistoricalDecisionComparisonExplanation{
		ComparisonID: value.ID, EpisodeID: value.EpisodeID, Category: value.Category,
		Comparable: value.Comparable, SignificantDivergence: value.SignificantDivergence,
		OverallAlignmentPermille: value.OverallAlignmentPermille, OverallDivergencePermille: value.OverallDivergencePermille,
		OverallCoveragePermille:            value.OverallCoveragePermille,
		SummaryCode:                        "historical_decision_comparison." + string(value.Category),
		HistoricalDecisionRetainsAuthority: true, CognitiveRecommendationHasNoAuthority: true,
		DoesNotOverrideHistoricalDecision: true, CalibrationOnly: true, NotAProbability: true,
		NotAnAlert: true, NotACommand: true, NotAnAction: true, NoSecurityMeaning: true,
	}
	for _, dimension := range value.Dimensions {
		out.DimensionSummaries = append(out.DimensionSummaries, ComparisonDimensionSummary{Kind: dimension.Kind, Status: dimension.Status, Comparable: dimension.Comparable, AlignmentPermille: dimension.AlignmentPermille, DivergencePermille: dimension.DivergencePermille, CoveragePermille: dimension.CoveragePermille, ReasonCodes: append([]string(nil), dimension.ReasonCodes...)})
		out.ReasonCodes = append(out.ReasonCodes, dimension.ReasonCodes...)
	}
	out.ReasonCodes = canonicalReasons(out.ReasonCodes, policy.MaxReasonCodes)
	return out, nil
}
