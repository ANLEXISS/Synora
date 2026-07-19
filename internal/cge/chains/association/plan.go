package association

import (
	"fmt"
	"strings"
	"time"

	"synora/internal/cge/chains"
)

// Decision is the explicit result of pure association planning.
type Decision string

const (
	DecisionAttachExisting  Decision = "attach_existing"
	DecisionCreateCandidate Decision = "create_candidate"
	DecisionAmbiguous       Decision = "ambiguous"
	DecisionAlreadyAttached Decision = "already_attached"
)

const (
	ReasonAlreadyAttached = "observation.already_attached"
	ReasonAttachExisting  = "association.attach_existing"
	ReasonCreateCandidate = "association.create_candidate"
	ReasonAmbiguous       = "association.ambiguous"
)

// Plan is a detached optimistic association plan. PlannedAt is supplied by
// the caller and is not used to derive any domain timestamp implicitly.
type Plan struct {
	PolicyVersion string
	PlannedAt     time.Time

	Decision    Decision
	Observation chains.ObservationRef

	SelectedChainID        chains.ChainID
	SelectedSourceRevision uint64
	NewChainID             chains.ChainID

	BestScore   int64
	ScoreMargin int64

	RankedCandidates []CandidateScore

	ReasonCode string
	Reason     string
}

// Validate checks the decision-specific shape of a plan before durable use.
func (p Plan) Validate() error {
	if strings.TrimSpace(p.PolicyVersion) == "" || p.PlannedAt.IsZero() || p.Decision == "" {
		return fmt.Errorf("%w: plan envelope is invalid", ErrInvalidPlan)
	}
	if err := p.Observation.Validate(); err != nil {
		return fmt.Errorf("%w: observation: %v", ErrInvalidPlan, err)
	}
	if p.BestScore < 0 || p.ScoreMargin < 0 || strings.TrimSpace(p.ReasonCode) == "" || strings.TrimSpace(p.Reason) == "" || strings.ContainsAny(p.Reason, "\r\n") || len([]rune(p.ReasonCode)) > 64 || len([]rune(p.Reason)) > 256 {
		return fmt.Errorf("%w: plan explanation is invalid", ErrInvalidPlan)
	}
	for _, candidate := range p.RankedCandidates {
		if err := candidate.validate(); err != nil {
			return fmt.Errorf("%w: candidate: %v", ErrInvalidPlan, err)
		}
	}
	switch p.Decision {
	case DecisionAttachExisting, DecisionAlreadyAttached:
		if _, err := chains.NewChainID(string(p.SelectedChainID)); err != nil || p.SelectedSourceRevision == 0 || p.NewChainID != "" {
			return fmt.Errorf("%w: selected chain is invalid", ErrInvalidPlan)
		}
	case DecisionCreateCandidate:
		if _, err := chains.NewChainID(string(p.NewChainID)); err != nil || p.SelectedChainID != "" || p.SelectedSourceRevision != 0 {
			return fmt.Errorf("%w: new candidate identity is invalid", ErrInvalidPlan)
		}
	case DecisionAmbiguous:
		if p.SelectedChainID != "" || p.NewChainID != "" || len(p.RankedCandidates) < 2 {
			return fmt.Errorf("%w: ambiguous plan lacks hypotheses", ErrInvalidPlan)
		}
	default:
		return fmt.Errorf("%w: unsupported decision %q", ErrInvalidPlan, p.Decision)
	}
	return nil
}

// Clone returns a detached plan copy.
func (p Plan) Clone() Plan {
	clone := p
	clone.RankedCandidates = make([]CandidateScore, len(p.RankedCandidates))
	for i, candidate := range p.RankedCandidates {
		clone.RankedCandidates[i] = candidate.clone()
	}
	return clone
}
