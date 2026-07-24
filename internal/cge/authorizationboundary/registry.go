package authorizationboundary

import (
	"sort"
	"sync"

	"synora/internal/cge/capabilitymapping"
)

type Registry struct {
	mu          sync.RWMutex
	policy      Policy
	assessments map[string]AuthorizationBoundaryAssessment
	revision    uint64
}

func NewRegistry() *Registry {
	return &Registry{policy: DefaultPolicy(), assessments: map[string]AuthorizationBoundaryAssessment{}}
}

func NewRegistryWithPolicy(policy Policy) *Registry {
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	return &Registry{policy: policy, assessments: map[string]AuthorizationBoundaryAssessment{}}
}

func (r *Registry) Get(requestID string) (AuthorizationBoundaryAssessment, bool) {
	if r == nil {
		return AuthorizationBoundaryAssessment{}, false
	}
	r.mu.RLock()
	value, ok := r.assessments[requestID]
	r.mu.RUnlock()
	if !ok {
		return AuthorizationBoundaryAssessment{}, false
	}
	return value.Clone(), true
}

func (r *Registry) List() []AuthorizationBoundaryAssessment {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	values := make([]AuthorizationBoundaryAssessment, 0, len(r.assessments))
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
	values := make([]AuthorizationBoundaryAssessment, 0, len(r.assessments))
	for _, value := range r.assessments {
		values = append(values, value.Clone())
	}
	revision, policy := r.revision, r.policy
	r.mu.RUnlock()
	sortAssessments(values)
	snapshot := RegistrySnapshot{Revision: revision, Assessments: values, RequestIndex: map[string]int{}, CapabilityIndex: map[capabilitymapping.CapabilityInstanceID][]string{}, PolicyFingerprint: policy.Fingerprint()}
	for i, assessment := range values {
		snapshot.RequestIndex[assessment.RequestID] = i
		for _, candidate := range assessment.Candidates {
			snapshot.CapabilityIndex[candidate.CapabilityInstanceID] = append(snapshot.CapabilityIndex[candidate.CapabilityInstanceID], assessment.RequestID)
		}
	}
	for key := range snapshot.CapabilityIndex {
		sort.Strings(snapshot.CapabilityIndex[key])
	}
	snapshot.Digest = registryDigest(snapshot)
	return snapshot
}

func (r *Registry) ApplyPlan(plan AuthorizationPlan) (ApplyResult, error) {
	if r == nil {
		return ApplyResult{}, ErrInvalidPlan
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := validatePlan(plan, r.policy); err != nil {
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
		if len(plan.Updates) != 1 || plan.Updates[0].Before.Fingerprint != current.Fingerprint || plan.ResultingAssessment.Revision <= current.Revision {
			return ApplyResult{}, ErrSourceRevisionConflict
		}
	} else if len(plan.Creates) != 1 || plan.Creates[0].Fingerprint != plan.ResultingAssessment.Fingerprint || plan.ResultingAssessment.Revision != 1 {
		return ApplyResult{}, ErrInvalidPlan
	}
	before := current.Clone()
	r.assessments[plan.RequestID] = plan.ResultingAssessment.Clone()
	r.revision++
	return ApplyResult{Applied: true, Before: before, After: plan.ResultingAssessment.Clone(), RegistryRevision: r.revision}, nil
}

func validatePlan(plan AuthorizationPlan, policy Policy) error {
	if plan.RequestID == "" || plan.SourceMappingFingerprint == "" || plan.SourcePolicyFingerprint == "" || plan.SourceGrantSnapshotFingerprint == "" || plan.SourceContextFingerprint == "" || plan.Fingerprint == "" || planFingerprint(plan) != plan.Fingerprint {
		return ErrInvalidPlan
	}
	if plan.ResultingAssessment.RequestID != plan.RequestID || plan.ResultingAssessment.SourceMappingAssessmentFingerprint != plan.SourceMappingFingerprint || plan.ResultingAssessment.SourcePolicyFingerprint != plan.SourcePolicyFingerprint || plan.ResultingAssessment.SourceGrantSnapshotFingerprint != plan.SourceGrantSnapshotFingerprint || plan.ResultingAssessment.SourceContextFingerprint != plan.SourceContextFingerprint {
		return ErrInvalidPlan
	}
	if err := validateAssessment(plan.ResultingAssessment); err != nil {
		return err
	}
	if policy.Validate() != nil {
		return ErrInvalidPolicy
	}
	return nil
}

func sortAssessments(values []AuthorizationBoundaryAssessment) {
	sort.Slice(values, func(i, j int) bool { return values[i].RequestID < values[j].RequestID })
}
