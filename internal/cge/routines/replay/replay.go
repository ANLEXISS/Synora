package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/routines"
)

// ReplayRecords reconstructs a new routine registry from all records. It
// ignores non-routine records but still counts them as examined. No partial
// registry is returned when any routine record fails validation.
func ReplayRecords(records []journal.Record) (*routines.Registry, Metadata, error) {
	metadata := Metadata{RecordsExamined: uint64(len(records))}
	if len(records) > 0 {
		metadata.FinalSequence = records[len(records)-1].Sequence
		metadata.FinalHash = records[len(records)-1].RecordHash
	}
	target := routines.NewRegistry()
	for _, record := range records {
		var err error
		var routineID routines.RoutineID
		switch record.Kind {
		case journal.RecordKindRoutineCreated:
			var payload journal.RoutineCreatedPayload
			payload, err = decode[journal.RoutineCreatedPayload](record.Payload)
			routineID = payload.RoutineID
			if err == nil {
				err = applyCreated(target, record, payload)
			}
			if err == nil {
				metadata.RoutinesCreated++
			}
		case journal.RecordKindRoutineOccurrenceAdded:
			var payload journal.RoutineOccurrenceAddedPayload
			payload, err = decode[journal.RoutineOccurrenceAddedPayload](record.Payload)
			routineID = payload.RoutineID
			if err == nil {
				err = applyOccurrence(target, record, payload)
			}
			if err == nil {
				metadata.OccurrencesAdded++
			}
		case journal.RecordKindRoutineStatusChanged:
			var payload journal.RoutineStatusChangedPayload
			payload, err = decode[journal.RoutineStatusChangedPayload](record.Payload)
			routineID = payload.RoutineID
			if err == nil {
				err = applyStatus(target, record, payload)
			}
			if err == nil {
				metadata.StatusChangesApplied++
			}
		default:
			continue
		}
		if err != nil {
			return nil, Metadata{}, ReplayError{Sequence: record.Sequence, Kind: string(record.Kind), RoutineID: string(routineID), Err: errors.Join(ErrRoutineReplayFailed, err)}
		}
	}
	for _, snapshot := range target.List() {
		if _, err := routines.Restore(snapshot); err != nil {
			return nil, Metadata{}, fmt.Errorf("%w: final routine=%s: %v", ErrRoutineReplayFailed, snapshot.ID, err)
		}
	}
	return target, metadata, nil
}

func applyCreated(target *routines.Registry, record journal.Record, payload journal.RoutineCreatedPayload) error {
	if payload.PreviousRevision != 0 || payload.NewRevision != 1 || payload.RoutineID != payload.Snapshot.ID || payload.Snapshot.History[0].Actor != record.Actor || payload.Snapshot.History[0].CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.Snapshot.UpdatedAt) {
		return ErrRoutineRevisionMismatch
	}
	fingerprint, err := payload.Snapshot.Fingerprint()
	if err != nil {
		return err
	}
	if fingerprint != payload.SnapshotFingerprint {
		return ErrRoutineSnapshotFingerprint
	}
	if err := target.ReplayCreated(payload.Snapshot); err != nil {
		return err
	}
	return nil
}

func applyOccurrence(target *routines.Registry, record journal.Record, payload journal.RoutineOccurrenceAddedPayload) error {
	if payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 || payload.Occurrence.RoutineID != payload.RoutineID || payload.Revision.RoutineID != payload.RoutineID || payload.Revision.PreviousRevision != payload.PreviousRevision || payload.Revision.NewRevision != payload.NewRevision || payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.Revision.At) {
		return ErrRoutineRevisionMismatch
	}
	after, err := target.ReplayOccurrence(payload.RoutineID, payload.PreviousRevision, payload.Occurrence, payload.Revision)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(after.MutationOutcome(), payload.Outcome) {
		return ErrRoutineReplayResultMismatch
	}
	fingerprint, err := after.Fingerprint()
	if err != nil {
		return err
	}
	if fingerprint != payload.ResultSnapshotFingerprint {
		return ErrRoutineSnapshotFingerprint
	}
	return nil
}

func applyStatus(target *routines.Registry, record journal.Record, payload journal.RoutineStatusChangedPayload) error {
	if payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 || payload.Revision.RoutineID != payload.RoutineID || payload.Revision.PreviousRevision != payload.PreviousRevision || payload.Revision.NewRevision != payload.NewRevision || payload.Revision.PreviousStatus != payload.PreviousStatus || payload.Revision.NewStatus != payload.NewStatus || payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.Revision.At) {
		return ErrRoutineRevisionMismatch
	}
	mutation := chains.MutationContext{At: payload.Revision.At, Actor: payload.Revision.Actor, Reason: payload.Revision.Reason, CorrelationID: payload.Revision.CorrelationID}
	after, err := target.ReplayStatus(routines.SetStatusCommand{RoutineID: payload.RoutineID, SourceRevision: payload.PreviousRevision, Target: payload.NewStatus, Mutation: mutation}, payload.Revision)
	if err != nil {
		return err
	}
	fingerprint, err := after.Fingerprint()
	if err != nil {
		return err
	}
	if fingerprint != payload.ResultSnapshotFingerprint {
		return ErrRoutineSnapshotFingerprint
	}
	return nil
}

func decode[T any](data []byte) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("%w: payload: %v", ErrInvalidReplayInput, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return value, fmt.Errorf("%w: multiple JSON values", ErrInvalidReplayInput)
		}
		return value, fmt.Errorf("%w: trailing payload: %v", ErrInvalidReplayInput, err)
	}
	return value, nil
}
