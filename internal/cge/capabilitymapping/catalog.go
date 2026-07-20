package capabilitymapping

import (
	"sort"
	"strings"
)

var requestKindToCapabilityKind = map[string]CapabilityKind{
	"identity_confirmation":             CapabilityIdentityObservation,
	"identity_continuity_confirmation":  CapabilityIdentityContinuityObservation,
	"spatial_continuity_confirmation":   CapabilitySpatialRelationObservation,
	"context_confirmation":              CapabilityContextStateObservation,
	"source_consistency_confirmation":   CapabilitySourceConsistencyObservation,
	"temporal_repetition_confirmation":  CapabilityTemporalRepetitionObservation,
	"pattern_alignment_confirmation":    CapabilityPatternAlignmentObservation,
	"entity_count_confirmation":         CapabilityEntityMultiplicityObservation,
	"context_completeness_confirmation": CapabilityInformationCompletenessObservation,
}

var requestKindToDimension = map[string]string{
	"identity_confirmation":             "identity",
	"identity_continuity_confirmation":  "identity_continuity",
	"spatial_continuity_confirmation":   "spatial_continuity",
	"context_confirmation":              "domestic_context",
	"source_consistency_confirmation":   "source_consistency",
	"temporal_repetition_confirmation":  "temporal_repetition",
	"pattern_alignment_confirmation":    "pattern_alignment",
	"entity_count_confirmation":         "entity_multiplicity",
	"context_completeness_confirmation": "information_completeness",
}

func Catalog() CapabilityCatalog {
	definitions := []CapabilityDefinition{
		{Kind: CapabilityIdentityObservation, DescriptionCode: "abstract_identity_state", SupportedRequestKinds: []string{"identity_confirmation"}, SupportedDimensions: []string{"identity"}, OutputFactCodes: []string{"identity.state"}, SupportedQualityDimensions: []QualityDimension{QualityReliability, QualityCompleteness, QualityFreshness}, DefaultCostClass: CapabilityCostLow, DefaultLatencyClass: CapabilityLatencyShort, DefaultSensitivityClass: CapabilitySensitivityModerate, AllowsRepeatedUse: true, AllowsConcurrentUse: true},
		{Kind: CapabilityIdentityContinuityObservation, DescriptionCode: "abstract_identity_continuity", SupportedRequestKinds: []string{"identity_continuity_confirmation"}, SupportedDimensions: []string{"identity_continuity"}, OutputFactCodes: []string{"identity.continuity"}, SupportedQualityDimensions: []QualityDimension{QualityReliability, QualityFreshness}, DefaultCostClass: CapabilityCostLow, DefaultLatencyClass: CapabilityLatencyShort, DefaultSensitivityClass: CapabilitySensitivityModerate, AllowsRepeatedUse: true, AllowsConcurrentUse: true},
		{Kind: CapabilitySpatialRelationObservation, DescriptionCode: "abstract_spatial_relation", SupportedRequestKinds: []string{"spatial_continuity_confirmation"}, SupportedDimensions: []string{"spatial_continuity"}, OutputFactCodes: []string{"spatial.relation"}, SupportedQualityDimensions: []QualityDimension{QualityReliability, QualityCompleteness, QualityFreshness}, DefaultCostClass: CapabilityCostMedium, DefaultLatencyClass: CapabilityLatencyShort, DefaultSensitivityClass: CapabilitySensitivityLow, AllowsRepeatedUse: true, AllowsConcurrentUse: true},
		{Kind: CapabilityContextStateObservation, DescriptionCode: "abstract_context_state", SupportedRequestKinds: []string{"context_confirmation"}, SupportedDimensions: []string{"domestic_context"}, OutputFactCodes: []string{"context.state"}, SupportedQualityDimensions: []QualityDimension{QualityReliability, QualityCompleteness, QualityFreshness}, DefaultCostClass: CapabilityCostLow, DefaultLatencyClass: CapabilityLatencyImmediate, DefaultSensitivityClass: CapabilitySensitivityLow, AllowsRepeatedUse: true, AllowsConcurrentUse: true},
		{Kind: CapabilitySourceConsistencyObservation, DescriptionCode: "abstract_source_consistency", SupportedRequestKinds: []string{"source_consistency_confirmation"}, SupportedDimensions: []string{"source_consistency"}, OutputFactCodes: []string{"source.consistency"}, SupportedQualityDimensions: []QualityDimension{QualityReliability, QualityFreshness}, DefaultCostClass: CapabilityCostMedium, DefaultLatencyClass: CapabilityLatencyShort, DefaultSensitivityClass: CapabilitySensitivityLow, AllowsRepeatedUse: true, AllowsConcurrentUse: true},
		{Kind: CapabilityTemporalRepetitionObservation, DescriptionCode: "abstract_temporal_repetition", SupportedRequestKinds: []string{"temporal_repetition_confirmation"}, SupportedDimensions: []string{"temporal_repetition"}, OutputFactCodes: []string{"temporal.repetition"}, SupportedQualityDimensions: []QualityDimension{QualityReliability, QualityCompleteness, QualityFreshness}, DefaultCostClass: CapabilityCostLow, DefaultLatencyClass: CapabilityLatencyExtended, DefaultSensitivityClass: CapabilitySensitivityLow, AllowsRepeatedUse: true, AllowsConcurrentUse: true},
		{Kind: CapabilityPatternAlignmentObservation, DescriptionCode: "abstract_pattern_alignment", SupportedRequestKinds: []string{"pattern_alignment_confirmation"}, SupportedDimensions: []string{"pattern_alignment"}, OutputFactCodes: []string{"pattern.alignment"}, SupportedQualityDimensions: []QualityDimension{QualityReliability, QualityCompleteness, QualityFreshness}, DefaultCostClass: CapabilityCostLow, DefaultLatencyClass: CapabilityLatencyImmediate, DefaultSensitivityClass: CapabilitySensitivityLow, AllowsRepeatedUse: true, AllowsConcurrentUse: true},
		{Kind: CapabilityEntityMultiplicityObservation, DescriptionCode: "abstract_entity_multiplicity", SupportedRequestKinds: []string{"entity_count_confirmation"}, SupportedDimensions: []string{"entity_multiplicity"}, OutputFactCodes: []string{"entity.multiplicity"}, SupportedQualityDimensions: []QualityDimension{QualityReliability, QualityCompleteness, QualityFreshness}, DefaultCostClass: CapabilityCostLow, DefaultLatencyClass: CapabilityLatencyShort, DefaultSensitivityClass: CapabilitySensitivityModerate, AllowsRepeatedUse: true, AllowsConcurrentUse: true},
		{Kind: CapabilityInformationCompletenessObservation, DescriptionCode: "abstract_information_completeness", SupportedRequestKinds: []string{"context_completeness_confirmation"}, SupportedDimensions: []string{"information_completeness"}, OutputFactCodes: []string{"information.completeness"}, SupportedQualityDimensions: []QualityDimension{QualityReliability, QualityCompleteness, QualityFreshness}, DefaultCostClass: CapabilityCostLow, DefaultLatencyClass: CapabilityLatencyImmediate, DefaultSensitivityClass: CapabilitySensitivityLow, AllowsRepeatedUse: true, AllowsConcurrentUse: true},
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Kind < definitions[j].Kind })
	catalog := CapabilityCatalog{Version: "capability-catalog-v1", Definitions: definitions}
	catalog.Fingerprint = catalogFingerprint(catalog)
	return catalog
}

func CatalogFingerprint(values ...CapabilityCatalog) string {
	catalog := Catalog()
	if len(values) > 0 {
		catalog = values[0]
	}
	return catalogFingerprint(catalog)
}

func ValidateCatalog(c CapabilityCatalog) error {
	if c.Version == "" || len(c.Definitions) == 0 || c.Fingerprint == "" || catalogFingerprint(c) != c.Fingerprint {
		return ErrInvalidCatalog
	}
	seen := map[CapabilityKind]struct{}{}
	last := CapabilityKind("")
	for _, definition := range c.Definitions {
		if definition.Kind == "" || definition.Kind <= last || !validCapabilityKind(definition.Kind) || forbiddenCapabilityText(string(definition.Kind)) || forbiddenCapabilityText(definition.DescriptionCode) {
			return ErrInvalidCapabilityDefinition
		}
		if _, ok := seen[definition.Kind]; ok {
			return ErrInvalidCapabilityDefinition
		}
		seen[definition.Kind] = struct{}{}
		if definition.DescriptionCode == "" || len(definition.SupportedRequestKinds) == 0 || len(definition.SupportedDimensions) == 0 || len(definition.OutputFactCodes) == 0 {
			return ErrInvalidCapabilityDefinition
		}
		if !validClass(string(definition.DefaultCostClass), classCost) || !validClass(string(definition.DefaultLatencyClass), classLatency) || !validClass(string(definition.DefaultSensitivityClass), classSensitivity) {
			return ErrInvalidCapabilityDefinition
		}
		if !sortedUniqueStrings(definition.SupportedRequestKinds) || !sortedUniqueStrings(definition.SupportedDimensions) || !sortedUniqueStrings(definition.OutputFactCodes) {
			return ErrInvalidCapabilityDefinition
		}
		for _, requestKind := range definition.SupportedRequestKinds {
			if requestKindToCapabilityKind[requestKind] != definition.Kind {
				return ErrInvalidCapabilityDefinition
			}
		}
		for _, dimension := range definition.SupportedDimensions {
			if requestKindToDimensionForCapability(definition.Kind) != dimension {
				return ErrInvalidCapabilityDefinition
			}
		}
		for _, dimension := range definition.SupportedQualityDimensions {
			if dimension != QualityReliability && dimension != QualityCompleteness && dimension != QualityFreshness {
				return ErrInvalidCapabilityDefinition
			}
		}
		last = definition.Kind
	}
	return nil
}

func requestKindToDimensionForCapability(kind CapabilityKind) string {
	for requestKind, candidateKind := range requestKindToCapabilityKind {
		if candidateKind == kind {
			return requestKindToDimension[requestKind]
		}
	}
	return ""
}

func validCapabilityKind(kind CapabilityKind) bool {
	for _, value := range allCapabilityKinds() {
		if value == kind {
			return true
		}
	}
	return false
}

type classOrder int

const (
	classCost classOrder = iota
	classLatency
	classSensitivity
)

func validClass(value string, kind classOrder) bool {
	switch kind {
	case classCost:
		return value == string(CapabilityCostLow) || value == string(CapabilityCostMedium) || value == string(CapabilityCostHigh) || value == string(CapabilityCostUnknown)
	case classLatency:
		return value == string(CapabilityLatencyImmediate) || value == string(CapabilityLatencyShort) || value == string(CapabilityLatencyExtended) || value == string(CapabilityLatencyUnknown)
	case classSensitivity:
		return value == string(CapabilitySensitivityLow) || value == string(CapabilitySensitivityModerate) || value == string(CapabilitySensitivityHigh) || value == string(CapabilitySensitivityUnknown)
	default:
		return false
	}
}

func sortedUniqueStrings(values []string) bool {
	for i, value := range values {
		if value == "" || forbiddenCapabilityText(value) || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}

func forbiddenCapabilityText(value string) bool {
	lowered := strings.ToLower(value)
	for _, term := range []string{"camera", "microphone", "pir", "network", "bluetooth", "zigbee", "capture", "record", "probe", "scan", "block", "quarantine", "lock", "alarm", "intrusion", "threat", "authorize", "authorization", "execute", "command", "action"} {
		if strings.Contains(lowered, term) {
			return true
		}
	}
	return false
}
