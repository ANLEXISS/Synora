package registry

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"synora/internal/cge/chains"
)

// ProposalApplyResult describes one independent attempt in a batch.
type ProposalApplyResult struct {
	ChainID        chains.ChainID
	SourceRevision uint64
	TargetStatus   chains.Status

	Applied bool

	BeforeRevision uint64
	AfterRevision  uint64

	ErrorCode string
	Error     string
}

// ApplyBatchResult is intentionally non-atomic across chains. Each proposal
// is applied transactionally by Registry.ApplyLifecycleProposal, while other
// proposals may independently succeed or fail.
type ApplyBatchResult struct {
	Results []ProposalApplyResult

	ProposalsReceived int
	Applied           int
	Stale             int
	Invalid           int
	NotFound          int
	Failed            int
}

// ApplyLifecycleBatch applies every proposal in a defensive copy of batch.
func (r *Registry) ApplyLifecycleBatch(batch EvaluationBatch, actor, correlationID string) ApplyBatchResult {
	return r.ApplyLifecycleProposals(batch.Proposals, actor, correlationID)
}

// ApplyLifecycleProposals applies a selected set of proposals in ChainID
// order. Duplicate chain IDs are rejected individually and never applied.
func (r *Registry) ApplyLifecycleProposals(proposals []chains.TransitionProposal, actor, correlationID string) ApplyBatchResult {
	ordered := make([]chains.TransitionProposal, len(proposals))
	for i, proposal := range proposals {
		ordered[i] = cloneTransitionProposal(proposal)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.ChainID != right.ChainID {
			return left.ChainID < right.ChainID
		}
		if left.SourceRevision != right.SourceRevision {
			return left.SourceRevision < right.SourceRevision
		}
		if left.From != right.From {
			return left.From < right.From
		}
		return left.To < right.To
	})

	result := ApplyBatchResult{
		Results:           make([]ProposalApplyResult, 0, len(ordered)),
		ProposalsReceived: len(ordered),
	}
	seen := make(map[chains.ChainID]struct{}, len(ordered))
	duplicates := make(map[chains.ChainID]struct{})
	for _, proposal := range ordered {
		if _, exists := seen[proposal.ChainID]; exists {
			duplicates[proposal.ChainID] = struct{}{}
		}
		seen[proposal.ChainID] = struct{}{}
	}
	for _, proposal := range ordered {
		item := ProposalApplyResult{
			ChainID:        proposal.ChainID,
			SourceRevision: proposal.SourceRevision,
			TargetStatus:   proposal.To,
		}
		if _, err := chains.NewChainID(string(proposal.ChainID)); err != nil {
			setApplyError(&result, &item, CodeInvalidProposal, err.Error())
			result.Results = append(result.Results, item)
			continue
		}
		before, getErr := r.Get(proposal.ChainID)
		if getErr == nil {
			item.BeforeRevision = before.Revision
		}
		if _, duplicate := duplicates[proposal.ChainID]; duplicate {
			setApplyError(&result, &item, CodeInvalidProposal, "duplicate proposal for chain")
			result.Results = append(result.Results, item)
			continue
		}
		if getErr != nil {
			setApplyError(&result, &item, applyErrorCode(getErr), getErr.Error())
			result.Results = append(result.Results, item)
			continue
		}
		if strings.TrimSpace(actor) == "" {
			setApplyError(&result, &item, CodeInvalidActor, "actor must not be empty")
			result.Results = append(result.Results, item)
			continue
		}
		if strings.TrimSpace(correlationID) == "" || strings.TrimSpace(correlationID) != correlationID {
			setApplyError(&result, &item, CodeInvalidCorrelation, "correlation id must be non-empty and have no surrounding whitespace")
			result.Results = append(result.Results, item)
			continue
		}
		if err := chains.ValidateTransition(proposal.From, proposal.To); err != nil {
			setApplyError(&result, &item, CodeInvalidTransition, err.Error())
			result.Results = append(result.Results, item)
			continue
		}
		correlation := fmt.Sprintf("%s/%s", correlationID, proposal.ChainID)
		after, applyErr := r.ApplyLifecycleProposal(proposal, actor, correlation)
		if applyErr != nil {
			setApplyError(&result, &item, applyErrorCode(applyErr), applyErr.Error())
			result.Results = append(result.Results, item)
			continue
		}
		item.Applied = true
		item.AfterRevision = after.Revision
		result.Applied++
		result.Results = append(result.Results, item)
	}
	return result
}

func setApplyError(result *ApplyBatchResult, item *ProposalApplyResult, code, message string) {
	item.ErrorCode = code
	item.Error = message
	switch code {
	case CodeStaleProposal:
		result.Stale++
	case CodeChainNotFound:
		result.NotFound++
	case CodeInvalidProposal, CodeInvalidTransition, CodeInvalidActor, CodeInvalidCorrelation:
		result.Invalid++
	default:
		result.Failed++
	}
}

func applyErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrChainNotFound):
		return CodeChainNotFound
	case errors.Is(err, ErrStaleProposal):
		return CodeStaleProposal
	case errors.Is(err, ErrInvalidProposal):
		return CodeInvalidProposal
	default:
		return CodeInternalValidationFailed
	}
}
