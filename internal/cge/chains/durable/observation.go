package durable

import (
	"context"
	"errors"
	"fmt"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
)

// AddObservation validates, journals and publishes one explicit observation
// command. MutationContext.At is the domain timestamp; recordedAt is the
// timestamp of the global journal envelope and is never implicit.
func (c *Coordinator) AddObservation(ctx context.Context, command chains.AddObservationCommand, recordedAt time.Time) (MutationResult, error) {
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
	return c.addObservationLocked(ctx, command, recordedAt)
}

func (c *Coordinator) addObservationLocked(ctx context.Context, command chains.AddObservationCommand, recordedAt time.Time) (MutationResult, error) {
	command = command.Clone()
	if err := command.Validate(); err != nil {
		return MutationResult{}, observationCoordinatorError(command, 0, "validate", fmt.Errorf("%w: %v", ErrInvalidObservationCommand, err))
	}
	if err := validateMutationInput(command.Mutation.Actor, command.Mutation.CorrelationID, recordedAt); err != nil {
		return MutationResult{}, observationCoordinatorError(command, 0, "validate", err)
	}
	candidate, err := cloneRegistry(ctx, c.current)
	if err != nil {
		return MutationResult{}, observationCoordinatorError(command, 0, "clone", err)
	}
	applied, err := candidate.AddObservation(command)
	if err != nil {
		return MutationResult{}, observationCoordinatorError(command, currentRevision(c.current, command.ChainID), "apply", errors.Join(ErrObservationApplyFailed, err))
	}
	revision := applied.Revision
	payload := journal.ObservationAddedPayload{
		ChainID: command.ChainID, PreviousRevision: applied.Before.Revision, NewRevision: applied.After.Revision,
		Observation: command.Observation, Revision: revision,
	}
	expectation := appendExpectation{
		kind: journal.RecordKindObservationAdded, recordedAt: recordedAt,
		actor: command.Mutation.Actor, correlation: command.Mutation.CorrelationID,
		observation: &payload,
	}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, err := c.journal.AppendObservationAdded(ctx, journal.ObservationAddedInput{
		ChainID: command.ChainID, PreviousRevision: applied.Before.Revision, NewRevision: applied.After.Revision,
		Observation: command.Observation, Revision: revision,
		RecordedAt: recordedAt, Actor: command.Mutation.Actor, CorrelationID: command.Mutation.CorrelationID,
	})
	result := MutationResult{Kind: MutationObservationAdded, ChainID: command.ChainID, Before: snapshotPointer(applied.Before), After: applied.After, Revision: revision}
	if err != nil {
		return c.appendObservationErrorLocked(command, beforeSequence, beforeHash, expectation, result, err)
	}
	if err := c.acceptAppendLocked("chain.observation_added", string(command.ChainID), expectation, record, beforeSequence, beforeHash); err != nil {
		return result, err
	}
	result.JournalSequence = record.Sequence
	result.JournalRecordHash = record.RecordHash
	return c.publishLocked("chain.observation_added", string(command.ChainID), candidate, result)
}

func (c *Coordinator) appendObservationErrorLocked(command chains.AddObservationCommand, beforeSequence uint64, beforeHash string, expectation appendExpectation, result MutationResult, cause error) (MutationResult, error) {
	observed, failure := c.classifyAppendErrorLocked(beforeSequence, beforeHash, expectation, cause)
	if failure.Outcome == AppendUncertain && observed.RecordHash != "" {
		result.JournalSequence = observed.Sequence
		result.JournalRecordHash = observed.RecordHash
	}
	return result, observationCoordinatorError(command, currentRevision(c.current, command.ChainID), "append", errors.Join(ErrObservationAppendFailed, failure))
}

func currentRevision(source interface {
	Get(chains.ChainID) (chains.Snapshot, error)
}, id chains.ChainID) uint64 {
	snapshot, err := source.Get(id)
	if err != nil {
		return 0
	}
	return snapshot.Revision
}

func observationCoordinatorError(command chains.AddObservationCommand, current uint64, step string, err error) error {
	return CoordinatorError{
		Operation: "chain.observation_added", Step: step, ChainID: string(command.ChainID),
		ObservationID: command.Observation.ID, ExpectedRevision: command.SourceRevision,
		CurrentRevision: current, Err: err,
	}
}
