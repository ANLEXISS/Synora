package advisoryrequests

import "time"

func (r *Registry) ApplyDisposition(d AdvisoryDisposition) (DispositionResult, error) {
	if r == nil {
		return DispositionResult{}, ErrInvalidDisposition
	}
	if !validDisposition(d.Kind) || d.RequestID == "" || d.Actor == "" || len(d.Actor) > 128 || d.At.IsZero() || d.At.Location() != time.UTC || forbiddenTerm(d.Actor) {
		return DispositionResult{}, ErrInvalidDisposition
	}
	if d.ReasonCode != "" && forbiddenTerm(d.ReasonCode) {
		return DispositionResult{}, ErrInvalidDisposition
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.requests[d.RequestID]
	if !ok {
		return DispositionResult{}, ErrRequestNotFound
	}
	if current.Revision != d.SourceRevision {
		return DispositionResult{}, ErrSourceRevisionConflict
	}
	if terminal(current.Status) {
		return DispositionResult{}, ErrRequestTerminal
	}
	next := current.Clone()
	var target AdvisoryRequestStatus
	switch d.Kind {
	case DispositionAcknowledge:
		target = StatusAcknowledged
	case DispositionDefer:
		if d.DeferUntil == nil {
			return DispositionResult{}, ErrDeferUntilRequired
		}
		if d.DeferUntil.IsZero() || d.DeferUntil.Location() != time.UTC || !d.DeferUntil.After(d.At) {
			return DispositionResult{}, ErrDeferUntilInvalid
		}
		target = StatusDeferred
		next.DeferredUntil = cloneTime(d.DeferUntil)
	case DispositionCancel:
		target = StatusCancelled
	case DispositionRestoreProposal:
		if current.Status != StatusDeferred && current.Status != StatusSuppressed {
			return DispositionResult{}, ErrInvalidTransition
		}
		target = StatusProposed
	}
	if !transitionAllowed(current.Status, target) {
		return DispositionResult{}, ErrInvalidTransition
	}
	next.Status = target
	if target != StatusDeferred {
		next.DeferredUntil = nil
	}
	next.StatusChangedAt, next.LastEvaluatedAt = d.At, d.At
	if d.ReasonCode != "" {
		next.ReasonCodes = uniqueStrings(append(next.ReasonCodes, d.ReasonCode))
	}
	next.Revision++
	next.Fingerprint = requestFingerprint(next)
	if err := validateRequest(next, r.policy); err != nil {
		return DispositionResult{}, err
	}
	r.requests[next.ID] = next.Clone()
	r.revision++
	return DispositionResult{Applied: true, Request: next.Clone(), RegistryRevision: r.revision}, nil
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
