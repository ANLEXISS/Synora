package decisioncomparison

import (
	"sort"

	"synora/internal/cge/cognitiverecommendation"
	"synora/internal/cge/cognitivesituation"
)

func Compare(input CompareInput, policy Policy) (HistoricalDecisionComparison, error) {
	if err := validateInput(input, policy); err != nil {
		return HistoricalDecisionComparison{}, err
	}
	if input.Historical.CurrentStateCode == "" {
		return finishComparison(HistoricalDecisionComparison{
			ID: "historical-decision-comparison-" + input.Situation.ID, EpisodeID: input.Situation.EpisodeID,
			SituationID: input.Situation.ID, RecommendationSetID: input.Recommendations.ID,
			HistoricalDecision: input.Historical.Clone(), Category: CategoryIncomparable, Status: ComparisonCurrent,
			SourceSituationFingerprint: input.Situation.Fingerprint, SourceRecommendationFingerprint: input.Recommendations.Fingerprint,
			SourceHistoricalFingerprint: input.Historical.Fingerprint, Markers: comparisonMarkers(),
		}, policy), nil
	}
	comparison := HistoricalDecisionComparison{
		ID:        "historical-decision-comparison-" + input.Situation.ID,
		EpisodeID: input.Situation.EpisodeID, SituationID: input.Situation.ID,
		RecommendationSetID: input.Recommendations.ID,
		HistoricalDecision:  input.Historical.Clone(), Status: ComparisonCurrent,
		HistoricalStateChanged:          input.Historical.StateChanged,
		SourceSituationFingerprint:      input.Situation.Fingerprint,
		SourceRecommendationFingerprint: input.Recommendations.Fingerprint,
		SourceHistoricalFingerprint:     input.Historical.Fingerprint,
		Markers:                         comparisonMarkers(),
	}
	if input.Previous != nil {
		comparison.PreviousComparisonID = input.Previous.ID
	}
	if input.Situation.Phase == cognitivesituation.PhaseStale {
		comparison.Category = CategoryStale
		return finishComparison(comparison, policy), nil
	}
	if input.Situation.Phase == cognitivesituation.PhaseInvalidated {
		comparison.Category = CategoryInvalidated
		return finishComparison(comparison, policy), nil
	}

	meaningful := meaningfulRecommendations(input.Recommendations)
	if !meaningful {
		comparison.Category = CategoryIncomparable
		return finishComparison(comparison, policy), nil
	}
	transitionFlag := input.Recommendations.HasCognitiveTransition || hasKind(input.Recommendations, cognitiverecommendation.RecommendationCognitiveTransition)
	comparison.CognitiveTransitionFlagged = transitionFlag
	comparison.Dimensions = buildDimensions(input, transitionFlag, policy)
	comparison.OverallAlignmentPermille, comparison.OverallDivergencePermille, comparison.OverallCoveragePermille = aggregate(comparison.Dimensions, policy)
	comparison.Comparable = comparableCount(comparison.Dimensions) >= 2 && comparison.OverallCoveragePermille >= policy.MinComparableCoveragePermille
	comparison.SignificantDivergence = comparison.Comparable && comparison.OverallDivergencePermille >= policy.MinSignificantDivergencePermille
	comparison.Category = classify(input, comparison)
	comparison.Revision = 1
	if input.Previous != nil {
		comparison.Revision = input.Previous.Revision
		if comparisonFingerprint(comparison) != input.Previous.Fingerprint {
			comparison.Revision++
		}
	}
	return finishComparison(comparison, policy), nil
}

func comparisonMarkers() ComparisonMarkers {
	return ComparisonMarkers{HistoricalDecisionRetainsAuthority: true, CognitiveRecommendationHasNoAuthority: true,
		NotAProductionDecision: true, DoesNotOverrideHistoricalDecision: true, NotAProbability: true,
		NotAnAlert: true, NotAuthorization: true, NotACommand: true, NotAnAction: true,
		NoSecurityMeaning: true, CalibrationOnly: true}
}

func meaningfulRecommendations(set cognitiverecommendation.CognitiveRecommendationSet) bool {
	for _, recommendation := range set.Recommendations {
		if recommendation.Kind != cognitiverecommendation.RecommendationNone {
			return true
		}
	}
	return false
}

func hasKind(set cognitiverecommendation.CognitiveRecommendationSet, kind cognitiverecommendation.RecommendationKind) bool {
	for _, recommendation := range set.Recommendations {
		if recommendation.Kind == kind {
			return true
		}
	}
	return false
}

func buildDimensions(input CompareInput, transitionFlag bool, policy Policy) []ComparisonDimension {
	set := input.Recommendations
	historical := input.Historical
	situation := input.Situation
	out := make([]ComparisonDimension, 0, 9)
	add := func(d ComparisonDimension) {
		if len(out) >= policy.MaxDimensions {
			return
		}
		d.HistoricalCodes = canonicalReasons(d.HistoricalCodes, policy.MaxReasonCodes)
		d.CognitiveCodes = canonicalReasons(d.CognitiveCodes, policy.MaxReasonCodes)
		d.ReasonCodes = canonicalReasons(d.ReasonCodes, policy.MaxReasonCodes)
		d.Fingerprint = dimensionFingerprint(d)
		out = append(out, d)
	}
	stable := !historical.StateChanged
	hasMaintain := hasKind(set, cognitiverecommendation.RecommendationMaintainInterpretation)
	hasObserve := hasKind(set, cognitiverecommendation.RecommendationContinueObservation) || hasKind(set, cognitiverecommendation.RecommendationReassessObservation)
	hasEvidence := hasKind(set, cognitiverecommendation.RecommendationAdditionalEvidence)

	if historical.CurrentStateCode != "" && historical.PreviousStateCode != "" {
		alignment, divergence, status := 500, 500, DimensionPartiallyAligned
		if stable && (hasMaintain || hasObserve) {
			alignment, divergence, status = 900, 100, DimensionAligned
		} else if !stable && transitionFlag {
			alignment, divergence, status = 900, 100, DimensionAligned
		} else if !stable {
			alignment, divergence, status = 200, 800, DimensionDivergent
		}
		add(ComparisonDimension{Kind: DimensionStateContinuity, Status: status, Comparable: true, AlignmentPermille: alignment, DivergencePermille: divergence, CoveragePermille: 1000, HistoricalCodes: []string{historical.PreviousStateCode, historical.CurrentStateCode}, CognitiveCodes: cognitiveKinds(set), ReasonCodes: []string{"state_codes_available"}})
	}
	if historical.StateChanged {
		status, alignment, divergence := DimensionDivergent, 200, 800
		if transitionFlag {
			status, alignment, divergence = DimensionAligned, 1000, 0
		}
		add(ComparisonDimension{Kind: DimensionStateTransition, Status: status, Comparable: true, AlignmentPermille: alignment, DivergencePermille: divergence, CoveragePermille: 1000, HistoricalCodes: []string{"historical_state_changed"}, CognitiveCodes: transitionCodes(transitionFlag), ReasonCodes: []string{"historical_transition_observed"}})
	} else if transitionFlag {
		add(ComparisonDimension{Kind: DimensionStateTransition, Status: DimensionDivergent, Comparable: true, AlignmentPermille: 250, DivergencePermille: 750, CoveragePermille: 1000, HistoricalCodes: []string{"historical_state_stable"}, CognitiveCodes: []string{"cognitive_transition"}, ReasonCodes: []string{"cognitive_transition_without_historical_transition"}})
	}
	if transitionFlag {
		status, alignment, divergence := DimensionDivergent, 250, 750
		if historical.StateChanged {
			status, alignment, divergence = DimensionAligned, 1000, 0
		}
		add(ComparisonDimension{Kind: DimensionCognitiveTransition, Status: status, Comparable: true, AlignmentPermille: alignment, DivergencePermille: divergence, CoveragePermille: 1000, HistoricalCodes: transitionCodes(historical.StateChanged), CognitiveCodes: []string{"cognitive_transition"}, ReasonCodes: []string{"cognitive_transition_posture_available"}})
	}
	if hasMaintain {
		status, alignment, divergence := DimensionAligned, 900, 100
		if !stable {
			status, alignment, divergence = DimensionPartiallyAligned, 450, 550
		}
		add(ComparisonDimension{Kind: DimensionInterpretationStability, Status: status, Comparable: true, AlignmentPermille: alignment, DivergencePermille: divergence, CoveragePermille: 900, CognitiveCodes: []string{"maintain_current_interpretation"}, ReasonCodes: []string{"interpretation_posture_available"}})
	}
	if situation.Phase == cognitivesituation.PhaseAmbiguous {
		add(ComparisonDimension{Kind: DimensionAmbiguityPosture, Status: DimensionPartiallyAligned, Comparable: historical.CurrentStateCode != "", AlignmentPermille: 500, DivergencePermille: 500, CoveragePermille: 1000, HistoricalCodes: []string{"historical_state_present"}, CognitiveCodes: []string{"ambiguous"}, ReasonCodes: []string{"ambiguity_preserved"}})
	} else {
		add(ComparisonDimension{Kind: DimensionAmbiguityPosture, Status: DimensionAligned, Comparable: true, AlignmentPermille: 800, DivergencePermille: 200, CoveragePermille: 700, CognitiveCodes: []string{"non_ambiguous"}, ReasonCodes: []string{"ambiguity_not_active"}})
	}
	if hasObserve {
		status, alignment, divergence := DimensionAligned, 800, 200
		if historical.Escalated || historical.StateChanged {
			status, alignment, divergence = DimensionDivergent, 200, 800
		}
		add(ComparisonDimension{Kind: DimensionObservationPosture, Status: status, Comparable: true, AlignmentPermille: alignment, DivergencePermille: divergence, CoveragePermille: 900, HistoricalCodes: []string{historical.CurrentStateCode}, CognitiveCodes: cognitiveKinds(set), ReasonCodes: []string{"observation_posture_available"}})
	}
	if hasEvidence {
		add(ComparisonDimension{Kind: DimensionEvidencePosture, Status: DimensionPartiallyAligned, Comparable: true, AlignmentPermille: 600, DivergencePermille: 400, CoveragePermille: 800, HistoricalCodes: []string{"historical_decision_present"}, CognitiveCodes: []string{"additional_evidence"}, ReasonCodes: []string{"evidence_request_is_advisory"}})
	}
	add(ComparisonDimension{Kind: DimensionFreshness, Status: DimensionAligned, Comparable: true, AlignmentPermille: 1000, DivergencePermille: 0, CoveragePermille: 1000, ReasonCodes: []string{"committed_cognitive_sources"}})
	if historical.DecidedAtUnixNano != 0 {
		add(ComparisonDimension{Kind: DimensionDecisionTiming, Status: DimensionInsufficientInformation, Comparable: false, AlignmentPermille: 0, DivergencePermille: 0, CoveragePermille: 0, ReasonCodes: []string{"cognitive_timing_reference_unavailable"}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

func cognitiveKinds(set cognitiverecommendation.CognitiveRecommendationSet) []string {
	out := make([]string, 0, len(set.Recommendations))
	for _, recommendation := range set.Recommendations {
		out = append(out, string(recommendation.Kind))
	}
	return out
}

func transitionCodes(flag bool) []string {
	if flag {
		return []string{"cognitive_transition"}
	}
	return []string{"no_cognitive_transition"}
}

func aggregate(dimensions []ComparisonDimension, policy Policy) (int, int, int) {
	var weightedAlignment, weightedDivergence, weightedCoverage, totalWeight int
	for _, dimension := range dimensions {
		if !dimension.Comparable || dimension.CoveragePermille <= 0 {
			continue
		}
		weight := dimensionWeight(policy, dimension.Kind)
		totalWeight += weight * dimension.CoveragePermille
		weightedAlignment += weight * dimension.CoveragePermille * dimension.AlignmentPermille
		weightedDivergence += weight * dimension.CoveragePermille * dimension.DivergencePermille
		weightedCoverage += weight * dimension.CoveragePermille * dimension.CoveragePermille
	}
	if totalWeight == 0 {
		return 0, 0, 0
	}
	return clamp(weightedAlignment / totalWeight), clamp(weightedDivergence / totalWeight), clamp(weightedCoverage / totalWeight)
}

func comparableCount(dimensions []ComparisonDimension) int {
	count := 0
	for _, dimension := range dimensions {
		if dimension.Comparable {
			count++
		}
	}
	return count
}

func classify(input CompareInput, comparison HistoricalDecisionComparison) ComparisonCategory {
	if input.Situation.Phase == cognitivesituation.PhaseAmbiguous && input.Historical.CurrentStateCode != "" {
		return CategoryCognitiveMoreConservative
	}
	if input.Historical.StateChanged && comparison.CognitiveTransitionFlagged {
		return CategoryAligned
	}
	if input.Historical.StateChanged && !comparison.CognitiveTransitionFlagged {
		return CategoryHistoricalTransitionOnly
	}
	if !input.Historical.StateChanged && comparison.CognitiveTransitionFlagged {
		return CategoryCognitiveTransitionOnly
	}
	if hasKind(input.Recommendations, cognitiverecommendation.RecommendationAdditionalEvidence) {
		return CategoryCognitiveMoreConservative
	}
	if hasKind(input.Recommendations, cognitiverecommendation.RecommendationContinueObservation) && input.Historical.Escalated {
		return CategoryHistoricalMoreDecisive
	}
	if comparison.SignificantDivergence {
		return CategoryDivergent
	}
	if comparison.OverallAlignmentPermille >= 700 {
		return CategoryAligned
	}
	return CategoryPartiallyAligned
}

func finishComparison(comparison HistoricalDecisionComparison, policy Policy) HistoricalDecisionComparison {
	comparison.Dimensions = append([]ComparisonDimension(nil), comparison.Dimensions...)
	sort.Slice(comparison.Dimensions, func(i, j int) bool { return comparison.Dimensions[i].Kind < comparison.Dimensions[j].Kind })
	if comparison.Revision == 0 {
		comparison.Revision = 1
	}
	comparison.Fingerprint = comparisonFingerprint(comparison)
	if comparison.PreviousComparisonID != "" && comparison.Status == "" {
		comparison.Status = ComparisonCurrent
	}
	_ = policy
	return comparison
}

func clamp(value int) int {
	if value < 0 {
		return 0
	}
	if value > 1000 {
		return 1000
	}
	return value
}
