package capabilitymapping

import (
	"sort"
	"sync"

	"synora/internal/cge/advisoryrequests"
)

type Registry struct {
	mu                 sync.RWMutex
	policy             Policy
	catalogFingerprint string
	assessments        map[advisoryrequests.AdvisoryRequestID]CapabilityMappingAssessment
	revision           uint64
}

func NewRegistry() *Registry { return NewRegistryWithCatalog(DefaultPolicy(), Catalog()) }

func NewRegistryWithPolicy(policy Policy) *Registry { return NewRegistryWithCatalog(policy, Catalog()) }

func NewRegistryWithCatalog(policy Policy, catalog CapabilityCatalog) *Registry {
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	if ValidateCatalog(catalog) != nil {
		catalog = Catalog()
	}
	return &Registry{policy: policy, catalogFingerprint: catalog.Fingerprint, assessments: map[advisoryrequests.AdvisoryRequestID]CapabilityMappingAssessment{}}
}

func (r *Registry) Get(requestID advisoryrequests.AdvisoryRequestID) (CapabilityMappingAssessment, bool) {
	if r == nil {
		return CapabilityMappingAssessment{}, false
	}
	r.mu.RLock()
	value, ok := r.assessments[requestID]
	r.mu.RUnlock()
	if !ok {
		return CapabilityMappingAssessment{}, false
	}
	return value.Clone(), true
}

func (r *Registry) List() []CapabilityMappingAssessment {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	values := make([]CapabilityMappingAssessment, 0, len(r.assessments))
	for _, value := range r.assessments {
		values = append(values, value.Clone())
	}
	r.mu.RUnlock()
	sortAssessments(values)
	return values
}

func (r *Registry) Snapshot() RegistrySnapshot {
	if r == nil {
		return RegistrySnapshot{}
	}
	r.mu.RLock()
	values := make([]CapabilityMappingAssessment, 0, len(r.assessments))
	for _, value := range r.assessments {
		values = append(values, value.Clone())
	}
	revision, policy, catalog := r.revision, r.policy, r.catalogFingerprint
	r.mu.RUnlock()
	sortAssessments(values)
	snapshot := RegistrySnapshot{Revision: revision, Assessments: values, RequestIndex: map[advisoryrequests.AdvisoryRequestID]int{}, CapabilityIndex: map[CapabilityInstanceID][]advisoryrequests.AdvisoryRequestID{}, CatalogFingerprint: catalog, PolicyFingerprint: policy.Fingerprint()}
	for i, assessment := range values {
		snapshot.RequestIndex[assessment.RequestID] = i
		for _, candidate := range assessment.Candidates {
			snapshot.CapabilityIndex[candidate.CapabilityInstanceID] = append(snapshot.CapabilityIndex[candidate.CapabilityInstanceID], assessment.RequestID)
		}
	}
	for key := range snapshot.CapabilityIndex {
		sort.Slice(snapshot.CapabilityIndex[key], func(i, j int) bool { return snapshot.CapabilityIndex[key][i] < snapshot.CapabilityIndex[key][j] })
	}
	snapshot.Digest = registryDigest(snapshot)
	return snapshot
}

func (r *Registry) ApplyPlan(plan MappingPlan) (ApplyResult, error) {
	if r == nil {
		return ApplyResult{}, ErrInvalidPlan
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validatePlanLocked(plan); err != nil {
		return ApplyResult{}, err
	}
	current, exists := r.assessments[plan.RequestID]
	if exists && current.Fingerprint == plan.ResultingAssessment.Fingerprint {
		return ApplyResult{Idempotent: true, Before: current.Clone(), After: current.Clone(), RegistryRevision: r.revision}, nil
	}
	if plan.SourceRegistryRevision != r.revision {
		return ApplyResult{}, ErrSourceRevisionConflict
	}
	if exists {
		if len(plan.Updates) != 1 || plan.Updates[0].Before.Fingerprint != current.Fingerprint {
			return ApplyResult{}, ErrSourceRevisionConflict
		}
		if plan.ResultingAssessment.Revision <= current.Revision {
			return ApplyResult{}, ErrStaleInventory
		}
	} else if len(plan.Creates) != 1 || plan.Creates[0].Fingerprint != plan.ResultingAssessment.Fingerprint {
		return ApplyResult{}, ErrInvalidPlan
	}
	if r.catalogFingerprint == "" {
		r.catalogFingerprint = plan.ResultingAssessment.CatalogFingerprint
	}
	before := current.Clone()
	r.assessments[plan.RequestID] = plan.ResultingAssessment.Clone()
	r.revision++
	return ApplyResult{Applied: true, Before: before, After: plan.ResultingAssessment.Clone(), RegistryRevision: r.revision}, nil
}

func (r *Registry) validatePlanLocked(plan MappingPlan) error {
	if plan.RequestID == "" || plan.SourceRequestFingerprint == "" || plan.SourceInventoryFingerprint == "" || plan.Fingerprint == "" || planFingerprint(plan) != plan.Fingerprint {
		return ErrInvalidPlan
	}
	if plan.ResultingAssessment.RequestID != plan.RequestID || plan.ResultingAssessment.SourceRequestFingerprint != plan.SourceRequestFingerprint || plan.ResultingAssessment.SourceInventoryFingerprint != plan.SourceInventoryFingerprint {
		return ErrInvalidPlan
	}
	if r.catalogFingerprint != "" && r.catalogFingerprint != plan.ResultingAssessment.CatalogFingerprint {
		return ErrInventoryCatalogMismatch
	}
	if plan.ResultingAssessment.PolicyFingerprint != r.policy.Fingerprint() {
		return ErrFingerprintMismatch
	}
	if err := validateAssessment(plan.ResultingAssessment, r.policy); err != nil {
		return err
	}
	for _, create := range plan.Creates {
		if create.RequestID != plan.RequestID || create.Revision != 1 || create.Fingerprint != plan.ResultingAssessment.Fingerprint {
			return ErrInvalidPlan
		}
	}
	for _, update := range plan.Updates {
		if update.After.RequestID != plan.RequestID || update.After.Fingerprint != plan.ResultingAssessment.Fingerprint || update.Before.RequestID != plan.RequestID {
			return ErrInvalidPlan
		}
	}
	return nil
}

func validateAssessment(a CapabilityMappingAssessment, policy Policy) error {
	if a.RequestID == "" || a.RequestKey == "" || a.EpisodeID == "" || a.SourceRequestFingerprint == "" || a.SourceInventoryFingerprint == "" || a.CatalogFingerprint == "" || a.PolicyFingerprint != policy.Fingerprint() || a.Revision == 0 || a.Fingerprint == "" || assessmentFingerprint(a) != a.Fingerprint {
		return ErrInvalidAssessment
	}
	if err := ValidateRequirement(a.Requirement); err != nil {
		return err
	}
	if a.Requirement.RequestID != a.RequestID || a.Requirement.RequestKey != a.RequestKey {
		return ErrInvalidAssessment
	}
	seen := map[string]struct{}{}
	for _, candidate := range a.Candidates {
		if candidate.ID == "" || candidate.Fingerprint == "" || candidateFingerprint(candidate) != candidate.Fingerprint || candidate.RequestID != a.RequestID || candidate.SourceRequestFingerprint != a.SourceRequestFingerprint || candidate.SourceInventoryFingerprint != a.SourceInventoryFingerprint {
			return ErrInvalidAssessment
		}
		if _, ok := seen[candidate.ID]; ok {
			return ErrCandidateIDCollision
		}
		seen[candidate.ID] = struct{}{}
		if candidate.CapabilityKind == "" || !validMappingStatus(candidate.Status) {
			return ErrInvalidAssessment
		}
		for _, score := range []int{candidate.CompatibilityPermille, candidate.QualityPermille, candidate.ConstraintPermille, candidate.ScopePermille, candidate.AvailabilityPermille, candidate.CostPenaltyPermille, candidate.LatencyPenaltyPermille, candidate.SensitivityPenaltyPermille, candidate.UtilityPermille} {
			if score < 0 || score > 1000 {
				return ErrInvalidAssessment
			}
		}
		if candidate.Compatible != (candidate.Status == MappingCompatible || candidate.Status == MappingCompatibleDegraded) {
			return ErrInvalidAssessment
		}
	}
	anyCompatible := false
	for _, candidate := range a.Candidates {
		if candidate.Compatible {
			anyCompatible = true
		}
	}
	if a.MappingAvailable != anyCompatible || a.PreferredMarginPermille < 0 || a.PreferredMarginPermille > 1000 {
		return ErrInvalidAssessment
	}
	if a.PreferredCandidateID != "" {
		found := false
		for _, candidate := range a.Candidates {
			if candidate.ID == a.PreferredCandidateID && candidate.Compatible {
				found = true
			}
		}
		if !found {
			return ErrInvalidAssessment
		}
	}
	return nil
}

func validMappingStatus(value CapabilityMappingStatus) bool {
	return value == MappingCandidate || value == MappingCompatible || value == MappingCompatibleDegraded || value == MappingUnavailable || value == MappingIncompatible || value == MappingObsolete || value == MappingInvalidated
}
