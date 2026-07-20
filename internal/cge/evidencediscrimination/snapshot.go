package evidencediscrimination

import (
	"sort"
	"sync"
)

type RegistrySnapshot struct {
	Revision           uint64
	Assessments        []DiscriminationAssessment
	EpisodeIndex       map[string]int
	CatalogFingerprint string
	PolicyFingerprint  string
	Digest             string
}

func (s RegistrySnapshot) Clone() RegistrySnapshot {
	out := s
	out.Assessments = cloneAssessments(s.Assessments)
	out.EpisodeIndex = map[string]int{}
	for k, v := range s.EpisodeIndex {
		out.EpisodeIndex[k] = v
	}
	return out
}

type ApplyResult struct {
	Applied          bool
	Idempotent       bool
	Before           DiscriminationAssessment
	After            DiscriminationAssessment
	RegistryRevision uint64
}

type Registry struct {
	mu          sync.RWMutex
	assessments map[string]DiscriminationAssessment
	revision    uint64
	catalog     EvidenceCatalog
	policy      Policy
	digest      string
}

func NewRegistry() *Registry { return NewRegistryWithPolicy(DefaultPolicy()) }
func NewRegistryWithPolicy(policy Policy) *Registry {
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	catalog := Catalog()
	return &Registry{assessments: map[string]DiscriminationAssessment{}, catalog: catalog, policy: policy}
}

func (r *Registry) Get(episodeID string) (DiscriminationAssessment, bool) {
	if r == nil {
		return DiscriminationAssessment{}, false
	}
	r.mu.RLock()
	a, ok := r.assessments[episodeID]
	r.mu.RUnlock()
	if !ok {
		return DiscriminationAssessment{}, false
	}
	return a.Clone(), true
}
func (r *Registry) List() []DiscriminationAssessment {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := cloneAssessmentsMap(r.assessments)
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].EpisodeID < out[j].EpisodeID })
	return out
}
func (r *Registry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	n := len(r.assessments)
	r.mu.RUnlock()
	return n
}
func cloneAssessmentsMap(values map[string]DiscriminationAssessment) []DiscriminationAssessment {
	out := make([]DiscriminationAssessment, 0, len(values))
	for _, a := range values {
		out = append(out, a.Clone())
	}
	return out
}

func (r *Registry) Snapshot() RegistrySnapshot {
	if r == nil {
		return RegistrySnapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	assessments := cloneAssessmentsMap(r.assessments)
	sort.Slice(assessments, func(i, j int) bool { return assessments[i].EpisodeID < assessments[j].EpisodeID })
	index := map[string]int{}
	for i, a := range assessments {
		index[a.EpisodeID] = i
	}
	s := RegistrySnapshot{Revision: r.revision, Assessments: assessments, EpisodeIndex: index, CatalogFingerprint: CatalogFingerprint(r.catalog), PolicyFingerprint: r.policy.Fingerprint()}
	if r.digest == "" {
		r.digest = registryDigest(s)
	}
	s.Digest = r.digest
	return s
}

func (r *Registry) ApplyPlan(plan EvidencePlan) (ApplyResult, error) {
	if r == nil {
		return ApplyResult{}, ErrInvalidPlan
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validatePlanLocked(plan); err != nil {
		return ApplyResult{}, err
	}
	current, exists := r.assessments[plan.EpisodeID]
	if exists && current.Fingerprint == plan.ResultingAssessment.Fingerprint {
		return ApplyResult{Idempotent: true, Before: current.Clone(), After: current.Clone(), RegistryRevision: r.revision}, nil
	}
	if plan.SourceRegistryRevision != r.revision {
		return ApplyResult{}, ErrSourceRevisionConflict
	}
	if exists {
		if plan.SourceAssessmentFingerprint != current.Fingerprint {
			return ApplyResult{}, ErrSourceRevisionConflict
		}
		if plan.ResultingAssessment.Revision <= current.Revision {
			return ApplyResult{}, ErrStaleHypothesisSet
		}
	} else if plan.SourceAssessmentFingerprint != "" {
		return ApplyResult{}, ErrSourceRevisionConflict
	}
	before := current.Clone()
	r.assessments[plan.EpisodeID] = plan.ResultingAssessment.Clone()
	r.revision++
	r.digest = ""
	return ApplyResult{Applied: true, Before: before, After: plan.ResultingAssessment.Clone(), RegistryRevision: r.revision}, nil
}

func (r *Registry) validatePlanLocked(plan EvidencePlan) error {
	if plan.EpisodeID == "" || plan.ResultingAssessment.EpisodeID != plan.EpisodeID || plan.ResultingAssessment.SourceFactSetFingerprint == "" || plan.ResultingAssessment.SourceHypothesisSetFingerprint == "" {
		return ErrInvalidPlan
	}
	if err := ValidateAssessment(plan.ResultingAssessment, r.catalog, r.policy); err != nil {
		return err
	}
	if plan.SourceAssessmentFingerprint != "" && plan.SourceAssessmentFingerprint != plan.SourceAssessment.Fingerprint {
		return ErrFingerprintMismatch
	}
	return nil
}

func (s RegistrySnapshot) Validate(catalog EvidenceCatalog, policy Policy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if err := ValidateCatalog(catalog); err != nil {
		return err
	}
	if s.CatalogFingerprint != CatalogFingerprint(catalog) || s.PolicyFingerprint != policy.Fingerprint() {
		return ErrFingerprintMismatch
	}
	last := ""
	for i, a := range s.Assessments {
		if a.EpisodeID <= last || s.EpisodeIndex[a.EpisodeID] != i {
			return ErrInvalidPlan
		}
		if err := ValidateAssessment(a, catalog, policy); err != nil {
			return err
		}
		last = a.EpisodeID
	}
	if len(s.EpisodeIndex) != len(s.Assessments) {
		return ErrInvalidPlan
	}
	if s.Digest != "" && s.Digest != registryDigest(s) {
		return ErrFingerprintMismatch
	}
	return nil
}
