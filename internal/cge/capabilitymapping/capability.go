package capabilitymapping

import "sort"

type CapabilityKind string
type CapabilityInstanceID string
type CapabilityCatalogID string
type CapabilityInventoryID string

const (
	CapabilityIdentityObservation                CapabilityKind = "identity_observation"
	CapabilityIdentityContinuityObservation      CapabilityKind = "identity_continuity_observation"
	CapabilitySpatialRelationObservation         CapabilityKind = "spatial_relation_observation"
	CapabilityContextStateObservation            CapabilityKind = "context_state_observation"
	CapabilitySourceConsistencyObservation       CapabilityKind = "source_consistency_observation"
	CapabilityTemporalRepetitionObservation      CapabilityKind = "temporal_repetition_observation"
	CapabilityPatternAlignmentObservation        CapabilityKind = "pattern_alignment_observation"
	CapabilityEntityMultiplicityObservation      CapabilityKind = "entity_multiplicity_observation"
	CapabilityInformationCompletenessObservation CapabilityKind = "information_completeness_observation"
)

const (
	KindIdentityObservation                = CapabilityIdentityObservation
	KindIdentityContinuityObservation      = CapabilityIdentityContinuityObservation
	KindSpatialRelationObservation         = CapabilitySpatialRelationObservation
	KindContextStateObservation            = CapabilityContextStateObservation
	KindSourceConsistencyObservation       = CapabilitySourceConsistencyObservation
	KindTemporalRepetitionObservation      = CapabilityTemporalRepetitionObservation
	KindPatternAlignmentObservation        = CapabilityPatternAlignmentObservation
	KindEntityMultiplicityObservation      = CapabilityEntityMultiplicityObservation
	KindInformationCompletenessObservation = CapabilityInformationCompletenessObservation
)

const (
	CapabilityStatusAvailable   CapabilityStatus = "available"
	CapabilityStatusDegraded    CapabilityStatus = "degraded"
	CapabilityStatusUnavailable CapabilityStatus = "unavailable"
	CapabilityStatusUnknown     CapabilityStatus = "unknown"
	CapabilityStatusRetired     CapabilityStatus = "retired"
	CapabilityStatusInvalidated CapabilityStatus = "invalidated"
)

const (
	StatusAvailable   = CapabilityStatusAvailable
	StatusDegraded    = CapabilityStatusDegraded
	StatusUnavailable = CapabilityStatusUnavailable
	StatusUnknown     = CapabilityStatusUnknown
	StatusRetired     = CapabilityStatusRetired
	StatusInvalidated = CapabilityStatusInvalidated
)

type CapabilityStatus string

type CapabilityCostClass string
type CapabilityLatencyClass string
type CapabilitySensitivityClass string

const (
	CapabilityCostLow     CapabilityCostClass = "low"
	CapabilityCostMedium  CapabilityCostClass = "medium"
	CapabilityCostHigh    CapabilityCostClass = "high"
	CapabilityCostUnknown CapabilityCostClass = "unknown"

	CapabilityLatencyImmediate CapabilityLatencyClass = "immediate"
	CapabilityLatencyShort     CapabilityLatencyClass = "short"
	CapabilityLatencyExtended  CapabilityLatencyClass = "extended"
	CapabilityLatencyUnknown   CapabilityLatencyClass = "unknown"

	CapabilitySensitivityLow      CapabilitySensitivityClass = "low"
	CapabilitySensitivityModerate CapabilitySensitivityClass = "moderate"
	CapabilitySensitivityHigh     CapabilitySensitivityClass = "high"
	CapabilitySensitivityUnknown  CapabilitySensitivityClass = "unknown"
)

const (
	CostLow             = CapabilityCostLow
	CostMedium          = CapabilityCostMedium
	CostHigh            = CapabilityCostHigh
	CostUnknown         = CapabilityCostUnknown
	LatencyImmediate    = CapabilityLatencyImmediate
	LatencyShort        = CapabilityLatencyShort
	LatencyExtended     = CapabilityLatencyExtended
	LatencyUnknown      = CapabilityLatencyUnknown
	SensitivityLow      = CapabilitySensitivityLow
	SensitivityModerate = CapabilitySensitivityModerate
	SensitivityHigh     = CapabilitySensitivityHigh
	SensitivityUnknown  = CapabilitySensitivityUnknown
)

type CapabilityQuality struct {
	ReliabilityPermille  int
	CompletenessPermille int
	FreshnessPermille    int
	Calibrated           bool
	SourceCount          int
}

type CapabilityScope struct {
	Kind string
	Ref  string
}

type ConstraintOperator string

const (
	ConstraintEquals    ConstraintOperator = "equals"
	ConstraintNotEquals ConstraintOperator = "not_equals"
	ConstraintContains  ConstraintOperator = "contains"
	ConstraintMinimum   ConstraintOperator = "minimum"
	ConstraintMaximum   ConstraintOperator = "maximum"
	ConstraintPresent   ConstraintOperator = "present"
	ConstraintAbsent    ConstraintOperator = "absent"
)

type ConstraintValue struct {
	String         string
	Bool           *bool
	NumberPermille int
}

type CapabilityConstraint struct {
	Code     string
	Operator ConstraintOperator
	Value    ConstraintValue
	Hard     bool
}

type CapabilityDefinition struct {
	Kind CapabilityKind

	DescriptionCode string

	SupportedRequestKinds []string
	SupportedDimensions   []string

	OutputFactCodes []string

	RequiredInputContextCodes []string

	SupportedQualityDimensions []QualityDimension

	DefaultCostClass        CapabilityCostClass
	DefaultLatencyClass     CapabilityLatencyClass
	DefaultSensitivityClass CapabilitySensitivityClass

	AllowsRepeatedUse   bool
	AllowsConcurrentUse bool
}

type QualityDimension string

const (
	QualityReliability  QualityDimension = "reliability"
	QualityCompleteness QualityDimension = "completeness"
	QualityFreshness    QualityDimension = "freshness"
)

type CapabilityCatalog struct {
	Version     string
	Definitions []CapabilityDefinition
	Fingerprint string
}

type CapabilityInstance struct {
	ID CapabilityInstanceID

	Kind CapabilityKind

	DomainID   string
	ProviderID string

	Status  CapabilityStatus
	Quality CapabilityQuality

	CostClass        CapabilityCostClass
	LatencyClass     CapabilityLatencyClass
	SensitivityClass CapabilitySensitivityClass

	SupportedScopes []CapabilityScope
	Constraints     []CapabilityConstraint

	Revision uint64

	DefinitionFingerprint string
	Fingerprint           string
}

func allCapabilityKinds() []CapabilityKind {
	return []CapabilityKind{
		CapabilityIdentityObservation,
		CapabilityIdentityContinuityObservation,
		CapabilitySpatialRelationObservation,
		CapabilityContextStateObservation,
		CapabilitySourceConsistencyObservation,
		CapabilityTemporalRepetitionObservation,
		CapabilityPatternAlignmentObservation,
		CapabilityEntityMultiplicityObservation,
		CapabilityInformationCompletenessObservation,
	}
}

func AllCapabilityKinds() []CapabilityKind {
	return append([]CapabilityKind(nil), allCapabilityKinds()...)
}

func (q CapabilityQuality) Clone() CapabilityQuality { return q }

func (s CapabilityScope) Clone() CapabilityScope { return s }

func (c CapabilityConstraint) Clone() CapabilityConstraint {
	out := c
	if c.Value.Bool != nil {
		value := *c.Value.Bool
		out.Value.Bool = &value
	}
	return out
}

func (d CapabilityDefinition) Clone() CapabilityDefinition {
	out := d
	out.SupportedRequestKinds = append([]string(nil), d.SupportedRequestKinds...)
	out.SupportedDimensions = append([]string(nil), d.SupportedDimensions...)
	out.OutputFactCodes = append([]string(nil), d.OutputFactCodes...)
	out.RequiredInputContextCodes = append([]string(nil), d.RequiredInputContextCodes...)
	out.SupportedQualityDimensions = append([]QualityDimension(nil), d.SupportedQualityDimensions...)
	return out
}

func (i CapabilityInstance) Clone() CapabilityInstance {
	out := i
	out.Quality = i.Quality.Clone()
	out.SupportedScopes = append([]CapabilityScope(nil), i.SupportedScopes...)
	out.Constraints = make([]CapabilityConstraint, len(i.Constraints))
	for n, constraint := range i.Constraints {
		out.Constraints[n] = constraint.Clone()
	}
	return out
}

func sortScopes(values []CapabilityScope) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Kind != values[j].Kind {
			return values[i].Kind < values[j].Kind
		}
		return values[i].Ref < values[j].Ref
	})
}

func sortConstraints(values []CapabilityConstraint) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Code != values[j].Code {
			return values[i].Code < values[j].Code
		}
		if values[i].Operator != values[j].Operator {
			return values[i].Operator < values[j].Operator
		}
		if values[i].Hard != values[j].Hard {
			return !values[i].Hard && values[j].Hard
		}
		if values[i].Value.String != values[j].Value.String {
			return values[i].Value.String < values[j].Value.String
		}
		if values[i].Value.NumberPermille != values[j].Value.NumberPermille {
			return values[i].Value.NumberPermille < values[j].Value.NumberPermille
		}
		return !boolValue(values[i].Value.Bool) && boolValue(values[j].Value.Bool)
	})
}
