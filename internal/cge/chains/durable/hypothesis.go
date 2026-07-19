package durable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"synora/internal/cge/chains/journal"
	"synora/internal/cge/hypotheses"
)

// AddHypothesis durably opens one already-created hypothesis set. It does not
// create the set from an ambiguity and never selects an alternative.
func (c *Coordinator) AddHypothesis(ctx context.Context, set *hypotheses.HypothesisSet, recordedAt time.Time) (HypothesisMutationResult, error) {
	if err := validateContext(ctx); err != nil {
		return HypothesisMutationResult{}, err
	}
	if c == nil {
		return HypothesisMutationResult{}, ErrCoordinatorClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return HypothesisMutationResult{}, err
	}
	if c.currentHypotheses == nil {
		return HypothesisMutationResult{}, ErrHypothesisRegistryCloneFailed
	}
	if set == nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.opened", "validate", "", hypotheses.ErrInvalidHypothesis)
	}
	snapshot := set.Snapshot()
	if err := validateInitialHypothesis(snapshot, recordedAt); err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.opened", "validate", string(snapshot.ID), err)
	}
	if existing, err := c.currentHypotheses.Get(snapshot.ID); err == nil {
		if reflect.DeepEqual(existing, snapshot) {
			return HypothesisMutationResult{Kind: HypothesisMutationOpened, SetID: snapshot.ID, After: existing, Idempotent: true}, nil
		}
		return HypothesisMutationResult{}, coordinatorError("hypothesis.opened", "validate", string(snapshot.ID), ErrHypothesisCollision)
	} else if !errors.Is(err, hypotheses.ErrHypothesisNotFound) {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.opened", "validate", string(snapshot.ID), err)
	}
	candidate, err := cloneHypothesisRegistry(ctx, c.currentHypotheses)
	if err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.opened", "clone", string(snapshot.ID), err)
	}
	if err := candidate.Add(set); err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.opened", "prepare", string(snapshot.ID), err)
	}
	after, err := candidate.Get(snapshot.ID)
	if err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.opened", "prepare", string(snapshot.ID), err)
	}
	revision := after.History[0]
	payload := journal.HypothesisOpenedPayload{Hypothesis: after}
	expectation := appendExpectation{kind: journal.RecordKindHypothesisOpened, recordedAt: recordedAt, actor: revision.Actor, correlation: revision.CorrelationID, hypothesisOpened: &payload}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, err := c.journal.AppendHypothesisOpened(ctx, journal.HypothesisOpenedInput{Hypothesis: after, RecordedAt: recordedAt, Actor: revision.Actor, CorrelationID: revision.CorrelationID})
	result := HypothesisMutationResult{Kind: HypothesisMutationOpened, SetID: after.ID, After: after, Revision: revision, Applied: true}
	if err != nil {
		return c.hypothesisAppendErrorLocked("hypothesis.opened", string(after.ID), beforeSequence, beforeHash, expectation, result, errors.Join(ErrHypothesisOpenAppendFailed, err))
	}
	if err := c.acceptAppendLocked("hypothesis.opened", string(after.ID), expectation, record, beforeSequence, beforeHash); err != nil {
		return result, err
	}
	result.JournalSequence, result.JournalRecordHash = record.Sequence, record.RecordHash
	return c.publishHypothesisLocked("hypothesis.opened", string(after.ID), candidate, result)
}

// SetHypothesisStatus durably applies one explicit optimistic status command.
func (c *Coordinator) SetHypothesisStatus(ctx context.Context, command hypotheses.SetStatusCommand, recordedAt time.Time) (HypothesisMutationResult, error) {
	if err := validateContext(ctx); err != nil {
		return HypothesisMutationResult{}, err
	}
	if c == nil {
		return HypothesisMutationResult{}, ErrCoordinatorClosed
	}
	command = command.Clone()
	if err := command.Validate(); err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.status_changed", "validate", string(command.SetID), err)
	}
	if !validRecordedAt(recordedAt) || recordedAt.Before(command.Mutation.At) {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.status_changed", "validate", string(command.SetID), ErrInvalidTimestamp)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return HypothesisMutationResult{}, err
	}
	if c.currentHypotheses == nil {
		return HypothesisMutationResult{}, ErrHypothesisRegistryCloneFailed
	}
	before, err := c.currentHypotheses.Get(command.SetID)
	if err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.status_changed", "prepare", string(command.SetID), err)
	}
	candidate, err := cloneHypothesisRegistry(ctx, c.currentHypotheses)
	if err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.status_changed", "clone", string(command.SetID), err)
	}
	after, err := candidate.SetStatus(command.SetID, command.SourceRevision, command.Target, command.Mutation)
	if err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.status_changed", "prepare", string(command.SetID), err)
	}
	if err := validateHypothesisStatusDelta(before, after, command); err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.status_changed", "prepare", string(command.SetID), err)
	}
	revision := after.History[len(after.History)-1]
	payload := journal.HypothesisStatusChangedPayload{SetID: command.SetID, PreviousRevision: before.Revision, NewRevision: after.Revision, PreviousStatus: before.Status, NewStatus: after.Status, Revision: revision}
	expectation := appendExpectation{kind: journal.RecordKindHypothesisStatusChanged, recordedAt: recordedAt, actor: command.Mutation.Actor, correlation: command.Mutation.CorrelationID, hypothesisStatus: &payload}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, err := c.journal.AppendHypothesisStatusChanged(ctx, journal.HypothesisStatusChangedInput{SetID: command.SetID, PreviousRevision: before.Revision, NewRevision: after.Revision, PreviousStatus: before.Status, NewStatus: after.Status, Revision: revision, RecordedAt: recordedAt, Actor: command.Mutation.Actor, CorrelationID: command.Mutation.CorrelationID})
	result := HypothesisMutationResult{Kind: HypothesisMutationStatusChanged, SetID: after.ID, Before: hypothesisSnapshotPointer(before), After: after, Revision: revision, Applied: true}
	if err != nil {
		return c.hypothesisAppendErrorLocked("hypothesis.status_changed", string(after.ID), beforeSequence, beforeHash, expectation, result, errors.Join(ErrHypothesisStatusAppendFailed, err))
	}
	if err := c.acceptAppendLocked("hypothesis.status_changed", string(after.ID), expectation, record, beforeSequence, beforeHash); err != nil {
		return result, err
	}
	result.JournalSequence, result.JournalRecordHash = record.Sequence, record.RecordHash
	return c.publishHypothesisLocked("hypothesis.status_changed", string(after.ID), candidate, result)
}

func (c *Coordinator) HypothesisLineage(id hypotheses.SetID) ([]hypotheses.Snapshot, error) {
	if c == nil {
		return nil, ErrCoordinatorClosed
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.currentHypotheses == nil {
		return nil, ErrHypothesisRegistryCloneFailed
	}
	return c.currentHypotheses.Lineage(id)
}

// RebaseHypothesis durably appends a new assessment version without changing
// the hypothesis status or any chain-domain state.
func (c *Coordinator) RebaseHypothesis(ctx context.Context, command hypotheses.RebaseCommand, recordedAt time.Time) (HypothesisMutationResult, error) {
	if err := validateContext(ctx); err != nil {
		return HypothesisMutationResult{}, err
	}
	if c == nil {
		return HypothesisMutationResult{}, ErrCoordinatorClosed
	}
	command = command.Clone()
	if err := command.Validate(); err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.rebased", "validate", string(command.SetID), err)
	}
	if !validRecordedAt(recordedAt) || recordedAt.Before(command.Mutation.At) {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.rebased", "validate", string(command.SetID), ErrInvalidTimestamp)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return HypothesisMutationResult{}, err
	}
	if c.currentHypotheses == nil {
		return HypothesisMutationResult{}, ErrHypothesisRegistryCloneFailed
	}
	before, err := c.currentHypotheses.Get(command.SetID)
	if err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.rebased", "prepare", string(command.SetID), err)
	}
	if len(before.Assessments) == 0 {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.rebased", "prepare", string(command.SetID), ErrInvalidHypothesisAssessment)
	}
	current := before.Assessments[len(before.Assessments)-1]
	if command.Assessment.Fingerprint == current.Fingerprint {
		return HypothesisMutationResult{Kind: HypothesisMutationRebased, SetID: before.ID, Before: hypothesisSnapshotPointer(before), After: before, Idempotent: true}, nil
	}
	if command.Assessment.ID == current.ID {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.rebased", "validate", string(command.SetID), hypotheses.ErrHypothesisAssessmentCollision)
	}
	candidate, err := cloneHypothesisRegistry(ctx, c.currentHypotheses)
	if err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.rebased", "clone", string(command.SetID), err)
	}
	command.Family, command.Subject = before.Family, before.Subject
	after, err := candidate.Rebase(command)
	if err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.rebased", "prepare", string(command.SetID), err)
	}
	if err := validateHypothesisRebaseDelta(before, after, command); err != nil {
		return HypothesisMutationResult{}, coordinatorError("hypothesis.rebased", "prepare", string(command.SetID), err)
	}
	revision := after.History[len(after.History)-1]
	payload := journal.HypothesisRebasedPayload{SetID: command.SetID, PreviousRevision: before.Revision, NewRevision: after.Revision, PreviousAssessmentVersion: current.Version, NewAssessmentVersion: command.Assessment.Version, PreviousAssessmentID: current.ID, NewAssessmentID: command.Assessment.ID, PreviousFingerprint: current.Fingerprint, NewFingerprint: command.Assessment.Fingerprint, Assessment: command.Assessment.Clone(), Revision: revision}
	expectation := appendExpectation{kind: journal.RecordKindHypothesisRebased, recordedAt: recordedAt, actor: command.Mutation.Actor, correlation: command.Mutation.CorrelationID, hypothesisRebased: &payload}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, err := c.journal.AppendHypothesisRebased(ctx, journal.HypothesisRebasedInput{SetID: command.SetID, PreviousRevision: before.Revision, NewRevision: after.Revision, PreviousAssessmentVersion: current.Version, NewAssessmentVersion: command.Assessment.Version, PreviousAssessmentID: current.ID, NewAssessmentID: command.Assessment.ID, PreviousFingerprint: current.Fingerprint, NewFingerprint: command.Assessment.Fingerprint, Assessment: command.Assessment.Clone(), Revision: revision, RecordedAt: recordedAt, Actor: command.Mutation.Actor, CorrelationID: command.Mutation.CorrelationID})
	result := HypothesisMutationResult{Kind: HypothesisMutationRebased, SetID: after.ID, Before: hypothesisSnapshotPointer(before), After: after, Revision: revision, Applied: true, PreviousAssessmentVersion: current.Version, NewAssessmentVersion: command.Assessment.Version, PreviousAssessmentID: current.ID, NewAssessmentID: command.Assessment.ID, PreviousAssessmentFingerprint: current.Fingerprint, NewAssessmentFingerprint: command.Assessment.Fingerprint}
	if err != nil {
		return c.hypothesisAppendErrorLocked("hypothesis.rebased", string(after.ID), beforeSequence, beforeHash, expectation, result, errors.Join(ErrHypothesisRebaseAppendFailed, err))
	}
	if err := c.acceptAppendLocked("hypothesis.rebased", string(after.ID), expectation, record, beforeSequence, beforeHash); err != nil {
		return result, err
	}
	result.JournalSequence, result.JournalRecordHash = record.Sequence, record.RecordHash
	return c.publishHypothesisLocked("hypothesis.rebased", string(after.ID), candidate, result)
}

func validateInitialHypothesis(snapshot hypotheses.Snapshot, recordedAt time.Time) error {
	if err := hypothesesRestore(snapshot); err != nil {
		return err
	}
	if snapshot.Status != hypotheses.StatusOpen || snapshot.Revision != 1 || len(snapshot.History) != 1 || snapshot.History[0].Operation != hypotheses.OperationHypothesisOpened || snapshot.History[0].PreviousRevision != 0 || snapshot.History[0].NewRevision != 1 {
		return fmt.Errorf("%w: hypothesis is not an initial open snapshot", hypotheses.ErrInvalidHypothesis)
	}
	if !validRecordedAt(recordedAt) || recordedAt.Before(snapshot.History[0].At) {
		return ErrInvalidTimestamp
	}
	return nil
}

func hypothesesRestore(snapshot hypotheses.Snapshot) error {
	_, err := hypotheses.Restore(snapshot)
	return err
}

func validateHypothesisStatusDelta(before, after hypotheses.Snapshot, command hypotheses.SetStatusCommand) error {
	if after.ID != before.ID || after.Revision != before.Revision+1 || after.Status != command.Target || len(after.History) != len(before.History)+1 {
		return ErrHypothesisResultMismatch
	}
	if !reflect.DeepEqual(before.Alternatives, after.Alternatives) || !reflect.DeepEqual(before.Assessments, after.Assessments) || before.CurrentAssessmentVersion != after.CurrentAssessmentVersion || !reflect.DeepEqual(before.Subject, after.Subject) || !reflect.DeepEqual(before.Provenance, after.Provenance) || before.CreatedAt != after.CreatedAt || before.ReasonCode != after.ReasonCode || before.Reason != after.Reason {
		return ErrHypothesisResultMismatch
	}
	revision := after.History[len(after.History)-1]
	if revision.Operation != hypotheses.OperationHypothesisStatusChanged || revision.PreviousRevision != command.SourceRevision || revision.NewRevision != after.Revision || revision.PreviousStatus != before.Status || revision.NewStatus != after.Status || revision.Actor != command.Mutation.Actor || revision.CorrelationID != command.Mutation.CorrelationID {
		return ErrHypothesisResultMismatch
	}
	return nil
}

func validateHypothesisRebaseDelta(before, after hypotheses.Snapshot, command hypotheses.RebaseCommand) error {
	if after.ID != before.ID || after.Revision != before.Revision+1 || after.Status != before.Status || len(after.History) != len(before.History)+1 || len(after.Assessments) != len(before.Assessments)+1 {
		return ErrHypothesisRebaseResultMismatch
	}
	if !reflect.DeepEqual(before.Subject, after.Subject) || before.Family != after.Family || !reflect.DeepEqual(before.History[:len(before.History)], after.History[:len(before.History)]) || !reflect.DeepEqual(after.Assessments[len(after.Assessments)-1], command.Assessment) || !reflect.DeepEqual(after.Alternatives, command.Assessment.Alternatives) || !reflect.DeepEqual(after.Provenance, command.Assessment.Provenance) {
		return ErrHypothesisRebaseResultMismatch
	}
	revision := after.History[len(after.History)-1]
	if revision.Operation != hypotheses.OperationHypothesisRebased || revision.PreviousRevision != before.Revision || revision.NewRevision != after.Revision || revision.PreviousStatus != before.Status || revision.NewStatus != before.Status || revision.PreviousAssessmentVersion != command.PreviousAssessmentVersion || revision.NewAssessmentVersion != command.Assessment.Version || revision.PreviousAssessmentID != command.PreviousAssessmentID || revision.NewAssessmentID != command.Assessment.ID || revision.Actor != command.Mutation.Actor || revision.CorrelationID != command.Mutation.CorrelationID {
		return ErrHypothesisRebaseResultMismatch
	}
	return nil
}

func cloneHypothesisRegistry(ctx context.Context, source *hypotheses.Registry) (*hypotheses.Registry, error) {
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, ErrHypothesisRegistryCloneFailed
	}
	clone, err := source.CloneShallow()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHypothesisRegistryCloneFailed, err)
	}
	return clone, nil
}

func hypothesisSnapshotPointer(snapshot hypotheses.Snapshot) *hypotheses.Snapshot {
	copy := snapshot
	return &copy
}

func (c *Coordinator) hypothesisAppendErrorLocked(operation, id string, beforeSequence uint64, beforeHash string, expectation appendExpectation, result HypothesisMutationResult, cause error) (HypothesisMutationResult, error) {
	observed, failure := c.classifyAppendErrorLocked(beforeSequence, beforeHash, expectation, cause)
	if failure.Outcome == AppendUncertain && observed.RecordHash != "" {
		result.JournalSequence, result.JournalRecordHash = observed.Sequence, observed.RecordHash
	}
	return result, coordinatorError(operation, "append", id, failure)
}

func (c *Coordinator) publishHypothesisLocked(operation, id string, candidate *hypotheses.Registry, result HypothesisMutationResult) (HypothesisMutationResult, error) {
	if c.publishHook != nil {
		if err := c.publishHook(); err != nil {
			c.state = StateDegraded
			c.degradedReason = "publication_failed"
			return result, coordinatorError(operation, "publish", id, fmt.Errorf("%w: %v", ErrPublicationFailed, err))
		}
	}
	c.currentHypotheses = candidate
	result.Published = true
	return result, nil
}
