package durable

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/evidence"
)

// EvidenceProposalApplyResult describes one independently committed or
// rejected proposal. It contains no mutable chain or contribution pointer.
type EvidenceProposalApplyResult struct {
	ChainID        chains.ChainID
	ContributionID string

	Decision evidence.Decision

	SourceRevision uint64

	Applied    bool
	Idempotent bool

	BeforeRevision uint64
	AfterRevision  uint64

	PreviousConfidence float64
	NewConfidence      float64

	JournalSequence   uint64
	JournalRecordHash string

	ErrorCode string
	Error     string
}

// EvidenceApplyBatchResult is intentionally non-atomic globally: every
// proposal has its own durable WAL transaction and later failures do not roll
// back earlier successes.
type EvidenceApplyBatchResult struct {
	ProposalsReceived int
	Applied           int
	Idempotent        int
	Stale             int
	Invalid           int
	NotFound          int
	Failed            int

	Results []EvidenceProposalApplyResult

	ErrorCode string
	Error     string
}

// EvaluateEvidenceBatch captures one coherent defensive view. Degraded
// coordinators remain readable, matching List's existing semantics; callers
// must bring the coordinator back to ready before applying its proposals.
func (c *Coordinator) EvaluateEvidenceBatch(evaluatedAt time.Time, policy evidence.Policy, options evidence.BatchOptions) (evidence.EvidenceBatch, error) {
	if c == nil {
		return evidence.EvidenceBatch{}, ErrCoordinatorClosed
	}
	c.mu.RLock()
	if c.state == StateClosed || c.current == nil {
		c.mu.RUnlock()
		return evidence.EvidenceBatch{}, ErrCoordinatorClosed
	}
	snapshots := c.current.List()
	c.mu.RUnlock()
	return evidence.EvaluateBatch(snapshots, evaluatedAt, policy, options)
}

// ApplyEvidenceProposals explicitly applies a caller-selected subset of a
// pure evidence batch. All global input validation occurs before the first
// mutation. Individual WAL operations are then performed in ChainID order.
func (c *Coordinator) ApplyEvidenceProposals(
	ctx context.Context,
	proposals []evidence.ContributionProposal,
	actor string,
	correlationID string,
	mutationAt time.Time,
	recordedAt time.Time,
) (EvidenceApplyBatchResult, error) {
	result := EvidenceApplyBatchResult{ProposalsReceived: len(proposals)}
	if err := validateContext(ctx); err != nil {
		return batchApplyFailure(result, "invalid_context", "invalid context", err)
	}
	if c == nil {
		return batchApplyFailure(result, "coordinator_not_ready", "coordinator is unavailable", ErrCoordinatorClosed)
	}
	if err := validateMutationInput(actor, correlationID, recordedAt); err != nil {
		return batchApplyFailure(result, evidenceApplyErrorCode(err), "batch input is invalid", err)
	}
	if mutationAt.IsZero() {
		return batchApplyFailure(result, "invalid_timestamp", "mutation timestamp is invalid", ErrInvalidTimestamp)
	}
	c.mu.RLock()
	ready := c.state == StateReady
	c.mu.RUnlock()
	if !ready {
		return batchApplyFailure(result, "coordinator_not_ready", "coordinator is not ready", ErrCoordinatorNotReady)
	}

	ordered := make([]evidence.ContributionProposal, len(proposals))
	for index, proposal := range proposals {
		ordered[index] = cloneEvidenceProposal(proposal)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ChainID != ordered[j].ChainID {
			return ordered[i].ChainID < ordered[j].ChainID
		}
		return ordered[i].Contribution.ID < ordered[j].Contribution.ID
	})
	if err := validateEvidenceProposalBatch(ordered); err != nil {
		result.Results = invalidProposalResults(ordered, err)
		result.Invalid = len(ordered)
		return batchApplyFailure(result, evidenceApplyErrorCode(err), "evidence proposal batch is invalid", err)
	}

	result.Results = make([]EvidenceProposalApplyResult, 0, len(ordered))
	for index, proposal := range ordered {
		item := c.applyEvidenceProposal(ctx, proposal, actor, deriveEvidenceCorrelation(correlationID, index+1), mutationAt, recordedAt)
		result.Results = append(result.Results, item)
		switch {
		case item.Applied:
			result.Applied++
		case item.Idempotent:
			result.Idempotent++
		case item.ErrorCode == "evidence_proposal_stale":
			result.Stale++
		case item.ErrorCode == "chain_not_found":
			result.NotFound++
		case item.ErrorCode == "invalid_contribution_command":
			result.Invalid++
		default:
			if item.ErrorCode != "" {
				result.Failed++
			}
		}
	}
	return cloneEvidenceApplyBatchResult(result), nil
}

func (c *Coordinator) applyEvidenceProposal(ctx context.Context, proposal evidence.ContributionProposal, actor, correlationID string, mutationAt, recordedAt time.Time) EvidenceProposalApplyResult {
	item := EvidenceProposalApplyResult{
		ChainID: proposal.ChainID, ContributionID: proposal.Contribution.ID,
		Decision: proposal.Decision, SourceRevision: proposal.SourceRevision,
	}
	current, err := c.Get(proposal.ChainID)
	if err != nil {
		item.ErrorCode = evidenceApplyErrorCode(err)
		item.Error = sanitizedApplyError(item.ErrorCode)
		return item
	}
	if existing, ok := findContribution(current, proposal.Contribution.ID); ok {
		item.BeforeRevision = current.Revision
		item.AfterRevision = current.Revision
		item.PreviousConfidence = current.CurrentConfidence
		item.NewConfidence = current.CurrentConfidence
		if sameContribution(existing, proposal.Contribution) {
			item.Idempotent = true
			item.ErrorCode = "evidence_proposal_idempotent"
			item.Error = "contribution is already durably present"
		} else {
			item.ErrorCode = "evidence_contribution_collision"
			item.Error = "contribution identity collides with existing data"
		}
		return item
	}
	if current.Revision != proposal.SourceRevision {
		item.BeforeRevision = current.Revision
		item.AfterRevision = current.Revision
		item.PreviousConfidence = current.CurrentConfidence
		item.NewConfidence = current.CurrentConfidence
		item.ErrorCode = "evidence_proposal_stale"
		item.Error = "source revision is no longer current"
		return item
	}

	reason := fmt.Sprintf("evidence.batch_apply decision=%s policy=%s", contributionKind(proposal.Contribution.Kind), proposal.PolicyVersion)
	command, err := proposal.Command(chains.MutationContext{
		At: mutationAt, Actor: actor, Reason: reason, CorrelationID: correlationID,
		ObservationIDs: append([]string(nil), proposal.Contribution.ObservationIDs...),
	})
	if err != nil {
		item.ErrorCode = "invalid_contribution_command"
		item.Error = "evidence proposal cannot become a mutation command"
		return item
	}
	mutation, err := c.AddContribution(ctx, command, recordedAt)
	if err != nil {
		if errors.Is(err, ErrStaleContributionCommand) {
			if current, readErr := c.Get(proposal.ChainID); readErr == nil {
				item.BeforeRevision = current.Revision
				item.AfterRevision = current.Revision
				item.PreviousConfidence = current.CurrentConfidence
				item.NewConfidence = current.CurrentConfidence
			}
		}
		item.ErrorCode = evidenceApplyErrorCode(err)
		item.Error = sanitizedApplyError(item.ErrorCode)
		return item
	}
	item.Applied = true
	item.BeforeRevision = mutation.Before.Revision
	item.AfterRevision = mutation.After.Revision
	item.PreviousConfidence = mutation.PreviousConfidence
	item.NewConfidence = mutation.NewConfidence
	item.JournalSequence = mutation.JournalSequence
	item.JournalRecordHash = mutation.JournalRecordHash
	return item
}

func validateEvidenceProposalBatch(proposals []evidence.ContributionProposal) error {
	chainIDs := make(map[chains.ChainID]struct{}, len(proposals))
	contributionIDs := make(map[string]struct{}, len(proposals))
	for _, proposal := range proposals {
		if err := proposal.Validate(); err != nil {
			return err
		}
		if _, exists := chainIDs[proposal.ChainID]; exists {
			return fmt.Errorf("%w: chain=%s", ErrDuplicateChainProposal, proposal.ChainID)
		}
		chainIDs[proposal.ChainID] = struct{}{}
		if _, exists := contributionIDs[proposal.Contribution.ID]; exists {
			return fmt.Errorf("%w: contribution=%s", ErrDuplicateContributionProposal, proposal.Contribution.ID)
		}
		contributionIDs[proposal.Contribution.ID] = struct{}{}
	}
	return nil
}

func invalidProposalResults(proposals []evidence.ContributionProposal, err error) []EvidenceProposalApplyResult {
	code := evidenceApplyErrorCode(err)
	results := make([]EvidenceProposalApplyResult, len(proposals))
	for index, proposal := range proposals {
		results[index] = EvidenceProposalApplyResult{
			ChainID: proposal.ChainID, ContributionID: proposal.Contribution.ID,
			Decision: proposal.Decision, SourceRevision: proposal.SourceRevision,
			ErrorCode: code, Error: sanitizedApplyError(code),
		}
	}
	return results
}

func findContribution(snapshot chains.Snapshot, id string) (chains.ConfidenceContribution, bool) {
	for _, contribution := range snapshot.Contributions {
		if contribution.ID == id {
			return contribution.Clone(), true
		}
	}
	return chains.ConfidenceContribution{}, false
}

func sameContribution(left, right chains.ConfidenceContribution) bool {
	if left.ID != right.ID || left.Source != right.Source || left.Kind != right.Kind || left.Value != right.Value || left.Reason != right.Reason {
		return false
	}
	if len(left.ObservationIDs) != len(right.ObservationIDs) {
		return false
	}
	for index := range left.ObservationIDs {
		if left.ObservationIDs[index] != right.ObservationIDs[index] {
			return false
		}
	}
	return true
}

func cloneEvidenceProposal(proposal evidence.ContributionProposal) evidence.ContributionProposal {
	proposal.Contribution = proposal.Contribution.Clone()
	return proposal
}

func contributionKind(kind chains.ContributionKind) string { return string(kind) }

func deriveEvidenceCorrelation(base string, index int) string {
	candidate := fmt.Sprintf("%s:%04d", base, index)
	if len([]rune(candidate)) <= maxCorrelationLength {
		return candidate
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", base, index)))
	return fmt.Sprintf("evidence-batch:%x:%04d", digest[:12], index)
}

func evidenceApplyErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidContext):
		return "invalid_context"
	case errors.Is(err, ErrDuplicateChainProposal):
		return "duplicate_chain_proposal"
	case errors.Is(err, ErrDuplicateContributionProposal):
		return "duplicate_contribution_proposal"
	case errors.Is(err, ErrCoordinatorClosed), errors.Is(err, ErrCoordinatorNotReady), errors.Is(err, ErrCoordinatorDegraded):
		return "coordinator_not_ready"
	case errors.Is(err, ErrChainNotFound):
		return "chain_not_found"
	case errors.Is(err, ErrStaleContributionCommand):
		return "evidence_proposal_stale"
	case errors.Is(err, ErrEvidenceContributionCollision):
		return "evidence_contribution_collision"
	case errors.Is(err, ErrInvalidContributionCommand), errors.Is(err, ErrContributionApplyFailed), errors.Is(err, ErrContributionNotAllowed):
		return "invalid_contribution_command"
	case errors.Is(err, ErrInvalidActor):
		return "invalid_actor"
	case errors.Is(err, ErrInvalidCorrelation):
		return "invalid_correlation"
	case errors.Is(err, ErrInvalidTimestamp):
		return "invalid_timestamp"
	default:
		return "evidence_batch_apply_failed"
	}
}

func sanitizedApplyError(code string) string {
	switch code {
	case "chain_not_found":
		return "chain was not found"
	case "evidence_proposal_stale":
		return "source revision is no longer current"
	case "evidence_contribution_collision":
		return "contribution identity collides with existing data"
	case "invalid_contribution_command":
		return "contribution command was rejected"
	case "coordinator_not_ready":
		return "coordinator is not ready"
	default:
		return "evidence proposal application failed"
	}
}

func batchApplyFailure(result EvidenceApplyBatchResult, code, message string, err error) (EvidenceApplyBatchResult, error) {
	result.ErrorCode = code
	result.Error = message
	return result, errors.Join(ErrEvidenceBatchApplyFailed, err)
}

func cloneEvidenceApplyBatchResult(result EvidenceApplyBatchResult) EvidenceApplyBatchResult {
	result.Results = append([]EvidenceProposalApplyResult(nil), result.Results...)
	return result
}
