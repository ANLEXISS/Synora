package authorizationboundary

import (
	"sort"

	"synora/internal/cge/capabilitymapping"
)

func ValidatePolicyRule(rule AuthorizationRule) error {
	if rule.ID == "" || rule.Effect == "" || !validEffect(rule.Effect) || len(rule.ID) > maxAuthorizationText || forbiddenAuthorizationText(rule.ID) || forbiddenAuthorizationText(rule.ReasonCode) {
		return ErrInvalidRule
	}
	if rule.MinimumQualityPermille < 0 || rule.MinimumQualityPermille > 1000 || rule.Priority < -1000 || rule.Priority > 1000 {
		return ErrInvalidRule
	}
	if !validAuthorizationClass(string(rule.MaximumSensitivityClass), 2) || !validAuthorizationClass(string(rule.MaximumCostClass), 0) || !validAuthorizationClass(string(rule.MaximumLatencyClass), 1) {
		return ErrInvalidRule
	}
	if !sortedUniquePurposes(rule.PurposeCodes) || !sortedUniqueCapabilityKinds(rule.CapabilityKinds) || !sortedUniqueStrings(rule.DomainIDs) || !validScopes(rule.RequiredScopes) || !validScopes(rule.ExcludedScopes) || !sortedUniqueGrantKinds(rule.RequiredGrantKinds) {
		return ErrInvalidRule
	}
	if rule.ValidFrom != nil && rule.ValidUntil != nil && rule.ValidUntil.Before(*rule.ValidFrom) {
		return ErrInvalidRule
	}
	return nil
}

func (r AuthorizationRule) Clone() AuthorizationRule {
	out := r
	out.PurposeCodes = append([]AuthorizationPurposeCode(nil), r.PurposeCodes...)
	out.CapabilityKinds = append([]capabilitymapping.CapabilityKind(nil), r.CapabilityKinds...)
	out.DomainIDs = append([]string(nil), r.DomainIDs...)
	out.RequiredScopes = append([]AuthorizationScope(nil), r.RequiredScopes...)
	out.ExcludedScopes = append([]AuthorizationScope(nil), r.ExcludedScopes...)
	out.RequiredGrantKinds = append([]ExternalGrantKind(nil), r.RequiredGrantKinds...)
	out.ValidFrom = timePointer(r.ValidFrom)
	out.ValidUntil = timePointer(r.ValidUntil)
	return out
}

func sortedUniquePurposes(values []AuthorizationPurposeCode) bool {
	seen := map[AuthorizationPurposeCode]struct{}{}
	for _, value := range values {
		if !validPurpose(value) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func sortedUniqueCapabilityKinds(values []capabilitymapping.CapabilityKind) bool {
	seen := map[capabilitymapping.CapabilityKind]struct{}{}
	for _, value := range values {
		if value == "" {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func sortedUniqueGrantKinds(values []ExternalGrantKind) bool {
	seen := map[ExternalGrantKind]struct{}{}
	for _, value := range values {
		if !validGrantKind(value) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validAuthorizationClass(value string, kind int) bool {
	if value == "" {
		return true
	}
	switch kind {
	case 0:
		return value == string(capabilitymapping.CapabilityCostLow) || value == string(capabilitymapping.CapabilityCostMedium) || value == string(capabilitymapping.CapabilityCostHigh) || value == string(capabilitymapping.CapabilityCostUnknown)
	case 1:
		return value == string(capabilitymapping.CapabilityLatencyImmediate) || value == string(capabilitymapping.CapabilityLatencyShort) || value == string(capabilitymapping.CapabilityLatencyExtended) || value == string(capabilitymapping.CapabilityLatencyUnknown)
	case 2:
		return value == string(capabilitymapping.CapabilitySensitivityLow) || value == string(capabilitymapping.CapabilitySensitivityModerate) || value == string(capabilitymapping.CapabilitySensitivityHigh) || value == string(capabilitymapping.CapabilitySensitivityUnknown)
	default:
		return false
	}
}

func ruleApplies(rule AuthorizationRule, context AuthorizationContext, candidate capabilitymapping.CapabilityMappingCandidate) bool {
	if len(rule.PurposeCodes) > 0 && !containsPurpose(rule.PurposeCodes, context.PurposeCode) {
		return false
	}
	if len(rule.CapabilityKinds) > 0 && !containsKind(rule.CapabilityKinds, candidate.CapabilityKind) {
		return false
	}
	if len(rule.DomainIDs) > 0 && !containsString(rule.DomainIDs, context.DomainID) {
		return false
	}
	if rule.ValidFrom != nil && context.RequestedAt.Before(*rule.ValidFrom) {
		return false
	}
	if rule.ValidUntil != nil && !context.RequestedAt.Before(*rule.ValidUntil) {
		return false
	}
	if !allScopesContained(context.RequestedScope, rule.RequiredScopes) || anyScopeContained(context.RequestedScope, rule.ExcludedScopes) {
		return false
	}
	return true
}

func containsPurpose(values []AuthorizationPurposeCode, wanted AuthorizationPurposeCode) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsKind(values []capabilitymapping.CapabilityKind, wanted capabilitymapping.CapabilityKind) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sortRules(values []AuthorizationRule) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Priority != values[j].Priority {
			return values[i].Priority > values[j].Priority
		}
		return values[i].ID < values[j].ID
	})
}

func applicableRules(set AuthorizationPolicySet, context AuthorizationContext, candidate capabilitymapping.CapabilityMappingCandidate) []AuthorizationRule {
	values := make([]AuthorizationRule, 0)
	for _, rule := range set.Rules {
		if ruleApplies(rule, context, candidate) {
			values = append(values, rule.Clone())
		}
	}
	sortRules(values)
	return values
}

func classExceeded(value, maximum string, kind int) bool {
	if maximum == "" || maximum == "unknown" || value == "unknown" {
		return false
	}
	valueRank := classRank(value, kind)
	maxRank := classRank(maximum, kind)
	return valueRank > maxRank
}

func classRank(value string, kind int) int {
	if value == "unknown" {
		return 3
	}
	switch kind {
	case 0:
		switch value {
		case string(capabilitymapping.CapabilityCostLow):
			return 0
		case string(capabilitymapping.CapabilityCostMedium):
			return 1
		case string(capabilitymapping.CapabilityCostHigh):
			return 2
		}
	case 1:
		switch value {
		case string(capabilitymapping.CapabilityLatencyImmediate):
			return 0
		case string(capabilitymapping.CapabilityLatencyShort):
			return 1
		case string(capabilitymapping.CapabilityLatencyExtended):
			return 2
		}
	case 2:
		switch value {
		case string(capabilitymapping.CapabilitySensitivityLow):
			return 0
		case string(capabilitymapping.CapabilitySensitivityModerate):
			return 1
		case string(capabilitymapping.CapabilitySensitivityHigh):
			return 2
		}
	}
	return 3
}
