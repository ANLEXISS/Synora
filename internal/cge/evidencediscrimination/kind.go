package evidencediscrimination

import (
	"strings"

	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

type EvidenceCandidateID string
type EvidenceDimension string
type EvidenceCandidateKind string

const (
	KindIdentityConfirmation            EvidenceCandidateKind = "identity_confirmation"
	KindIdentityContinuityConfirmation  EvidenceCandidateKind = "identity_continuity_confirmation"
	KindSpatialContinuityConfirmation   EvidenceCandidateKind = "spatial_continuity_confirmation"
	KindContextConfirmation             EvidenceCandidateKind = "context_confirmation"
	KindSourceConsistencyConfirmation   EvidenceCandidateKind = "source_consistency_confirmation"
	KindTemporalRepetitionConfirmation  EvidenceCandidateKind = "temporal_repetition_confirmation"
	KindPatternAlignmentConfirmation    EvidenceCandidateKind = "pattern_alignment_confirmation"
	KindEntityCountConfirmation         EvidenceCandidateKind = "entity_count_confirmation"
	KindContextCompletenessConfirmation EvidenceCandidateKind = "context_completeness_confirmation"
)

const (
	DimensionIdentity                EvidenceDimension = "identity"
	DimensionIdentityContinuity      EvidenceDimension = "identity_continuity"
	DimensionSpatialContinuity       EvidenceDimension = "spatial_continuity"
	DimensionDomesticContext         EvidenceDimension = "domestic_context"
	DimensionSourceConsistency       EvidenceDimension = "source_consistency"
	DimensionTemporalRepetition      EvidenceDimension = "temporal_repetition"
	DimensionPatternAlignment        EvidenceDimension = "pattern_alignment"
	DimensionEntityMultiplicity      EvidenceDimension = "entity_multiplicity"
	DimensionInformationCompleteness EvidenceDimension = "information_completeness"
)

type EvidenceCostClass string
type EvidenceLatencyClass string
type EvidenceSensitivityClass string

const (
	CostLow     EvidenceCostClass = "low"
	CostMedium  EvidenceCostClass = "medium"
	CostHigh    EvidenceCostClass = "high"
	CostUnknown EvidenceCostClass = "unknown"

	LatencyImmediate EvidenceLatencyClass = "immediate"
	LatencyShort     EvidenceLatencyClass = "short"
	LatencyExtended  EvidenceLatencyClass = "extended"
	LatencyUnknown   EvidenceLatencyClass = "unknown"

	SensitivityLow      EvidenceSensitivityClass = "low"
	SensitivityModerate EvidenceSensitivityClass = "moderate"
	SensitivityHigh     EvidenceSensitivityClass = "high"
	SensitivityUnknown  EvidenceSensitivityClass = "unknown"
)

type OutcomeOperator string

const (
	OutcomeFactPresent      OutcomeOperator = "fact_present"
	OutcomeFactAbsent       OutcomeOperator = "fact_absent"
	OutcomeValueEquals      OutcomeOperator = "value_equals"
	OutcomeValueNotEquals   OutcomeOperator = "value_not_equals"
	OutcomeValueGreaterThan OutcomeOperator = "value_greater_than"
	OutcomeValueLessThan    OutcomeOperator = "value_less_than"
	OutcomeConflictPresent  OutcomeOperator = "conflict_present"
	OutcomeConflictAbsent   OutcomeOperator = "conflict_absent"
)

type HypothesisPair struct {
	First  situationhypotheses.HypothesisID
	Second situationhypotheses.HypothesisID
}

func canonicalPair(first, second situationhypotheses.HypothesisID) (HypothesisPair, bool) {
	if first == "" || second == "" || first == second {
		return HypothesisPair{}, false
	}
	if second < first {
		first, second = second, first
	}
	return HypothesisPair{First: first, Second: second}, true
}

func validCost(value EvidenceCostClass) bool {
	return value == CostLow || value == CostMedium || value == CostHigh || value == CostUnknown
}
func validLatency(value EvidenceLatencyClass) bool {
	return value == LatencyImmediate || value == LatencyShort || value == LatencyExtended || value == LatencyUnknown
}
func validSensitivity(value EvidenceSensitivityClass) bool {
	return value == SensitivityLow || value == SensitivityModerate || value == SensitivityHigh || value == SensitivityUnknown
}

func forbiddenCandidateTerm(value string) bool {
	lowered := strings.ToLower(value)
	for _, term := range []string{"intrusion", "attack", "threat", "danger", "malicious", "suspicious", "criminal", "hostile", "safe", "unsafe", "emergency", "intent", "visitor_expected", "compromise", "burglary", "weapon"} {
		if strings.Contains(lowered, term) {
			return true
		}
	}
	return false
}

func allCandidateKinds() []EvidenceCandidateKind {
	return []EvidenceCandidateKind{KindIdentityConfirmation, KindIdentityContinuityConfirmation, KindSpatialContinuityConfirmation, KindContextConfirmation, KindSourceConsistencyConfirmation, KindTemporalRepetitionConfirmation, KindPatternAlignmentConfirmation, KindEntityCountConfirmation, KindContextCompletenessConfirmation}
}

func AllCandidateKinds() []EvidenceCandidateKind {
	return append([]EvidenceCandidateKind(nil), allCandidateKinds()...)
}

func validFactCode(code situationfacts.FactCode) bool {
	_, ok := situationfacts.Schema().Definition(code)
	return ok
}
