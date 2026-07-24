package situationhypotheses

import (
	"sort"
	"sync"
)

type HypothesisLocation struct {
	EpisodeIndex    int
	HypothesisIndex int
}

type RegistrySnapshot struct {
	Revision uint64

	EpisodeSets []CompetingHypothesisSet

	EpisodeIndex    map[string]int
	HypothesisIndex map[HypothesisID]HypothesisLocation

	SchemaFingerprint string
	PolicyFingerprint string
	Digest            string
}

func (s RegistrySnapshot) Clone() RegistrySnapshot {
	out := s
	out.EpisodeSets = make([]CompetingHypothesisSet, len(s.EpisodeSets))
	for i, set := range s.EpisodeSets {
		out.EpisodeSets[i] = set.Clone()
	}
	out.EpisodeIndex = make(map[string]int, len(s.EpisodeIndex))
	for key, value := range s.EpisodeIndex {
		out.EpisodeIndex[key] = value
	}
	out.HypothesisIndex = make(map[HypothesisID]HypothesisLocation, len(s.HypothesisIndex))
	for key, value := range s.HypothesisIndex {
		out.HypothesisIndex[key] = value
	}
	return out
}

type Registry struct {
	mu sync.RWMutex

	sets     map[string]CompetingHypothesisSet
	revision uint64
	schema   HypothesisSchema
	policy   Policy

	digest string
}

type ApplyResult struct {
	Applied          bool
	Idempotent       bool
	Before           CompetingHypothesisSet
	After            CompetingHypothesisSet
	RegistryRevision uint64
}

func (s RegistrySnapshot) Validate(schema HypothesisSchema, policy Policy) error {
	if err := schema.Validate(); err != nil {
		return ErrInvalidSchema
	}
	if err := policy.Validate(); err != nil {
		return ErrInvalidPolicy
	}
	if s.SchemaFingerprint != schemaFingerprint(schema) || s.PolicyFingerprint != policy.Fingerprint() {
		return ErrFingerprintMismatch
	}
	last := ""
	seen := map[string]struct{}{}
	expectedHypotheses := map[HypothesisID]HypothesisLocation{}
	for i, set := range s.EpisodeSets {
		if i > 0 && set.EpisodeID <= last {
			return ErrInvalidPlan
		}
		if _, ok := seen[set.EpisodeID]; ok || s.EpisodeIndex[set.EpisodeID] != i {
			return ErrInvalidPlan
		}
		seen[set.EpisodeID] = struct{}{}
		for j, hypothesis := range set.Hypotheses {
			expectedHypotheses[hypothesis.ID] = HypothesisLocation{EpisodeIndex: i, HypothesisIndex: j}
		}
		last = set.EpisodeID
	}
	if len(seen) != len(s.EpisodeIndex) || len(expectedHypotheses) != len(s.HypothesisIndex) {
		return ErrInvalidPlan
	}
	for id, location := range expectedHypotheses {
		if s.HypothesisIndex[id] != location {
			return ErrInvalidPlan
		}
	}
	if s.Digest != "" && s.Digest != registryDigest(s) {
		return ErrFingerprintMismatch
	}
	return nil
}

func NewRegistry() *Registry { return NewRegistryWithPolicy(DefaultPolicy()) }

func NewRegistryWithPolicy(policy Policy) *Registry {
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	return &Registry{sets: map[string]CompetingHypothesisSet{}, schema: compiledSchema(), policy: policy}
}

func (r *Registry) GetEpisodeSet(episodeID string) (CompetingHypothesisSet, bool) {
	if r == nil {
		return CompetingHypothesisSet{}, false
	}
	r.mu.RLock()
	set, ok := r.sets[episodeID]
	r.mu.RUnlock()
	if !ok {
		return CompetingHypothesisSet{}, false
	}
	return set.Clone(), true
}

func (r *Registry) GetHypothesis(id HypothesisID) (SituationHypothesis, bool) {
	if r == nil {
		return SituationHypothesis{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, set := range r.sets {
		for _, hypothesis := range set.Hypotheses {
			if hypothesis.ID == id {
				return hypothesis.Clone(), true
			}
		}
	}
	return SituationHypothesis{}, false
}

func (r *Registry) ListEpisodeSets() []CompetingHypothesisSet {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	result := make([]CompetingHypothesisSet, 0, len(r.sets))
	for _, set := range r.sets {
		result = append(result, set.Clone())
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].EpisodeID < result[j].EpisodeID })
	return result
}

func (r *Registry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	value := len(r.sets)
	r.mu.RUnlock()
	return value
}

func (r *Registry) Snapshot() RegistrySnapshot {
	if r == nil {
		return RegistrySnapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	sets := make([]CompetingHypothesisSet, 0, len(r.sets))
	for _, set := range r.sets {
		sets = append(sets, set.Clone())
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].EpisodeID < sets[j].EpisodeID })
	index := make(map[string]int, len(sets))
	hypothesisIndex := map[HypothesisID]HypothesisLocation{}
	for i, set := range sets {
		index[set.EpisodeID] = i
		for j, hypothesis := range set.Hypotheses {
			hypothesisIndex[hypothesis.ID] = HypothesisLocation{EpisodeIndex: i, HypothesisIndex: j}
		}
	}
	snapshot := RegistrySnapshot{Revision: r.revision, EpisodeSets: sets, EpisodeIndex: index, HypothesisIndex: hypothesisIndex, SchemaFingerprint: SchemaFingerprint(), PolicyFingerprint: r.policy.Fingerprint()}
	if r.digest == "" {
		r.digest = registryDigest(snapshot)
	}
	snapshot.Digest = r.digest
	return snapshot
}

func (r *Registry) ApplyPlan(plan HypothesisPlan) (ApplyResult, error) {
	if r == nil {
		return ApplyResult{}, ErrInvalidPlan
	}
	if err := r.validatePlan(plan); err != nil {
		return ApplyResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.sets[plan.EpisodeID]
	if exists {
		if plan.ResultingSet.Fingerprint == current.Fingerprint {
			return ApplyResult{Idempotent: true, Before: current.Clone(), After: current.Clone(), RegistryRevision: r.revision}, nil
		}
	}
	if plan.SourceRegistryRevision != r.revision {
		return ApplyResult{}, ErrSourceRevisionConflict
	}
	if exists {
		if plan.ResultingSet.Revision < current.Revision {
			return ApplyResult{}, ErrStaleFactSet
		}
		if plan.ResultingSet.Revision == current.Revision {
			return ApplyResult{}, ErrSourceRevisionConflict
		}
	}
	before := current.Clone()
	r.sets[plan.EpisodeID] = plan.ResultingSet.Clone()
	r.revision++
	r.digest = ""
	return ApplyResult{Applied: true, Before: before, After: plan.ResultingSet.Clone(), RegistryRevision: r.revision}, nil
}

func (r *Registry) validatePlan(plan HypothesisPlan) error {
	if plan.EpisodeID == "" || plan.SourceFactSetFingerprint == "" || plan.SourceFactSetFingerprint != plan.SourceFactSet.Fingerprint || plan.ResultingSet.EpisodeID != plan.EpisodeID || plan.ResultingSet.FactSetFingerprint != plan.SourceFactSetFingerprint {
		return ErrInvalidPlan
	}
	if err := plan.SourceFactSetFingerprintValidate(); err != nil {
		return err
	}
	if err := validateSet(plan.ResultingSet, plan.SourceFactSet, r.schema, r.policy); err != nil {
		return err
	}
	return nil
}

func (p HypothesisPlan) SourceFactSetFingerprintValidate() error {
	if p.SourceFactSet.Fingerprint == "" || p.SourceFactSet.Fingerprint != p.SourceFactSetFingerprint {
		return ErrMissingFactSetFingerprint
	}
	return nil
}
