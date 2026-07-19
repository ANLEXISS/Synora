package evidence

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/chains"
)

var (
	ErrInvalidEvidenceBatchOptions = errors.New("invalid_evidence_batch_options")
	ErrInvalidEvidenceBatch        = errors.New("invalid_evidence_batch")
	ErrEvidenceBatchEvaluation     = errors.New("evidence_batch_evaluation_failed")
)

// BatchOptions bounds and selects one deterministic pure evaluation pass.
// Active includes candidate, active, confirmed, declining, and reactivated
// chains. Historical includes dormant and archived chains.
type BatchOptions struct {
	MaxChains               int
	MaxObservationsPerChain int

	IncludeActive     bool
	IncludeHistorical bool
}

// DefaultBatchOptions is deliberately bounded. A batch never has an implicit
// unlimited traversal.
func DefaultBatchOptions() BatchOptions {
	return BatchOptions{
		MaxChains:               1000,
		MaxObservationsPerChain: 256,
		IncludeActive:           true,
		IncludeHistorical:       true,
	}
}

func (o BatchOptions) Validate() error {
	if o.MaxChains <= 0 || o.MaxObservationsPerChain <= 0 {
		return fmt.Errorf("%w: limits must be strictly positive", ErrInvalidEvidenceBatchOptions)
	}
	if !o.IncludeActive && !o.IncludeHistorical {
		return fmt.Errorf("%w: at least one chain category must be included", ErrInvalidEvidenceBatchOptions)
	}
	return nil
}

// ObservationEvaluationResult is a detached result for one selected
// observation. Error is a stable, sanitized message rather than a Go error.
type ObservationEvaluationResult struct {
	ChainID             chains.ChainID
	SourceRevision      uint64
	TargetObservationID string

	Evaluated bool
	Deferred  bool

	Evaluation *EvidenceEvaluation

	ErrorCode string
	Error     string
}

// ChainEvidenceResult contains all selected work for one chain. At most one
// proposal can be selected, because applying the first proposal advances the
// source revision used by every later proposal from that chain.
type ChainEvidenceResult struct {
	ChainID        chains.ChainID
	SourceRevision uint64
	Status         chains.Status

	ObservationsConsidered int
	ObservationsEvaluated  int
	ObservationsDeferred   int

	SelectedProposal *ContributionProposal

	Results []ObservationEvaluationResult

	ErrorCode string
	Error     string
}

// EvidenceBatch is a complete detached result of one explicit pure pass.
type EvidenceBatch struct {
	EvaluatedAt time.Time

	PolicyNamespace string
	PolicyVersion   string

	Options BatchOptions

	CapturedChainCount int
	SelectedChainCount int

	ObservationsConsidered int
	ObservationsEvaluated  int
	ObservationsDeferred   int

	SupportProposals       int
	ContradictionProposals int
	NeutralProposals       int

	AlreadyEvaluated     int
	Ambiguous            int
	InsufficientEvidence int
	EvaluationErrors     int

	ChainResults []ChainEvidenceResult
	Proposals    []ContributionProposal
}

type batchChain struct {
	snapshot chains.Snapshot
	valid    bool
}

// EvaluateBatch evaluates bounded observations in deterministic chain and
// observation order. It never writes, creates revisions, or applies a
// proposal. A malformed chain is isolated to its ChainEvidenceResult;
// malformed global policy, options, or timestamp fail the entire batch.
func EvaluateBatch(snapshots []chains.Snapshot, evaluatedAt time.Time, policy Policy, options BatchOptions) (EvidenceBatch, error) {
	if err := policy.Validate(); err != nil {
		return EvidenceBatch{}, err
	}
	if evaluatedAt.IsZero() {
		return EvidenceBatch{}, fmt.Errorf("%w: evaluated_at must not be zero", ErrInvalidEvidenceBatch)
	}
	if err := options.Validate(); err != nil {
		return EvidenceBatch{}, err
	}

	batch := EvidenceBatch{
		EvaluatedAt:        evaluatedAt,
		PolicyNamespace:    policy.Namespace,
		PolicyVersion:      policy.Version,
		Options:            options,
		CapturedChainCount: len(snapshots),
	}
	candidates := make([]batchChain, 0, len(snapshots))
	for _, snapshot := range snapshots {
		_, err := chains.Restore(snapshot)
		candidates = append(candidates, batchChain{snapshot: snapshot, valid: err == nil})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].snapshot.ID < candidates[j].snapshot.ID
	})
	for index := 1; index < len(candidates); index++ {
		if candidates[index-1].snapshot.ID == candidates[index].snapshot.ID {
			return EvidenceBatch{}, fmt.Errorf("%w: duplicate chain snapshot %s", ErrInvalidEvidenceBatch, candidates[index].snapshot.ID)
		}
	}
	selected := make([]batchChain, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.valid || includeStatus(candidate.snapshot.Status, options) {
			selected = append(selected, candidate)
		}
		if len(selected) == options.MaxChains {
			break
		}
	}
	batch.SelectedChainCount = len(selected)
	batch.ChainResults = make([]ChainEvidenceResult, 0, len(selected))

	for _, candidate := range selected {
		result := evaluateChain(candidate.snapshot, candidate.valid, evaluatedAt, policy, options)
		batch.ChainResults = append(batch.ChainResults, result)
		batch.ObservationsConsidered += result.ObservationsConsidered
		batch.ObservationsEvaluated += result.ObservationsEvaluated
		batch.ObservationsDeferred += result.ObservationsDeferred
		if result.ErrorCode != "" {
			batch.EvaluationErrors++
		}
		if result.SelectedProposal != nil {
			batch.Proposals = append(batch.Proposals, result.SelectedProposal.clone())
			switch result.SelectedProposal.Decision {
			case DecisionProposeSupport:
				batch.SupportProposals++
			case DecisionProposeContradiction:
				batch.ContradictionProposals++
			case DecisionProposeNeutral:
				batch.NeutralProposals++
			}
		}
		for _, observationResult := range result.Results {
			if observationResult.ErrorCode != "" && !observationResult.Deferred {
				batch.EvaluationErrors++
			}
			if !observationResult.Evaluated || observationResult.Evaluation == nil {
				continue
			}
			switch observationResult.Evaluation.Decision {
			case DecisionAlreadyEvaluated:
				batch.AlreadyEvaluated++
			case DecisionAmbiguous:
				batch.Ambiguous++
			case DecisionInsufficientEvidence:
				batch.InsufficientEvidence++
			}
		}
	}
	return cloneBatch(batch), nil
}

func includeStatus(status chains.Status, options BatchOptions) bool {
	if options.IncludeActive {
		switch status {
		case chains.StatusCandidate, chains.StatusActive, chains.StatusConfirmed, chains.StatusDeclining, chains.StatusReactivated:
			return true
		}
	}
	if options.IncludeHistorical {
		return status == chains.StatusDormant || status == chains.StatusArchived
	}
	return false
}

func evaluateChain(snapshot chains.Snapshot, valid bool, evaluatedAt time.Time, policy Policy, options BatchOptions) ChainEvidenceResult {
	result := ChainEvidenceResult{
		ChainID:        snapshot.ID,
		SourceRevision: snapshot.Revision,
		Status:         snapshot.Status,
	}
	if !valid {
		result.ErrorCode = "invalid_chain_snapshot"
		result.Error = "chain snapshot validation failed"
		return result
	}
	observations := append([]chains.ObservationRef(nil), snapshot.Observations...)
	sort.SliceStable(observations, func(i, j int) bool {
		if observations[i].Timestamp.Equal(observations[j].Timestamp) {
			return observations[i].ID < observations[j].ID
		}
		return observations[i].Timestamp.Before(observations[j].Timestamp)
	})
	if len(observations) > options.MaxObservationsPerChain {
		observations = observations[:options.MaxObservationsPerChain]
	}
	result.ObservationsConsidered = len(observations)
	result.Results = make([]ObservationEvaluationResult, 0, len(observations))
	proposalSelected := false
	for _, observation := range observations {
		if proposalSelected {
			result.ObservationsDeferred++
			result.Results = append(result.Results, ObservationEvaluationResult{
				ChainID: snapshot.ID, SourceRevision: snapshot.Revision,
				TargetObservationID: observation.ID, Deferred: true,
				ErrorCode: "deferred_after_selected_proposal",
				Error:     "observation deferred after the chain proposal was selected",
			})
			continue
		}
		evaluation, err := EvaluateObservation(snapshot, observation.ID, evaluatedAt, policy)
		if err != nil {
			result.Results = append(result.Results, ObservationEvaluationResult{
				ChainID: snapshot.ID, SourceRevision: snapshot.Revision,
				TargetObservationID: observation.ID, ErrorCode: evaluationErrorCode(err),
				Error: sanitizedEvaluationError(evaluationErrorCode(err)),
			})
			continue
		}
		result.ObservationsEvaluated++
		observationResult := ObservationEvaluationResult{
			ChainID: snapshot.ID, SourceRevision: snapshot.Revision,
			TargetObservationID: observation.ID, Evaluated: true,
			Evaluation: cloneEvaluationPointer(&evaluation),
		}
		if evaluation.Proposal != nil {
			proposal := evaluation.Proposal.clone()
			result.SelectedProposal = &proposal
			proposalSelected = true
		}
		result.Results = append(result.Results, observationResult)
	}
	return cloneChainResult(result)
}

func evaluationErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrEvidenceContributionCollision):
		return "evidence_contribution_collision"
	case errors.Is(err, ErrTargetObservationNotFound):
		return "target_evaluation_failed"
	case errors.Is(err, ErrUnsupportedObservationType):
		return "target_evaluation_failed"
	case errors.Is(err, ErrEvidenceEvaluationNotAllowed):
		return "target_evaluation_failed"
	case errors.Is(err, ErrInvalidEvaluationTime):
		return "target_evaluation_failed"
	default:
		return "evaluation_failed"
	}
}

func sanitizedEvaluationError(code string) string {
	if code == "evidence_contribution_collision" {
		return "evidence contribution identity collides with existing data"
	}
	return "observation evaluation failed"
}

func (p ContributionProposal) clone() ContributionProposal {
	p.Contribution = p.Contribution.Clone()
	return p
}

func (p *ContributionProposal) clonePointer() *ContributionProposal {
	if p == nil {
		return nil
	}
	copy := p.clone()
	return &copy
}

func cloneEvaluationPointer(evaluation *EvidenceEvaluation) *EvidenceEvaluation {
	if evaluation == nil {
		return nil
	}
	copy := cloneEvaluation(*evaluation)
	return &copy
}

func cloneChainResult(result ChainEvidenceResult) ChainEvidenceResult {
	result.Results = append([]ObservationEvaluationResult(nil), result.Results...)
	for index := range result.Results {
		result.Results[index].Evaluation = cloneEvaluationPointer(result.Results[index].Evaluation)
	}
	result.SelectedProposal = result.SelectedProposal.clonePointer()
	return result
}

func cloneBatch(batch EvidenceBatch) EvidenceBatch {
	chainResults := batch.ChainResults
	batch.ChainResults = make([]ChainEvidenceResult, len(chainResults))
	for index, result := range chainResults {
		batch.ChainResults[index] = cloneChainResult(result)
	}
	proposals := batch.Proposals
	batch.Proposals = make([]ContributionProposal, len(proposals))
	for index, proposal := range proposals {
		batch.Proposals[index] = proposal.clone()
	}
	return batch
}
