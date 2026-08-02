package cognitiverecommendation

import "errors"

var (
	ErrInvalidPolicy               = errors.New("invalid cognitive recommendation policy")
	ErrInvalidPlanInput            = errors.New("invalid cognitive recommendation plan input")
	ErrInvalidSituation            = errors.New("invalid cognitive situation")
	ErrSituationNotReady           = errors.New("cognitive situation not ready")
	ErrSituationStale              = errors.New("cognitive situation stale")
	ErrSituationInvalidated        = errors.New("cognitive situation invalidated")
	ErrInvalidRecommendationKind   = errors.New("invalid recommendation kind")
	ErrInvalidRecommendationTarget = errors.New("invalid recommendation target")
	ErrInvalidRecommendationStatus = errors.New("invalid recommendation status")
	ErrInvalidRecommendation       = errors.New("invalid cognitive recommendation")
	ErrInvalidRecommendationSet    = errors.New("invalid cognitive recommendation set")
	ErrInvalidRecommendationDiff   = errors.New("invalid cognitive recommendation diff")
	ErrInvalidExplanation          = errors.New("invalid cognitive recommendation explanation")
	ErrMissingAdvisoryReference    = errors.New("missing advisory reference")
	ErrSourceFingerprintMismatch   = errors.New("source fingerprint mismatch")
	ErrSourceRevisionConflict      = errors.New("source revision conflict")
	ErrPrimaryMarginInsufficient   = errors.New("primary recommendation margin insufficient")
	ErrRecoveryRebuildFailed       = errors.New("recommendation recovery rebuild failed")
	ErrProjectionSnapshotConflict  = errors.New("projection snapshot conflict")
)
