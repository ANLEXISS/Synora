package durable

import (
	"context"
	"errors"
	"reflect"
	"time"

	"synora/internal/cge/chains/journal"
	"synora/internal/cge/hypotheses"
)

func (c *Coordinator) SupersedeHypothesis(ctx context.Context, command hypotheses.SupersedeCommand, recordedAt time.Time) (HypothesisSupersessionResult, error) {
	if err := validateContext(ctx); err != nil {
		return HypothesisSupersessionResult{}, err
	}
	if c == nil {
		return HypothesisSupersessionResult{}, ErrCoordinatorClosed
	}
	command = command.Clone()
	if err := command.Validate(); err != nil {
		return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "validate", string(command.PreviousSetID), err)
	}
	if !validRecordedAt(recordedAt) || recordedAt.Before(command.Mutation.At) {
		return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "validate", string(command.PreviousSetID), ErrInvalidTimestamp)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return HypothesisSupersessionResult{}, err
	}
	if c.currentHypotheses == nil {
		return HypothesisSupersessionResult{}, ErrHypothesisRegistryCloneFailed
	}
	previous, err := c.currentHypotheses.Get(command.PreviousSetID)
	if err != nil {
		return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "prepare", string(command.PreviousSetID), err)
	}
	if previous.Status == hypotheses.StatusSuperseded {
		if previous.Lineage.SuccessorSetID != command.NewSetID {
			return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "validate", string(command.PreviousSetID), ErrHypothesisSupersessionCollision)
		}
		successor, err := c.currentHypotheses.Get(command.NewSetID)
		if err != nil {
			return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "validate", string(command.PreviousSetID), ErrHypothesisLineageDivergence)
		}
		if !reflect.DeepEqual(successor, command.NewSet) {
			return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "validate", string(command.NewSetID), ErrHypothesisSuccessorCollision)
		}
		return HypothesisSupersessionResult{PreviousSetID: command.PreviousSetID, NewSetID: command.NewSetID, PreviousBefore: previous, PreviousAfter: previous, NewAfter: successor, Idempotent: true}, nil
	}
	if _, err := c.currentHypotheses.Get(command.NewSetID); err == nil {
		return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "validate", string(command.NewSetID), ErrHypothesisSuccessorCollision)
	} else if !errors.Is(err, hypotheses.ErrHypothesisNotFound) {
		return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "validate", string(command.NewSetID), err)
	}
	candidate, err := cloneHypothesisRegistry(ctx, c.currentHypotheses)
	if err != nil {
		return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "clone", string(command.PreviousSetID), err)
	}
	applyResult, err := candidate.Supersede(command)
	if err != nil {
		return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "prepare", string(command.PreviousSetID), err)
	}
	if applyResult.NewAfter.Lineage.PredecessorSetID != command.PreviousSetID || applyResult.PreviousAfter.Lineage.SuccessorSetID != command.NewSetID {
		return HypothesisSupersessionResult{}, coordinatorError("hypothesis.superseded", "prepare", string(command.PreviousSetID), ErrHypothesisSupersessionResultMismatch)
	}
	payload := journal.HypothesisSupersededPayload{PreviousSetID: command.PreviousSetID, NewSetID: command.NewSetID, PreviousRevision: previous.Revision, NewPreviousRevision: applyResult.NewAfter.Revision, PreviousStatus: previous.Status, NewStatus: hypotheses.StatusSuperseded, PreviousSuccessorSetID: "", NewSuccessorSetID: command.NewSetID, PreviousSetRevision: applyResult.PreviousRevision, NewHypothesis: applyResult.NewAfter}
	expectation := appendExpectation{kind: journal.RecordKindHypothesisSuperseded, recordedAt: recordedAt, actor: command.Mutation.Actor, correlation: command.Mutation.CorrelationID, hypothesisSuperseded: &payload}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, err := c.journal.AppendHypothesisSuperseded(ctx, journal.HypothesisSupersededInput{PreviousSetID: command.PreviousSetID, NewSetID: command.NewSetID, PreviousRevision: previous.Revision, NewPreviousRevision: applyResult.NewAfter.Revision, PreviousStatus: previous.Status, NewStatus: hypotheses.StatusSuperseded, PreviousSuccessorSetID: "", NewSuccessorSetID: command.NewSetID, PreviousSetRevision: applyResult.PreviousRevision, NewHypothesis: applyResult.NewAfter, RecordedAt: recordedAt, Actor: command.Mutation.Actor, CorrelationID: command.Mutation.CorrelationID})
	result := HypothesisSupersessionResult{PreviousSetID: command.PreviousSetID, NewSetID: command.NewSetID, PreviousBefore: previous, PreviousAfter: applyResult.PreviousAfter, NewAfter: applyResult.NewAfter, PreviousRevision: applyResult.PreviousRevision, NewOpeningRevision: applyResult.NewOpeningRevision, Applied: true}
	if err != nil {
		return c.hypothesisSupersessionAppendErrorLocked(beforeSequence, beforeHash, expectation, result, errors.Join(ErrHypothesisSupersessionAppendFailed, err))
	}
	if err := c.acceptAppendLocked("hypothesis.superseded", string(command.PreviousSetID), expectation, record, beforeSequence, beforeHash); err != nil {
		return result, err
	}
	result.JournalSequence, result.JournalRecordHash = record.Sequence, record.RecordHash
	if c.publishHook != nil {
		if err := c.publishHook(); err != nil {
			c.state = StateDegraded
			c.degradedReason = "publication_failed"
			return result, coordinatorError("hypothesis.superseded", "publish", string(command.PreviousSetID), ErrPublicationFailed)
		}
	}
	c.currentHypotheses = candidate
	result.Published = true
	return result, nil
}

func (c *Coordinator) hypothesisSupersessionAppendErrorLocked(beforeSequence uint64, beforeHash string, expectation appendExpectation, result HypothesisSupersessionResult, cause error) (HypothesisSupersessionResult, error) {
	observed, failure := c.classifyAppendErrorLocked(beforeSequence, beforeHash, expectation, cause)
	if failure.Outcome == AppendUncertain && observed.RecordHash != "" {
		result.JournalSequence, result.JournalRecordHash = observed.Sequence, observed.RecordHash
	}
	return result, coordinatorError("hypothesis.superseded", "append", string(result.PreviousSetID), failure)
}
