package episodes

import (
	"fmt"
	"time"
)

type LifecycleChange struct {
	EpisodeID      EpisodeID
	From           EpisodeStatus
	To             EpisodeStatus
	SourceRevision uint64
	Reason         string
	EvaluatedAt    time.Time
}

type LifecycleBatch struct {
	SourceRevision    uint64
	EvaluatedAt       time.Time
	Changes           []LifecycleChange
	PolicyFingerprint string
}

type LifecycleApplyResult struct {
	Applied          int
	RegistryRevision uint64
}

func EvaluateLifecycle(snapshot Snapshot, evaluatedAt time.Time, policy Policy) LifecycleBatch {
	batch := LifecycleBatch{SourceRevision: snapshot.Revision, EvaluatedAt: evaluatedAt, PolicyFingerprint: policy.Fingerprint()}
	if evaluatedAt.IsZero() || policy.Validate() != nil || snapshot.Validate() != nil {
		return batch
	}
	for _, episode := range snapshot.Episodes {
		inactive := evaluatedAt.Sub(episode.LastObservedAt)
		switch episode.Status {
		case StatusOpen:
			if inactive >= policy.QuiescentAfter {
				batch.Changes = append(batch.Changes, LifecycleChange{EpisodeID: episode.ID, From: StatusOpen, To: StatusQuiescent, SourceRevision: episode.Revision, Reason: "inactive.quiescent_after", EvaluatedAt: evaluatedAt})
			}
		case StatusQuiescent:
			if inactive >= policy.CloseAfter {
				batch.Changes = append(batch.Changes, LifecycleChange{EpisodeID: episode.ID, From: StatusQuiescent, To: StatusClosed, SourceRevision: episode.Revision, Reason: "inactive.close_after", EvaluatedAt: evaluatedAt})
			}
		}
	}
	return batch
}

func (r *Registry) ApplyLifecycleBatch(batch LifecycleBatch, actor string) (LifecycleApplyResult, error) {
	if r == nil {
		return LifecycleApplyResult{}, ErrEpisodeNotFound
	}
	if batch.EvaluatedAt.IsZero() || batch.PolicyFingerprint == "" || actor == "" {
		return LifecycleApplyResult{}, ErrInvalidPlan
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if batch.PolicyFingerprint != r.policyFingerprint || batch.SourceRevision != r.revision {
		return LifecycleApplyResult{}, ErrSourceRevisionConflict
	}
	working := make(map[EpisodeID]Episode, len(batch.Changes))
	seen := make(map[EpisodeID]struct{}, len(batch.Changes))
	for _, change := range batch.Changes {
		if _, exists := seen[change.EpisodeID]; exists {
			return LifecycleApplyResult{}, fmt.Errorf("%w: duplicate lifecycle change", ErrInvalidTransition)
		}
		seen[change.EpisodeID] = struct{}{}
		episode, ok := r.episodes[change.EpisodeID]
		if !ok {
			return LifecycleApplyResult{}, ErrEpisodeNotFound
		}
		if episode.Revision != change.SourceRevision || episode.Status != change.From {
			return LifecycleApplyResult{}, ErrSourceRevisionConflict
		}
		if !validLifecycleTransition(change.From, change.To) {
			return LifecycleApplyResult{}, fmt.Errorf("%w: %s to %s", ErrInvalidTransition, change.From, change.To)
		}
		working[episode.ID] = episode.Clone()
	}
	for _, change := range batch.Changes {
		episode := working[change.EpisodeID]
		episode.Status = change.To
		episode.StatusChangedAt = change.EvaluatedAt
		if change.To == StatusClosed {
			closed := change.EvaluatedAt
			episode.ClosedAt = &closed
		}
		episode.Revision++
		if err := episode.Validate(); err != nil {
			return LifecycleApplyResult{}, err
		}
		working[episode.ID] = episode
	}
	for id, episode := range working {
		r.episodes[id] = episode
	}
	if len(working) > 0 {
		r.revision++
	}
	return LifecycleApplyResult{Applied: len(working), RegistryRevision: r.revision}, nil
}

// CanTransition reports whether a lifecycle transition is allowed by the
// episode state machine. It has no side effects.
func CanTransition(from, to EpisodeStatus) bool { return validLifecycleTransition(from, to) }

func validLifecycleTransition(from, to EpisodeStatus) bool {
	if from == to {
		return false
	}
	switch from {
	case StatusOpen:
		return to == StatusQuiescent || to == StatusClosed || to == StatusInvalidated
	case StatusQuiescent:
		return to == StatusOpen || to == StatusClosed || to == StatusInvalidated
	default:
		return false
	}
}
