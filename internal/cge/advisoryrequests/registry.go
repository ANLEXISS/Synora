package advisoryrequests

import (
	"sync"
)

type ApplyResult struct {
	Applied          bool
	Idempotent       bool
	Before           []AdvisoryEvidenceRequest
	After            []AdvisoryEvidenceRequest
	RegistryRevision uint64
}

type DispositionResult struct {
	Applied          bool
	Request          AdvisoryEvidenceRequest
	RegistryRevision uint64
}

type Registry struct {
	mu       sync.RWMutex
	policy   Policy
	requests map[AdvisoryRequestID]AdvisoryEvidenceRequest
	revision uint64
}

func NewRegistry() *Registry { return NewRegistryWithPolicy(DefaultPolicy()) }

func NewRegistryWithPolicy(policy Policy) *Registry {
	if policy.Validate() != nil {
		policy = DefaultPolicy()
	}
	return &Registry{policy: policy, requests: map[AdvisoryRequestID]AdvisoryEvidenceRequest{}}
}

func (r *Registry) Get(id AdvisoryRequestID) (AdvisoryEvidenceRequest, bool) {
	if r == nil {
		return AdvisoryEvidenceRequest{}, false
	}
	r.mu.RLock()
	value, ok := r.requests[id]
	r.mu.RUnlock()
	if !ok {
		return AdvisoryEvidenceRequest{}, false
	}
	return value.Clone(), true
}

func (r *Registry) GetByEpisode(episodeID string) []AdvisoryEvidenceRequest {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	values := make([]AdvisoryEvidenceRequest, 0)
	for _, value := range r.requests {
		if value.EpisodeID == episodeID {
			values = append(values, value.Clone())
		}
	}
	r.mu.RUnlock()
	sortRequests(values)
	return values
}

func (r *Registry) List() []AdvisoryEvidenceRequest {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	values := make([]AdvisoryEvidenceRequest, 0, len(r.requests))
	for _, value := range r.requests {
		values = append(values, value.Clone())
	}
	r.mu.RUnlock()
	sortRequests(values)
	return values
}

func (r *Registry) Snapshot() RegistrySnapshot {
	if r == nil {
		return RegistrySnapshot{}
	}
	r.mu.RLock()
	values := make([]AdvisoryEvidenceRequest, 0, len(r.requests))
	for _, value := range r.requests {
		values = append(values, value.Clone())
	}
	revision, policy := r.revision, r.policy
	r.mu.RUnlock()
	return buildSnapshot(revision, policy, values)
}

func (r *Registry) ApplyPlan(plan AdvisoryPlan) (ApplyResult, error) {
	if r == nil {
		return ApplyResult{}, ErrInvalidPlan
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := validatePlan(plan, r.policy); err != nil {
		return ApplyResult{}, err
	}
	current := r.episodeRequestsLocked(plan.EpisodeID)
	desired := cloneRequests(plan.ResultingRequests)
	sortRequests(desired)
	if sameRequestSet(current, desired) {
		return ApplyResult{Idempotent: true, Before: cloneRequests(current), After: cloneRequests(current), RegistryRevision: r.revision}, nil
	}
	if plan.SourceRegistryRevision != r.revision {
		return ApplyResult{}, ErrSourceRevisionConflict
	}
	if err := validatePlanAgainstCurrent(plan, current, r.policy); err != nil {
		return ApplyResult{}, err
	}
	desiredIDs := map[AdvisoryRequestID]struct{}{}
	for _, value := range desired {
		desiredIDs[value.ID] = struct{}{}
	}
	for id, value := range r.requests {
		if value.EpisodeID == plan.EpisodeID {
			if _, keep := desiredIDs[id]; !keep {
				return ApplyResult{}, ErrInvalidPlan
			}
		}
	}
	for _, value := range desired {
		r.requests[value.ID] = value.Clone()
	}
	r.revision++
	return ApplyResult{Applied: true, Before: cloneRequests(current), After: cloneRequests(desired), RegistryRevision: r.revision}, nil
}

func (r *Registry) episodeRequestsLocked(episodeID string) []AdvisoryEvidenceRequest {
	values := make([]AdvisoryEvidenceRequest, 0)
	for _, value := range r.requests {
		if value.EpisodeID == episodeID {
			values = append(values, value.Clone())
		}
	}
	sortRequests(values)
	return values
}

func sameRequestSet(a, b []AdvisoryEvidenceRequest) bool {
	if len(a) != len(b) {
		return false
	}
	aa, bb := cloneRequests(a), cloneRequests(b)
	sortRequests(aa)
	sortRequests(bb)
	for i := range aa {
		if aa[i].Fingerprint != bb[i].Fingerprint || aa[i].ID != bb[i].ID {
			return false
		}
	}
	return true
}

func validatePlan(plan AdvisoryPlan, policy Policy) error {
	if plan.EpisodeID == "" || plan.SourceAssessmentFingerprint == "" || plan.PolicyFingerprint != policy.Fingerprint() || plan.Fingerprint == "" || planFingerprint(plan) != plan.Fingerprint {
		return ErrInvalidPlan
	}
	seen := map[AdvisoryRequestID]struct{}{}
	for _, value := range plan.ResultingRequests {
		if value.EpisodeID != plan.EpisodeID {
			return ErrInvalidPlan
		}
		if _, ok := seen[value.ID]; ok {
			return ErrRequestIDCollision
		}
		seen[value.ID] = struct{}{}
		if err := validateRequest(value, policy); err != nil {
			return err
		}
	}
	resultByID := make(map[AdvisoryRequestID]AdvisoryEvidenceRequest, len(plan.ResultingRequests))
	for _, value := range plan.ResultingRequests {
		resultByID[value.ID] = value
	}
	for _, create := range plan.Creates {
		if create.EpisodeID != plan.EpisodeID || create.Revision != 1 {
			return ErrInvalidPlan
		}
		result, ok := resultByID[create.ID]
		if !ok || result.Fingerprint != create.Fingerprint {
			return ErrInvalidPlan
		}
	}
	for _, update := range plan.Updates {
		result, ok := resultByID[update.After.ID]
		if !ok || result.Fingerprint != update.After.Fingerprint || update.Before.ID != update.After.ID {
			return ErrInvalidPlan
		}
		if update.After.Revision <= update.Before.Revision {
			return ErrInvalidPlan
		}
	}
	for _, transition := range plan.Transitions {
		if transition.RequestID == "" || transition.Before == transition.After || !transitionAllowed(transition.Before, transition.After) {
			return ErrInvalidTransition
		}
		result, ok := resultByID[transition.RequestID]
		if !ok || result.Status != transition.After {
			return ErrInvalidPlan
		}
	}
	return nil
}

func validatePlanAgainstCurrent(plan AdvisoryPlan, current []AdvisoryEvidenceRequest, policy Policy) error {
	currentByID := map[AdvisoryRequestID]AdvisoryEvidenceRequest{}
	for _, value := range current {
		currentByID[value.ID] = value
	}
	resultByID := map[AdvisoryRequestID]AdvisoryEvidenceRequest{}
	for _, value := range plan.ResultingRequests {
		resultByID[value.ID] = value
	}
	for id, old := range currentByID {
		next, ok := resultByID[id]
		if !ok {
			return ErrInvalidPlan
		}
		if old.Status != next.Status {
			if !transitionAllowed(old.Status, next.Status) {
				return ErrInvalidTransition
			}
		}
		if old.Fingerprint != next.Fingerprint && next.Revision <= old.Revision {
			return ErrInvalidPlan
		}
		if old.Fingerprint == next.Fingerprint && old.Revision != next.Revision {
			return ErrInvalidPlan
		}
	}
	for _, update := range plan.Updates {
		old, ok := currentByID[update.Before.ID]
		if !ok || old.Fingerprint != update.Before.Fingerprint {
			return ErrSourceRevisionConflict
		}
	}
	for id, next := range resultByID {
		if _, existed := currentByID[id]; !existed && next.Revision != 1 {
			return ErrInvalidPlan
		}
	}
	return nil
}
