package evidencediscrimination

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

type OutcomeDefinition struct {
	ID                string
	FactCode          situationfacts.FactCode
	Operator          OutcomeOperator
	Value             *situationfacts.FactValue
	DescriptionCode   string
	Supports          []situationhypotheses.HypothesisKind
	Contradicts       []situationhypotheses.HypothesisKind
	ReducesMissingFor []situationhypotheses.HypothesisKind
}

type EvidenceCandidateDefinition struct {
	Kind                      EvidenceCandidateKind
	Dimension                 EvidenceDimension
	Description               string
	RequiredFactCodes         []situationfacts.FactCode
	Outcomes                  []OutcomeDefinition
	ApplicableHypothesisKinds []situationhypotheses.HypothesisKind
	DefaultCostClass          EvidenceCostClass
	DefaultLatencyClass       EvidenceLatencyClass
	DefaultSensitivityClass   EvidenceSensitivityClass
}

type EvidenceCatalog struct {
	Version     string
	Definitions []EvidenceCandidateDefinition
	index       map[EvidenceCandidateKind]EvidenceCandidateDefinition
}

func (c EvidenceCatalog) Definition(kind EvidenceCandidateKind) (EvidenceCandidateDefinition, bool) {
	if c.index != nil {
		d, ok := c.index[kind]
		return cloneDefinition(d), ok
	}
	for _, d := range c.Definitions {
		if d.Kind == kind {
			return cloneDefinition(d), true
		}
	}
	return EvidenceCandidateDefinition{}, false
}

func cloneDefinition(d EvidenceCandidateDefinition) EvidenceCandidateDefinition {
	out := d
	out.RequiredFactCodes = append([]situationfacts.FactCode(nil), d.RequiredFactCodes...)
	out.ApplicableHypothesisKinds = append([]situationhypotheses.HypothesisKind(nil), d.ApplicableHypothesisKinds...)
	out.Outcomes = make([]OutcomeDefinition, len(d.Outcomes))
	for i, o := range d.Outcomes {
		out.Outcomes[i] = o
		if o.Value != nil {
			v := o.Value.Clone()
			out.Outcomes[i].Value = &v
		}
		out.Outcomes[i].Supports = append([]situationhypotheses.HypothesisKind(nil), o.Supports...)
		out.Outcomes[i].Contradicts = append([]situationhypotheses.HypothesisKind(nil), o.Contradicts...)
		out.Outcomes[i].ReducesMissingFor = append([]situationhypotheses.HypothesisKind(nil), o.ReducesMissingFor...)
	}
	return out
}

func boolOutcome(id string, code situationfacts.FactCode, description string, supports, contradicts, missing []situationhypotheses.HypothesisKind, value bool) OutcomeDefinition {
	v := situationfacts.BoolFactValue(value)
	return OutcomeDefinition{ID: id, FactCode: code, Operator: OutcomeValueEquals, Value: &v, DescriptionCode: description, Supports: supports, Contradicts: contradicts, ReducesMissingFor: missing}
}
func intOutcome(id string, code situationfacts.FactCode, operator OutcomeOperator, value int64, description string, supports, contradicts, missing []situationhypotheses.HypothesisKind) OutcomeDefinition {
	def, _ := situationfacts.Schema().Definition(code)
	var v situationfacts.FactValue
	if def.ValueKind == situationfacts.ValuePermille {
		v = situationfacts.PermilleFactValue(value)
	} else {
		v = situationfacts.IntFactValue(value)
	}
	return OutcomeDefinition{ID: id, FactCode: code, Operator: operator, Value: &v, DescriptionCode: description, Supports: supports, Contradicts: contradicts, ReducesMissingFor: missing}
}
func presenceOutcome(id string, code situationfacts.FactCode, description string, supports, contradicts, missing []situationhypotheses.HypothesisKind) OutcomeDefinition {
	return OutcomeDefinition{ID: id, FactCode: code, Operator: OutcomeFactPresent, DescriptionCode: description, Supports: supports, Contradicts: contradicts, ReducesMissingFor: missing}
}

func Catalog() EvidenceCatalog { return buildCatalog() }
func Schema() EvidenceCatalog  { return Catalog() }

func buildCatalog() EvidenceCatalog {
	var defs = []EvidenceCandidateDefinition{
		{Kind: KindIdentityConfirmation, Dimension: DimensionIdentity, Description: "Descriptive confirmation of the identity state and its candidate set.", RequiredFactCodes: []situationfacts.FactCode{situationfacts.CodeIdentityKnownPresent, situationfacts.CodeIdentityUnknownPresent, situationfacts.CodeIdentityUncertainPresent, situationfacts.CodeIdentityCandidateEntitySet, situationfacts.CodeIdentityKnownEntitySet}, Outcomes: []OutcomeDefinition{
			boolOutcome("known", situationfacts.CodeIdentityKnownPresent, "identity_known_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindPatternConsistent, situationhypotheses.KindMultiEntityActivity}, []situationhypotheses.HypothesisKind{situationhypotheses.KindIdentityResolutionFailure, situationhypotheses.KindCoherentUnrecognizedActivity}, nil, true),
			boolOutcome("unknown", situationfacts.CodeIdentityUnknownPresent, "identity_unknown_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindCoherentUnrecognizedActivity}, []situationhypotheses.HypothesisKind{situationhypotheses.KindPatternConsistent}, nil, true),
			boolOutcome("uncertain", situationfacts.CodeIdentityUncertainPresent, "identity_uncertain_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindIdentityResolutionFailure}, nil, []situationhypotheses.HypothesisKind{situationhypotheses.KindIdentityResolutionFailure}, true),
		}, ApplicableHypothesisKinds: []situationhypotheses.HypothesisKind{situationhypotheses.KindIdentityResolutionFailure, situationhypotheses.KindCoherentUnrecognizedActivity, situationhypotheses.KindMultiEntityActivity}, DefaultCostClass: CostMedium, DefaultLatencyClass: LatencyShort, DefaultSensitivityClass: SensitivityModerate},
		{Kind: KindIdentityContinuityConfirmation, Dimension: DimensionIdentityContinuity, Description: "Descriptive confirmation of technical continuity across observations.", RequiredFactCodes: []situationfacts.FactCode{situationfacts.CodeContinuitySharedTrack, situationfacts.CodeContinuitySharedActivation, situationfacts.CodeContinuitySharedSequence, situationfacts.CodeContinuityMultipleNodesSameTrack}, Outcomes: []OutcomeDefinition{
			boolOutcome("shared_track", situationfacts.CodeContinuitySharedTrack, "shared_track_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindCoherentUnrecognizedActivity}, nil, []situationhypotheses.HypothesisKind{situationhypotheses.KindIdentityResolutionFailure}, true),
			boolOutcome("track_break", situationfacts.CodeContinuitySharedTrack, "shared_track_not_observed", nil, []situationhypotheses.HypothesisKind{situationhypotheses.KindCoherentUnrecognizedActivity}, nil, false),
			presenceOutcome("activation", situationfacts.CodeContinuitySharedActivation, "shared_activation_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindIdentityResolutionFailure, situationhypotheses.KindCoherentUnrecognizedActivity}, nil, nil),
		}, ApplicableHypothesisKinds: []situationhypotheses.HypothesisKind{situationhypotheses.KindIdentityResolutionFailure, situationhypotheses.KindCoherentUnrecognizedActivity}, DefaultCostClass: CostMedium, DefaultLatencyClass: LatencyShort, DefaultSensitivityClass: SensitivityModerate},
		{Kind: KindSpatialContinuityConfirmation, Dimension: DimensionSpatialContinuity, Description: "Descriptive confirmation of spatial relationships between observed nodes.", RequiredFactCodes: []situationfacts.FactCode{situationfacts.CodeSpatialReachableTransitionCount, situationfacts.CodeSpatialUnreachableTransitionCount, situationfacts.CodeSpatialUnknownTransitionCount, situationfacts.CodeSpatialTopologyAvailable, situationfacts.CodeSpatialNodeSequence}, Outcomes: []OutcomeDefinition{
			intOutcome("reachable", situationfacts.CodeSpatialReachableTransitionCount, OutcomeValueGreaterThan, 0, "reachable_transition_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindCoherentUnrecognizedActivity}, nil, nil),
			intOutcome("unreachable", situationfacts.CodeSpatialUnreachableTransitionCount, OutcomeValueGreaterThan, 0, "unreachable_transition_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindContextOrSensorInconsistency}, []situationhypotheses.HypothesisKind{situationhypotheses.KindCoherentUnrecognizedActivity}, nil),
			boolOutcome("topology_unavailable", situationfacts.CodeSpatialTopologyAvailable, "topology_unavailable", []situationhypotheses.HypothesisKind{situationhypotheses.KindInsufficientInformation}, nil, []situationhypotheses.HypothesisKind{situationhypotheses.KindCoherentUnrecognizedActivity}, false),
		}, ApplicableHypothesisKinds: []situationhypotheses.HypothesisKind{situationhypotheses.KindCoherentUnrecognizedActivity, situationhypotheses.KindInsufficientInformation, situationhypotheses.KindContextOrSensorInconsistency}, DefaultCostClass: CostMedium, DefaultLatencyClass: LatencyShort, DefaultSensitivityClass: SensitivityLow},
		{Kind: KindContextConfirmation, Dimension: DimensionDomesticContext, Description: "Descriptive confirmation of available contextual dimensions and changes.", RequiredFactCodes: []situationfacts.FactCode{situationfacts.CodeContextHouseModeSet, situationfacts.CodeContextHouseModeChanged, situationfacts.CodeContextHouseModeConflict, situationfacts.CodeContextOccupancySet, situationfacts.CodeContextOccupancyChanged, situationfacts.CodeContextOccupancyConflict, situationfacts.CodeContextPartialPresent, situationfacts.CodeContextMissingPresent}, Outcomes: []OutcomeDefinition{
			presenceOutcome("context_conflict", situationfacts.CodeContextHouseModeConflict, "context_conflict_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindContextOrSensorInconsistency}, nil, nil),
			presenceOutcome("context_change", situationfacts.CodeContextHouseModeChanged, "context_change_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindPatternConsistent}, nil, nil),
			boolOutcome("context_partial", situationfacts.CodeContextPartialPresent, "context_partial_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindInsufficientInformation}, nil, []situationhypotheses.HypothesisKind{situationhypotheses.KindInsufficientInformation}, true),
		}, ApplicableHypothesisKinds: []situationhypotheses.HypothesisKind{situationhypotheses.KindContextOrSensorInconsistency, situationhypotheses.KindInsufficientInformation, situationhypotheses.KindPatternConsistent}, DefaultCostClass: CostLow, DefaultLatencyClass: LatencyImmediate, DefaultSensitivityClass: SensitivityLow},
		{Kind: KindSourceConsistencyConfirmation, Dimension: DimensionSourceConsistency, Description: "Descriptive confirmation of compatibility among available sources.", RequiredFactCodes: []situationfacts.FactCode{situationfacts.CodeIdentityConflict, situationfacts.CodeContextHouseModeConflict, situationfacts.CodeContextOccupancyConflict, situationfacts.CodeSpatialUnreachableTransitionCount}, Outcomes: []OutcomeDefinition{
			{ID: "conflict_present", FactCode: situationfacts.CodeContextHouseModeConflict, Operator: OutcomeConflictPresent, DescriptionCode: "source_conflict_present", Supports: []situationhypotheses.HypothesisKind{situationhypotheses.KindContextOrSensorInconsistency}},
			{ID: "conflict_absent", FactCode: situationfacts.CodeContextHouseModeConflict, Operator: OutcomeConflictAbsent, DescriptionCode: "source_conflict_not_observed", Contradicts: []situationhypotheses.HypothesisKind{situationhypotheses.KindContextOrSensorInconsistency}},
		}, ApplicableHypothesisKinds: []situationhypotheses.HypothesisKind{situationhypotheses.KindContextOrSensorInconsistency}, DefaultCostClass: CostMedium, DefaultLatencyClass: LatencyShort, DefaultSensitivityClass: SensitivityLow},
		{Kind: KindTemporalRepetitionConfirmation, Dimension: DimensionTemporalRepetition, Description: "Descriptive confirmation of repetition across time and reference assessments.", RequiredFactCodes: []situationfacts.FactCode{situationfacts.CodeEpisodeObservationCount, situationfacts.CodeTemporalMinimumGapMS, situationfacts.CodeTemporalMaximumGapMS, situationfacts.CodeMemoryDeviationPresent, situationfacts.CodeMemoryDeviationMaximumScore, situationfacts.CodeMemoryDeviationTemporalPositive, situationfacts.CodeMemoryRoutineRefCount}, Outcomes: []OutcomeDefinition{
			intOutcome("repeated", situationfacts.CodeEpisodeObservationCount, OutcomeValueGreaterThan, 1, "repeated_observations", []situationhypotheses.HypothesisKind{situationhypotheses.KindPossiblePatternShift}, nil, []situationhypotheses.HypothesisKind{situationhypotheses.KindPossiblePatternShift}),
			intOutcome("single", situationfacts.CodeEpisodeObservationCount, OutcomeValueLessThan, 2, "single_observation", []situationhypotheses.HypothesisKind{situationhypotheses.KindIsolatedDeviation}, []situationhypotheses.HypothesisKind{situationhypotheses.KindPossiblePatternShift}, nil),
			presenceOutcome("temporal_deviation", situationfacts.CodeMemoryDeviationTemporalPositive, "temporal_deviation_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindIsolatedDeviation, situationhypotheses.KindPossiblePatternShift}, nil, nil),
		}, ApplicableHypothesisKinds: []situationhypotheses.HypothesisKind{situationhypotheses.KindIsolatedDeviation, situationhypotheses.KindPossiblePatternShift}, DefaultCostClass: CostMedium, DefaultLatencyClass: LatencyExtended, DefaultSensitivityClass: SensitivityLow},
		{Kind: KindPatternAlignmentConfirmation, Dimension: DimensionPatternAlignment, Description: "Descriptive confirmation of alignment with an available reference pattern.", RequiredFactCodes: []situationfacts.FactCode{situationfacts.CodeMemoryRoutineRefCount, situationfacts.CodeMemoryDeviationEvaluated, situationfacts.CodeMemoryDeviationMaximumScore, situationfacts.CodeMemoryDeviationMaximumCoverage, situationfacts.CodeMemoryDeviationStructuralPositive, situationfacts.CodeMemoryDeviationTemporalPositive, situationfacts.CodeMemoryDeviationIntervalPositive}, Outcomes: []OutcomeDefinition{
			intOutcome("low_score", situationfacts.CodeMemoryDeviationMaximumScore, OutcomeValueLessThan, 200, "low_reference_deviation", []situationhypotheses.HypothesisKind{situationhypotheses.KindPatternConsistent}, []situationhypotheses.HypothesisKind{situationhypotheses.KindIsolatedDeviation}, nil),
			intOutcome("positive_score", situationfacts.CodeMemoryDeviationMaximumScore, OutcomeValueGreaterThan, 0, "positive_reference_deviation", []situationhypotheses.HypothesisKind{situationhypotheses.KindIsolatedDeviation, situationhypotheses.KindPossiblePatternShift}, []situationhypotheses.HypothesisKind{situationhypotheses.KindPatternConsistent}, nil),
			presenceOutcome("routine_reference", situationfacts.CodeMemoryRoutineRefCount, "routine_reference_available", []situationhypotheses.HypothesisKind{situationhypotheses.KindPatternConsistent, situationhypotheses.KindIsolatedDeviation}, nil, nil),
		}, ApplicableHypothesisKinds: []situationhypotheses.HypothesisKind{situationhypotheses.KindPatternConsistent, situationhypotheses.KindIsolatedDeviation, situationhypotheses.KindPossiblePatternShift}, DefaultCostClass: CostLow, DefaultLatencyClass: LatencyImmediate, DefaultSensitivityClass: SensitivityLow},
		{Kind: KindEntityCountConfirmation, Dimension: DimensionEntityMultiplicity, Description: "Descriptive confirmation of the number of distinct referenced entities.", RequiredFactCodes: []situationfacts.FactCode{situationfacts.CodeEpisodeEntityCount, situationfacts.CodeEpisodeMultipleEntities, situationfacts.CodeIdentityMultipleKnownEntities, situationfacts.CodeIdentityKnownEntitySet}, Outcomes: []OutcomeDefinition{
			intOutcome("multiple", situationfacts.CodeEpisodeEntityCount, OutcomeValueGreaterThan, 1, "multiple_entities_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindMultiEntityActivity}, nil, nil),
			intOutcome("single", situationfacts.CodeEpisodeEntityCount, OutcomeValueLessThan, 2, "single_entity_observed", nil, []situationhypotheses.HypothesisKind{situationhypotheses.KindMultiEntityActivity}, nil),
		}, ApplicableHypothesisKinds: []situationhypotheses.HypothesisKind{situationhypotheses.KindMultiEntityActivity, situationhypotheses.KindIdentityResolutionFailure}, DefaultCostClass: CostLow, DefaultLatencyClass: LatencyShort, DefaultSensitivityClass: SensitivityModerate},
		{Kind: KindContextCompletenessConfirmation, Dimension: DimensionInformationCompleteness, Description: "Descriptive confirmation of completeness for context and topology dimensions.", RequiredFactCodes: []situationfacts.FactCode{situationfacts.CodeContextCompleteCount, situationfacts.CodeContextPartialCount, situationfacts.CodeContextMissingCount, situationfacts.CodeContextPartialPresent, situationfacts.CodeContextMissingPresent, situationfacts.CodeSpatialTopologyAvailable}, Outcomes: []OutcomeDefinition{
			boolOutcome("partial", situationfacts.CodeContextPartialPresent, "partial_context_observed", []situationhypotheses.HypothesisKind{situationhypotheses.KindInsufficientInformation}, nil, nil, true),
			boolOutcome("complete", situationfacts.CodeContextPartialPresent, "complete_context_observed", nil, []situationhypotheses.HypothesisKind{situationhypotheses.KindInsufficientInformation}, []situationhypotheses.HypothesisKind{situationhypotheses.KindInsufficientInformation}, false),
			boolOutcome("topology", situationfacts.CodeSpatialTopologyAvailable, "topology_available", []situationhypotheses.HypothesisKind{situationhypotheses.KindCoherentUnrecognizedActivity}, nil, []situationhypotheses.HypothesisKind{situationhypotheses.KindInsufficientInformation}, true),
		}, ApplicableHypothesisKinds: []situationhypotheses.HypothesisKind{situationhypotheses.KindInsufficientInformation, situationhypotheses.KindCoherentUnrecognizedActivity}, DefaultCostClass: CostLow, DefaultLatencyClass: LatencyImmediate, DefaultSensitivityClass: SensitivityLow},
	}
	// Keep catalog order canonical without changing its semantic content.
	sort.Slice(defs, func(i, j int) bool { return defs[i].Kind < defs[j].Kind })
	return EvidenceCatalog{Version: "v1", Definitions: defs}
}

func CatalogFingerprint(c EvidenceCatalog) string {
	copy := c
	copy.index = nil
	copy.Definitions = make([]EvidenceCandidateDefinition, len(c.Definitions))
	for i, d := range c.Definitions {
		copy.Definitions[i] = cloneDefinition(d)
	}
	sort.Slice(copy.Definitions, func(i, j int) bool { return copy.Definitions[i].Kind < copy.Definitions[j].Kind })
	payload, _ := json.Marshal(copy)
	d := sha256.Sum256(payload)
	return "evidence-discrimination-catalog-v1:" + hex.EncodeToString(d[:])
}
func catalogFingerprint(c EvidenceCatalog) string { return CatalogFingerprint(c) }
