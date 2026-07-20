package capabilitymapping

type LifecycleDecision struct {
	Allowed    bool
	ReasonCode string
}

func EvaluateMappingLifecycle(previous, next CapabilityMappingStatus) LifecycleDecision {
	if previous == "" || next == "" || previous == next || mappingTerminal(previous) {
		return LifecycleDecision{ReasonCode: "mapping_transition_not_allowed"}
	}
	allowed := false
	switch previous {
	case MappingCandidate:
		allowed = next == MappingCompatible || next == MappingCompatibleDegraded || next == MappingUnavailable || next == MappingIncompatible || next == MappingInvalidated || next == MappingObsolete
	case MappingCompatible, MappingCompatibleDegraded:
		allowed = next == MappingCompatible || next == MappingCompatibleDegraded || next == MappingUnavailable || next == MappingIncompatible || next == MappingInvalidated || next == MappingObsolete
	case MappingUnavailable, MappingIncompatible:
		allowed = next == MappingCandidate || next == MappingCompatible || next == MappingCompatibleDegraded || next == MappingUnavailable || next == MappingIncompatible || next == MappingInvalidated || next == MappingObsolete
	}
	if !allowed {
		return LifecycleDecision{ReasonCode: "mapping_transition_not_allowed"}
	}
	return LifecycleDecision{Allowed: true, ReasonCode: "mapping_transition_allowed"}
}

func mappingTerminal(value CapabilityMappingStatus) bool {
	return value == MappingObsolete || value == MappingInvalidated
}
