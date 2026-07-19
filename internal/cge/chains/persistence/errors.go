package persistence

import (
	"errors"
	"fmt"
)

var (
	ErrSnapshotNotFound     = errors.New("snapshot_not_found")
	ErrSnapshotEmpty        = errors.New("snapshot_empty")
	ErrSnapshotTooLarge     = errors.New("snapshot_too_large")
	ErrUnsupportedSchema    = errors.New("unsupported_schema")
	ErrChecksumMismatch     = errors.New("checksum_mismatch")
	ErrInvalidEnvelope      = errors.New("invalid_envelope")
	ErrInvalidPayload       = errors.New("invalid_payload")
	ErrChainCountMismatch   = errors.New("chain_count_mismatch")
	ErrDuplicateChainID     = errors.New("duplicate_chain_id")
	ErrChainRestoreFailed   = errors.New("chain_restore_failed")
	ErrAtomicWriteFailed    = errors.New("atomic_write_failed")
	ErrInvalidPath          = errors.New("invalid_path")
	ErrInvalidFileMode      = errors.New("invalid_file_mode")
	ErrInvalidSnapshotLimit = errors.New("invalid_snapshot_limit")
	ErrInvalidContext       = errors.New("invalid_context")
)

// UnsupportedSchemaError retains the version found on disk for diagnostics.
type UnsupportedSchemaError struct {
	Found     int
	Supported int
}

func (e UnsupportedSchemaError) Error() string {
	return fmt.Sprintf("%s: found=%d supported=%d", ErrUnsupportedSchema, e.Found, e.Supported)
}

func (e UnsupportedSchemaError) Unwrap() error { return ErrUnsupportedSchema }

// ChecksumMismatchError describes a failed payload integrity check without
// including payload contents.
type ChecksumMismatchError struct {
	Expected string
	Found    string
}

func (e ChecksumMismatchError) Error() string {
	return fmt.Sprintf("%s: expected=%s found=%s", ErrChecksumMismatch, e.Expected, e.Found)
}

func (e ChecksumMismatchError) Unwrap() error { return ErrChecksumMismatch }

// ChainCountMismatchError reports an envelope payload count mismatch.
type ChainCountMismatchError struct {
	Declared int
	Actual   int
}

func (e ChainCountMismatchError) Error() string {
	return fmt.Sprintf("%s: declared=%d actual=%d", ErrChainCountMismatch, e.Declared, e.Actual)
}

func (e ChainCountMismatchError) Unwrap() error { return ErrChainCountMismatch }
