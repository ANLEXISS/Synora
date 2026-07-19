package episodes

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

type ApplyResult struct {
	Decision         IngestDecision
	Applied          bool
	Idempotent       bool
	Episode          EpisodeSnapshot
	RegistryRevision uint64
}

type Registry struct {
	mu                sync.RWMutex
	episodes          map[EpisodeID]Episode
	eventIndex        map[string]EpisodeID
	revision          uint64
	policyFingerprint string
	policy            Policy
	metrics           Metrics
}

func NewRegistry() *Registry { return NewRegistryWithPolicy(DefaultPolicy()) }

func NewRegistryWithPolicy(policy Policy) *Registry {
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	return &Registry{episodes: map[EpisodeID]Episode{}, eventIndex: map[string]EpisodeID{}, policyFingerprint: policy.Fingerprint(), policy: policy}
}

func (r *Registry) Add(episode Episode) error {
	if r == nil {
		return ErrEpisodeNotFound
	}
	if err := episode.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.episodes[episode.ID]; exists {
		return fmt.Errorf("%w: %s", ErrEpisodeIDCollision, episode.ID)
	}
	owned := episode.Clone()
	for _, observation := range owned.Observations {
		if existing, ok := r.eventIndex[observation.EventID]; ok && existing != owned.ID {
			return fmt.Errorf("%w: %s", ErrDuplicateEvent, observation.EventID)
		}
	}
	r.episodes[owned.ID] = owned
	for _, observation := range owned.Observations {
		r.eventIndex[observation.EventID] = owned.ID
	}
	r.revision++
	return nil
}

func (r *Registry) Get(id EpisodeID) (Episode, bool) {
	if r == nil {
		return Episode{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	episode, ok := r.episodes[id]
	if !ok {
		return Episode{}, false
	}
	return episode.Clone(), true
}

func (r *Registry) List() []Episode {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]EpisodeID, 0, len(r.episodes))
	for id := range r.episodes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]Episode, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.episodes[id].Clone())
	}
	return out
}

func (r *Registry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.episodes)
}

func (r *Registry) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	episodes := make([]EpisodeSnapshot, 0, len(r.episodes))
	for _, episode := range r.episodes {
		episodes = append(episodes, episode.Clone())
	}
	return Snapshot{Revision: r.revision, Episodes: sortedEpisodes(episodes), EventIndex: cloneEventIndex(r.eventIndex), PolicyFingerprint: r.policyFingerprint}
}

func cloneEventIndex(source map[string]EpisodeID) map[string]EpisodeID {
	out := make(map[string]EpisodeID, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func (r *Registry) ApplyIngestPlan(plan IngestPlan, observation ObservationRef, at time.Time) (ApplyResult, error) {
	if r == nil {
		return ApplyResult{}, ErrEpisodeNotFound
	}
	if err := plan.Validate(); err != nil {
		return ApplyResult{}, err
	}
	if err := observation.Validate(); err != nil {
		return ApplyResult{}, err
	}
	if plan.ObservationEventID != observation.EventID {
		return ApplyResult{}, fmt.Errorf("%w: observation mismatch", ErrInvalidPlan)
	}
	_ = at // Application time is deliberately not stored as a technical timestamp.
	r.mu.Lock()
	defer r.mu.Unlock()
	if plan.PolicyFingerprint != r.policyFingerprint {
		return ApplyResult{}, fmt.Errorf("%w: policy fingerprint", ErrSourceRevisionConflict)
	}
	if plan.SourceRevision != r.revision {
		return ApplyResult{}, fmt.Errorf("%w: expected=%d current=%d", ErrSourceRevisionConflict, plan.SourceRevision, r.revision)
	}
	if existingID, exists := r.eventIndex[observation.EventID]; exists {
		if plan.Decision != DecisionDuplicate {
			return ApplyResult{}, fmt.Errorf("%w: event already indexed", ErrDuplicateEvent)
		}
		r.metrics.DuplicateCount++
		return ApplyResult{Decision: DecisionDuplicate, Idempotent: true, Episode: r.episodes[existingID].Clone(), RegistryRevision: r.revision}, nil
	}
	switch plan.Decision {
	case DecisionAmbiguous:
		r.metrics.AmbiguousPlanCount++
		return ApplyResult{Decision: plan.Decision, RegistryRevision: r.revision}, ErrAmbiguousPlan
	case DecisionRejected:
		r.metrics.RejectedCount++
		return ApplyResult{Decision: plan.Decision, RegistryRevision: r.revision}, ErrRejectedPlan
	case DecisionCreateEpisode:
		id, err := DeriveEpisodeID(r.policy, observation)
		if err != nil {
			return ApplyResult{}, err
		}
		if _, exists := r.episodes[id]; exists {
			return ApplyResult{}, fmt.Errorf("%w: %s", ErrEpisodeIDCollision, id)
		}
		episode := newEpisode(id, observation)
		if err := episode.Validate(); err != nil {
			return ApplyResult{}, err
		}
		if len(episode.Observations) > r.policy.MaxObservations {
			return ApplyResult{}, ErrObservationLimitReached
		}
		r.episodes[id] = episode
		r.eventIndex[observation.EventID] = id
		r.revision++
		return ApplyResult{Decision: plan.Decision, Applied: true, Episode: episode.Clone(), RegistryRevision: r.revision}, nil
	case DecisionAttachExisting:
		episode, ok := r.episodes[plan.SelectedEpisodeID]
		if !ok {
			return ApplyResult{}, ErrEpisodeNotFound
		}
		if episode.Status == StatusClosed || episode.Status == StatusInvalidated {
			return ApplyResult{}, ErrEpisodeClosed
		}
		for _, candidate := range plan.Candidates {
			if candidate.EpisodeID == episode.ID && candidate.EpisodeRevision != episode.Revision {
				return ApplyResult{}, fmt.Errorf("%w: episode revision", ErrSourceRevisionConflict)
			}
		}
		if len(episode.Observations) >= r.policy.MaxObservations {
			return ApplyResult{}, ErrObservationLimitReached
		}
		if observation.ObservedAt.Before(episode.LastObservedAt) && episode.LastObservedAt.Sub(observation.ObservedAt) > r.policy.LateObservationGrace {
			return ApplyResult{}, ErrLateObservationOutsideGrace
		}
		before := episode.Clone()
		if episode.Status == StatusQuiescent {
			episode.Status = StatusOpen
			episode.StatusChangedAt = observation.ObservedAt
			episode.ClosedAt = nil
		}
		episode.Observations = append(episode.Observations, observation.Clone())
		recomputeAggregates(&episode)
		episode.Revision++
		if err := episode.Validate(); err != nil {
			return ApplyResult{}, err
		}
		r.episodes[episode.ID] = episode
		r.eventIndex[observation.EventID] = episode.ID
		r.revision++
		_ = before
		return ApplyResult{Decision: plan.Decision, Applied: true, Episode: episode.Clone(), RegistryRevision: r.revision}, nil
	default:
		return ApplyResult{}, fmt.Errorf("%w: decision", ErrInvalidPlan)
	}
}

func newEpisode(id EpisodeID, observation ObservationRef) Episode {
	return Episode{ID: id, Status: StatusOpen, CreatedAt: observation.ObservedAt, StartedAt: observation.ObservedAt, LastObservedAt: observation.ObservedAt, StatusChangedAt: observation.ObservedAt, Observations: []ObservationRef{observation.Clone()}, Revision: 1}
}

func recomputeAggregates(episode *Episode) {
	sort.SliceStable(episode.Observations, func(i, j int) bool {
		if !episode.Observations[i].ObservedAt.Equal(episode.Observations[j].ObservedAt) {
			return episode.Observations[i].ObservedAt.Before(episode.Observations[j].ObservedAt)
		}
		return episode.Observations[i].EventID < episode.Observations[j].EventID
	})
	episode.StartedAt = episode.Observations[0].ObservedAt
	episode.LastObservedAt = episode.Observations[len(episode.Observations)-1].ObservedAt
	episode.DurationObserved = episode.LastObservedAt.Sub(episode.StartedAt)
	if episode.CreatedAt.After(episode.StartedAt) {
		episode.CreatedAt = episode.StartedAt
	}
	subjects := map[string]SubjectRef{}
	nodes := map[string]NodeRef{}
	chains := map[string]ChainRef{}
	routines := map[string]RoutineRef{}
	eventTypes := map[string]struct{}{}
	qualities := map[string]struct{}{}
	for _, observation := range episode.Observations {
		subject := normalizeSubject(observation.Subject)
		subjects[subjectFingerprint(subject)] = subject
		if observation.NodeID != "" {
			nodes[observation.NodeID+"\x00"+observation.ZoneID] = NodeRef{ID: observation.NodeID, ZoneID: observation.ZoneID}
		}
		if observation.ChainID != "" {
			chains[observation.ChainID] = ChainRef{ID: observation.ChainID}
		}
		for _, id := range observation.RoutineIDs {
			routines[id] = RoutineRef{ID: id}
		}
		if observation.EventType != "" {
			eventTypes[observation.EventType] = struct{}{}
		}
		if observation.ContextQuality != "" {
			qualities[observation.ContextQuality] = struct{}{}
		}
	}
	keys := make([]string, 0, len(subjects))
	for key := range subjects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	episode.Subjects = episode.Subjects[:0]
	for _, key := range keys {
		episode.Subjects = append(episode.Subjects, subjects[key])
	}
	keys = keys[:0]
	for key := range nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	episode.Nodes = episode.Nodes[:0]
	for _, key := range keys {
		episode.Nodes = append(episode.Nodes, nodes[key])
	}
	episode.ChainRefs = episode.ChainRefs[:0]
	keys = keys[:0]
	for key := range chains {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		episode.ChainRefs = append(episode.ChainRefs, chains[key])
	}
	episode.RoutineRefs = episode.RoutineRefs[:0]
	keys = keys[:0]
	for key := range routines {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		episode.RoutineRefs = append(episode.RoutineRefs, routines[key])
	}
	episode.EventTypes = episode.EventTypes[:0]
	for key := range eventTypes {
		episode.EventTypes = append(episode.EventTypes, key)
	}
	sort.Strings(episode.EventTypes)
	episode.ContextQualities = episode.ContextQualities[:0]
	for key := range qualities {
		episode.ContextQualities = append(episode.ContextQualities, key)
	}
	sort.Strings(episode.ContextQualities)
}
