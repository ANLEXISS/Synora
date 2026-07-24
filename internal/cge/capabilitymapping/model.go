package capabilitymapping

import (
	"sort"

	"synora/internal/cge/advisoryrequests"
)

type CapabilityQualityRequirement struct {
	ReliabilityPermille  int
	CompletenessPermille int
	FreshnessPermille    int
	RequireCalibrated    bool
}

type CapabilityRequirement struct {
	RequestID  advisoryrequests.AdvisoryRequestID
	RequestKey advisoryrequests.AdvisoryRequestKey

	RequiredKinds       []CapabilityKind
	RequiredDimensions  []string
	RequiredFactCodes   []string
	RequiredScopes      []CapabilityScope
	RequiredConstraints []CapabilityConstraint

	MinimumQuality CapabilityQualityRequirement

	MaximumCostClass        CapabilityCostClass
	MaximumLatencyClass     CapabilityLatencyClass
	MaximumSensitivityClass CapabilitySensitivityClass

	AllowsDegraded bool

	Fingerprint string
}

type CapabilityInventory struct {
	ID CapabilityInventoryID

	DomainID string
	Revision uint64

	Instances []CapabilityInstance

	CatalogFingerprint string
	Fingerprint        string
}

type CapabilityMappingStatus string

const (
	MappingCandidate          CapabilityMappingStatus = "candidate"
	MappingCompatible         CapabilityMappingStatus = "compatible"
	MappingCompatibleDegraded CapabilityMappingStatus = "compatible_degraded"
	MappingUnavailable        CapabilityMappingStatus = "unavailable"
	MappingIncompatible       CapabilityMappingStatus = "incompatible"
	MappingObsolete           CapabilityMappingStatus = "obsolete"
	MappingInvalidated        CapabilityMappingStatus = "invalidated"
)

const (
	MappingStatusCandidate          = MappingCandidate
	MappingStatusCompatible         = MappingCompatible
	MappingStatusCompatibleDegraded = MappingCompatibleDegraded
	MappingStatusUnavailable        = MappingUnavailable
	MappingStatusIncompatible       = MappingIncompatible
	MappingStatusObsolete           = MappingObsolete
	MappingStatusInvalidated        = MappingInvalidated
)

type CapabilityMappingCandidate struct {
	ID string

	RequestID            advisoryrequests.AdvisoryRequestID
	CapabilityInstanceID CapabilityInstanceID
	CapabilityKind       CapabilityKind
	Status               CapabilityMappingStatus

	CostClass         CapabilityCostClass
	LatencyClass      CapabilityLatencyClass
	SensitivityClass  CapabilitySensitivityClass
	QualityCalibrated bool

	Compatible bool

	CompatibilityPermille int
	QualityPermille       int
	ConstraintPermille    int
	ScopePermille         int
	AvailabilityPermille  int

	CostPenaltyPermille        int
	LatencyPenaltyPermille     int
	SensitivityPenaltyPermille int

	UtilityPermille int

	SatisfiedRequirements []string
	MissingRequirements   []string
	ViolatedConstraints   []string
	ReasonCodes           []string

	SourceRequestFingerprint   string
	SourceInventoryFingerprint string

	Fingerprint string
}

type CapabilityMappingAssessment struct {
	RequestID  advisoryrequests.AdvisoryRequestID
	RequestKey advisoryrequests.AdvisoryRequestKey
	EpisodeID  string

	SourceRequestFingerprint   string
	SourceInventoryFingerprint string

	CatalogFingerprint string
	PolicyFingerprint  string

	Requirement CapabilityRequirement

	Candidates []CapabilityMappingCandidate

	PreferredCandidateID    string
	PreferredMarginPermille int

	MappingAvailable      bool
	MappingAmbiguous      bool
	CapabilityUnavailable bool

	Revision    uint64
	Fingerprint string
}

type AnalysisInput struct {
	Request advisoryrequests.AdvisoryEvidenceRequest

	Catalog   CapabilityCatalog
	Inventory CapabilityInventory

	PreviousAssessment *CapabilityMappingAssessment
	Requirement        *CapabilityRequirement
}

type CapabilityMappingUpdate struct {
	Before CapabilityMappingAssessment
	After  CapabilityMappingAssessment
}

type CapabilityMappingInvalidation struct {
	RequestID  advisoryrequests.AdvisoryRequestID
	ReasonCode string
}

type CapabilityMappingDiff struct {
	RequestID         advisoryrequests.AdvisoryRequestID
	Added             []CapabilityMappingAssessment
	Updated           []CapabilityMappingUpdate
	Removed           []advisoryrequests.AdvisoryRequestID
	BeforeFingerprint string
	AfterFingerprint  string
	Before            *CapabilityMappingAssessment
	After             *CapabilityMappingAssessment
}

type MappingPlan struct {
	RequestID advisoryrequests.AdvisoryRequestID

	SourceRequestFingerprint   string
	SourceInventoryFingerprint string
	SourceRegistryRevision     uint64

	Creates     []CapabilityMappingAssessment
	Updates     []CapabilityMappingUpdate
	Invalidates []CapabilityMappingInvalidation

	ResultingAssessment CapabilityMappingAssessment
	ReasonCodes         []string
	Fingerprint         string
}

type RegistrySnapshot struct {
	Revision uint64

	Assessments []CapabilityMappingAssessment

	RequestIndex    map[advisoryrequests.AdvisoryRequestID]int
	CapabilityIndex map[CapabilityInstanceID][]advisoryrequests.AdvisoryRequestID

	CatalogFingerprint string
	PolicyFingerprint  string
	Digest             string
}

type ApplyResult struct {
	Applied          bool
	Idempotent       bool
	Before           CapabilityMappingAssessment
	After            CapabilityMappingAssessment
	RegistryRevision uint64
}

type MappingExplanation struct {
	RequestID            advisoryrequests.AdvisoryRequestID
	CandidateID          string
	CapabilityInstanceID CapabilityInstanceID
	CapabilityKind       CapabilityKind
	Compatible           bool
	Status               CapabilityMappingStatus
	SummaryCode          string

	SatisfiedRequirements []string
	MissingRequirements   []string
	ViolatedConstraints   []string
	ReasonCodes           []string

	CompatibilityPermille int
	QualityPermille       int
	ConstraintPermille    int
	ScopePermille         int
	AvailabilityPermille  int
	UtilityPermille       int

	NotACommand       bool
	NotAuthorization  bool
	NotAProbability   bool
	NoSecurityMeaning bool
}

func (i CapabilityInventory) Clone() CapabilityInventory {
	out := i
	out.Instances = make([]CapabilityInstance, len(i.Instances))
	for n, instance := range i.Instances {
		out.Instances[n] = instance.Clone()
	}
	return out
}

func (r CapabilityQualityRequirement) Clone() CapabilityQualityRequirement { return r }

func (r CapabilityRequirement) Clone() CapabilityRequirement {
	out := r
	out.RequiredKinds = append([]CapabilityKind(nil), r.RequiredKinds...)
	out.RequiredDimensions = append([]string(nil), r.RequiredDimensions...)
	out.RequiredFactCodes = append([]string(nil), r.RequiredFactCodes...)
	out.RequiredScopes = append([]CapabilityScope(nil), r.RequiredScopes...)
	out.RequiredConstraints = make([]CapabilityConstraint, len(r.RequiredConstraints))
	for i, constraint := range r.RequiredConstraints {
		out.RequiredConstraints[i] = constraint.Clone()
	}
	return out
}

func (c CapabilityMappingCandidate) Clone() CapabilityMappingCandidate {
	out := c
	out.SatisfiedRequirements = append([]string(nil), c.SatisfiedRequirements...)
	out.MissingRequirements = append([]string(nil), c.MissingRequirements...)
	out.ViolatedConstraints = append([]string(nil), c.ViolatedConstraints...)
	out.ReasonCodes = append([]string(nil), c.ReasonCodes...)
	return out
}

func (a CapabilityMappingAssessment) Clone() CapabilityMappingAssessment {
	out := a
	out.Requirement = a.Requirement.Clone()
	out.Candidates = make([]CapabilityMappingCandidate, len(a.Candidates))
	for i, candidate := range a.Candidates {
		out.Candidates[i] = candidate.Clone()
	}
	return out
}

func (u CapabilityMappingUpdate) Clone() CapabilityMappingUpdate {
	return CapabilityMappingUpdate{Before: u.Before.Clone(), After: u.After.Clone()}
}

func (p MappingPlan) Clone() MappingPlan {
	out := p
	out.Creates = make([]CapabilityMappingAssessment, len(p.Creates))
	for i, value := range p.Creates {
		out.Creates[i] = value.Clone()
	}
	out.Updates = make([]CapabilityMappingUpdate, len(p.Updates))
	for i, value := range p.Updates {
		out.Updates[i] = value.Clone()
	}
	out.Invalidates = append([]CapabilityMappingInvalidation(nil), p.Invalidates...)
	out.ResultingAssessment = p.ResultingAssessment.Clone()
	out.ReasonCodes = append([]string(nil), p.ReasonCodes...)
	return out
}

func (s RegistrySnapshot) Clone() RegistrySnapshot {
	out := s
	out.Assessments = make([]CapabilityMappingAssessment, len(s.Assessments))
	for i, value := range s.Assessments {
		out.Assessments[i] = value.Clone()
	}
	out.RequestIndex = make(map[advisoryrequests.AdvisoryRequestID]int, len(s.RequestIndex))
	for k, v := range s.RequestIndex {
		out.RequestIndex[k] = v
	}
	out.CapabilityIndex = make(map[CapabilityInstanceID][]advisoryrequests.AdvisoryRequestID, len(s.CapabilityIndex))
	for k, v := range s.CapabilityIndex {
		out.CapabilityIndex[k] = append([]advisoryrequests.AdvisoryRequestID(nil), v...)
	}
	return out
}

func sortAssessments(values []CapabilityMappingAssessment) {
	sort.Slice(values, func(i, j int) bool { return values[i].RequestID < values[j].RequestID })
}
