package situationfacts

import (
	"sort"
	"sync"

	"synora/internal/cge/episodes"
)

type ApplyResult struct {
	Applied          bool
	Idempotent       bool
	Diff             FactSetDiff
	Before           FactSet
	After            FactSet
	RegistryRevision uint64
}

type Registry struct {
	mu                sync.RWMutex
	sets              map[episodes.EpisodeID]FactSet
	revision          uint64
	schema            FactSchema
	policy            Policy
	policyFingerprint string
	digest            string
}

// planningSnapshot is an internal immutable view. It shares FactSet backing
// storage only inside this package; public Snapshot continues to deep-clone.
type planningSnapshot struct {
	Revision          uint64
	FactSets          []FactSet
	EpisodeIndex      map[episodes.EpisodeID]int
	SchemaFingerprint string
	PolicyFingerprint string
	Digest            string
}

func NewRegistry() *Registry { return NewRegistryWithPolicy(DefaultPolicy()) }

func NewRegistryWithPolicy(policy Policy) *Registry {
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	return &Registry{sets: map[episodes.EpisodeID]FactSet{}, schema: compiledSchema(), policy: policy, policyFingerprint: policy.Fingerprint()}
}

func (r *Registry) Get(id episodes.EpisodeID) (FactSet, bool) {
	if r == nil {
		return FactSet{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	set, ok := r.sets[id]
	if !ok {
		return FactSet{}, false
	}
	return set.Clone(), true
}

func (r *Registry) List() []FactSet {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]FactSet, 0, len(r.sets))
	for _, set := range r.sets {
		out = append(out, set.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EpisodeID < out[j].EpisodeID })
	return out
}

func (r *Registry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sets)
}

func (r *Registry) planningSnapshot() planningSnapshot {
	if r == nil {
		return planningSnapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	sets := make([]FactSet, 0, len(r.sets))
	for _, set := range r.sets {
		sets = append(sets, set)
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].EpisodeID < sets[j].EpisodeID })
	index := make(map[episodes.EpisodeID]int, len(sets))
	for i, set := range sets {
		index[set.EpisodeID] = i
	}
	return planningSnapshot{Revision: r.revision, FactSets: sets, EpisodeIndex: index, SchemaFingerprint: SchemaFingerprint(), PolicyFingerprint: r.policyFingerprint, Digest: r.digest}
}

func (r *Registry) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	sets := make([]FactSet, 0, len(r.sets))
	for _, set := range r.sets {
		sets = append(sets, set.Clone())
	}
	sort.Slice(sets, func(i, j int) bool { return sets[i].EpisodeID < sets[j].EpisodeID })
	index := map[episodes.EpisodeID]int{}
	for i, set := range sets {
		index[set.EpisodeID] = i
	}
	snapshot := Snapshot{Revision: r.revision, FactSets: sets, EpisodeIndex: index, SchemaFingerprint: SchemaFingerprint(), PolicyFingerprint: r.policyFingerprint}
	if r.digest == "" {
		r.digest = RegistryDigest(snapshot)
	}
	snapshot.Digest = r.digest
	return snapshot
}

func (r *Registry) Apply(set FactSet) (ApplyResult, error) {
	if r == nil {
		return ApplyResult{}, ErrInvalidFactSet
	}
	if err := set.Validate(r.schema, r.policy); err != nil {
		return ApplyResult{}, err
	}
	if set.SchemaFingerprint != SchemaFingerprint() || set.PolicyFingerprint != r.policyFingerprint {
		return ApplyResult{}, ErrFingerprintMismatch
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.sets[set.EpisodeID]
	if exists && set.EpisodeRevision < current.EpisodeRevision {
		return ApplyResult{}, ErrStaleEpisodeRevision
	}
	if exists && set.EpisodeRevision == current.EpisodeRevision {
		if set.Fingerprint == current.Fingerprint {
			return ApplyResult{Idempotent: true, Before: current.Clone(), After: current.Clone(), RegistryRevision: r.revision, Diff: emptyDiff(current)}, nil
		}
		return ApplyResult{}, ErrSourceRevisionConflict
	}
	var diff FactSetDiff
	var err error
	if exists {
		diff, err = Diff(current, set)
		if err != nil {
			return ApplyResult{}, err
		}
	} else {
		diff = FactSetDiff{EpisodeID: set.EpisodeID, AfterEpisodeRevision: set.EpisodeRevision, AfterFingerprint: set.Fingerprint}
	}
	r.sets[set.EpisodeID] = set.Clone()
	r.revision++
	r.digest = ""
	return ApplyResult{Applied: true, Diff: diff, Before: current.Clone(), After: set.Clone(), RegistryRevision: r.revision}, nil
}

func emptyDiff(set FactSet) FactSetDiff {
	return FactSetDiff{EpisodeID: set.EpisodeID, BeforeEpisodeRevision: set.EpisodeRevision, AfterEpisodeRevision: set.EpisodeRevision, BeforeFingerprint: set.Fingerprint, AfterFingerprint: set.Fingerprint}
}
