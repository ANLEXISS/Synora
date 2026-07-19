package validation

import (
	"fmt"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/registry"
	"synora/internal/cge/hypotheses"
)

type CapabilityStatus string

const (
	CapabilityReachable   CapabilityStatus = "reachable"
	CapabilityUnreachable CapabilityStatus = "unreachable"
	CapabilityDormant     CapabilityStatus = "dormant"
)

type AssociationCapabilityReport struct {
	AttachExistingStatus         CapabilityStatus `json:"attach_existing_status"`
	CreateCandidateStatus        CapabilityStatus `json:"create_candidate_status"`
	AmbiguousAttachReachable     bool             `json:"ambiguous_attach_reachable"`
	AmbiguousCreateReachable     bool             `json:"ambiguous_create_reachable"`
	TransactionalCreateCandidate bool             `json:"transactional_create_candidate"`
	ReasonCode                   string           `json:"reason_code"`
	Details                      []string         `json:"details"`
}

// InspectAssociationCapabilities explores only public planner/conversion
// paths. It never mutates a durable registry or creates a hypothesis.
func InspectAssociationCapabilities(policy association.Policy) (AssociationCapabilityReport, error) {
	if err := policy.Validate(); err != nil {
		return AssociationCapabilityReport{}, err
	}
	base := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	target := chains.ObservationRef{ID: "capability-target", EventType: "vision.identity", Timestamp: base.Add(2 * time.Second), EntityID: "entity", SequenceKey: "sequence"}
	makeCandidate := func(id, seed string) (chains.Snapshot, error) {
		chain, err := chains.New(chains.ChainID(id), chains.MutationContext{At: base, Actor: "capability-audit", Reason: "synthetic planner input", CorrelationID: id})
		if err != nil {
			return chains.Snapshot{}, err
		}
		if err := chain.AddObservation(chains.ObservationRef{ID: seed, EventType: "vision.identity", Timestamp: base.Add(time.Second), EntityID: "entity", SequenceKey: "sequence"}, chains.MutationContext{At: base.Add(time.Second), Actor: "capability-audit", Reason: "synthetic seed", CorrelationID: seed}); err != nil {
			return chains.Snapshot{}, err
		}
		return chain.Snapshot(), nil
	}
	first, err := makeCandidate("cge-capability-a", "capability-seed-a")
	if err != nil {
		return AssociationCapabilityReport{}, err
	}
	second, err := makeCandidate("cge-capability-b", "capability-seed-b")
	if err != nil {
		return AssociationCapabilityReport{}, err
	}
	ambiguous, err := association.PlanAssociation([]chains.Snapshot{first, second}, association.Input{Observation: target}, base.Add(3*time.Second), policy)
	if err != nil {
		return AssociationCapabilityReport{}, err
	}
	empty, err := association.PlanAssociation(nil, association.Input{Observation: target}, base.Add(3*time.Second), policy)
	if err != nil {
		return AssociationCapabilityReport{}, err
	}
	report := AssociationCapabilityReport{AttachExistingStatus: CapabilityUnreachable, CreateCandidateStatus: CapabilityDormant, Details: []string{}, ReasonCode: "association_create_candidate_not_reachable"}
	if ambiguous.Decision == association.DecisionAmbiguous {
		report.AmbiguousAttachReachable = true
		report.AttachExistingStatus = CapabilityReachable
		set, conversionErr := hypotheses.FromAmbiguousAssociation(ambiguous, base.Add(4*time.Second), chains.MutationContext{At: base.Add(4 * time.Second), Actor: "capability-audit", Reason: "audit conversion", CorrelationID: "capability-conversion"})
		if conversionErr != nil {
			return AssociationCapabilityReport{}, conversionErr
		}
		for _, alternative := range set.Snapshot().Alternatives {
			if alternative.Kind != hypotheses.AlternativeAttachExisting {
				return AssociationCapabilityReport{}, fmt.Errorf("ambiguous association conversion produced unexpected alternative")
			}
		}
		report.Details = append(report.Details, "public ambiguous plans contain eligible existing candidates only")
	}
	if empty.Decision != association.DecisionCreateCandidate || empty.NewChainID == "" {
		return AssociationCapabilityReport{}, fmt.Errorf("public create-candidate branch was not observed")
	}
	effect := hypotheses.CreateCandidateEffect{ChainID: empty.NewChainID, Observation: target, InitialStatus: chains.StatusCandidate, InitialConfidence: 0}
	chain, err := effect.BuildChain(chains.MutationContext{At: base.Add(5 * time.Second), Actor: "capability-audit", Reason: "transactional capability", CorrelationID: "capability-create"})
	if err != nil {
		return AssociationCapabilityReport{}, err
	}
	owned := registry.New()
	if err := owned.Add(chain); err != nil {
		return AssociationCapabilityReport{}, err
	}
	created, err := owned.Get(empty.NewChainID)
	if err != nil || created.Status != chains.StatusCandidate || len(created.Observations) != 1 {
		return AssociationCapabilityReport{}, fmt.Errorf("create-candidate constructor capability failed: %v", err)
	}
	report.TransactionalCreateCandidate = true
	report.Details = append(report.Details, "create_candidate is produced by the public planner only when no eligible candidate remains")
	report.Details = append(report.Details, "hypotheses.FromAmbiguousAssociation accepts only DecisionAmbiguous and converts every eligible candidate to attach_existing")
	return report, nil
}
