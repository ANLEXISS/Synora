package authorizationboundary

import "synora/internal/cge/capabilitymapping"

type AuthorizationEffect string

const (
	EffectAllowEligibility            AuthorizationEffect = "allow_eligibility"
	EffectDeny                        AuthorizationEffect = "deny"
	EffectRequireExternalConfirmation AuthorizationEffect = "require_external_confirmation"
	EffectDefer                       AuthorizationEffect = "defer"

	AuthorizationAllowEligibility            = EffectAllowEligibility
	AuthorizationDeny                        = EffectDeny
	AuthorizationRequireExternalConfirmation = EffectRequireExternalConfirmation
	AuthorizationDefer                       = EffectDefer
	AllowEligibility                         = EffectAllowEligibility
	Deny                                     = EffectDeny
	RequireExternalConfirmation              = EffectRequireExternalConfirmation
	Defer                                    = EffectDefer
)

type AuthorizationPurposeCode string

const (
	PurposeResolveIdentityAmbiguity       AuthorizationPurposeCode = "resolve_identity_ambiguity"
	PurposeVerifyIdentityContinuity       AuthorizationPurposeCode = "verify_identity_continuity"
	PurposeVerifySpatialContinuity        AuthorizationPurposeCode = "verify_spatial_continuity"
	PurposeVerifyContextState             AuthorizationPurposeCode = "verify_context_state"
	PurposeVerifySourceConsistency        AuthorizationPurposeCode = "verify_source_consistency"
	PurposeObserveTemporalRepetition      AuthorizationPurposeCode = "observe_temporal_repetition"
	PurposeVerifyPatternAlignment         AuthorizationPurposeCode = "verify_pattern_alignment"
	PurposeVerifyEntityMultiplicity       AuthorizationPurposeCode = "verify_entity_multiplicity"
	PurposeImproveInformationCompleteness AuthorizationPurposeCode = "improve_information_completeness"
)

type AuthorizationEligibilityStatus string

const (
	EligibilityCandidate                    AuthorizationEligibilityStatus = "candidate"
	EligibilityEligible                     AuthorizationEligibilityStatus = "eligible"
	EligibilityDenied                       AuthorizationEligibilityStatus = "denied"
	EligibilityDeniedByDefault              AuthorizationEligibilityStatus = "denied_by_default"
	EligibilityRequiresExternalConfirmation AuthorizationEligibilityStatus = "requires_external_confirmation"
	EligibilityDeferred                     AuthorizationEligibilityStatus = "deferred"
	EligibilityMappingUnavailable           AuthorizationEligibilityStatus = "mapping_unavailable"
	EligibilityPolicyConflict               AuthorizationEligibilityStatus = "policy_conflict"
	EligibilityObsolete                     AuthorizationEligibilityStatus = "obsolete"
	EligibilityInvalidated                  AuthorizationEligibilityStatus = "invalidated"
)

const (
	StatusCandidate                    = EligibilityCandidate
	StatusEligible                     = EligibilityEligible
	StatusDenied                       = EligibilityDenied
	StatusDeniedByDefault              = EligibilityDeniedByDefault
	StatusRequiresExternalConfirmation = EligibilityRequiresExternalConfirmation
	StatusDeferred                     = EligibilityDeferred
	StatusMappingUnavailable           = EligibilityMappingUnavailable
	StatusPolicyConflict               = EligibilityPolicyConflict
	StatusObsolete                     = EligibilityObsolete
	StatusInvalidated                  = EligibilityInvalidated
)

const (
	AuthorizationStatusCandidate                    = EligibilityCandidate
	AuthorizationStatusEligible                     = EligibilityEligible
	AuthorizationStatusDenied                       = EligibilityDenied
	AuthorizationStatusDeniedByDefault              = EligibilityDeniedByDefault
	AuthorizationStatusRequiresExternalConfirmation = EligibilityRequiresExternalConfirmation
	AuthorizationStatusDeferred                     = EligibilityDeferred
	AuthorizationStatusMappingUnavailable           = EligibilityMappingUnavailable
	AuthorizationStatusPolicyConflict               = EligibilityPolicyConflict
	AuthorizationStatusObsolete                     = EligibilityObsolete
	AuthorizationStatusInvalidated                  = EligibilityInvalidated
)

type ExternalGrantKind string
type ExternalGrantID string

const (
	GrantOwnerPolicy          ExternalGrantKind = "owner_policy_grant"
	GrantOperatorConfirmation ExternalGrantKind = "operator_confirmation"
	GrantPrivacyConsent       ExternalGrantKind = "privacy_consent"
	GrantDomainApproval       ExternalGrantKind = "domain_approval"
	GrantMaintenanceWindow    ExternalGrantKind = "maintenance_window_grant"
)

func allPurposes() []AuthorizationPurposeCode {
	return []AuthorizationPurposeCode{
		PurposeResolveIdentityAmbiguity,
		PurposeVerifyIdentityContinuity,
		PurposeVerifySpatialContinuity,
		PurposeVerifyContextState,
		PurposeVerifySourceConsistency,
		PurposeObserveTemporalRepetition,
		PurposeVerifyPatternAlignment,
		PurposeVerifyEntityMultiplicity,
		PurposeImproveInformationCompleteness,
	}
}

type AuthorizationAssessmentStatus string

const (
	AssessmentActive               AuthorizationAssessmentStatus = "active"
	AssessmentEligible             AuthorizationAssessmentStatus = "eligible"
	AssessmentDenied               AuthorizationAssessmentStatus = "denied"
	AssessmentConfirmationRequired AuthorizationAssessmentStatus = "confirmation_required"
	AssessmentDeferred             AuthorizationAssessmentStatus = "deferred"
	AssessmentObsolete             AuthorizationAssessmentStatus = "obsolete"
	AssessmentInvalidated          AuthorizationAssessmentStatus = "invalidated"
)

func validPurpose(value AuthorizationPurposeCode) bool {
	for _, purpose := range allPurposes() {
		if purpose == value {
			return true
		}
	}
	return false
}

func purposeForCapability(kind capabilitymapping.CapabilityKind) AuthorizationPurposeCode {
	switch kind {
	case capabilitymapping.CapabilityIdentityObservation:
		return PurposeResolveIdentityAmbiguity
	case capabilitymapping.CapabilityIdentityContinuityObservation:
		return PurposeVerifyIdentityContinuity
	case capabilitymapping.CapabilitySpatialRelationObservation:
		return PurposeVerifySpatialContinuity
	case capabilitymapping.CapabilityContextStateObservation:
		return PurposeVerifyContextState
	case capabilitymapping.CapabilitySourceConsistencyObservation:
		return PurposeVerifySourceConsistency
	case capabilitymapping.CapabilityTemporalRepetitionObservation:
		return PurposeObserveTemporalRepetition
	case capabilitymapping.CapabilityPatternAlignmentObservation:
		return PurposeVerifyPatternAlignment
	case capabilitymapping.CapabilityEntityMultiplicityObservation:
		return PurposeVerifyEntityMultiplicity
	case capabilitymapping.CapabilityInformationCompletenessObservation:
		return PurposeImproveInformationCompleteness
	default:
		return ""
	}
}

func validEffect(value AuthorizationEffect) bool {
	return value == EffectAllowEligibility || value == EffectDeny || value == EffectRequireExternalConfirmation || value == EffectDefer
}

func validEligibilityStatus(value AuthorizationEligibilityStatus) bool {
	return value == EligibilityCandidate || value == EligibilityEligible || value == EligibilityDenied || value == EligibilityDeniedByDefault || value == EligibilityRequiresExternalConfirmation || value == EligibilityDeferred || value == EligibilityMappingUnavailable || value == EligibilityPolicyConflict || value == EligibilityObsolete || value == EligibilityInvalidated
}

func validGrantKind(value ExternalGrantKind) bool {
	return value == GrantOwnerPolicy || value == GrantOperatorConfirmation || value == GrantPrivacyConsent || value == GrantDomainApproval || value == GrantMaintenanceWindow
}
