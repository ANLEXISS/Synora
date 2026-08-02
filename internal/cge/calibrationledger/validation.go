package calibrationledger

import (
	"sort"
	"strings"

	"synora/internal/cge/decisioncomparison"
)

func allRecordMarkers(m CalibrationRecordMarkers) bool {
	return m.HistoricalDecisionRetainsAuthority && m.NotAProductionDecision && m.NotARecommendation && m.NotAuthorization && m.NotACommand && m.NotAnAction && m.NotAnAlert && m.DoesNotUpdateThresholds && m.DoesNotUpdateWeights && m.DoesNotTrainAutomatically && m.DoesNotOverrideHistoricalDecision && m.CalibrationOnly && m.NoSecurityMeaning
}

func validCategory(v string) bool {
	switch decisioncomparison.ComparisonCategory(v) {
	case decisioncomparison.CategoryAligned, decisioncomparison.CategoryPartiallyAligned, decisioncomparison.CategoryDivergent, decisioncomparison.CategoryCognitiveMoreConservative, decisioncomparison.CategoryHistoricalMoreDecisive, decisioncomparison.CategoryCognitiveTransitionOnly, decisioncomparison.CategoryHistoricalTransitionOnly, decisioncomparison.CategoryIncomparable, decisioncomparison.CategoryInsufficientInformation, decisioncomparison.CategoryStale, decisioncomparison.CategoryInvalidated:
		return true
	}
	return false
}
func validDimensionKind(v string) bool {
	switch decisioncomparison.ComparisonDimensionKind(v) {
	case decisioncomparison.DimensionStateContinuity, decisioncomparison.DimensionStateTransition, decisioncomparison.DimensionCognitiveTransition, decisioncomparison.DimensionInterpretationStability, decisioncomparison.DimensionAmbiguityPosture, decisioncomparison.DimensionObservationPosture, decisioncomparison.DimensionEvidencePosture, decisioncomparison.DimensionFreshness, decisioncomparison.DimensionDecisionTiming:
		return true
	}
	return false
}
func validDimensionStatus(v string) bool {
	switch decisioncomparison.ComparisonDimensionStatus(v) {
	case decisioncomparison.DimensionAligned, decisioncomparison.DimensionPartiallyAligned, decisioncomparison.DimensionDivergent, decisioncomparison.DimensionIncomparable, decisioncomparison.DimensionInsufficientInformation, decisioncomparison.DimensionStale, decisioncomparison.DimensionInvalidated:
		return true
	}
	return false
}

func (r CalibrationRecord) Validate(p Policy) error {
	if p.Validate() != nil || r.SchemaVersion != RecordSchemaVersion || strings.TrimSpace(r.RecordID) == "" || strings.TrimSpace(r.ComparisonFingerprint) == "" || !validCategory(r.Category) || r.RecordFingerprint == "" || len(r.Dimensions) > p.MaxDimensionsPerRecord || r.AlignmentPermille < 0 || r.AlignmentPermille > 1000 || r.DivergencePermille < 0 || r.DivergencePermille > 1000 || r.CoveragePermille < 0 || r.CoveragePermille > 1000 {
		return ErrInvalidRecord
	}
	if !allRecordMarkers(r.Markers) {
		return ErrInvalidMarkers
	}
	if r.RecordFingerprint != recordFingerprint(r) {
		return ErrRecordFingerprintMismatch
	}
	for _, d := range r.Dimensions {
		if !validDimensionKind(d.Kind) || !validDimensionStatus(d.Status) || d.Fingerprint == "" || d.AlignmentPermille < 0 || d.AlignmentPermille > 1000 || d.DivergencePermille < 0 || d.DivergencePermille > 1000 || d.CoveragePermille < 0 || d.CoveragePermille > 1000 {
			return ErrInvalidRecord
		}
	}
	return nil
}

func canonicalDimensions(values []CalibrationDimensionSummary) []CalibrationDimensionSummary {
	out := append([]CalibrationDimensionSummary(nil), values...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Fingerprint < out[j].Fingerprint
	})
	return out
}
