package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/persistence"
)

type checkpointMatch struct {
	record  journal.Record
	payload journal.SnapshotCheckpointPayload
}

func selectCheckpoint(ctx context.Context, snapshot journal.JournalSnapshot, metadata persistence.SnapshotMetadata) (checkpointMatch, error) {
	checkpointCount := 0
	matching := make([]checkpointMatch, 0)
	for _, record := range snapshot.Records {
		if ctx == nil {
			return checkpointMatch{}, ErrInvalidContext
		}
		if err := ctx.Err(); err != nil {
			return checkpointMatch{}, fmt.Errorf("%w: %v", ErrInvalidContext, err)
		}
		if record.Kind != journal.RecordKindSnapshotCheckpointed {
			continue
		}
		checkpointCount++
		payload, err := decodeCheckpoint(record)
		if err != nil {
			return checkpointMatch{}, &ReplayError{Sequence: record.Sequence, Kind: record.Kind, Err: err}
		}
		if payload.SnapshotSchemaVersion != metadata.SchemaVersion ||
			!payload.SnapshotCreatedAt.Equal(metadata.CreatedAt) ||
			payload.SnapshotChainCount != metadata.ChainCount ||
			payload.SnapshotPayloadSHA256 != metadata.PayloadSHA256 ||
			payload.SnapshotSizeBytes != metadata.SizeBytes {
			continue
		}
		if err := validateCheckpointPosition(snapshot, record, payload); err != nil {
			return checkpointMatch{}, &ReplayError{Sequence: record.Sequence, Kind: record.Kind, Err: err}
		}
		matching = append(matching, checkpointMatch{record: record, payload: payload})
	}
	if len(matching) == 0 {
		if checkpointCount == 0 {
			return checkpointMatch{}, ErrCheckpointNotFound
		}
		return checkpointMatch{}, ErrCheckpointMetadataMismatch
	}

	selected := matching[0]
	for _, candidate := range matching[1:] {
		if candidate.payload.JournalSequence == selected.payload.JournalSequence &&
			candidate.record.RecordHash != selected.record.RecordHash {
			return checkpointMatch{}, ErrAmbiguousCheckpoint
		}
		if candidate.payload.JournalSequence > selected.payload.JournalSequence {
			selected = candidate
		}
	}
	return selected, nil
}

func selectCheckpointAt(
	ctx context.Context,
	snapshot journal.JournalSnapshot,
	metadata persistence.SnapshotMetadata,
	sequence uint64,
	hash string,
) (checkpointMatch, error) {
	if sequence == 0 || hash == "" {
		return checkpointMatch{}, ErrCheckpointPositionMismatch
	}
	for _, record := range snapshot.Records {
		if err := contextError(ctx); err != nil {
			return checkpointMatch{}, err
		}
		if record.Sequence != sequence || record.RecordHash != hash {
			continue
		}
		if record.Kind != journal.RecordKindSnapshotCheckpointed {
			return checkpointMatch{}, ErrCheckpointPositionMismatch
		}
		payload, err := decodeCheckpoint(record)
		if err != nil {
			return checkpointMatch{}, &ReplayError{Sequence: record.Sequence, Kind: record.Kind, Err: err}
		}
		if payload.SnapshotSchemaVersion != metadata.SchemaVersion ||
			!payload.SnapshotCreatedAt.Equal(metadata.CreatedAt) ||
			payload.SnapshotChainCount != metadata.ChainCount ||
			payload.SnapshotPayloadSHA256 != metadata.PayloadSHA256 ||
			payload.SnapshotSizeBytes != metadata.SizeBytes {
			return checkpointMatch{}, ErrCheckpointMetadataMismatch
		}
		if err := validateCheckpointPosition(snapshot, record, payload); err != nil {
			return checkpointMatch{}, err
		}
		return checkpointMatch{record: record, payload: payload}, nil
	}
	return checkpointMatch{}, ErrCheckpointNotFound
}

func decodeCheckpoint(record journal.Record) (journal.SnapshotCheckpointPayload, error) {
	var payload journal.SnapshotCheckpointPayload
	if err := decodePayload(record.Payload, &payload); err != nil {
		return journal.SnapshotCheckpointPayload{}, fmt.Errorf("%w: checkpoint: %v", ErrInvalidReplayInput, err)
	}
	if payload.SnapshotSchemaVersion <= 0 || payload.SnapshotCreatedAt.IsZero() ||
		payload.SnapshotChainCount < 0 || payload.SnapshotPayloadSHA256 == "" ||
		payload.SnapshotSizeBytes < 0 || payload.JournalHeadHash == "" {
		return journal.SnapshotCheckpointPayload{}, ErrCheckpointMetadataMismatch
	}
	return payload, nil
}

func validateCheckpointPosition(snapshot journal.JournalSnapshot, record journal.Record, payload journal.SnapshotCheckpointPayload) error {
	return validateCheckpointPositionRecords(snapshot.Records, snapshot.HeadSequence, record, payload)
}

func validateCheckpointPositionRecords(records []journal.Record, headSequence uint64, record journal.Record, payload journal.SnapshotCheckpointPayload) error {
	if payload.JournalSequence >= headSequence {
		return ErrSnapshotAheadOfJournal
	}
	if payload.JournalSequence == 0 || record.Sequence != payload.JournalSequence+1 {
		return ErrCheckpointPositionMismatch
	}
	if payload.JournalSequence >= uint64(len(records)) {
		return ErrSnapshotAheadOfJournal
	}
	previous := records[payload.JournalSequence-1]
	if previous.Sequence != payload.JournalSequence || previous.RecordHash != payload.JournalHeadHash {
		return ErrCheckpointPositionMismatch
	}
	return nil
}

func decodeChainAdded(record journal.Record) (chains.Snapshot, error) {
	var payload journal.ChainAddedPayload
	if err := decodePayload(record.Payload, &payload); err != nil {
		return chains.Snapshot{}, fmt.Errorf("%w: chain.added: %v", ErrInvalidReplayInput, err)
	}
	return payload.Chain, nil
}

func decodeLifecycle(record journal.Record) (journal.LifecycleTransitionPayload, error) {
	var payload journal.LifecycleTransitionPayload
	if err := decodePayload(record.Payload, &payload); err != nil {
		return journal.LifecycleTransitionPayload{}, fmt.Errorf("%w: lifecycle transition: %v", ErrInvalidReplayInput, err)
	}
	return payload, nil
}

func decodeObservationAdded(record journal.Record) (journal.ObservationAddedPayload, error) {
	var payload journal.ObservationAddedPayload
	if err := decodePayload(record.Payload, &payload); err != nil {
		return journal.ObservationAddedPayload{}, fmt.Errorf("%w: observation.added: %v", ErrInvalidReplayInput, err)
	}
	if _, err := chains.NewChainID(string(payload.ChainID)); err != nil || payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 {
		return journal.ObservationAddedPayload{}, ErrRevisionMismatch
	}
	if err := payload.Observation.Validate(); err != nil {
		return journal.ObservationAddedPayload{}, fmt.Errorf("%w: observation: %v", ErrInvalidReplayInput, err)
	}
	if err := payload.Revision.Validate(); err != nil {
		return journal.ObservationAddedPayload{}, fmt.Errorf("%w: revision: %v", ErrInvalidReplayInput, err)
	}
	revision := payload.Revision
	if revision.ChainID != payload.ChainID || revision.Operation != chains.OperationObservationAdded || revision.PreviousRevision != payload.PreviousRevision || revision.NewRevision != payload.NewRevision || len(revision.ObservationIDs) != 1 || revision.ObservationIDs[0] != payload.Observation.ID || len(revision.ContributionIDs) != 0 || revision.PreviousStatus != "" || revision.NewStatus != "" || revision.Actor != record.Actor || revision.CorrelationID != record.CorrelationID {
		return journal.ObservationAddedPayload{}, ErrRevisionRecordMismatch
	}
	return payload, nil
}

func decodeContributionAdded(record journal.Record) (journal.ContributionAddedPayload, error) {
	var payload journal.ContributionAddedPayload
	if err := decodePayload(record.Payload, &payload); err != nil {
		return journal.ContributionAddedPayload{}, fmt.Errorf("%w: contribution.added: %v", ErrInvalidReplayInput, err)
	}
	if _, err := chains.NewChainID(string(payload.ChainID)); err != nil || payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 {
		return journal.ContributionAddedPayload{}, ErrRevisionMismatch
	}
	if err := payload.Contribution.Validate(); err != nil {
		return journal.ContributionAddedPayload{}, fmt.Errorf("%w: contribution: %v", ErrInvalidReplayInput, err)
	}
	if _, err := chains.ProjectedConfidence(payload.PreviousConfidence, payload.Contribution); err != nil {
		return journal.ContributionAddedPayload{}, fmt.Errorf("%w: previous confidence: %v", ErrInvalidReplayInput, err)
	}
	expectedConfidence, _ := chains.ProjectedConfidence(payload.PreviousConfidence, payload.Contribution)
	if expectedConfidence != payload.NewConfidence {
		return journal.ContributionAddedPayload{}, errors.Join(ErrContributionResultMismatch, ErrRevisionMismatch)
	}
	if err := payload.Revision.Validate(); err != nil {
		return journal.ContributionAddedPayload{}, fmt.Errorf("%w: revision: %v", ErrInvalidReplayInput, err)
	}
	revision := payload.Revision
	if revision.ChainID != payload.ChainID || revision.Operation != chains.OperationContributionAdded || revision.PreviousRevision != payload.PreviousRevision || revision.NewRevision != payload.NewRevision || len(revision.ContributionIDs) != 1 || revision.ContributionIDs[0] != payload.Contribution.ID || revision.PreviousStatus != "" || revision.NewStatus != "" || revision.PreviousConfidence != nil || revision.NewConfidence != nil || revision.Actor != record.Actor || revision.CorrelationID != record.CorrelationID {
		return journal.ContributionAddedPayload{}, ErrRevisionRecordMismatch
	}
	for _, observationID := range payload.Contribution.ObservationIDs {
		if !containsObservationIDFromRevision(revision.ObservationIDs, observationID) {
			return journal.ContributionAddedPayload{}, ErrRevisionRecordMismatch
		}
	}
	expectedSupport, expectedContradiction := payload.PreviousSupportCount, payload.PreviousContradictionCount
	switch payload.Contribution.Kind {
	case chains.ContributionSupport:
		expectedSupport++
	case chains.ContributionContradiction:
		expectedContradiction++
	}
	if payload.NewSupportCount != expectedSupport || payload.NewContradictionCount != expectedContradiction {
		return journal.ContributionAddedPayload{}, errors.Join(ErrContributionResultMismatch, ErrRevisionMismatch)
	}
	return payload, nil
}

func containsObservationIDFromRevision(ids []string, expected string) bool {
	for _, id := range ids {
		if id == expected {
			return true
		}
	}
	return false
}

func decodeGenesis(record journal.Record) (journal.GenesisPayload, error) {
	var payload journal.GenesisPayload
	if err := decodePayload(record.Payload, &payload); err != nil {
		return journal.GenesisPayload{}, fmt.Errorf("%w: genesis: %v", ErrInvalidReplayInput, err)
	}
	return payload, nil
}

func decodePayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return ErrInvalidReplayInput
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return ErrInvalidReplayInput
		}
		return err
	}
	return nil
}
