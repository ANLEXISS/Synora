package replay

import (
	"errors"
	"fmt"

	"synora/internal/cge/chains/journal"
	"synora/internal/cge/hypotheses"
)

var (
	ErrInvalidReplayInput     = errors.New("invalid_hypothesis_replay_input")
	ErrUnsupportedRecord      = errors.New("unsupported_hypothesis_record")
	ErrHypothesisReplayFailed = errors.New("hypothesis_replay_failed")
	ErrRevisionMismatch       = errors.New("hypothesis_revision_mismatch")
	ErrStatusMismatch         = errors.New("hypothesis_status_mismatch")
	ErrRevisionRecordMismatch = errors.New("hypothesis_revision_record_mismatch")
	ErrFinalRegistryInvalid   = errors.New("hypothesis_final_registry_invalid")
)

// ReplayError identifies the exact global journal record that failed.
type ReplayError struct {
	Sequence uint64
	Kind     journal.RecordKind
	SetID    hypotheses.SetID
	Err      error
}

func (e ReplayError) Error() string {
	if e.SetID != "" {
		return fmt.Sprintf("hypothesis replay failed at sequence=%d kind=%s set=%s: %v", e.Sequence, e.Kind, e.SetID, e.Err)
	}
	return fmt.Sprintf("hypothesis replay failed at sequence=%d kind=%s: %v", e.Sequence, e.Kind, e.Err)
}

func (e ReplayError) Unwrap() error { return e.Err }
