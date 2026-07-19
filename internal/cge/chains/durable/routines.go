package durable

import (
	"context"
	"errors"
	"fmt"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/routines"
)

// ApplyRoutineOccurrence prepares one targeted routine mutation, makes its
// WAL record durable, and only then publishes that routine in memory.
func (c *Coordinator) ApplyRoutineOccurrence(ctx context.Context, occurrence routines.Occurrence, mutation chains.MutationContext, recordedAt time.Time) (RoutineOccurrenceResult, error) {
	if err := validateContext(ctx); err != nil {
		return RoutineOccurrenceResult{}, err
	}
	if c == nil {
		return RoutineOccurrenceResult{}, ErrCoordinatorClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return RoutineOccurrenceResult{}, err
	}
	if c.currentRoutines == nil {
		return RoutineOccurrenceResult{}, ErrCoordinatorNotReady
	}
	if err := occurrence.Validate(); err != nil {
		return RoutineOccurrenceResult{}, coordinatorError("routine.occurrence_added", "validate", string(occurrence.RoutineID), err)
	}
	if err := mutation.Validate(); err != nil {
		return RoutineOccurrenceResult{}, coordinatorError("routine.occurrence_added", "validate", string(occurrence.RoutineID), err)
	}
	if recordedAt.IsZero() {
		return RoutineOccurrenceResult{}, coordinatorError("routine.occurrence_added", "validate", string(occurrence.RoutineID), ErrInvalidTimestamp)
	}
	prepared, err := c.currentRoutines.PrepareOccurrence(occurrence, mutation)
	if err != nil {
		return RoutineOccurrenceResult{}, coordinatorError("routine.occurrence_added", "prepare", string(occurrence.RoutineID), err)
	}
	result := RoutineOccurrenceResult{RoutineID: occurrence.RoutineID, OccurrenceID: occurrence.ID, Before: cloneRoutineSnapshotPointer(prepared.Before), After: prepared.After, Created: prepared.Created, Idempotent: prepared.Idempotent}
	if prepared.Idempotent {
		return result, nil
	}
	if len(prepared.After.History) == 0 {
		return RoutineOccurrenceResult{}, coordinatorError("routine.occurrence_added", "prepare", string(occurrence.RoutineID), ErrRoutineResultMismatch)
	}
	revision := prepared.After.History[len(prepared.After.History)-1]
	fingerprint, err := prepared.After.Fingerprint()
	if err != nil {
		return RoutineOccurrenceResult{}, coordinatorError("routine.occurrence_added", "prepare", string(occurrence.RoutineID), err)
	}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	expectation := appendExpectation{kind: journal.RecordKindRoutineOccurrenceAdded, recordedAt: recordedAt, actor: mutation.Actor, correlation: mutation.CorrelationID}
	if prepared.Created {
		fingerprint, err := prepared.After.Fingerprint()
		if err != nil {
			return RoutineOccurrenceResult{}, coordinatorError("routine.created", "prepare", string(occurrence.RoutineID), err)
		}
		expectation.kind = journal.RecordKindRoutineCreated
		expectation.routineCreated = &journal.RoutineCreatedPayload{RoutineID: occurrence.RoutineID, PreviousRevision: 0, NewRevision: 1, Snapshot: prepared.After, SnapshotFingerprint: fingerprint}
		record, appendErr := c.journal.AppendRoutineCreated(ctx, journal.RoutineCreatedInput{RoutineID: occurrence.RoutineID, PreviousRevision: 0, NewRevision: 1, Snapshot: prepared.After, SnapshotFingerprint: fingerprint, RecordedAt: recordedAt, Actor: mutation.Actor, CorrelationID: mutation.CorrelationID})
		if appendErr != nil {
			return c.routineOccurrenceAppendErrorLocked(beforeSequence, beforeHash, expectation, result, appendErr)
		}
		if err := c.acceptAppendLocked("routine.created", string(occurrence.RoutineID), expectation, record, beforeSequence, beforeHash); err != nil {
			return result, err
		}
		result.Revision = revision
		result.JournalSequence, result.JournalRecordHash = record.Sequence, record.RecordHash
		if err := c.publishRoutineOccurrenceLocked("routine.created", result, prepared); err != nil {
			return result, err
		}
		result.Applied = true
		result.Published = true
		return result, nil
	}
	expectation.routineOccurrence = &journal.RoutineOccurrenceAddedPayload{RoutineID: occurrence.RoutineID, PreviousRevision: prepared.Before.Revision, NewRevision: prepared.After.Revision, Occurrence: occurrence, Revision: revision, Outcome: prepared.After.MutationOutcome(), ResultSnapshotFingerprint: fingerprint}
	record, appendErr := c.journal.AppendRoutineOccurrenceAdded(ctx, journal.RoutineOccurrenceAddedInput{RoutineID: occurrence.RoutineID, PreviousRevision: prepared.Before.Revision, NewRevision: prepared.After.Revision, Occurrence: occurrence, Revision: revision, Outcome: prepared.After.MutationOutcome(), ResultSnapshotFingerprint: fingerprint, RecordedAt: recordedAt, Actor: mutation.Actor, CorrelationID: mutation.CorrelationID})
	if appendErr != nil {
		return c.routineOccurrenceAppendErrorLocked(beforeSequence, beforeHash, expectation, result, appendErr)
	}
	if err := c.acceptAppendLocked("routine.occurrence_added", string(occurrence.RoutineID), expectation, record, beforeSequence, beforeHash); err != nil {
		return result, err
	}
	result.Applied = true
	result.Revision = revision
	result.JournalSequence, result.JournalRecordHash = record.Sequence, record.RecordHash
	if err := c.publishRoutineOccurrenceLocked("routine.occurrence_added", result, prepared); err != nil {
		return result, err
	}
	result.Published = true
	return result, nil
}

func (c *Coordinator) publishRoutineOccurrenceLocked(operation string, result RoutineOccurrenceResult, prepared routines.PreparedOccurrence) error {
	if c.publishHook != nil {
		if err := c.publishHook(); err != nil {
			c.state = StateDegraded
			c.degradedReason = "publication_failed"
			return coordinatorError(operation, "publish", string(result.RoutineID), fmt.Errorf("%w: %v", ErrPublicationFailed, err))
		}
	}
	if err := c.currentRoutines.PublishPreparedOccurrence(prepared); err != nil {
		c.state = StateDegraded
		c.degradedReason = "routine_publication_failed"
		return coordinatorError(operation, "publish", string(result.RoutineID), err)
	}
	return nil
}

func (c *Coordinator) routineOccurrenceAppendErrorLocked(beforeSequence uint64, beforeHash string, expectation appendExpectation, result RoutineOccurrenceResult, cause error) (RoutineOccurrenceResult, error) {
	observed, failure := c.classifyAppendErrorLocked(beforeSequence, beforeHash, expectation, cause)
	if failure.Outcome == AppendUncertain && observed.RecordHash != "" {
		result.JournalSequence, result.JournalRecordHash = observed.Sequence, observed.RecordHash
	}
	return result, coordinatorError("routine.occurrence_added", "append", string(result.RoutineID), errors.Join(ErrRoutineAppendFailed, failure))
}

// SetRoutineStatus is an explicit durable operation. No Shadow path calls it.
func (c *Coordinator) SetRoutineStatus(ctx context.Context, command routines.SetStatusCommand, recordedAt time.Time) (RoutineStatusResult, error) {
	if err := validateContext(ctx); err != nil {
		return RoutineStatusResult{}, err
	}
	if c == nil {
		return RoutineStatusResult{}, ErrCoordinatorClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return RoutineStatusResult{}, err
	}
	if c.currentRoutines == nil {
		return RoutineStatusResult{}, ErrCoordinatorNotReady
	}
	prepared, err := c.currentRoutines.PrepareStatus(command)
	if err != nil {
		return RoutineStatusResult{}, coordinatorError("routine.status_changed", "prepare", string(command.RoutineID), err)
	}
	if recordedAt.IsZero() {
		return RoutineStatusResult{}, coordinatorError("routine.status_changed", "validate", string(command.RoutineID), ErrInvalidTimestamp)
	}
	revision := prepared.After.History[len(prepared.After.History)-1]
	fingerprint, err := prepared.After.Fingerprint()
	if err != nil {
		return RoutineStatusResult{}, coordinatorError("routine.status_changed", "prepare", string(command.RoutineID), err)
	}
	expectation := appendExpectation{kind: journal.RecordKindRoutineStatusChanged, recordedAt: recordedAt, actor: command.Mutation.Actor, correlation: command.Mutation.CorrelationID, routineStatus: &journal.RoutineStatusChangedPayload{RoutineID: command.RoutineID, PreviousRevision: prepared.Before.Revision, NewRevision: prepared.After.Revision, PreviousStatus: prepared.Before.Status, NewStatus: prepared.After.Status, Revision: revision, ResultSnapshotFingerprint: fingerprint}}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, appendErr := c.journal.AppendRoutineStatusChanged(ctx, journal.RoutineStatusChangedInput{RoutineID: command.RoutineID, PreviousRevision: prepared.Before.Revision, NewRevision: prepared.After.Revision, PreviousStatus: prepared.Before.Status, NewStatus: prepared.After.Status, Revision: revision, ResultSnapshotFingerprint: fingerprint, RecordedAt: recordedAt, Actor: command.Mutation.Actor, CorrelationID: command.Mutation.CorrelationID})
	if appendErr != nil {
		observed, failure := c.classifyAppendErrorLocked(beforeSequence, beforeHash, expectation, appendErr)
		result := RoutineStatusResult{RoutineID: command.RoutineID, Before: cloneRoutineSnapshotPointer(&prepared.Before), After: prepared.After}
		if failure.Outcome == AppendUncertain && observed.RecordHash != "" {
			result.JournalSequence, result.JournalRecordHash = observed.Sequence, observed.RecordHash
		}
		return result, coordinatorError("routine.status_changed", "append", string(command.RoutineID), errors.Join(ErrRoutineAppendFailed, failure))
	}
	if err := c.acceptAppendLocked("routine.status_changed", string(command.RoutineID), expectation, record, beforeSequence, beforeHash); err != nil {
		return RoutineStatusResult{}, err
	}
	result := RoutineStatusResult{RoutineID: command.RoutineID, Before: cloneRoutineSnapshotPointer(&prepared.Before), After: prepared.After, Revision: revision, Applied: true, JournalSequence: record.Sequence, JournalRecordHash: record.RecordHash}
	if c.publishHook != nil {
		if err := c.publishHook(); err != nil {
			c.state = StateDegraded
			c.degradedReason = "publication_failed"
			return result, coordinatorError("routine.status_changed", "publish", string(command.RoutineID), fmt.Errorf("%w: %v", ErrPublicationFailed, err))
		}
	}
	if err := c.currentRoutines.PublishPreparedStatus(prepared); err != nil {
		c.state = StateDegraded
		c.degradedReason = "routine_publication_failed"
		return result, coordinatorError("routine.status_changed", "publish", string(command.RoutineID), err)
	}
	result.Published = true
	return result, nil
}

// ApplyRoutineLearningPlan applies each planned occurrence as an independent
// WAL transaction. The plan is not re-extracted or made globally atomic.
func (c *Coordinator) ApplyRoutineLearningPlan(ctx context.Context, plan routines.LearningPlan, actor, correlationID string) RoutineLearningResult {
	result := RoutineLearningResult{ChainID: plan.ChainID}
	for _, occurrence := range plan.Occurrences {
		if c == nil || c.Status().State != StateReady {
			result.ErrorCount++
			result.Results = append(result.Results, routines.LearningOccurrenceResult{RoutineID: occurrence.RoutineID, OccurrenceID: occurrence.ID, ErrorCode: "coordinator_degraded"})
			break
		}
		mutation := chains.MutationContext{At: occurrence.ObservedAt, Actor: actor, Reason: "routine occurrence extracted", CorrelationID: boundedRoutineCorrelation(correlationID, occurrence.ID)}
		applied, err := c.ApplyRoutineOccurrence(ctx, occurrence, mutation, occurrence.ObservedAt)
		item := routines.LearningOccurrenceResult{RoutineID: occurrence.RoutineID, OccurrenceID: occurrence.ID, Applied: applied.Applied, Created: applied.Created, Idempotent: applied.Idempotent}
		if err != nil {
			item.ErrorCode = "routine_error"
			result.ErrorCount++
		} else if applied.Idempotent {
			result.IdempotentCount++
		} else if applied.Applied || applied.Created {
			result.AppliedCount++
		}
		result.Results = append(result.Results, item)
	}
	return result
}

func boundedRoutineCorrelation(base string, id routines.OccurrenceID) string {
	value := base + ":occ:" + string(id)
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}

func cloneRoutineSnapshotPointer(value *routines.Snapshot) *routines.Snapshot {
	if value == nil {
		return nil
	}
	copy := *value
	copy.Occurrences = append([]routines.OccurrenceRef(nil), value.Occurrences...)
	for i := range copy.Occurrences {
		copy.Occurrences[i].ObservationIDs = append([]string(nil), value.Occurrences[i].ObservationIDs...)
		copy.Occurrences[i].TopologyRevisions = append([]string(nil), value.Occurrences[i].TopologyRevisions...)
	}
	copy.TemporalBins = append([]routines.TemporalBin(nil), value.TemporalBins...)
	copy.DayPartCounts = append([]routines.DayPartCount(nil), value.DayPartCounts...)
	copy.History = append([]routines.RevisionRecord(nil), value.History...)
	return &copy
}
