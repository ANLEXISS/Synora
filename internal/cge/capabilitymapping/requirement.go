package capabilitymapping

import (
	"sort"

	"synora/internal/cge/advisoryrequests"
)

func BuildRequirement(request advisoryrequests.AdvisoryEvidenceRequest, policy Policy) (CapabilityRequirement, error) {
	if err := policy.Validate(); err != nil {
		return CapabilityRequirement{}, err
	}
	if err := validateRequest(request, policy); err != nil {
		return CapabilityRequirement{}, err
	}
	kind := requestKindToCapabilityKind[string(request.Kind)]
	if kind == "" {
		return CapabilityRequirement{}, ErrInvalidRequirement
	}
	requirement := CapabilityRequirement{
		RequestID: request.ID, RequestKey: request.Key,
		RequiredKinds: []CapabilityKind{kind}, RequiredDimensions: []string{string(request.Dimension)}, RequiredFactCodes: append([]string(nil), request.RequiredFactCodes...),
		MinimumQuality:   CapabilityQualityRequirement{ReliabilityPermille: policy.MinQualityPermille, CompletenessPermille: policy.MinQualityPermille, FreshnessPermille: policy.MinQualityPermille, RequireCalibrated: policy.RequireCalibratedQuality},
		MaximumCostClass: CapabilityCostClass(classFromRequest(request.CostClass, classCost)), MaximumLatencyClass: CapabilityLatencyClass(classFromRequest(request.LatencyClass, classLatency)), MaximumSensitivityClass: CapabilitySensitivityClass(classFromRequest(request.SensitivityClass, classSensitivity)), AllowsDegraded: policy.AllowDegradedCapabilities,
	}
	sort.Slice(requirement.RequiredKinds, func(i, j int) bool { return requirement.RequiredKinds[i] < requirement.RequiredKinds[j] })
	sort.Strings(requirement.RequiredDimensions)
	sort.Strings(requirement.RequiredFactCodes)
	requirement.Fingerprint = requirementFingerprint(requirement)
	return requirement, nil
}

func classFromRequest(value string, kind classOrder) string {
	if validClass(value, kind) {
		return value
	}
	return "unknown"
}

func ValidateRequirement(requirement CapabilityRequirement) error {
	if requirement.RequestID == "" || requirement.RequestKey == "" || len(requirement.RequiredKinds) == 0 || len(requirement.RequiredDimensions) == 0 || requirement.Fingerprint == "" || requirementFingerprint(requirement) != requirement.Fingerprint {
		return ErrInvalidRequirement
	}
	if !sortedUniqueCapabilityKinds(requirement.RequiredKinds) || !sortedUniqueStrings(requirement.RequiredDimensions) || !sortedUniqueStrings(requirement.RequiredFactCodes) || !validScopes(requirement.RequiredScopes) || !validConstraints(requirement.RequiredConstraints) {
		return ErrInvalidRequirement
	}
	for _, kind := range requirement.RequiredKinds {
		if !validCapabilityKind(kind) {
			return ErrUnknownCapabilityKind
		}
		if expected := requestKindToDimensionForCapability(kind); expected != "" && len(requirement.RequiredDimensions) == 1 && requirement.RequiredDimensions[0] != expected {
			return ErrInvalidRequirement
		}
	}
	for _, value := range []int{requirement.MinimumQuality.ReliabilityPermille, requirement.MinimumQuality.CompletenessPermille, requirement.MinimumQuality.FreshnessPermille} {
		if value < 0 || value > 1000 {
			return ErrInvalidRequirement
		}
	}
	if !validClass(string(requirement.MaximumCostClass), classCost) || !validClass(string(requirement.MaximumLatencyClass), classLatency) || !validClass(string(requirement.MaximumSensitivityClass), classSensitivity) {
		return ErrInvalidRequirement
	}
	return nil
}

func sortedUniqueCapabilityKinds(values []CapabilityKind) bool {
	for i, value := range values {
		if value == "" || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}
