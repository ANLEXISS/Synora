package deviation

import "errors"

var (
	ErrInvalidDeviationPolicy       = errors.New("invalid_deviation_policy")
	ErrInvalidDeviationScore        = errors.New("invalid_deviation_score")
	ErrInvalidDeviationOccurrence   = errors.New("invalid_deviation_occurrence")
	ErrInvalidDeviationCandidate    = errors.New("invalid_deviation_candidate")
	ErrDeviationSubjectMismatch     = errors.New("deviation_subject_mismatch")
	ErrDeviationKindMismatch        = errors.New("deviation_kind_mismatch")
	ErrDeviationOccurrenceCollision = errors.New("deviation_occurrence_collision")
	ErrCandidateLimitExceeded       = errors.New("candidate_limit_exceeded")
	ErrInvalidDeviationAssessment   = errors.New("invalid_deviation_assessment")
	ErrInvalidDeviationFingerprint  = errors.New("invalid_deviation_fingerprint")
	ErrInvalidTimestamp             = errors.New("invalid_timestamp")
)
