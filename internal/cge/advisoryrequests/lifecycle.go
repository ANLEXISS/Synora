package advisoryrequests

type LifecycleDecision struct {
	Allowed    bool
	ReasonCode string
}

func EvaluateLifecycle(previous, next AdvisoryRequestStatus) LifecycleDecision {
	if !validStatus(previous) || !validStatus(next) || previous == next || terminal(previous) {
		return LifecycleDecision{ReasonCode: "transition_not_allowed"}
	}
	allowed := false
	switch previous {
	case StatusProposed:
		allowed = next == StatusAcknowledged || next == StatusDeferred || next == StatusSuppressed || terminal(next)
	case StatusAcknowledged:
		allowed = next == StatusDeferred || next == StatusSuppressed || terminal(next)
	case StatusDeferred:
		allowed = next == StatusProposed || next == StatusAcknowledged || next == StatusSuppressed || terminal(next)
	case StatusSuppressed:
		allowed = next == StatusProposed || terminal(next)
	}
	if !allowed {
		return LifecycleDecision{ReasonCode: "transition_not_allowed"}
	}
	return LifecycleDecision{Allowed: true, ReasonCode: "transition_allowed"}
}
func transitionAllowed(previous, next AdvisoryRequestStatus) bool {
	return EvaluateLifecycle(previous, next).Allowed
}
