package situationfacts

import "errors"

var (
	ErrInvalidPolicy          = errors.New("situationfacts: invalid policy")
	ErrInvalidFact            = errors.New("situationfacts: invalid fact")
	ErrUnknownFactCode        = errors.New("situationfacts: unknown fact code")
	ErrInvalidFactValue       = errors.New("situationfacts: invalid fact value")
	ErrFactLimitReached       = errors.New("situationfacts: fact limit reached")
	ErrProvenanceLimitReached = errors.New("situationfacts: provenance limit reached")
	ErrMissingEpisodeID       = errors.New("situationfacts: missing episode id")
	ErrMissingEpisodeRevision = errors.New("situationfacts: missing episode revision")
	ErrInvalidFactSet         = errors.New("situationfacts: invalid fact set")
	ErrFactIDCollision        = errors.New("situationfacts: fact id collision")
	ErrFactKeyCollision       = errors.New("situationfacts: fact key collision")
	ErrSourceRevisionConflict = errors.New("situationfacts: source revision conflict")
	ErrStaleEpisodeRevision   = errors.New("situationfacts: stale episode revision")
	ErrFingerprintMismatch    = errors.New("situationfacts: fingerprint mismatch")
	ErrInvalidConflict        = errors.New("situationfacts: invalid conflict")
	ErrInvalidDiff            = errors.New("situationfacts: invalid diff")
)
