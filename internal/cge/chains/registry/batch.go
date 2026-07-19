package registry

import (
	"errors"
	"sort"
	"time"

	"synora/internal/cge/chains"
)

const (
	CodeEvaluationFailed         = "evaluation_failed"
	CodeChainNotFound            = "chain_not_found"
	CodeStaleProposal            = "stale_proposal"
	CodeInvalidProposal          = "invalid_proposal"
	CodeInvalidTransition        = "invalid_transition"
	CodeInvalidActor             = "invalid_actor"
	CodeInvalidCorrelation       = "invalid_correlation"
	CodeInternalValidationFailed = "internal_validation_failed"
)

// ChainEvaluationResult is one deterministic result for one captured chain.
// Error is a stable string representation; no non-serializable error object is
// retained in the batch.
type ChainEvaluationResult struct {
	ChainID    chains.ChainID
	Revision   uint64
	Evaluation *chains.LifecycleEvaluation
	ErrorCode  string
	Error      string
}

// EvaluationBatch is a value result representing one coherent snapshot view
// of the registry at EvaluatedAt. It contains no mutable chain pointers.
// Evaluations contains successful evaluations; Results contains one entry per
// captured chain, including evaluation failures.
type EvaluationBatch struct {
	EvaluatedAt time.Time
	Policy      chains.LifecyclePolicy

	ChainCount       int
	ProposalCount    int
	EvaluationCount  int
	HealthyCount     int
	EvaluationErrors int

	Evaluations []chains.LifecycleEvaluation
	Proposals   []chains.TransitionProposal
	Results     []ChainEvaluationResult
}

// Clone returns a defensive copy of the batch, including proposal facts and
// nested evaluation proposals.
func (b EvaluationBatch) Clone() EvaluationBatch {
	clone := b
	if b.Evaluations != nil {
		clone.Evaluations = make([]chains.LifecycleEvaluation, len(b.Evaluations))
		for i, evaluation := range b.Evaluations {
			clone.Evaluations[i] = cloneLifecycleEvaluation(evaluation)
		}
	}
	if b.Proposals != nil {
		clone.Proposals = make([]chains.TransitionProposal, len(b.Proposals))
		for i, proposal := range b.Proposals {
			clone.Proposals[i] = cloneTransitionProposal(proposal)
		}
	}
	if b.Results != nil {
		clone.Results = make([]ChainEvaluationResult, len(b.Results))
		for i, result := range b.Results {
			clone.Results[i] = result
			if result.Evaluation != nil {
				evaluation := cloneLifecycleEvaluation(*result.Evaluation)
				clone.Results[i].Evaluation = &evaluation
			}
		}
	}
	return clone
}

// EvaluateLifecycle captures snapshots while holding the registry read lock,
// then evaluates them after releasing the lock. The returned batch is not a
// durable lock or a live view: subsequent mutations may make proposals stale.
func (r *Registry) EvaluateLifecycle(evaluatedAt time.Time, policy chains.LifecyclePolicy) (EvaluationBatch, error) {
	if r == nil {
		return EvaluationBatch{}, errors.New("registry is nil")
	}
	if err := policy.Validate(); err != nil {
		return EvaluationBatch{}, err
	}
	if evaluatedAt.IsZero() {
		return EvaluationBatch{}, errors.New("lifecycle evaluation timestamp must not be zero")
	}

	snapshots := r.List()
	// List already captures all snapshots under one RLock. Keep the explicit
	// sort here as a local invariant if List's implementation evolves.
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].ID < snapshots[j].ID })
	batch := EvaluationBatch{
		EvaluatedAt: evaluatedAt,
		Policy:      policy,
		ChainCount:  len(snapshots),
		Results:     make([]ChainEvaluationResult, 0, len(snapshots)),
		Evaluations: make([]chains.LifecycleEvaluation, 0, len(snapshots)),
		Proposals:   make([]chains.TransitionProposal, 0),
	}
	for _, snapshot := range snapshots {
		result := ChainEvaluationResult{ChainID: snapshot.ID, Revision: snapshot.Revision}
		evaluation, err := chains.EvaluateLifecycle(snapshot, evaluatedAt, policy)
		if err != nil {
			result.ErrorCode = CodeEvaluationFailed
			result.Error = err.Error()
			batch.EvaluationErrors++
			batch.Results = append(batch.Results, result)
			continue
		}
		evaluationCopy := cloneLifecycleEvaluation(evaluation)
		result.Evaluation = &evaluationCopy
		batch.Evaluations = append(batch.Evaluations, cloneLifecycleEvaluation(evaluation))
		if evaluation.Proposal == nil {
			batch.HealthyCount++
		} else {
			batch.Proposals = append(batch.Proposals, cloneTransitionProposal(*evaluation.Proposal))
		}
		batch.Results = append(batch.Results, result)
	}
	batch.EvaluationCount = len(batch.Results)
	batch.ProposalCount = len(batch.Proposals)
	return batch, nil
}

func cloneLifecycleEvaluation(evaluation chains.LifecycleEvaluation) chains.LifecycleEvaluation {
	clone := evaluation
	if evaluation.Proposal != nil {
		proposal := cloneTransitionProposal(*evaluation.Proposal)
		clone.Proposal = &proposal
	}
	return clone
}

func cloneTransitionProposal(proposal chains.TransitionProposal) chains.TransitionProposal {
	clone := proposal
	clone.SupportingFacts = append([]chains.LifecycleFact(nil), proposal.SupportingFacts...)
	return clone
}
