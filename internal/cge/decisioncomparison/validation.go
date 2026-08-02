package decisioncomparison

import (
	"sort"
	"strings"

	"synora/internal/cge/cognitiverecommendation"
	"synora/internal/cge/cognitivesituation"
)

func (r HistoricalDecisionRef) Validate(policy Policy) error {
	if policy.Validate() != nil || strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.SourceEventRef) == "" ||
		!r.HistoricalDecisionHasProductionAuthority || r.Fingerprint == "" || r.Fingerprint != historicalDecisionFingerprint(r) {
		return ErrInvalidHistoricalDecisionRef
	}
	if r.DecisionScorePermille < 0 || r.DecisionScorePermille > 1000 || r.CoveragePermille < 0 || r.CoveragePermille > 1000 ||
		len(r.ReasonCodes) > policy.MaxReasonCodes {
		return ErrInvalidHistoricalDecisionRef
	}
	return nil
}

func validDimension(kind ComparisonDimensionKind) bool {
	switch kind {
	case DimensionStateContinuity, DimensionStateTransition, DimensionCognitiveTransition,
		DimensionInterpretationStability, DimensionAmbiguityPosture, DimensionObservationPosture,
		DimensionEvidencePosture, DimensionFreshness, DimensionDecisionTiming:
		return true
	default:
		return false
	}
}

func validDimensionStatus(status ComparisonDimensionStatus) bool {
	switch status {
	case DimensionAligned, DimensionPartiallyAligned, DimensionDivergent, DimensionIncomparable,
		DimensionInsufficientInformation, DimensionStale, DimensionInvalidated:
		return true
	default:
		return false
	}
}

func validCategory(category ComparisonCategory) bool {
	switch category {
	case CategoryAligned, CategoryPartiallyAligned, CategoryDivergent, CategoryCognitiveMoreConservative,
		CategoryHistoricalMoreDecisive, CategoryCognitiveTransitionOnly, CategoryHistoricalTransitionOnly,
		CategoryIncomparable, CategoryInsufficientInformation, CategoryStale, CategoryInvalidated:
		return true
	default:
		return false
	}
}

func (d ComparisonDimension) Validate(policy Policy) error {
	if policy.Validate() != nil || !validDimension(d.Kind) || !validDimensionStatus(d.Status) ||
		d.Fingerprint == "" || d.Fingerprint != dimensionFingerprint(d) {
		return ErrInvalidDimension
	}
	if d.AlignmentPermille < 0 || d.AlignmentPermille > 1000 || d.DivergencePermille < 0 || d.DivergencePermille > 1000 ||
		d.CoveragePermille < 0 || d.CoveragePermille > 1000 || len(d.ReasonCodes) > policy.MaxReasonCodes {
		return ErrInvalidDimension
	}
	return nil
}

func (c HistoricalDecisionComparison) Validate(policy Policy) error {
	if policy.Validate() != nil || c.ID == "" || c.EpisodeID == "" || c.SituationID == "" ||
		c.RecommendationSetID == "" || !validCategory(c.Category) || c.Fingerprint == "" ||
		c.Fingerprint != comparisonFingerprint(c) || len(c.Dimensions) > policy.MaxDimensions {
		return ErrInvalidComparison
	}
	if c.OverallAlignmentPermille < 0 || c.OverallAlignmentPermille > 1000 ||
		c.OverallDivergencePermille < 0 || c.OverallDivergencePermille > 1000 ||
		c.OverallCoveragePermille < 0 || c.OverallCoveragePermille > 1000 {
		return ErrInvalidComparison
	}
	if err := c.HistoricalDecision.Validate(policy); err != nil {
		return err
	}
	for _, dimension := range c.Dimensions {
		if err := dimension.Validate(policy); err != nil {
			return err
		}
	}
	if !c.Markers.HistoricalDecisionRetainsAuthority || !c.Markers.CognitiveRecommendationHasNoAuthority ||
		!c.Markers.NotAProductionDecision || !c.Markers.DoesNotOverrideHistoricalDecision ||
		!c.Markers.NotAProbability || !c.Markers.NotAnAlert || !c.Markers.NotAuthorization ||
		!c.Markers.NotACommand || !c.Markers.NotAnAction || !c.Markers.NoSecurityMeaning || !c.Markers.CalibrationOnly {
		return ErrInvalidComparison
	}
	return nil
}

func (s HistoricalDecisionComparisonSnapshot) Validate(policy Policy) error {
	if policy.Validate() != nil || s.Digest == "" || s.Digest != snapshotFingerprint(s) ||
		len(s.Comparisons) != len(s.EpisodeIndex) {
		return ErrInvalidComparison
	}
	for i, comparison := range s.Comparisons {
		if s.EpisodeIndex[comparison.EpisodeID] != i {
			return ErrInvalidComparison
		}
		if err := comparison.Validate(policy); err != nil {
			return err
		}
	}
	return nil
}

func validateInput(input CompareInput, policy Policy) error {
	if policy.Validate() != nil {
		return ErrInvalidPolicy
	}
	if input.Historical.ID == "" {
		return ErrMissingHistoricalDecision
	}
	if err := input.Historical.Validate(policy); err != nil {
		return err
	}
	if err := input.Situation.Validate(cognitivesituation.DefaultPolicy()); err != nil {
		return ErrInvalidSituation
	}
	if err := input.Recommendations.Validate(cognitiverecommendation.DefaultPolicy()); err != nil {
		return ErrInvalidRecommendationSet
	}
	if input.Situation.EpisodeID == "" || input.Recommendations.EpisodeID != input.Situation.EpisodeID ||
		!sourceFingerprintsMatch(input.Situation, input.Recommendations) {
		return ErrSourceFingerprintMismatch
	}
	if input.Previous != nil && input.Previous.EpisodeID != input.Situation.EpisodeID {
		return ErrSourceRevisionConflict
	}
	if input.Previous != nil {
		if err := input.Previous.Validate(policy); err != nil {
			return ErrInvalidComparison
		}
	}
	return nil
}

func canonicalReasons(values []string, max int) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	if len(out) > max {
		out = out[:max]
	}
	return out
}
