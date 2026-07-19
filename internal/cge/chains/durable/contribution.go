package durable

import (
	"context"
	"errors"
	"fmt"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
)

// AddContribution validates, journals, and publishes one explicit
// contribution command. MutationContext.At and recordedAt remain distinct.
func (c *Coordinator) AddContribution(ctx context.Context, command chains.AddContributionCommand, recordedAt time.Time) (MutationResult, error) {
	if err := validateContext(ctx); err != nil {
		return MutationResult{}, err
	}
	if c == nil {
		return MutationResult{}, ErrCoordinatorClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return MutationResult{}, err
	}
	command = command.Clone()
	if err := command.Validate(); err != nil {
		return MutationResult{}, contributionCoordinatorError(command, 0, "validate", fmt.Errorf("%w: %v", ErrInvalidContributionCommand, err))
	}
	if err := validateMutationInput(command.Mutation.Actor, command.Mutation.CorrelationID, recordedAt); err != nil {
		return MutationResult{}, contributionCoordinatorError(command, 0, "validate", err)
	}
	candidate, err := cloneRegistry(ctx, c.current)
	if err != nil {
		return MutationResult{}, contributionCoordinatorError(command, 0, "clone", err)
	}
	applied, err := candidate.AddContribution(command)
	if err != nil {
		return MutationResult{}, contributionCoordinatorError(command, currentRevision(c.current, command.ChainID), "apply", errors.Join(ErrContributionApplyFailed, err))
	}
	payload := journal.ContributionAddedPayload{
		ChainID: command.ChainID, PreviousRevision: applied.Before.Revision, NewRevision: applied.After.Revision,
		Contribution: command.Contribution.Clone(), PreviousConfidence: applied.Before.CurrentConfidence,
		NewConfidence: applied.After.CurrentConfidence, PreviousSupportCount: applied.Before.ConfirmationCount,
		NewSupportCount: applied.After.ConfirmationCount, PreviousContradictionCount: applied.Before.ContradictionCount,
		NewContradictionCount: applied.After.ContradictionCount, Revision: applied.Revision,
	}
	expectation := appendExpectation{
		kind: journal.RecordKindContributionAdded, recordedAt: recordedAt,
		actor: command.Mutation.Actor, correlation: command.Mutation.CorrelationID,
		contribution: &payload,
	}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, err := c.journal.AppendContributionAdded(ctx, journal.ContributionAddedInput{
		ChainID: command.ChainID, PreviousRevision: applied.Before.Revision, NewRevision: applied.After.Revision,
		Contribution: command.Contribution.Clone(), PreviousConfidence: applied.Before.CurrentConfidence,
		NewConfidence: applied.After.CurrentConfidence, PreviousSupportCount: applied.Before.ConfirmationCount,
		NewSupportCount: applied.After.ConfirmationCount, PreviousContradictionCount: applied.Before.ContradictionCount,
		NewContradictionCount: applied.After.ContradictionCount, Revision: applied.Revision,
		RecordedAt: recordedAt, Actor: command.Mutation.Actor, CorrelationID: command.Mutation.CorrelationID,
	})
	result := MutationResult{
		Kind: MutationContributionAdded, ChainID: command.ChainID, Before: snapshotPointer(applied.Before),
		After: applied.After, Revision: applied.Revision, ContributionID: command.Contribution.ID,
		PreviousConfidence: applied.Before.CurrentConfidence, NewConfidence: applied.After.CurrentConfidence,
	}
	if err != nil {
		return c.appendErrorLocked("chain.contribution_added", string(command.ChainID), beforeSequence, beforeHash, expectation, applied.After, errors.Join(ErrContributionAppendFailed, err))
	}
	if err := c.acceptAppendLocked("chain.contribution_added", string(command.ChainID), expectation, record, beforeSequence, beforeHash); err != nil {
		return result, err
	}
	result.JournalSequence = record.Sequence
	result.JournalRecordHash = record.RecordHash
	return c.publishLocked("chain.contribution_added", string(command.ChainID), candidate, result)
}

func contributionCoordinatorError(command chains.AddContributionCommand, current uint64, step string, err error) error {
	return CoordinatorError{
		Operation: "chain.contribution_added", Step: step, ChainID: string(command.ChainID),
		ContributionID: command.Contribution.ID, ExpectedRevision: command.SourceRevision,
		CurrentRevision: current, Err: err,
	}
}
