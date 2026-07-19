package journal

import (
	"errors"
	"fmt"
)

var (
	ErrJournalNotFound              = errors.New("journal_not_found")
	ErrJournalEmpty                 = errors.New("journal_empty")
	ErrJournalAlreadyInitialized    = errors.New("journal_already_initialized")
	ErrJournalNotInitialized        = errors.New("journal_not_initialized")
	ErrJournalTooLarge              = errors.New("journal_too_large")
	ErrRecordTooLarge               = errors.New("record_too_large")
	ErrInvalidSchema                = errors.New("invalid_schema")
	ErrUnsupportedSchema            = errors.New("unsupported_schema")
	ErrInvalidSequence              = errors.New("invalid_sequence")
	ErrSequenceGap                  = errors.New("sequence_gap")
	ErrInvalidRecordKind            = errors.New("invalid_record_kind")
	ErrInvalidRecord                = errors.New("invalid_record")
	ErrInvalidGenesis               = errors.New("invalid_genesis")
	ErrDuplicateGenesis             = errors.New("duplicate_genesis")
	ErrInvalidPayload               = errors.New("invalid_payload")
	ErrPayloadChecksumMismatch      = errors.New("payload_checksum_mismatch")
	ErrRecordHashMismatch           = errors.New("record_hash_mismatch")
	ErrPreviousHashMismatch         = errors.New("previous_hash_mismatch")
	ErrInvalidCheckpoint            = errors.New("invalid_checkpoint")
	ErrDuplicateObservation         = errors.New("duplicate_observation")
	ErrInvalidPath                  = errors.New("invalid_path")
	ErrInvalidFileMode              = errors.New("invalid_file_mode")
	ErrInvalidLimit                 = errors.New("invalid_limit")
	ErrAppendFailed                 = errors.New("append_failed")
	ErrExternalModificationDetected = errors.New("external_modification_detected")
	ErrInvalidContext               = errors.New("invalid_context")
)

type UnsupportedSchemaError struct {
	Found     int
	Supported int
}

func (e UnsupportedSchemaError) Error() string {
	return fmt.Sprintf("%s: found=%d supported=%d", ErrUnsupportedSchema, e.Found, e.Supported)
}

func (e UnsupportedSchemaError) Unwrap() error { return ErrUnsupportedSchema }

type SequenceError struct {
	Expected uint64
	Found    uint64
}

func (e SequenceError) Error() string {
	return fmt.Sprintf("%s: expected=%d found=%d", ErrSequenceGap, e.Expected, e.Found)
}

func (e SequenceError) Unwrap() error { return ErrSequenceGap }

type InvalidSequenceError struct {
	Sequence uint64
}

func (e InvalidSequenceError) Error() string {
	return fmt.Sprintf("%s: sequence=%d", ErrInvalidSequence, e.Sequence)
}

func (e InvalidSequenceError) Unwrap() error { return ErrInvalidSequence }
