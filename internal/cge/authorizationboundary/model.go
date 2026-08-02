package authorizationboundary

import (
	"time"

	"synora/internal/cge/capabilitymapping"
)

type AuthorizationScope struct {
	Kind string
	Ref  string
}

type AuthorizationContext struct {
	ID string

	DomainID    string
	PurposeCode AuthorizationPurposeCode

	RequestedScope []AuthorizationScope

	RequestedAt time.Time
	ValidUntil  time.Time

	RequestActorClass string
	RequestOrigin     string

	InteractiveConfirmationAvailable bool

	Revision    uint64
	Fingerprint string
}

type AuthorizationRule struct {
	ID string

	Effect AuthorizationEffect

	PurposeCodes    []AuthorizationPurposeCode
	CapabilityKinds []capabilitymapping.CapabilityKind

	DomainIDs []string

	RequiredScopes []AuthorizationScope
	ExcludedScopes []AuthorizationScope

	MaximumSensitivityClass capabilitymapping.CapabilitySensitivityClass
	MaximumCostClass        capabilitymapping.CapabilityCostClass
	MaximumLatencyClass     capabilitymapping.CapabilityLatencyClass

	RequiresCalibratedQuality bool
	MinimumQualityPermille    int

	RequiredGrantKinds []ExternalGrantKind

	ValidFrom  *time.Time
	ValidUntil *time.Time

	Priority   int
	ReasonCode string
}

type AuthorizationPolicySet struct {
	ID      string
	Version string

	DefaultEffect AuthorizationEffect
	Rules         []AuthorizationRule

	Revision    uint64
	Fingerprint string
}

type ExternalGrant struct {
	ID   ExternalGrantID
	Kind ExternalGrantKind

	SubjectClass string
	DomainID     string

	PurposeCodes    []AuthorizationPurposeCode
	CapabilityKinds []capabilitymapping.CapabilityKind
	Scopes          []AuthorizationScope

	ValidFrom  time.Time
	ValidUntil time.Time

	Revoked   bool
	RevokedAt *time.Time

	IssuerID string

	Revision    uint64
	Fingerprint string
}

type ExternalGrantSnapshot struct {
	Revision uint64
	Grants   []ExternalGrant
	Index    map[ExternalGrantID]int

	Fingerprint string
}

type AuthorizationPolicyConflict struct {
	ID string

	CandidateID string
	RuleIDs     []string
	Effects     []AuthorizationEffect

	ReasonCode  string
	Fingerprint string
}

type AuthorizationCandidateAssessment struct {
	ID string

	MappingCandidateID   string
	CapabilityInstanceID capabilitymapping.CapabilityInstanceID
	CapabilityKind       capabilitymapping.CapabilityKind

	CostClass        capabilitymapping.CapabilityCostClass
	LatencyClass     capabilitymapping.CapabilityLatencyClass
	SensitivityClass capabilitymapping.CapabilitySensitivityClass

	Status   AuthorizationEligibilityStatus
	Eligible bool

	AppliedRuleIDs      []string
	DenyingRuleIDs      []string
	ConfirmationRuleIDs []string
	DeferredRuleIDs     []string

	SatisfiedGrantIDs []ExternalGrantID
	MissingGrantKinds []ExternalGrantKind
	RejectedGrantIDs  []ExternalGrantID

	SatisfiedConditions []string
	MissingConditions   []string
	ViolatedConditions  []string

	PolicyCoveragePermille int
	GrantCoveragePermille  int
	ScopeCoveragePermille  int
	EligibilityPermille    int

	ReasonCodes []string

	SourceMappingCandidateFingerprint string
	SourcePolicyFingerprint           string
	SourceGrantSnapshotFingerprint    string
	SourceContextFingerprint          string

	Fingerprint string
}

type AuthorizationBoundaryAssessment struct {
	RequestID string
	EpisodeID string
	Status    AuthorizationAssessmentStatus

	SourceMappingAssessmentFingerprint string
	SourcePolicyFingerprint            string
	SourceGrantSnapshotFingerprint     string
	SourceContextFingerprint           string

	Candidates []AuthorizationCandidateAssessment
	Conflicts  []AuthorizationPolicyConflict

	PreferredEligibleCandidateID string
	PreferredMarginPermille      int

	EligibleCandidateCount    int
	DeniedCandidateCount      int
	ConfirmationRequiredCount int

	AuthorizationEligible        bool
	AuthorizationAmbiguous       bool
	ExternalConfirmationRequired bool
	DeniedByDefault              bool

	Revision    uint64
	Fingerprint string
}

type AnalysisInput struct {
	Mapping   capabilitymapping.CapabilityMappingAssessment
	Context   AuthorizationContext
	PolicySet AuthorizationPolicySet
	Grants    ExternalGrantSnapshot

	PreviousAssessment *AuthorizationBoundaryAssessment
}

type AuthorizationAssessmentUpdate struct {
	Before AuthorizationBoundaryAssessment
	After  AuthorizationBoundaryAssessment
}

type AuthorizationAssessmentInvalidation struct {
	RequestID  string
	ReasonCode string
}

type AuthorizationPlan struct {
	RequestID string

	SourceMappingFingerprint       string
	SourcePolicyFingerprint        string
	SourceGrantSnapshotFingerprint string
	SourceContextFingerprint       string
	SourceRegistryRevision         uint64

	Creates     []AuthorizationBoundaryAssessment
	Updates     []AuthorizationAssessmentUpdate
	Invalidates []AuthorizationAssessmentInvalidation

	ResultingAssessment AuthorizationBoundaryAssessment
	ReasonCodes         []string
	Fingerprint         string
}

type RegistrySnapshot struct {
	Revision uint64

	Assessments []AuthorizationBoundaryAssessment

	RequestIndex    map[string]int
	CapabilityIndex map[capabilitymapping.CapabilityInstanceID][]string

	PolicyFingerprint string
	Digest            string
}

type ApplyResult struct {
	Applied          bool
	Idempotent       bool
	Before           AuthorizationBoundaryAssessment
	After            AuthorizationBoundaryAssessment
	RegistryRevision uint64
}

func (s AuthorizationScope) clone() AuthorizationScope { return s }

func (r AuthorizationRule) clone() AuthorizationRule { return r.Clone() }

func (s AuthorizationPolicySet) clone() AuthorizationPolicySet { return s.Clone() }

func (g ExternalGrant) clone() ExternalGrant { return g.Clone() }

func (s ExternalGrantSnapshot) clone() ExternalGrantSnapshot { return s.Clone() }

func (c AuthorizationPolicyConflict) Clone() AuthorizationPolicyConflict {
	out := c
	out.RuleIDs = append([]string(nil), c.RuleIDs...)
	out.Effects = append([]AuthorizationEffect(nil), c.Effects...)
	return out
}

func (c AuthorizationCandidateAssessment) Clone() AuthorizationCandidateAssessment {
	out := c
	out.AppliedRuleIDs = append([]string(nil), c.AppliedRuleIDs...)
	out.DenyingRuleIDs = append([]string(nil), c.DenyingRuleIDs...)
	out.ConfirmationRuleIDs = append([]string(nil), c.ConfirmationRuleIDs...)
	out.DeferredRuleIDs = append([]string(nil), c.DeferredRuleIDs...)
	out.SatisfiedGrantIDs = append([]ExternalGrantID(nil), c.SatisfiedGrantIDs...)
	out.MissingGrantKinds = append([]ExternalGrantKind(nil), c.MissingGrantKinds...)
	out.RejectedGrantIDs = append([]ExternalGrantID(nil), c.RejectedGrantIDs...)
	out.SatisfiedConditions = append([]string(nil), c.SatisfiedConditions...)
	out.MissingConditions = append([]string(nil), c.MissingConditions...)
	out.ViolatedConditions = append([]string(nil), c.ViolatedConditions...)
	out.ReasonCodes = append([]string(nil), c.ReasonCodes...)
	return out
}

func (a AuthorizationBoundaryAssessment) Clone() AuthorizationBoundaryAssessment {
	out := a
	out.Candidates = make([]AuthorizationCandidateAssessment, len(a.Candidates))
	for i, candidate := range a.Candidates {
		out.Candidates[i] = candidate.Clone()
	}
	out.Conflicts = make([]AuthorizationPolicyConflict, len(a.Conflicts))
	for i, conflict := range a.Conflicts {
		out.Conflicts[i] = conflict.Clone()
	}
	return out
}

func (u AuthorizationAssessmentUpdate) Clone() AuthorizationAssessmentUpdate {
	return AuthorizationAssessmentUpdate{Before: u.Before.Clone(), After: u.After.Clone()}
}

func (p AuthorizationPlan) Clone() AuthorizationPlan {
	out := p
	out.Creates = make([]AuthorizationBoundaryAssessment, len(p.Creates))
	for i, assessment := range p.Creates {
		out.Creates[i] = assessment.Clone()
	}
	out.Updates = make([]AuthorizationAssessmentUpdate, len(p.Updates))
	for i, update := range p.Updates {
		out.Updates[i] = update.Clone()
	}
	out.Invalidates = append([]AuthorizationAssessmentInvalidation(nil), p.Invalidates...)
	out.ResultingAssessment = p.ResultingAssessment.Clone()
	out.ReasonCodes = append([]string(nil), p.ReasonCodes...)
	return out
}

func (s RegistrySnapshot) Clone() RegistrySnapshot {
	out := s
	out.Assessments = make([]AuthorizationBoundaryAssessment, len(s.Assessments))
	for i, assessment := range s.Assessments {
		out.Assessments[i] = assessment.Clone()
	}
	out.RequestIndex = make(map[string]int, len(s.RequestIndex))
	for key, value := range s.RequestIndex {
		out.RequestIndex[key] = value
	}
	out.CapabilityIndex = make(map[capabilitymapping.CapabilityInstanceID][]string, len(s.CapabilityIndex))
	for key, value := range s.CapabilityIndex {
		out.CapabilityIndex[key] = append([]string(nil), value...)
	}
	return out
}
