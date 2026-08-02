package situationfacts

type SituationFactsReadiness struct {
	SchemaImplemented       bool
	ExtractionDeterministic bool
	ProvenancePreserved     bool
	ConflictsPreserved      bool
	UnknownDimensionsSafe   bool
	DiffImplemented         bool
	RegistrySafe            bool
	ConcurrencyValidated    bool

	RuntimeIntegrated                  bool
	Durable                            bool
	SituationHypothesesImplemented     bool
	SituationInterpretationImplemented bool
	SecurityAuthority                  bool

	ReadyForSituationHypotheses bool
	Limitations                 []string
}

type SituationFactsPerformanceReadiness struct {
	SemanticEquivalenceValidated bool
	GoldenCorpusStable           bool

	FullExtractionOptimized          bool
	IncrementalExtractionImplemented bool
	IncrementalFallbackSafe          bool
	DiffOptimized                    bool
	RegistryOptimized                bool
	SnapshotOptimized                bool
	DigestCacheSafe                  bool

	ConcurrencyValidated bool

	SchemaFingerprintUnchanged bool
	PolicyFingerprintUnchanged bool
	FactIdentifiersUnchanged   bool

	RuntimeIntegrated              bool
	Durable                        bool
	SituationHypothesesImplemented bool
	SecurityAuthority              bool

	ReadyForSituationHypotheses bool
	Limitations                 []string
}

type PerformanceReadinessInput struct {
	SemanticEquivalenceValidated     bool
	GoldenCorpusStable               bool
	FullExtractionOptimized          bool
	IncrementalExtractionImplemented bool
	IncrementalFallbackSafe          bool
	DiffOptimized                    bool
	RegistryOptimized                bool
	SnapshotOptimized                bool
	DigestCacheSafe                  bool
	ConcurrencyValidated             bool
	SchemaFingerprintUnchanged       bool
	PolicyFingerprintUnchanged       bool
	FactIdentifiersUnchanged         bool
	RuntimeIntegrated                bool
	Durable                          bool
	SituationHypothesesImplemented   bool
	SecurityAuthority                bool
}

func BuildPerformanceReadiness(input PerformanceReadinessInput) SituationFactsPerformanceReadiness {
	value := SituationFactsPerformanceReadiness{
		SemanticEquivalenceValidated:     input.SemanticEquivalenceValidated,
		GoldenCorpusStable:               input.GoldenCorpusStable,
		FullExtractionOptimized:          input.FullExtractionOptimized,
		IncrementalExtractionImplemented: input.IncrementalExtractionImplemented,
		IncrementalFallbackSafe:          input.IncrementalFallbackSafe,
		DiffOptimized:                    input.DiffOptimized,
		RegistryOptimized:                input.RegistryOptimized,
		SnapshotOptimized:                input.SnapshotOptimized,
		DigestCacheSafe:                  input.DigestCacheSafe,
		ConcurrencyValidated:             input.ConcurrencyValidated,
		SchemaFingerprintUnchanged:       input.SchemaFingerprintUnchanged,
		PolicyFingerprintUnchanged:       input.PolicyFingerprintUnchanged,
		FactIdentifiersUnchanged:         input.FactIdentifiersUnchanged,
		RuntimeIntegrated:                input.RuntimeIntegrated,
		Durable:                          input.Durable,
		SituationHypothesesImplemented:   input.SituationHypothesesImplemented,
		SecurityAuthority:                input.SecurityAuthority,
	}
	value.ReadyForSituationHypotheses = value.SemanticEquivalenceValidated && value.GoldenCorpusStable && value.FullExtractionOptimized && value.IncrementalExtractionImplemented && value.IncrementalFallbackSafe && value.DiffOptimized && value.RegistryOptimized && value.SnapshotOptimized && value.DigestCacheSafe && value.ConcurrencyValidated && value.SchemaFingerprintUnchanged && value.PolicyFingerprintUnchanged && value.FactIdentifiersUnchanged && !value.RuntimeIntegrated && !value.Durable && !value.SituationHypothesesImplemented && !value.SecurityAuthority
	if !value.ReadyForSituationHypotheses {
		value.Limitations = append(value.Limitations, "performance_or_equivalence_validation_incomplete")
	}
	return value
}

type ReadinessInput struct {
	SchemaImplemented       bool
	ExtractionDeterministic bool
	ProvenancePreserved     bool
	ConflictsPreserved      bool
	UnknownDimensionsSafe   bool
	DiffImplemented         bool
	RegistrySafe            bool
	ConcurrencyValidated    bool
	RuntimeIntegrated       bool
	Durable                 bool
	SituationHypotheses     bool
	SituationInterpretation bool
	SecurityAuthority       bool
}

func BuildReadiness(input ReadinessInput) SituationFactsReadiness {
	value := SituationFactsReadiness{SchemaImplemented: input.SchemaImplemented, ExtractionDeterministic: input.ExtractionDeterministic, ProvenancePreserved: input.ProvenancePreserved, ConflictsPreserved: input.ConflictsPreserved, UnknownDimensionsSafe: input.UnknownDimensionsSafe, DiffImplemented: input.DiffImplemented, RegistrySafe: input.RegistrySafe, ConcurrencyValidated: input.ConcurrencyValidated, RuntimeIntegrated: input.RuntimeIntegrated, Durable: input.Durable, SituationHypothesesImplemented: input.SituationHypotheses, SituationInterpretationImplemented: input.SituationInterpretation, SecurityAuthority: input.SecurityAuthority}
	value.ReadyForSituationHypotheses = value.SchemaImplemented && value.ExtractionDeterministic && value.ProvenancePreserved && value.ConflictsPreserved && value.UnknownDimensionsSafe && value.DiffImplemented && value.RegistrySafe && value.ConcurrencyValidated && !value.RuntimeIntegrated && !value.Durable && !value.SituationHypothesesImplemented && !value.SituationInterpretationImplemented && !value.SecurityAuthority
	if !value.SchemaImplemented {
		value.Limitations = append(value.Limitations, "schema_not_implemented")
	}
	if !value.ExtractionDeterministic {
		value.Limitations = append(value.Limitations, "extraction_not_validated")
	}
	if !value.ProvenancePreserved {
		value.Limitations = append(value.Limitations, "provenance_not_preserved")
	}
	if !value.ConflictsPreserved {
		value.Limitations = append(value.Limitations, "conflicts_not_preserved")
	}
	if !value.UnknownDimensionsSafe {
		value.Limitations = append(value.Limitations, "unknown_dimensions_not_safe")
	}
	if !value.DiffImplemented {
		value.Limitations = append(value.Limitations, "diff_not_implemented")
	}
	if !value.RegistrySafe {
		value.Limitations = append(value.Limitations, "registry_not_safe")
	}
	if !value.ConcurrencyValidated {
		value.Limitations = append(value.Limitations, "concurrency_not_validated")
	}
	if !value.ReadyForSituationHypotheses {
		value.Limitations = append(value.Limitations, "experimental_in_memory_facts")
	}
	return value
}
