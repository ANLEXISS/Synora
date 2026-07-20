package situationhypotheses

type HypothesisID string
type HypothesisKind string

const (
	KindPatternConsistent            HypothesisKind = "pattern_consistent"
	KindIsolatedDeviation            HypothesisKind = "isolated_deviation"
	KindPossiblePatternShift         HypothesisKind = "possible_pattern_shift"
	KindIdentityResolutionFailure    HypothesisKind = "identity_resolution_failure"
	KindCoherentUnrecognizedActivity HypothesisKind = "coherent_unrecognized_activity"
	KindContextOrSensorInconsistency HypothesisKind = "context_or_sensor_inconsistency"
	KindMultiEntityActivity          HypothesisKind = "multi_entity_activity"
	KindInsufficientInformation      HypothesisKind = "insufficient_information"
)

const (
	HypothesisPatternConsistent            = KindPatternConsistent
	HypothesisIsolatedDeviation            = KindIsolatedDeviation
	HypothesisPossiblePatternShift         = KindPossiblePatternShift
	HypothesisIdentityResolutionFailure    = KindIdentityResolutionFailure
	HypothesisCoherentUnrecognizedActivity = KindCoherentUnrecognizedActivity
	HypothesisContextOrSensorInconsistency = KindContextOrSensorInconsistency
	HypothesisMultiEntityActivity          = KindMultiEntityActivity
	HypothesisInsufficientInformation      = KindInsufficientInformation
)

func allHypothesisKinds() []HypothesisKind {
	return []HypothesisKind{
		KindPatternConsistent,
		KindIsolatedDeviation,
		KindPossiblePatternShift,
		KindIdentityResolutionFailure,
		KindCoherentUnrecognizedActivity,
		KindContextOrSensorInconsistency,
		KindMultiEntityActivity,
		KindInsufficientInformation,
	}
}

func AllHypothesisKinds() []HypothesisKind {
	return append([]HypothesisKind(nil), allHypothesisKinds()...)
}

type HypothesisStatus string

const (
	StatusCandidate               HypothesisStatus = "candidate"
	StatusSupported               HypothesisStatus = "supported"
	StatusWeakened                HypothesisStatus = "weakened"
	StatusContradicted            HypothesisStatus = "contradicted"
	StatusInsufficientInformation HypothesisStatus = "insufficient_information"
	StatusInvalidated             HypothesisStatus = "invalidated"
)

func validHypothesisStatus(value HypothesisStatus) bool {
	switch value {
	case StatusCandidate, StatusSupported, StatusWeakened, StatusContradicted, StatusInsufficientInformation, StatusInvalidated:
		return true
	default:
		return false
	}
}

type ContributionRole string

const (
	ContributionSupport       ContributionRole = "support"
	ContributionContradiction ContributionRole = "contradiction"
	ContributionNeutral       ContributionRole = "neutral"
)

func validContributionRole(value ContributionRole) bool {
	return value == ContributionSupport || value == ContributionContradiction || value == ContributionNeutral
}

type IncrementalEvaluationMode string

const (
	EvaluationModeFull IncrementalEvaluationMode = "full"
	EvaluationModeDiff IncrementalEvaluationMode = "diff"
)
