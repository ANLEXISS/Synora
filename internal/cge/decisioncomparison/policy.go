package decisioncomparison

import "fmt"

type Policy struct {
	MaxDimensions                    int
	MaxReasonCodes                   int
	MinComparableCoveragePermille    int
	MinSignificantDivergencePermille int

	StateContinuityWeight         int
	StateTransitionWeight         int
	CognitiveTransitionWeight     int
	InterpretationStabilityWeight int
	AmbiguityPostureWeight        int
	ObservationPostureWeight      int
	EvidencePostureWeight         int
	FreshnessWeight               int
	DecisionTimingWeight          int

	PreserveIncomparableDimensions bool
}

func DefaultPolicy() Policy {
	return Policy{
		MaxDimensions: 16, MaxReasonCodes: 64,
		MinComparableCoveragePermille: 600, MinSignificantDivergencePermille: 500,
		StateContinuityWeight: 150, StateTransitionWeight: 200, CognitiveTransitionWeight: 150,
		InterpretationStabilityWeight: 100, AmbiguityPostureWeight: 100,
		ObservationPostureWeight: 100, EvidencePostureWeight: 100,
		FreshnessWeight: 75, DecisionTimingWeight: 25,
		PreserveIncomparableDimensions: true,
	}
}

func (p Policy) Validate() error {
	if p.MaxDimensions <= 0 || p.MaxDimensions > 64 || p.MaxReasonCodes <= 0 || p.MaxReasonCodes > 256 {
		return ErrInvalidPolicy
	}
	if p.MinComparableCoveragePermille < 0 || p.MinComparableCoveragePermille > 1000 || p.MinSignificantDivergencePermille < 0 || p.MinSignificantDivergencePermille > 1000 {
		return ErrInvalidPolicy
	}
	weights := p.StateContinuityWeight + p.StateTransitionWeight + p.CognitiveTransitionWeight +
		p.InterpretationStabilityWeight + p.AmbiguityPostureWeight + p.ObservationPostureWeight +
		p.EvidencePostureWeight + p.FreshnessWeight + p.DecisionTimingWeight
	if weights != 1000 {
		return fmt.Errorf("%w: weights must total 1000", ErrInvalidPolicy)
	}
	return nil
}

func (p Policy) Fingerprint() string {
	return fingerprint("historical-decision-comparison-policy-v1:", p)
}

func dimensionWeight(p Policy, kind ComparisonDimensionKind) int {
	switch kind {
	case DimensionStateContinuity:
		return p.StateContinuityWeight
	case DimensionStateTransition:
		return p.StateTransitionWeight
	case DimensionCognitiveTransition:
		return p.CognitiveTransitionWeight
	case DimensionInterpretationStability:
		return p.InterpretationStabilityWeight
	case DimensionAmbiguityPosture:
		return p.AmbiguityPostureWeight
	case DimensionObservationPosture:
		return p.ObservationPostureWeight
	case DimensionEvidencePosture:
		return p.EvidencePostureWeight
	case DimensionFreshness:
		return p.FreshnessWeight
	case DimensionDecisionTiming:
		return p.DecisionTimingWeight
	default:
		return 0
	}
}
