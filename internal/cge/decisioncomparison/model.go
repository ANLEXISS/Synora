package decisioncomparison

import (
	"synora/internal/cge/cognitiverecommendation"
	"synora/internal/cge/cognitivesituation"
)

type HistoricalDecisionRef struct {
	ID             string
	SourceEventRef string

	PreviousStateCode string
	CurrentStateCode  string

	StateChanged bool
	Escalated    bool
	Deescalated  bool

	ImmediateOverrideApplied bool
	DecisionLockActive       bool

	DecisionScorePermille int
	CoveragePermille      int

	ReasonCodes []string

	DecidedAtUnixNano int64

	Revision    uint64
	Fingerprint string

	HistoricalDecisionHasProductionAuthority bool
}

type ComparisonDimensionKind string

const (
	DimensionStateContinuity         ComparisonDimensionKind = "state_continuity"
	DimensionStateTransition         ComparisonDimensionKind = "state_transition"
	DimensionCognitiveTransition     ComparisonDimensionKind = "cognitive_transition"
	DimensionInterpretationStability ComparisonDimensionKind = "interpretation_stability"
	DimensionAmbiguityPosture        ComparisonDimensionKind = "ambiguity_posture"
	DimensionObservationPosture      ComparisonDimensionKind = "observation_posture"
	DimensionEvidencePosture         ComparisonDimensionKind = "evidence_posture"
	DimensionFreshness               ComparisonDimensionKind = "freshness"
	DimensionDecisionTiming          ComparisonDimensionKind = "decision_timing"
)

type ComparisonDimensionStatus string

const (
	DimensionAligned                 ComparisonDimensionStatus = "aligned"
	DimensionPartiallyAligned        ComparisonDimensionStatus = "partially_aligned"
	DimensionDivergent               ComparisonDimensionStatus = "divergent"
	DimensionIncomparable            ComparisonDimensionStatus = "incomparable"
	DimensionInsufficientInformation ComparisonDimensionStatus = "insufficient_information"
	DimensionStale                   ComparisonDimensionStatus = "stale"
	DimensionInvalidated             ComparisonDimensionStatus = "invalidated"
)

type ComparisonDimension struct {
	Kind ComparisonDimensionKind

	Status     ComparisonDimensionStatus
	Comparable bool

	AlignmentPermille  int
	DivergencePermille int
	CoveragePermille   int

	HistoricalCodes []string
	CognitiveCodes  []string
	ReasonCodes     []string

	Fingerprint string
}

type ComparisonCategory string

const (
	CategoryAligned                   ComparisonCategory = "aligned"
	CategoryPartiallyAligned          ComparisonCategory = "partially_aligned"
	CategoryDivergent                 ComparisonCategory = "divergent"
	CategoryCognitiveMoreConservative ComparisonCategory = "cognitive_more_conservative"
	CategoryHistoricalMoreDecisive    ComparisonCategory = "historical_more_decisive"
	CategoryCognitiveTransitionOnly   ComparisonCategory = "cognitive_transition_only"
	CategoryHistoricalTransitionOnly  ComparisonCategory = "historical_transition_only"
	CategoryIncomparable              ComparisonCategory = "incomparable"
	CategoryInsufficientInformation   ComparisonCategory = "insufficient_information"
	CategoryStale                     ComparisonCategory = "stale"
	CategoryInvalidated               ComparisonCategory = "invalidated"
)

type ComparisonLifecycleStatus string

const (
	ComparisonCurrent     ComparisonLifecycleStatus = "current"
	ComparisonSuperseded  ComparisonLifecycleStatus = "superseded"
	ComparisonWithdrawn   ComparisonLifecycleStatus = "withdrawn"
	ComparisonInvalidated ComparisonLifecycleStatus = "invalidated"
)

type ComparisonMarkers struct {
	HistoricalDecisionRetainsAuthority    bool
	CognitiveRecommendationHasNoAuthority bool

	NotAProductionDecision            bool
	DoesNotOverrideHistoricalDecision bool
	NotAProbability                   bool
	NotAnAlert                        bool
	NotAuthorization                  bool
	NotACommand                       bool
	NotAnAction                       bool
	NoSecurityMeaning                 bool
	CalibrationOnly                   bool
}

type HistoricalDecisionComparison struct {
	ID string

	EpisodeID           string
	SituationID         string
	RecommendationSetID string
	HistoricalDecision  HistoricalDecisionRef

	Category   ComparisonCategory
	Status     ComparisonLifecycleStatus
	Dimensions []ComparisonDimension

	OverallAlignmentPermille  int
	OverallDivergencePermille int
	OverallCoveragePermille   int

	Comparable            bool
	SignificantDivergence bool

	HistoricalStateChanged     bool
	CognitiveTransitionFlagged bool

	SourceSituationFingerprint      string
	SourceRecommendationFingerprint string
	SourceHistoricalFingerprint     string

	PreviousComparisonID string
	Revision             uint64
	Fingerprint          string

	Markers ComparisonMarkers
}

type CompareInput struct {
	Historical      HistoricalDecisionRef
	Situation       cognitivesituation.CognitiveSituation
	Recommendations cognitiverecommendation.CognitiveRecommendationSet
	Previous        *HistoricalDecisionComparison
}

type ComparisonDimensionSummary struct {
	Kind               ComparisonDimensionKind
	Status             ComparisonDimensionStatus
	Comparable         bool
	AlignmentPermille  int
	DivergencePermille int
	CoveragePermille   int
	ReasonCodes        []string
}

type HistoricalDecisionComparisonExplanation struct {
	ComparisonID string
	EpisodeID    string

	Category                  ComparisonCategory
	Comparable                bool
	SignificantDivergence     bool
	OverallAlignmentPermille  int
	OverallDivergencePermille int
	OverallCoveragePermille   int

	DimensionSummaries []ComparisonDimensionSummary
	SummaryCode        string
	ReasonCodes        []string

	HistoricalDecisionRetainsAuthority    bool
	CognitiveRecommendationHasNoAuthority bool
	DoesNotOverrideHistoricalDecision     bool
	CalibrationOnly                       bool
	NotAProbability                       bool
	NotAnAlert                            bool
	NotACommand                           bool
	NotAnAction                           bool
	NoSecurityMeaning                     bool
}

type HistoricalDecisionComparisonSnapshot struct {
	WorkflowRevision uint64
	ProjectionDigest string
	Comparisons      []HistoricalDecisionComparison
	EpisodeIndex     map[string]int
	Digest           string
}

func (r HistoricalDecisionRef) Clone() HistoricalDecisionRef {
	r.ReasonCodes = append([]string(nil), r.ReasonCodes...)
	return r
}

func (d ComparisonDimension) Clone() ComparisonDimension {
	d.HistoricalCodes = append([]string(nil), d.HistoricalCodes...)
	d.CognitiveCodes = append([]string(nil), d.CognitiveCodes...)
	d.ReasonCodes = append([]string(nil), d.ReasonCodes...)
	return d
}

func (c HistoricalDecisionComparison) Clone() HistoricalDecisionComparison {
	out := c
	out.HistoricalDecision = c.HistoricalDecision.Clone()
	out.Dimensions = make([]ComparisonDimension, len(c.Dimensions))
	for i, dimension := range c.Dimensions {
		out.Dimensions[i] = dimension.Clone()
	}
	out.Markers = c.Markers
	return out
}

func (s HistoricalDecisionComparisonSnapshot) Clone() HistoricalDecisionComparisonSnapshot {
	out := s
	out.Comparisons = make([]HistoricalDecisionComparison, len(s.Comparisons))
	for i, comparison := range s.Comparisons {
		out.Comparisons[i] = comparison.Clone()
	}
	out.EpisodeIndex = make(map[string]int, len(s.EpisodeIndex))
	for key, value := range s.EpisodeIndex {
		out.EpisodeIndex[key] = value
	}
	return out
}
