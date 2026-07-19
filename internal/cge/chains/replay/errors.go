package replay

import (
	"errors"
	"fmt"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
)

var (
	ErrInvalidReplayInput          = errors.New("invalid_replay_input")
	ErrUnsupportedRecord           = errors.New("unsupported_record")
	ErrChainAlreadyExists          = errors.New("chain_already_exists")
	ErrChainNotFound               = errors.New("chain_not_found")
	ErrObservationNotAllowed       = chains.ErrObservationNotAllowed
	ErrDuplicateObservation        = chains.ErrDuplicateObservation
	ErrContributionNotAllowed      = chains.ErrContributionNotAllowed
	ErrDuplicateContribution       = chains.ErrDuplicateContribution
	ErrUnknownObservationReference = chains.ErrUnknownObservationReference
	ErrContributionResultMismatch  = errors.New("contribution_result_mismatch")
	ErrChainRestoreFailed          = errors.New("chain_restore_failed")
	ErrRevisionMismatch            = errors.New("revision_mismatch")
	ErrStatusMismatch              = errors.New("status_mismatch")
	ErrRevisionRecordMismatch      = errors.New("revision_record_mismatch")
	ErrInvalidTransition           = errors.New("invalid_transition")
	ErrCheckpointNotFound          = errors.New("checkpoint_not_found")
	ErrCheckpointMetadataMismatch  = errors.New("checkpoint_metadata_mismatch")
	ErrCheckpointPositionMismatch  = errors.New("checkpoint_position_mismatch")
	ErrAmbiguousCheckpoint         = errors.New("ambiguous_checkpoint")
	ErrSnapshotAheadOfJournal      = errors.New("snapshot_ahead_of_journal")
	ErrSnapshotCloneFailed         = errors.New("snapshot_clone_failed")
	ErrFinalRegistryInvalid        = errors.New("final_registry_invalid")
	ErrInvalidContext              = errors.New("invalid_context")
)

// ReplayError identifies the journal entry at which semantic reconstruction
// failed. The wrapped cause remains available to errors.Is/errors.As.
type ReplayError struct {
	Sequence uint64
	Kind     journal.RecordKind
	ChainID  chains.ChainID
	Err      error
}

func (e ReplayError) Error() string {
	if e.ChainID != "" {
		return fmt.Sprintf("replay failed at sequence=%d kind=%s chain=%s: %v", e.Sequence, e.Kind, e.ChainID, e.Err)
	}
	return fmt.Sprintf("replay failed at sequence=%d kind=%s: %v", e.Sequence, e.Kind, e.Err)
}

func (e ReplayError) Unwrap() error { return e.Err }
