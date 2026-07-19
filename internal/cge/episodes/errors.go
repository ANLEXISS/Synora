package episodes

import "errors"

var (
	ErrInvalidPolicy               = errors.New("episodes: invalid policy")
	ErrInvalidObservation          = errors.New("episodes: invalid observation")
	ErrMissingEventID              = errors.New("episodes: missing event id")
	ErrMissingObservedAt           = errors.New("episodes: missing observed at")
	ErrDuplicateEvent              = errors.New("episodes: duplicate event")
	ErrEpisodeNotFound             = errors.New("episodes: episode not found")
	ErrEpisodeIDCollision          = errors.New("episodes: episode id collision")
	ErrEpisodeClosed               = errors.New("episodes: episode is not modifiable")
	ErrInvalidTransition           = errors.New("episodes: invalid transition")
	ErrSourceRevisionConflict      = errors.New("episodes: source revision conflict")
	ErrAmbiguousPlan               = errors.New("episodes: ambiguous plan")
	ErrRejectedPlan                = errors.New("episodes: rejected plan")
	ErrObservationLimitReached     = errors.New("episodes: observation limit reached")
	ErrLateObservationOutsideGrace = errors.New("episodes: late observation outside grace")
	ErrInvalidTopology             = errors.New("episodes: invalid topology")
	ErrInvalidSnapshot             = errors.New("episodes: invalid snapshot")
	ErrInvalidPlan                 = errors.New("episodes: invalid ingest plan")
	ErrInvalidEpisode              = errors.New("episodes: invalid episode")
)
