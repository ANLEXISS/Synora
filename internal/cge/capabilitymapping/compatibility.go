package capabilitymapping

import (
	"sort"

	"synora/internal/cge/advisoryrequests"
)

type compatibilityResult struct {
	candidate   CapabilityMappingCandidate
	unavailable bool
}

func evaluateCompatibility(request CapabilityRequirement, definition CapabilityDefinition, instance CapabilityInstance, policy Policy, sourceRequest, sourceInventory string) compatibilityResult {
	result := compatibilityResult{}
	candidate := &result.candidate
	*candidate = CapabilityMappingCandidate{ID: mappingCandidateID(request.RequestID, instance.ID), RequestID: request.RequestID, CapabilityInstanceID: instance.ID, CapabilityKind: instance.Kind, Status: MappingCandidate, CostClass: instance.CostClass, LatencyClass: instance.LatencyClass, SensitivityClass: instance.SensitivityClass, QualityCalibrated: instance.Quality.Calibrated, SourceRequestFingerprint: sourceRequest, SourceInventoryFingerprint: sourceInventory, CompatibilityPermille: 0, QualityPermille: 0, ConstraintPermille: 0, ScopePermille: 0, AvailabilityPermille: 0}
	add := func(list *[]string, value string) {
		if value != "" {
			*list = append(*list, value)
		}
	}
	if !containsCapabilityKind(request.RequiredKinds, instance.Kind) || !containsString(definition.SupportedDimensions, firstOrEmpty(request.RequiredDimensions)) {
		candidate.Status = MappingIncompatible
		add(&candidate.ReasonCodes, "capability.kind_mismatch")
		candidate.MissingRequirements = append(candidate.MissingRequirements, "kind_or_dimension")
		return finalizeCompatibility(result, policy)
	}
	candidate.CompatibilityPermille = 1000
	candidate.SatisfiedRequirements = append(candidate.SatisfiedRequirements, "kind", "dimension")

	switch instance.Status {
	case CapabilityStatusAvailable:
		candidate.AvailabilityPermille = 1000
	case CapabilityStatusDegraded:
		if !policy.AllowDegradedCapabilities || !request.AllowsDegraded {
			candidate.Status = MappingIncompatible
			add(&candidate.ReasonCodes, "capability.degraded_not_allowed")
			add(&candidate.MissingRequirements, "available_status")
			return finalizeCompatibility(result, policy)
		}
		candidate.AvailabilityPermille = 650
		candidate.Status = MappingCompatibleDegraded
		add(&candidate.ReasonCodes, "capability.degraded")
	case CapabilityStatusUnavailable:
		candidate.Status = MappingUnavailable
		result.unavailable = true
		add(&candidate.ReasonCodes, "capability.unavailable")
		candidate.MissingRequirements = append(candidate.MissingRequirements, "available_status")
		return finalizeCompatibility(result, policy)
	case CapabilityStatusUnknown:
		if !policy.AllowUnknownStatus {
			candidate.Status = MappingUnavailable
			result.unavailable = true
			add(&candidate.ReasonCodes, "capability.status_unknown")
			candidate.MissingRequirements = append(candidate.MissingRequirements, "known_status")
			return finalizeCompatibility(result, policy)
		}
		candidate.Status = MappingUnavailable
		result.unavailable = true
		add(&candidate.ReasonCodes, "capability.status_unknown")
		return finalizeCompatibility(result, policy)
	case CapabilityStatusRetired:
		candidate.Status = MappingIncompatible
		add(&candidate.ReasonCodes, "capability.retired")
		candidate.MissingRequirements = append(candidate.MissingRequirements, "active_status")
		return finalizeCompatibility(result, policy)
	case CapabilityStatusInvalidated:
		candidate.Status = MappingInvalidated
		add(&candidate.ReasonCodes, "capability.catalog_mismatch")
		return finalizeCompatibility(result, policy)
	default:
		candidate.Status = MappingInvalidated
		add(&candidate.ReasonCodes, "capability.status_unknown")
		return finalizeCompatibility(result, policy)
	}

	candidate.QualityPermille = qualityScore(instance.Quality)
	if instance.Quality.SourceCount == 0 && candidate.QualityPermille == 0 {
		add(&candidate.ReasonCodes, "capability.quality_unknown")
	}
	if request.MinimumQuality.RequireCalibrated && !instance.Quality.Calibrated {
		candidate.Status = MappingIncompatible
		add(&candidate.ReasonCodes, "capability.quality_uncalibrated")
		candidate.MissingRequirements = append(candidate.MissingRequirements, "calibrated_quality")
		return finalizeCompatibility(result, policy)
	}
	if !instance.Quality.Calibrated {
		add(&candidate.ReasonCodes, "capability.quality_uncalibrated")
		candidate.QualityPermille = clamp(candidate.QualityPermille - 100)
	}
	if candidate.QualityPermille < minimumQuality(request.MinimumQuality) {
		candidate.Status = MappingIncompatible
		add(&candidate.ReasonCodes, "capability.quality_insufficient")
		candidate.MissingRequirements = append(candidate.MissingRequirements, "minimum_quality")
		return finalizeCompatibility(result, policy)
	}
	candidate.SatisfiedRequirements = append(candidate.SatisfiedRequirements, "quality")

	constraintScore, failedHard, violated, satisfied := evaluateConstraints(request.RequiredConstraints, instance.Constraints)
	candidate.ConstraintPermille = constraintScore
	candidate.ViolatedConstraints = append(candidate.ViolatedConstraints, violated...)
	candidate.SatisfiedRequirements = append(candidate.SatisfiedRequirements, satisfied...)
	if failedHard {
		candidate.Status = MappingIncompatible
		add(&candidate.ReasonCodes, "capability.constraint_failed")
		return finalizeCompatibility(result, policy)
	}

	var failedScope bool
	var scopeReason string
	candidate.ScopePermille, failedScope, scopeReason = evaluateScopes(request.RequiredScopes, instance.SupportedScopes, policy.AllowUnknownScope)
	if scopeReason != "" {
		add(&candidate.ReasonCodes, scopeReason)
	}
	if failedScope {
		candidate.Status = MappingIncompatible
		add(&candidate.ReasonCodes, "capability.scope_mismatch")
		candidate.MissingRequirements = append(candidate.MissingRequirements, "scope")
		return finalizeCompatibility(result, policy)
	}
	candidate.SatisfiedRequirements = append(candidate.SatisfiedRequirements, "scope")

	if exceedsClass(string(instance.CostClass), string(request.MaximumCostClass), classCost) {
		candidate.Status = MappingIncompatible
		add(&candidate.ReasonCodes, "capability.cost_exceeded")
		candidate.MissingRequirements = append(candidate.MissingRequirements, "cost_limit")
		return finalizeCompatibility(result, policy)
	}
	if exceedsClass(string(instance.LatencyClass), string(request.MaximumLatencyClass), classLatency) {
		candidate.Status = MappingIncompatible
		add(&candidate.ReasonCodes, "capability.latency_exceeded")
		candidate.MissingRequirements = append(candidate.MissingRequirements, "latency_limit")
		return finalizeCompatibility(result, policy)
	}
	if exceedsClass(string(instance.SensitivityClass), string(request.MaximumSensitivityClass), classSensitivity) {
		candidate.Status = MappingIncompatible
		add(&candidate.ReasonCodes, "capability.sensitivity_exceeded")
		candidate.MissingRequirements = append(candidate.MissingRequirements, "sensitivity_limit")
		return finalizeCompatibility(result, policy)
	}
	candidate.SatisfiedRequirements = append(candidate.SatisfiedRequirements, "descriptive_limits")
	if candidate.Status == MappingCandidate {
		candidate.Status = MappingCompatible
	}
	return finalizeCompatibility(result, policy)
}

func finalizeCompatibility(result compatibilityResult, policy Policy) compatibilityResult {
	candidate := &result.candidate
	candidate.SatisfiedRequirements = uniqueSorted(candidate.SatisfiedRequirements)
	candidate.MissingRequirements = uniqueSorted(candidate.MissingRequirements)
	candidate.ViolatedConstraints = uniqueSorted(candidate.ViolatedConstraints)
	candidate.ReasonCodes = uniqueSorted(candidate.ReasonCodes)
	candidate.Compatible = candidate.Status == MappingCompatible || candidate.Status == MappingCompatibleDegraded
	// Penalty values are filled later from the instance in scoreCandidate.
	_ = policy
	return result
}

func scoreCandidate(candidate CapabilityMappingCandidate, instance CapabilityInstance, policy Policy) CapabilityMappingCandidate {
	candidate.CostPenaltyPermille = classPenalty(string(instance.CostClass), classCost)
	candidate.LatencyPenaltyPermille = classPenalty(string(instance.LatencyClass), classLatency)
	candidate.SensitivityPenaltyPermille = classPenalty(string(instance.SensitivityClass), classSensitivity)
	positive := candidate.CompatibilityPermille*policy.CompatibilityWeightPermille + candidate.QualityPermille*policy.QualityWeightPermille + candidate.ConstraintPermille*policy.ConstraintWeightPermille + candidate.ScopePermille*policy.ScopeWeightPermille + candidate.AvailabilityPermille*policy.AvailabilityWeightPermille
	penalty := candidate.CostPenaltyPermille*policy.CostPenaltyWeightPermille + candidate.LatencyPenaltyPermille*policy.LatencyPenaltyWeightPermille + candidate.SensitivityPenaltyPermille*policy.SensitivityPenaltyWeightPermille
	candidate.UtilityPermille = clamp((positive - penalty) / 1000)
	if !candidate.Compatible {
		candidate.UtilityPermille = 0
	}
	candidate.Fingerprint = candidateFingerprint(candidate)
	return candidate
}

func evaluateConstraints(required, provided []CapabilityConstraint) (score int, failedHard bool, violated, satisfied []string) {
	if len(required) == 0 {
		return 1000, false, nil, []string{"constraints"}
	}
	score = 1000
	for _, requirement := range required {
		found := false
		for _, claim := range provided {
			if claim.Code == requirement.Code && constraintMatches(requirement, claim) {
				found = true
				break
			}
		}
		if found {
			satisfied = append(satisfied, requirement.Code)
		} else {
			violated = append(violated, requirement.Code)
			score -= 250
			if requirement.Hard {
				failedHard = true
			}
		}
	}
	return clamp(score), failedHard, violated, satisfied
}

func constraintMatches(required, provided CapabilityConstraint) bool {
	if required.Code != provided.Code {
		return false
	}
	switch required.Operator {
	case ConstraintPresent:
		return true
	case ConstraintAbsent:
		return false
	case ConstraintEquals:
		return valueEqual(required.Value, provided.Value)
	case ConstraintNotEquals:
		return !valueEqual(required.Value, provided.Value)
	case ConstraintContains:
		return provided.Value.String == required.Value.String || provided.Value.String != "" && required.Value.String != "" && provided.Value.String >= required.Value.String
	case ConstraintMinimum:
		return provided.Value.NumberPermille >= required.Value.NumberPermille
	case ConstraintMaximum:
		return provided.Value.NumberPermille <= required.Value.NumberPermille
	default:
		return false
	}
}

func valueEqual(a, b ConstraintValue) bool {
	return a.String == b.String && a.NumberPermille == b.NumberPermille && boolPointerValue(a.Bool) == boolPointerValue(b.Bool)
}
func boolPointerValue(value *bool) (result bool) {
	if value != nil {
		return *value
	}
	return false
}

func evaluateScopes(required, provided []CapabilityScope, allowUnknown bool) (int, bool, string) {
	if len(required) == 0 {
		return 1000, false, ""
	}
	if len(provided) == 0 {
		if allowUnknown {
			return 500, false, "capability.scope_unknown"
		}
		return 0, true, "capability.scope_unknown"
	}
	matched := 0
	for _, wanted := range required {
		for _, actual := range provided {
			if wanted == actual {
				matched++
				break
			}
		}
	}
	if matched == len(required) {
		return 1000, false, ""
	}
	if allowUnknown && matched > 0 {
		return matched * 1000 / len(required), false, "capability.scope_unknown"
	}
	return matched * 1000 / len(required), true, "capability.scope_mismatch"
}

func qualityScore(q CapabilityQuality) int {
	value := q.ReliabilityPermille
	if q.CompletenessPermille < value {
		value = q.CompletenessPermille
	}
	if q.FreshnessPermille < value {
		value = q.FreshnessPermille
	}
	return clamp(value)
}
func minimumQuality(q CapabilityQualityRequirement) int {
	value := q.ReliabilityPermille
	if q.CompletenessPermille > value {
		value = q.CompletenessPermille
	}
	if q.FreshnessPermille > value {
		value = q.FreshnessPermille
	}
	return value
}
func classPenalty(value string, kind classOrder) int {
	if value == "unknown" {
		return 600
	}
	switch kind {
	case classCost:
		if value == string(CapabilityCostLow) {
			return 0
		}
		if value == string(CapabilityCostMedium) {
			return 500
		}
		return 1000
	case classLatency:
		if value == string(CapabilityLatencyImmediate) {
			return 0
		}
		if value == string(CapabilityLatencyShort) {
			return 500
		}
		return 1000
	case classSensitivity:
		if value == string(CapabilitySensitivityLow) {
			return 0
		}
		if value == string(CapabilitySensitivityModerate) {
			return 500
		}
		return 1000
	}
	return 1000
}
func classRank(value string, kind classOrder) int {
	if value == "unknown" {
		return 3
	}
	switch kind {
	case classCost:
		if value == string(CapabilityCostLow) {
			return 0
		}
		if value == string(CapabilityCostMedium) {
			return 1
		}
		return 2
	case classLatency:
		if value == string(CapabilityLatencyImmediate) {
			return 0
		}
		if value == string(CapabilityLatencyShort) {
			return 1
		}
		return 2
	case classSensitivity:
		if value == string(CapabilitySensitivityLow) {
			return 0
		}
		if value == string(CapabilitySensitivityModerate) {
			return 1
		}
		return 2
	}
	return 3
}
func exceedsClass(value, maximum string, kind classOrder) bool {
	if maximum == "unknown" {
		return false
	}
	if value == "unknown" {
		return true
	}
	return classRank(value, kind) > classRank(maximum, kind)
}
func uniqueSorted(values []string) []string {
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if value != "" && (len(out) == 0 || out[len(out)-1] != value) {
			out = append(out, value)
		}
	}
	return out
}
func containsCapabilityKind(values []CapabilityKind, value CapabilityKind) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
func clamp(value int) int {
	if value < 0 {
		return 0
	}
	if value > 1000 {
		return 1000
	}
	return value
}
func mappingCandidateID(requestID advisoryrequests.AdvisoryRequestID, instanceID CapabilityInstanceID) string {
	return digestJSON("capability-mapping-candidate-", struct{ Request, Instance string }{string(requestID), string(instanceID)})
}
