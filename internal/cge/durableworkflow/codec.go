package durableworkflow

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const recordVersion uint16 = 1

func EncodeRecord(record Record, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, ErrInvalidPolicy
	}
	if record.Version == 0 {
		record.Version = recordVersion
	}
	if record.Version != recordVersion || record.Sequence == 0 && record.Kind != RecordGenesis || !validRecordKind(record.Kind) || len(record.Payload) == 0 {
		return nil, ErrInvalidRecord
	}
	record.Payload = append([]byte(nil), record.Payload...)
	if record.PayloadLength == 0 {
		record.PayloadLength = uint64(len(record.Payload))
	}
	if record.PayloadLength != uint64(len(record.Payload)) {
		return nil, ErrInvalidRecord
	}
	payloadFingerprint := recordPayloadFingerprint(record.Payload)
	if record.PayloadFingerprint != "" && record.PayloadFingerprint != payloadFingerprint {
		return nil, ErrFingerprintMismatch
	}
	record.PayloadFingerprint = payloadFingerprint
	checksum := recordChecksum(record)
	if record.Checksum != "" && record.Checksum != checksum {
		return nil, ErrChecksumMismatch
	}
	record.Checksum = checksum
	encoded, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRecord, err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maxBytes {
		return nil, ErrRecordTooLarge
	}
	return encoded, nil
}

func DecodeRecord(data []byte, maxBytes int) (Record, error) {
	if len(data) == 0 || maxBytes <= 0 || len(data) > maxBytes {
		return Record{}, ErrRecordTooLarge
	}
	if data[len(data)-1] != '\n' {
		return Record{}, ErrTruncatedRecord
	}
	data = bytes.TrimSuffix(data, []byte{'\n'})
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalidRecord, err)
	}
	if record.Version != recordVersion || !validRecordKind(record.Kind) || len(record.Payload) == 0 {
		return Record{}, ErrInvalidRecord
	}
	if record.PayloadLength != uint64(len(record.Payload)) {
		return Record{}, ErrInvalidRecord
	}
	if record.Sequence == 0 && record.Kind != RecordGenesis {
		return Record{}, ErrSequenceRegression
	}
	if record.PayloadFingerprint != recordPayloadFingerprint(record.Payload) || record.Checksum != recordChecksum(record) {
		return Record{}, ErrChecksumMismatch
	}
	return record, nil
}

func validRecordKind(kind RecordKind) bool {
	return kind == RecordGenesis || kind == RecordTransaction || kind == RecordCheckpointMarker
}

func encodeJSON(value any, maxBytes int) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && len(encoded) > maxBytes {
		return nil, ErrCheckpointTooLarge
	}
	return encoded, nil
}

func encodeCheckpoint(checkpoint Checkpoint, maxBytes int) ([]byte, error) {
	if checkpoint.Fingerprint == "" || checkpoint.Checksum == "" || checkpoint.Fingerprint != CheckpointFingerprint(checkpoint) || checkpoint.Checksum != CheckpointChecksum(checkpoint) || checkpoint.State.Digest != WorkflowStateFingerprint(checkpoint.State) || checkpoint.SchemaFingerprint != SchemaFingerprint() {
		return nil, ErrCheckpointCorrupt
	}
	return encodeJSON(checkpoint, maxBytes)
}

func decodeCheckpoint(data []byte, maxBytes int) (Checkpoint, error) {
	if len(data) == 0 || len(data) > maxBytes {
		return Checkpoint{}, ErrCheckpointTooLarge
	}
	var checkpoint Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return Checkpoint{}, fmt.Errorf("%w: %v", ErrCheckpointCorrupt, err)
	}
	if checkpoint.Fingerprint != CheckpointFingerprint(checkpoint) || checkpoint.Checksum != CheckpointChecksum(checkpoint) || checkpoint.State.Digest != WorkflowStateFingerprint(checkpoint.State) || checkpoint.SchemaFingerprint != SchemaFingerprint() {
		return Checkpoint{}, ErrCheckpointCorrupt
	}
	return checkpoint, nil
}
