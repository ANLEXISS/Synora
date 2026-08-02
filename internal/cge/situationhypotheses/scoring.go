package situationhypotheses

func lifecycleStatus(previous, next HypothesisStatus, plausibility, support, contradiction int) HypothesisStatus {
	if previous == StatusInvalidated {
		return StatusInvalidated
	}
	if next == StatusCandidate || next == StatusSupported || next == StatusWeakened || next == StatusContradicted || next == StatusInsufficientInformation {
		return next
	}
	return StatusCandidate
}

func EvaluateLifecycle(previous SituationHypothesis, nextEvaluation SituationHypothesis, policy Policy) LifecycleDecision {
	if err := policy.Validate(); err != nil {
		return LifecycleDecision{Source: previous.Status, Target: previous.Status, ReasonCode: "invalid_policy", Valid: false}
	}
	if previous.ID == "" || nextEvaluation.ID != previous.ID || previous.Status == StatusInvalidated {
		return LifecycleDecision{Source: previous.Status, Target: StatusInvalidated, ReasonCode: "terminal_or_identity_mismatch", Valid: false}
	}
	valid := validHypothesisStatus(nextEvaluation.Status) && allowedTransition(previous.Status, nextEvaluation.Status)
	return LifecycleDecision{Source: previous.Status, Target: nextEvaluation.Status, ReasonCode: "evaluation_updated", Valid: valid}
}

func allowedTransition(source, target HypothesisStatus) bool {
	if source == target {
		return true
	}
	if target == StatusInvalidated {
		return source != StatusInvalidated
	}
	switch source {
	case StatusCandidate:
		return target == StatusSupported || target == StatusWeakened || target == StatusContradicted || target == StatusInsufficientInformation
	case StatusSupported:
		return target == StatusWeakened || target == StatusContradicted || target == StatusInsufficientInformation
	case StatusWeakened:
		return target == StatusSupported || target == StatusContradicted || target == StatusInsufficientInformation
	case StatusContradicted:
		return target == StatusWeakened || target == StatusSupported
	case StatusInsufficientInformation:
		return target == StatusCandidate || target == StatusSupported || target == StatusWeakened
	default:
		return false
	}
}

type LifecycleDecision struct {
	Source     HypothesisStatus
	Target     HypothesisStatus
	ReasonCode string
	Valid      bool
}
