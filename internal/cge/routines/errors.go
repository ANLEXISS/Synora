package routines

import "errors"

var (
	ErrInvalidRoutine             = errors.New("invalid_routine")
	ErrInvalidRoutineID           = errors.New("invalid_routine_id")
	ErrInvalidOccurrence          = errors.New("invalid_routine_occurrence")
	ErrInvalidOccurrenceID        = errors.New("invalid_routine_occurrence_id")
	ErrInvalidSubject             = errors.New("invalid_routine_subject")
	ErrInvalidPattern             = errors.New("invalid_routine_pattern")
	ErrInvalidPolicy              = errors.New("invalid_routine_extraction_policy")
	ErrRoutineNotFound            = errors.New("routine_not_found")
	ErrRoutineRevisionStale       = errors.New("routine_revision_stale")
	ErrDuplicateRoutineOccurrence = errors.New("duplicate_routine_occurrence")
	ErrRoutineOccurrenceCollision = errors.New("routine_occurrence_collision")
	ErrRoutineMismatch            = errors.New("routine_mismatch")
	ErrRoutineStatusTransition    = errors.New("routine_status_transition_not_allowed")
	ErrObservationNotApplicable   = errors.New("routine_observation_not_applicable")
)

type SkipCode string

const (
	SkipContextMissing           SkipCode = "context_missing"
	SkipPartialDisallowed        SkipCode = "partial_context_disallowed"
	SkipPreviousContextMissing   SkipCode = "previous_context_missing"
	SkipTransitionGapExceeded    SkipCode = "transition_gap_exceeded"
	SkipTopologyMissing          SkipCode = "topology_missing"
	SkipTopologyRevisionMismatch SkipCode = "topology_revision_mismatch"
	SkipTransitionUnreachable    SkipCode = "transition_unreachable"
	SkipTargetObservationMissing SkipCode = "target_observation_missing"
)

type NotApplicableError struct {
	Code SkipCode
}

func (e NotApplicableError) Error() string { return string(e.Code) }
func (e NotApplicableError) Unwrap() error { return ErrObservationNotApplicable }
