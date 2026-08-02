package authorizationboundary

import (
	"sort"

	"synora/internal/cge/capabilitymapping"
)

func Analyze(input AnalysisInput, policy Policy) (AuthorizationBoundaryAssessment, error) {
	if err := policy.Validate(); err != nil {
		return AuthorizationBoundaryAssessment{}, err
	}
	if err := ValidateAuthorizationContext(input.Context); err != nil {
		return AuthorizationBoundaryAssessment{}, err
	}
	if !input.Context.RequestedAt.Before(input.Context.ValidUntil) {
		return AuthorizationBoundaryAssessment{}, ErrContextExpired
	}
	if err := ValidatePolicySet(input.PolicySet); err != nil {
		return AuthorizationBoundaryAssessment{}, err
	}
	if len(input.PolicySet.Rules) > policy.MaxRules {
		return AuthorizationBoundaryAssessment{}, ErrRuleLimitReached
	}
	if err := ValidateGrantSnapshot(input.Grants); err != nil {
		return AuthorizationBoundaryAssessment{}, err
	}
	if len(input.Grants.Grants) > policy.MaxGrants {
		return AuthorizationBoundaryAssessment{}, ErrGrantLimitReached
	}
	if len(input.Context.RequestedScope) > policy.MaxScopes {
		return AuthorizationBoundaryAssessment{}, ErrScopeLimitReached
	}
	if err := validateMapping(input.Mapping); err != nil {
		return AuthorizationBoundaryAssessment{}, err
	}

	assessment := AuthorizationBoundaryAssessment{
		RequestID:                          string(input.Mapping.RequestID),
		EpisodeID:                          input.Mapping.EpisodeID,
		SourceMappingAssessmentFingerprint: input.Mapping.Fingerprint,
		SourcePolicyFingerprint:            input.PolicySet.Fingerprint,
		SourceGrantSnapshotFingerprint:     input.Grants.Fingerprint,
		SourceContextFingerprint:           input.Context.Fingerprint,
		Revision:                           1,
	}
	if input.PreviousAssessment != nil {
		if err := validateAssessment(*input.PreviousAssessment); err != nil {
			return AuthorizationBoundaryAssessment{}, err
		}
		if input.PreviousAssessment.RequestID != assessment.RequestID {
			return AuthorizationBoundaryAssessment{}, ErrSourceRevisionConflict
		}
		assessment.Revision = input.PreviousAssessment.Revision + 1
	}

	for _, mappingCandidate := range input.Mapping.Candidates {
		candidate, conflicts := evaluateCandidate(mappingCandidate, input.Context, input.PolicySet, input.Grants, policy)
		if len(candidate.ReasonCodes) > policy.MaxReasonCodes {
			return AuthorizationBoundaryAssessment{}, ErrReasonLimitReached
		}
		assessment.Candidates = append(assessment.Candidates, candidate)
		assessment.Conflicts = append(assessment.Conflicts, conflicts...)
	}
	rankCandidates(assessment.Candidates)
	sortConflicts(assessment.Conflicts)
	if len(assessment.Candidates) > policy.MaxCandidates {
		if policy.PreserveDeniedCandidates {
			assessment.Candidates = append([]AuthorizationCandidateAssessment(nil), assessment.Candidates[:policy.MaxCandidates]...)
		} else {
			eligible := make([]AuthorizationCandidateAssessment, 0, policy.MaxCandidates)
			for _, candidate := range assessment.Candidates {
				if candidate.Eligible && len(eligible) < policy.MaxCandidates {
					eligible = append(eligible, candidate)
				}
			}
			assessment.Candidates = eligible
		}
	}
	for _, candidate := range assessment.Candidates {
		switch candidate.Status {
		case EligibilityEligible:
			assessment.EligibleCandidateCount++
		case EligibilityRequiresExternalConfirmation:
			assessment.ConfirmationRequiredCount++
		case EligibilityDenied, EligibilityDeniedByDefault, EligibilityPolicyConflict:
			assessment.DeniedCandidateCount++
		}
	}
	assessment.AuthorizationEligible = assessment.EligibleCandidateCount > 0
	assessment.ExternalConfirmationRequired = assessment.ConfirmationRequiredCount > 0
	assessment.DeniedByDefault = len(assessment.Candidates) > 0 && assessment.EligibleCandidateCount == 0 && assessment.ConfirmationRequiredCount == 0 && allStatus(assessment.Candidates, EligibilityDeniedByDefault)
	assessment.Status = assessmentStatus(assessment)
	eligible := eligibleCandidates(assessment.Candidates)
	if len(eligible) > 1 && sameEligibilityRank(eligible[0], eligible[1]) {
		assessment.AuthorizationAmbiguous = true
	}
	if len(eligible) > 0 {
		margin := 1000
		if len(eligible) > 1 {
			margin = eligible[0].EligibilityPermille - eligible[1].EligibilityPermille
			if margin < 0 {
				margin = 0
			}
		}
		assessment.PreferredMarginPermille = margin
		if !assessment.AuthorizationAmbiguous && margin >= policy.MinPreferredMarginPermille && eligible[0].EligibilityPermille >= policy.MinEligibilityPermille {
			assessment.PreferredEligibleCandidateID = eligible[0].ID
		}
	}
	assessment.Fingerprint = assessmentFingerprint(assessment)
	return assessment, nil
}

func assessmentStatus(assessment AuthorizationBoundaryAssessment) AuthorizationAssessmentStatus {
	if assessment.EligibleCandidateCount > 0 {
		return AssessmentEligible
	}
	if assessment.ConfirmationRequiredCount > 0 {
		return AssessmentConfirmationRequired
	}
	for _, candidate := range assessment.Candidates {
		if candidate.Status == EligibilityDeferred {
			return AssessmentDeferred
		}
		if candidate.Status == EligibilityInvalidated {
			return AssessmentInvalidated
		}
		if candidate.Status == EligibilityObsolete {
			return AssessmentObsolete
		}
	}
	if len(assessment.Candidates) > 0 {
		return AssessmentDenied
	}
	return AssessmentActive
}

func validateMapping(mapping capabilitymapping.CapabilityMappingAssessment) error {
	if mapping.RequestID == "" || mapping.EpisodeID == "" || mapping.Fingerprint == "" || mapping.SourceRequestFingerprint == "" || mapping.CatalogFingerprint == "" || capabilitymapping.CapabilityMappingAssessmentFingerprint(mapping) != mapping.Fingerprint {
		return ErrInvalidMappingAssessment
	}
	for _, candidate := range mapping.Candidates {
		if candidate.ID == "" || candidate.CapabilityInstanceID == "" || candidate.CapabilityKind == "" || candidate.Fingerprint == "" || capabilitymapping.CapabilityMappingCandidateFingerprint(candidate) != candidate.Fingerprint {
			return ErrInvalidMappingAssessment
		}
		for _, value := range []int{candidate.CompatibilityPermille, candidate.QualityPermille, candidate.ConstraintPermille, candidate.ScopePermille, candidate.AvailabilityPermille, candidate.UtilityPermille} {
			if value < 0 || value > 1000 {
				return ErrInvalidMappingAssessment
			}
		}
	}
	return nil
}

func evaluateCandidate(mappingCandidate capabilitymapping.CapabilityMappingCandidate, context AuthorizationContext, set AuthorizationPolicySet, grants ExternalGrantSnapshot, policy Policy) (AuthorizationCandidateAssessment, []AuthorizationPolicyConflict) {
	candidate := AuthorizationCandidateAssessment{ID: authorizationCandidateID(mappingCandidate.ID), MappingCandidateID: mappingCandidate.ID, CapabilityInstanceID: mappingCandidate.CapabilityInstanceID, CapabilityKind: mappingCandidate.CapabilityKind, CostClass: mappingCandidate.CostClass, LatencyClass: mappingCandidate.LatencyClass, SensitivityClass: mappingCandidate.SensitivityClass, Status: EligibilityCandidate, SourceMappingCandidateFingerprint: mappingCandidate.Fingerprint, SourcePolicyFingerprint: set.Fingerprint, SourceGrantSnapshotFingerprint: grants.Fingerprint, SourceContextFingerprint: context.Fingerprint}
	addReason := func(value string) {
		if value != "" {
			candidate.ReasonCodes = append(candidate.ReasonCodes, value)
		}
	}
	if mappingCandidate.Status == capabilitymapping.MappingInvalidated {
		candidate.Status = EligibilityInvalidated
		addReason("mapping.invalidated")
		return finalizeCandidate(candidate), nil
	}
	if mappingCandidate.Status == capabilitymapping.MappingObsolete {
		candidate.Status = EligibilityObsolete
		addReason("mapping.obsolete")
		return finalizeCandidate(candidate), nil
	}
	if mappingCandidate.Status == capabilitymapping.MappingUnavailable {
		candidate.Status = EligibilityMappingUnavailable
		addReason("mapping.unavailable")
		return finalizeCandidate(candidate), nil
	}
	if !mappingCandidate.Compatible {
		candidate.Status = EligibilityDenied
		addReason("mapping.incompatible")
		return finalizeCandidate(candidate), nil
	}
	if purposeForCapability(mappingCandidate.CapabilityKind) != context.PurposeCode {
		candidate.Status = EligibilityDenied
		candidate.MissingConditions = append(candidate.MissingConditions, "purpose")
		addReason("authorization.purpose_mismatch")
		return finalizeCandidate(candidate), nil
	}

	rules := applicableRules(set, context, mappingCandidate)
	if len(rules) == 0 {
		candidate.Status = EligibilityDeniedByDefault
		candidate.MissingConditions = append(candidate.MissingConditions, "explicit_allow_rule")
		addReason("authorization.denied_by_default")
		return finalizeCandidate(candidate), nil
	}

	var effectiveAllow, effectiveDeny, effectiveConfirm, effectiveDefer []AuthorizationRule
	for _, rule := range rules {
		candidate.AppliedRuleIDs = append(candidate.AppliedRuleIDs, rule.ID)
		ok, reasons, satisfied, missing, satisfiedGrants, missingGrants, rejectedGrants := evaluateRuleConditions(rule, mappingCandidate, context, grants, policy)
		candidate.SatisfiedConditions = append(candidate.SatisfiedConditions, satisfied...)
		candidate.MissingConditions = append(candidate.MissingConditions, missing...)
		candidate.ViolatedConditions = append(candidate.ViolatedConditions, reasons...)
		candidate.SatisfiedGrantIDs = append(candidate.SatisfiedGrantIDs, satisfiedGrants...)
		candidate.MissingGrantKinds = append(candidate.MissingGrantKinds, missingGrants...)
		if policy.PreserveRejectedGrants {
			candidate.RejectedGrantIDs = append(candidate.RejectedGrantIDs, rejectedGrants...)
		}
		if !ok {
			continue
		}
		switch rule.Effect {
		case EffectDeny:
			candidate.DenyingRuleIDs = append(candidate.DenyingRuleIDs, rule.ID)
			effectiveDeny = append(effectiveDeny, rule)
		case EffectAllowEligibility:
			effectiveAllow = append(effectiveAllow, rule)
		case EffectRequireExternalConfirmation:
			candidate.ConfirmationRuleIDs = append(candidate.ConfirmationRuleIDs, rule.ID)
			effectiveConfirm = append(effectiveConfirm, rule)
		case EffectDefer:
			candidate.DeferredRuleIDs = append(candidate.DeferredRuleIDs, rule.ID)
			effectiveDefer = append(effectiveDefer, rule)
		}
	}

	conflicts := make([]AuthorizationPolicyConflict, 0)
	if len(effectiveAllow) > 0 && len(effectiveDeny) > 0 {
		conflict := AuthorizationPolicyConflict{ID: authorizationConflictID(candidate.ID), CandidateID: candidate.ID, ReasonCode: "authorization.policy_conflict"}
		for _, rule := range append(append([]AuthorizationRule(nil), effectiveAllow...), effectiveDeny...) {
			conflict.RuleIDs = append(conflict.RuleIDs, rule.ID)
			conflict.Effects = append(conflict.Effects, rule.Effect)
		}
		conflict.Fingerprint = conflictFingerprint(conflict)
		conflicts = append(conflicts, conflict)
		candidate.Status = EligibilityPolicyConflict
		addReason("authorization.policy_conflict")
		return finalizeCandidate(candidate), conflicts
	}
	if len(effectiveDeny) > 0 {
		candidate.Status = EligibilityDenied
		addReason("authorization.explicit_deny")
		return finalizeCandidate(candidate), conflicts
	}
	if mappingCandidate.SensitivityClass == capabilitymapping.CapabilitySensitivityHigh {
		privacyGrantValid := false
		for _, grant := range grants.Grants {
			if grant.Kind != GrantPrivacyConsent {
				continue
			}
			state := grantMatches(grant, context, mappingCandidate, context.PurposeCode, context.RequestedScope)
			if state == grantValid {
				privacyGrantValid = true
				candidate.SatisfiedGrantIDs = append(candidate.SatisfiedGrantIDs, grant.ID)
			} else {
				candidate.ReasonCodes = append(candidate.ReasonCodes, string(state))
				candidate.RejectedGrantIDs = append(candidate.RejectedGrantIDs, grant.ID)
			}
		}
		if !privacyGrantValid {
			candidate.Status = EligibilityRequiresExternalConfirmation
			candidate.MissingGrantKinds = append(candidate.MissingGrantKinds, GrantPrivacyConsent)
			candidate.MissingConditions = append(candidate.MissingConditions, "high_sensitivity_consent")
			addReason("authorization.high_sensitivity_confirmation_required")
			return finalizeCandidate(candidate), conflicts
		}
	}
	if len(effectiveConfirm) > 0 || len(candidate.MissingGrantKinds) > 0 {
		candidate.Status = EligibilityRequiresExternalConfirmation
		addReason("authorization.external_confirmation_required")
		return finalizeCandidate(candidate), conflicts
	}
	if len(effectiveDefer) > 0 {
		candidate.Status = EligibilityDeferred
		addReason("authorization.deferred")
		return finalizeCandidate(candidate), conflicts
	}
	if len(effectiveAllow) == 0 {
		candidate.Status = EligibilityDeniedByDefault
		addReason("authorization.no_effective_allow")
		return finalizeCandidate(candidate), conflicts
	}
	candidate.Status = EligibilityEligible
	candidate.Eligible = true
	candidate.EligibilityPermille = 1000
	addReason("authorization.eligible")
	return finalizeCandidate(candidate), conflicts
}

func evaluateRuleConditions(rule AuthorizationRule, mappingCandidate capabilitymapping.CapabilityMappingCandidate, context AuthorizationContext, grants ExternalGrantSnapshot, policy Policy) (bool, []string, []string, []string, []ExternalGrantID, []ExternalGrantKind, []ExternalGrantID) {
	reasons, satisfied, missing := []string{}, []string{}, []string{}
	satisfiedGrants, missingGrants, rejectedGrants := []ExternalGrantID{}, []ExternalGrantKind{}, []ExternalGrantID{}
	if classExceeded(string(mappingCandidate.SensitivityClass), string(rule.MaximumSensitivityClass), 2) || policy.UnknownMeansDenied && mappingCandidate.SensitivityClass == capabilitymapping.CapabilitySensitivityUnknown && rule.MaximumSensitivityClass != "" {
		missing = append(missing, "sensitivity")
		reasons = append(reasons, "authorization.sensitivity_exceeded")
	} else {
		satisfied = append(satisfied, "sensitivity")
	}
	if classExceeded(string(mappingCandidate.CostClass), string(rule.MaximumCostClass), 0) || policy.UnknownMeansDenied && mappingCandidate.CostClass == capabilitymapping.CapabilityCostUnknown && rule.MaximumCostClass != "" {
		missing = append(missing, "cost")
		reasons = append(reasons, "authorization.cost_exceeded")
	} else {
		satisfied = append(satisfied, "cost")
	}
	if classExceeded(string(mappingCandidate.LatencyClass), string(rule.MaximumLatencyClass), 1) || policy.UnknownMeansDenied && mappingCandidate.LatencyClass == capabilitymapping.CapabilityLatencyUnknown && rule.MaximumLatencyClass != "" {
		missing = append(missing, "latency")
		reasons = append(reasons, "authorization.latency_exceeded")
	} else {
		satisfied = append(satisfied, "latency")
	}
	if mappingCandidate.QualityPermille < rule.MinimumQualityPermille {
		missing = append(missing, "quality")
		reasons = append(reasons, "authorization.quality_insufficient")
	} else {
		satisfied = append(satisfied, "quality")
	}
	if rule.RequiresCalibratedQuality && !mappingCandidate.QualityCalibrated {
		missing = append(missing, "calibrated_quality")
		reasons = append(reasons, "authorization.quality_uncalibrated")
	} else {
		satisfied = append(satisfied, "calibration")
	}
	if mappingCandidate.ScopePermille < 1000 {
		missing = append(missing, "mapping_scope")
		reasons = append(reasons, "authorization.scope_insufficient")
	} else {
		satisfied = append(satisfied, "mapping_scope")
	}
	for _, requiredKind := range rule.RequiredGrantKinds {
		matched := false
		for _, grant := range grants.Grants {
			if grant.Kind != requiredKind {
				continue
			}
			state := grantMatches(grant, context, mappingCandidate, context.PurposeCode, context.RequestedScope)
			if state == grantValid {
				matched = true
				satisfiedGrants = append(satisfiedGrants, grant.ID)
				break
			}
			reasons = append(reasons, string(state))
			if policy.PreserveRejectedGrants {
				rejectedGrants = append(rejectedGrants, grant.ID)
			}
		}
		if !matched {
			missingGrants = append(missingGrants, requiredKind)
			missing = append(missing, "grant:"+string(requiredKind))
			reasons = append(reasons, string(grantMissing))
		}
	}
	if len(missing) > 0 {
		return false, uniqueSorted(reasons), uniqueSorted(satisfied), uniqueSorted(missing), uniqueSortedGrantIDs(satisfiedGrants), uniqueSortedGrantKinds(missingGrants), uniqueSortedGrantIDs(rejectedGrants)
	}
	return true, uniqueSorted(reasons), uniqueSorted(satisfied), uniqueSorted(missing), uniqueSortedGrantIDs(satisfiedGrants), uniqueSortedGrantKinds(missingGrants), uniqueSortedGrantIDs(rejectedGrants)
}

func finalizeCandidate(candidate AuthorizationCandidateAssessment) AuthorizationCandidateAssessment {
	candidate.AppliedRuleIDs = uniqueSorted(candidate.AppliedRuleIDs)
	candidate.DenyingRuleIDs = uniqueSorted(candidate.DenyingRuleIDs)
	candidate.ConfirmationRuleIDs = uniqueSorted(candidate.ConfirmationRuleIDs)
	candidate.DeferredRuleIDs = uniqueSorted(candidate.DeferredRuleIDs)
	candidate.SatisfiedGrantIDs = uniqueSortedGrantIDs(candidate.SatisfiedGrantIDs)
	candidate.MissingGrantKinds = uniqueSortedGrantKinds(candidate.MissingGrantKinds)
	candidate.RejectedGrantIDs = uniqueSortedGrantIDs(candidate.RejectedGrantIDs)
	candidate.SatisfiedConditions = uniqueSorted(candidate.SatisfiedConditions)
	candidate.MissingConditions = uniqueSorted(candidate.MissingConditions)
	candidate.ViolatedConditions = uniqueSorted(candidate.ViolatedConditions)
	candidate.ReasonCodes = uniqueSorted(candidate.ReasonCodes)
	candidate.ReasonCodes = uniqueSorted(append(candidate.ReasonCodes, candidate.ViolatedConditions...))
	if candidate.Status == EligibilityEligible {
		candidate.PolicyCoveragePermille, candidate.GrantCoveragePermille, candidate.ScopeCoveragePermille = 1000, 1000, 1000
	} else {
		candidate.PolicyCoveragePermille = coverage(candidate.SatisfiedConditions, candidate.MissingConditions)
		candidate.GrantCoveragePermille = grantCoverage(candidate.SatisfiedGrantIDs, candidate.MissingGrantKinds)
		candidate.ScopeCoveragePermille = 1000
		if containsString(candidate.MissingConditions, "mapping_scope") {
			candidate.ScopeCoveragePermille = 0
		}
		if candidate.EligibilityPermille == 0 {
			candidate.EligibilityPermille = candidate.PolicyCoveragePermille * candidate.GrantCoveragePermille / 1000
			candidate.EligibilityPermille = candidate.EligibilityPermille * candidate.ScopeCoveragePermille / 1000
		}
	}
	if candidate.Status == EligibilityDenied || candidate.Status == EligibilityDeniedByDefault || candidate.Status == EligibilityPolicyConflict || candidate.Status == EligibilityMappingUnavailable || candidate.Status == EligibilityObsolete || candidate.Status == EligibilityInvalidated {
		candidate.EligibilityPermille = 0
	}
	candidate.Fingerprint = candidateFingerprint(candidate)
	return candidate
}

func coverage(satisfied, missing []string) int {
	total := len(satisfied) + len(missing)
	if total == 0 {
		return 0
	}
	return len(satisfied) * 1000 / total
}

func grantCoverage(satisfied []ExternalGrantID, missing []ExternalGrantKind) int {
	total := len(satisfied) + len(missing)
	if total == 0 {
		return 1000
	}
	return len(satisfied) * 1000 / total
}

func allStatus(values []AuthorizationCandidateAssessment, wanted AuthorizationEligibilityStatus) bool {
	for _, value := range values {
		if value.Status != wanted {
			return false
		}
	}
	return true
}

func uniqueSorted(values []string) []string { return uniqueSortedStrings(values) }

func uniqueSortedGrantIDs(values []ExternalGrantID) []ExternalGrantID {
	out := canonicalGrantIDs(values)
	result := out[:0]
	for _, value := range out {
		if value != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}

func uniqueSortedGrantKinds(values []ExternalGrantKind) []ExternalGrantKind {
	out := canonicalGrantKinds(values)
	result := out[:0]
	for _, value := range out {
		if value != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}

func authorizationCandidateID(mappingCandidateID string) string {
	return "authorization-candidate-" + mappingCandidateID
}
func authorizationConflictID(candidateID string) string {
	return "authorization-conflict-" + candidateID
}

func sortConflicts(values []AuthorizationPolicyConflict) {
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
}
