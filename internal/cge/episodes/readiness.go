package episodes

type EpisodeWorkingMemoryReadiness struct {
	DomainImplemented       bool
	PlannerDeterministic    bool
	RegistrySafe            bool
	LifecycleImplemented    bool
	PartialContextSupported bool
	OutOfOrderSupported     bool
	ConcurrencyValidated    bool

	RuntimeIntegrated             bool
	Durable                       bool
	SituationInferenceImplemented bool
	SecurityAuthority             bool

	ReadyForSituationFacts bool
	Limitations            []string
}

// ReadinessInput is evidence supplied by the qualification harness. Runtime
// integration, durability, situation inference and authority are intentionally
// not inferred from this package's unit tests.
type ReadinessInput struct {
	DomainImplemented       bool
	PlannerDeterministic    bool
	RegistrySafe            bool
	LifecycleImplemented    bool
	PartialContextSupported bool
	OutOfOrderSupported     bool
	ConcurrencyValidated    bool
	RuntimeIntegrated       bool
	Durable                 bool
	SituationInference      bool
	SecurityAuthority       bool
}

func BuildReadiness(input ReadinessInput) EpisodeWorkingMemoryReadiness {
	value := EpisodeWorkingMemoryReadiness{DomainImplemented: input.DomainImplemented, PlannerDeterministic: input.PlannerDeterministic, RegistrySafe: input.RegistrySafe, LifecycleImplemented: input.LifecycleImplemented, PartialContextSupported: input.PartialContextSupported, OutOfOrderSupported: input.OutOfOrderSupported, ConcurrencyValidated: input.ConcurrencyValidated, RuntimeIntegrated: input.RuntimeIntegrated, Durable: input.Durable, SituationInferenceImplemented: input.SituationInference, SecurityAuthority: input.SecurityAuthority}
	value.ReadyForSituationFacts = value.DomainImplemented && value.PlannerDeterministic && value.RegistrySafe && value.LifecycleImplemented && value.PartialContextSupported && value.OutOfOrderSupported && value.ConcurrencyValidated && !value.RuntimeIntegrated && !value.Durable && !value.SituationInferenceImplemented && !value.SecurityAuthority
	if !value.DomainImplemented {
		value.Limitations = append(value.Limitations, "domain_not_implemented")
	}
	if !value.PlannerDeterministic {
		value.Limitations = append(value.Limitations, "planner_not_validated")
	}
	if !value.RegistrySafe {
		value.Limitations = append(value.Limitations, "registry_not_validated")
	}
	if !value.LifecycleImplemented {
		value.Limitations = append(value.Limitations, "lifecycle_not_validated")
	}
	if !value.PartialContextSupported {
		value.Limitations = append(value.Limitations, "partial_context_not_supported")
	}
	if !value.OutOfOrderSupported {
		value.Limitations = append(value.Limitations, "out_of_order_not_supported")
	}
	if !value.ConcurrencyValidated {
		value.Limitations = append(value.Limitations, "concurrency_not_validated")
	}
	if !value.ReadyForSituationFacts {
		value.Limitations = append(value.Limitations, "experimental_in_memory_domain")
	}
	return value
}
