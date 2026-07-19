package replay

import "errors"

var (
	ErrInvalidReplayInput          = errors.New("invalid_routine_replay_input")
	ErrUnsupportedRecord           = errors.New("unsupported_routine_record")
	ErrRoutineReplayFailed         = errors.New("routine_replay_failed")
	ErrRoutineRevisionMismatch     = errors.New("routine_replay_revision_mismatch")
	ErrRoutineSnapshotFingerprint  = errors.New("routine_snapshot_fingerprint_mismatch")
	ErrRoutineReplayResultMismatch = errors.New("routine_replay_result_mismatch")
	ErrRoutineAlreadyExists        = errors.New("routine_replay_already_exists")
)

type ReplayError struct {
	Sequence  uint64
	Kind      string
	RoutineID string
	Err       error
}

func (e ReplayError) Error() string {
	return "routine replay failed"
}

func (e ReplayError) Unwrap() error { return e.Err }
