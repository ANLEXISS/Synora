package situationhypotheses

type SituationHypothesesReadiness struct {
	SchemaImplemented            bool
	EvaluationDeterministic      bool
	CompetingHypothesesPreserved bool
	ContributionsTraceable       bool
	ContradictionsPreserved      bool
	MissingInformationPreserved  bool

	DiffReevaluationImplemented  bool
	FullDiffEquivalenceValidated bool

	RegistrySafe            bool
	ConcurrencyValidated    bool
	ExplanationsImplemented bool

	RuntimeIntegrated                 bool
	Durable                           bool
	ActiveEvidenceRequestsImplemented bool
	SituationResolvedAutomatically    bool
	SecurityAuthority                 bool

	ReadyForEvidenceDiscrimination bool
	Limitations                    []string
}

type ReadinessInput struct {
	SchemaImplemented                 bool
	EvaluationDeterministic           bool
	CompetingHypothesesPreserved      bool
	ContributionsTraceable            bool
	ContradictionsPreserved           bool
	MissingInformationPreserved       bool
	DiffReevaluationImplemented       bool
	FullDiffEquivalenceValidated      bool
	RegistrySafe                      bool
	ConcurrencyValidated              bool
	ExplanationsImplemented           bool
	RuntimeIntegrated                 bool
	Durable                           bool
	ActiveEvidenceRequestsImplemented bool
	SituationResolvedAutomatically    bool
	SecurityAuthority                 bool
}

func BuildReadiness(input ReadinessInput) SituationHypothesesReadiness {
	value := SituationHypothesesReadiness{
		SchemaImplemented: input.SchemaImplemented, EvaluationDeterministic: input.EvaluationDeterministic,
		CompetingHypothesesPreserved: input.CompetingHypothesesPreserved, ContributionsTraceable: input.ContributionsTraceable,
		ContradictionsPreserved: input.ContradictionsPreserved, MissingInformationPreserved: input.MissingInformationPreserved,
		DiffReevaluationImplemented: input.DiffReevaluationImplemented, FullDiffEquivalenceValidated: input.FullDiffEquivalenceValidated,
		RegistrySafe: input.RegistrySafe, ConcurrencyValidated: input.ConcurrencyValidated, ExplanationsImplemented: input.ExplanationsImplemented,
		RuntimeIntegrated: input.RuntimeIntegrated, Durable: input.Durable, ActiveEvidenceRequestsImplemented: input.ActiveEvidenceRequestsImplemented,
		SituationResolvedAutomatically: input.SituationResolvedAutomatically, SecurityAuthority: input.SecurityAuthority,
	}
	value.ReadyForEvidenceDiscrimination = value.SchemaImplemented && value.EvaluationDeterministic && value.CompetingHypothesesPreserved && value.ContributionsTraceable && value.ContradictionsPreserved && value.MissingInformationPreserved && value.DiffReevaluationImplemented && value.FullDiffEquivalenceValidated && value.RegistrySafe && value.ConcurrencyValidated && value.ExplanationsImplemented && !value.RuntimeIntegrated && !value.Durable && !value.ActiveEvidenceRequestsImplemented && !value.SituationResolvedAutomatically && !value.SecurityAuthority
	if !value.ReadyForEvidenceDiscrimination {
		value.Limitations = append(value.Limitations, "hypothesis_domain_validation_incomplete")
	}
	return value
}
