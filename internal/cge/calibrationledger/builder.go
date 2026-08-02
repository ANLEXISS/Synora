package calibrationledger

import (
	"crypto/sha256"
	"encoding/hex"

	"synora/internal/cge/decisioncomparison"
)

type BuildRecordInput struct {
	Comparison                      decisioncomparison.HistoricalDecisionComparison
	SituationPolicyFingerprint      string
	RecommendationPolicyFingerprint string
	ComparisonPolicyFingerprint     string
	Previous                        *CalibrationRecord
}

func BuildRecord(input BuildRecordInput, policy Policy) (CalibrationRecord, error) {
	if policy.Validate() != nil {
		return CalibrationRecord{}, ErrInvalidPolicy
	}
	if err := input.Comparison.Validate(decisioncomparison.DefaultPolicy()); err != nil {
		return CalibrationRecord{}, ErrInvalidRecord
	}
	c := input.Comparison
	seed := c.Fingerprint
	h := sha256.Sum256([]byte(seed))
	record := CalibrationRecord{SchemaVersion: RecordSchemaVersion, RecordID: "calibration-record-" + hex.EncodeToString(h[:]), ComparisonFingerprint: c.Fingerprint, Category: string(c.Category), Comparable: c.Comparable, SignificantDivergence: c.SignificantDivergence, AlignmentPermille: c.OverallAlignmentPermille, DivergencePermille: c.OverallDivergencePermille, CoveragePermille: c.OverallCoveragePermille, HistoricalStateChanged: c.HistoricalStateChanged, CognitiveTransitionFound: c.CognitiveTransitionFlagged, HistoricalMoreDecisive: c.Category == decisioncomparison.CategoryHistoricalMoreDecisive, CognitiveMoreConservative: c.Category == decisioncomparison.CategoryCognitiveMoreConservative, HistoricalDecisionRevision: c.HistoricalDecision.Revision, ComparisonRevision: c.Revision, SituationPolicyFingerprint: input.SituationPolicyFingerprint, RecommendationPolicyFingerprint: input.RecommendationPolicyFingerprint, ComparisonPolicyFingerprint: input.ComparisonPolicyFingerprint, SourceDecisionFingerprint: c.SourceHistoricalFingerprint, SourceSituationFingerprint: c.SourceSituationFingerprint, SourceRecommendationFingerprint: c.SourceRecommendationFingerprint, SourceDecidedAtUnixNano: c.HistoricalDecision.DecidedAtUnixNano, Dimensions: make([]CalibrationDimensionSummary, 0, len(c.Dimensions)), Markers: CalibrationRecordMarkers{HistoricalDecisionRetainsAuthority: true, NotAProductionDecision: true, NotARecommendation: true, NotAuthorization: true, NotACommand: true, NotAnAction: true, NotAnAlert: true, DoesNotUpdateThresholds: true, DoesNotUpdateWeights: true, DoesNotTrainAutomatically: true, DoesNotOverrideHistoricalDecision: true, CalibrationOnly: true, NoSecurityMeaning: true}}
	if input.Previous != nil {
		copy := *input.Previous
		if copy.RecordFingerprint == "" {
			return CalibrationRecord{}, ErrInvalidRecord
		}
		record.PreviousRecordFingerprint = copy.RecordFingerprint
	}
	for _, d := range c.Dimensions {
		record.Dimensions = append(record.Dimensions, CalibrationDimensionSummary{Kind: string(d.Kind), Status: string(d.Status), Comparable: d.Comparable, AlignmentPermille: d.AlignmentPermille, DivergencePermille: d.DivergencePermille, CoveragePermille: d.CoveragePermille, Fingerprint: d.Fingerprint})
	}
	record.Dimensions = canonicalDimensions(record.Dimensions)
	record.RecordFingerprint = recordFingerprint(record)
	if err := record.Validate(policy); err != nil {
		return CalibrationRecord{}, err
	}
	return record, nil
}
