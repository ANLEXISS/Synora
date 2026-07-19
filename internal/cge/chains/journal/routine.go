package journal

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"synora/internal/cge/routines"
)

// AppendRoutineCreated records the complete initial routine snapshot. The
// journal remains the single global WAL shared by all CGE domains.
func (j *FileJournal) AppendRoutineCreated(ctx context.Context, input RoutineCreatedInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateRoutineCreatedInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindRoutineCreated, input.RecordedAt, input.Actor, input.CorrelationID, RoutineCreatedPayload{
		RoutineID: input.RoutineID, PreviousRevision: input.PreviousRevision, NewRevision: input.NewRevision,
		Snapshot: input.Snapshot, SnapshotFingerprint: input.SnapshotFingerprint,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendRoutineOccurrenceAdded records one compact occurrence delta and its
// deterministic derived-statistics outcome.
func (j *FileJournal) AppendRoutineOccurrenceAdded(ctx context.Context, input RoutineOccurrenceAddedInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateRoutineOccurrenceAddedInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindRoutineOccurrenceAdded, input.RecordedAt, input.Actor, input.CorrelationID, RoutineOccurrenceAddedPayload{
		RoutineID: input.RoutineID, PreviousRevision: input.PreviousRevision, NewRevision: input.NewRevision,
		Occurrence: input.Occurrence, Revision: input.Revision, Outcome: input.Outcome,
		ResultSnapshotFingerprint: input.ResultSnapshotFingerprint,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

// AppendRoutineStatusChanged records an explicit routine status mutation.
func (j *FileJournal) AppendRoutineStatusChanged(ctx context.Context, input RoutineStatusChangedInput) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	if err := j.validate(); err != nil {
		return Record{}, err
	}
	if err := validateRoutineStatusChangedInput(input); err != nil {
		return Record{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadAppendBaseLocked(ctx)
	if err != nil {
		return Record{}, err
	}
	record, err := buildRecord(state.snapshot.HeadSequence+1, RecordKindRoutineStatusChanged, input.RecordedAt, input.Actor, input.CorrelationID, RoutineStatusChangedPayload{
		RoutineID: input.RoutineID, PreviousRevision: input.PreviousRevision, NewRevision: input.NewRevision,
		PreviousStatus: input.PreviousStatus, NewStatus: input.NewStatus, Revision: input.Revision,
		ResultSnapshotFingerprint: input.ResultSnapshotFingerprint,
	})
	if err != nil {
		return Record{}, err
	}
	record, err = j.appendRecordLocked(ctx, state, record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(record), nil
}

func validateRoutineCreatedInput(input RoutineCreatedInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if input.PreviousRevision != 0 || input.NewRevision != 1 || input.RoutineID != input.Snapshot.ID || input.Snapshot.Status != routines.StatusCandidate || input.Snapshot.Revision != 1 || len(input.Snapshot.Occurrences) != 1 || len(input.Snapshot.History) != 1 {
		return fmt.Errorf("%w: routine creation revision or shape", ErrInvalidPayload)
	}
	if input.Snapshot.History[0].Operation != routines.OperationRoutineCreated || input.Snapshot.History[0].RoutineID != input.RoutineID || input.Snapshot.History[0].Actor != input.Actor || input.Snapshot.History[0].CorrelationID != input.CorrelationID {
		return fmt.Errorf("%w: routine creation provenance", ErrInvalidPayload)
	}
	if input.RecordedAt.Before(input.Snapshot.UpdatedAt) {
		return fmt.Errorf("%w: routine record precedes update", ErrInvalidPayload)
	}
	if _, err := routines.Restore(input.Snapshot); err != nil {
		return fmt.Errorf("%w: routine snapshot: %v", ErrInvalidPayload, err)
	}
	return validateSnapshotFingerprint(input.Snapshot, input.SnapshotFingerprint)
}

func validateRoutineOccurrenceAddedInput(input RoutineOccurrenceAddedInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if input.PreviousRevision == 0 || input.NewRevision != input.PreviousRevision+1 || input.RoutineID != input.Occurrence.RoutineID || input.Revision.RoutineID != input.RoutineID || input.Revision.Operation != routines.OperationOccurrenceAdded || input.Revision.PreviousRevision != input.PreviousRevision || input.Revision.NewRevision != input.NewRevision || input.Revision.OccurrenceID != input.Occurrence.ID || input.Revision.Actor != input.Actor || input.Revision.CorrelationID != input.CorrelationID {
		return fmt.Errorf("%w: routine occurrence revision", ErrInvalidPayload)
	}
	if err := input.Occurrence.Validate(); err != nil {
		return fmt.Errorf("%w: routine occurrence: %v", ErrInvalidPayload, err)
	}
	if input.RecordedAt.Before(input.Revision.At) {
		return fmt.Errorf("%w: routine occurrence record precedes mutation", ErrInvalidPayload)
	}
	if input.Outcome.OccurrenceCount == 0 || input.Outcome.FirstSeenAt.IsZero() || input.Outcome.LastSeenAt.IsZero() {
		return fmt.Errorf("%w: routine occurrence outcome", ErrInvalidPayload)
	}
	return validateFingerprint(input.ResultSnapshotFingerprint)
}

func validateRoutineStatusChangedInput(input RoutineStatusChangedInput) error {
	if err := validateRecordedMutation(input.RecordedAt, input.Actor, input.CorrelationID); err != nil {
		return err
	}
	if input.PreviousRevision == 0 || input.NewRevision != input.PreviousRevision+1 || input.Revision.RoutineID != input.RoutineID || input.Revision.Operation != routines.OperationStatusChanged || input.Revision.PreviousRevision != input.PreviousRevision || input.Revision.NewRevision != input.NewRevision || input.Revision.PreviousStatus != input.PreviousStatus || input.Revision.NewStatus != input.NewStatus || input.Revision.Actor != input.Actor || input.Revision.CorrelationID != input.CorrelationID || input.PreviousStatus == input.NewStatus {
		return fmt.Errorf("%w: routine status revision", ErrInvalidPayload)
	}
	if input.PreviousStatus == routines.StatusInvalidated || input.NewStatus == routines.StatusCandidate || input.NewStatus == routines.StatusInvalidated && input.Revision.PreviousStatus == routines.StatusInvalidated {
		return fmt.Errorf("%w: routine status transition", ErrInvalidPayload)
	}
	if input.RecordedAt.Before(input.Revision.At) {
		return fmt.Errorf("%w: routine status record precedes mutation", ErrInvalidPayload)
	}
	return validateFingerprint(input.ResultSnapshotFingerprint)
}

func validateSnapshotFingerprint(snapshot routines.Snapshot, fingerprint string) error {
	expected, err := snapshot.Fingerprint()
	if err != nil {
		return fmt.Errorf("%w: snapshot fingerprint source: %v", ErrInvalidPayload, err)
	}
	if fingerprint != expected {
		return fmt.Errorf("%w: snapshot fingerprint mismatch", ErrInvalidPayload)
	}
	return nil
}

func validateFingerprint(value string) error {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return fmt.Errorf("%w: fingerprint", ErrInvalidPayload)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:")); err != nil {
		return fmt.Errorf("%w: fingerprint", ErrInvalidPayload)
	}
	return nil
}

func decodeRoutineCreatedPayload(record Record) (RoutineCreatedPayload, error) {
	var payload RoutineCreatedPayload
	if err := decodeStrictJSON(record.Payload, &payload); err != nil {
		return payload, fmt.Errorf("%w: routine.created: %v", ErrInvalidPayload, err)
	}
	if err := validateRoutineCreatedInput(RoutineCreatedInput{
		RoutineID: payload.RoutineID, PreviousRevision: payload.PreviousRevision, NewRevision: payload.NewRevision,
		Snapshot: payload.Snapshot, SnapshotFingerprint: payload.SnapshotFingerprint,
		RecordedAt: record.RecordedAt, Actor: record.Actor, CorrelationID: record.CorrelationID,
	}); err != nil {
		return payload, err
	}
	return payload, nil
}

func decodeRoutineOccurrenceAddedPayload(record Record) (RoutineOccurrenceAddedPayload, error) {
	var payload RoutineOccurrenceAddedPayload
	if err := decodeStrictJSON(record.Payload, &payload); err != nil {
		return payload, fmt.Errorf("%w: routine.occurrence_added: %v", ErrInvalidPayload, err)
	}
	if err := validateRoutineOccurrenceAddedInput(RoutineOccurrenceAddedInput{
		RoutineID: payload.RoutineID, PreviousRevision: payload.PreviousRevision, NewRevision: payload.NewRevision,
		Occurrence: payload.Occurrence, Revision: payload.Revision, Outcome: payload.Outcome,
		ResultSnapshotFingerprint: payload.ResultSnapshotFingerprint,
		RecordedAt:                record.RecordedAt, Actor: record.Actor, CorrelationID: record.CorrelationID,
	}); err != nil {
		return payload, err
	}
	return payload, nil
}

func decodeRoutineStatusChangedPayload(record Record) (RoutineStatusChangedPayload, error) {
	var payload RoutineStatusChangedPayload
	if err := decodeStrictJSON(record.Payload, &payload); err != nil {
		return payload, fmt.Errorf("%w: routine.status_changed: %v", ErrInvalidPayload, err)
	}
	if err := validateRoutineStatusChangedInput(RoutineStatusChangedInput{
		RoutineID: payload.RoutineID, PreviousRevision: payload.PreviousRevision, NewRevision: payload.NewRevision,
		PreviousStatus: payload.PreviousStatus, NewStatus: payload.NewStatus, Revision: payload.Revision,
		ResultSnapshotFingerprint: payload.ResultSnapshotFingerprint,
		RecordedAt:                record.RecordedAt, Actor: record.Actor, CorrelationID: record.CorrelationID,
	}); err != nil {
		return payload, err
	}
	return payload, nil
}
