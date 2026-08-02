package situationhypotheses

import "errors"

var (
	ErrInvalidPolicy                  = errors.New("situationhypotheses: invalid policy")
	ErrInvalidSchema                  = errors.New("situationhypotheses: invalid schema")
	ErrInvalidDefinition              = errors.New("situationhypotheses: invalid definition")
	ErrInvalidRule                    = errors.New("situationhypotheses: invalid rule")
	ErrInvalidFactSet                 = errors.New("situationhypotheses: invalid fact set")
	ErrUnknownFactCode                = errors.New("situationhypotheses: unknown fact code")
	ErrMissingFactSetFingerprint      = errors.New("situationhypotheses: missing fact set fingerprint")
	ErrInvalidHypothesis              = errors.New("situationhypotheses: invalid hypothesis")
	ErrInvalidContribution            = errors.New("situationhypotheses: invalid contribution")
	ErrContributionWithoutFact        = errors.New("situationhypotheses: contribution without fact")
	ErrUnknownFactReference           = errors.New("situationhypotheses: unknown fact reference")
	ErrHypothesisLimitReached         = errors.New("situationhypotheses: hypothesis limit reached")
	ErrContributionLimitReached       = errors.New("situationhypotheses: contribution limit reached")
	ErrMissingRequirementLimitReached = errors.New("situationhypotheses: missing requirement limit reached")
	ErrHypothesisNotFound             = errors.New("situationhypotheses: hypothesis not found")
	ErrHypothesisIDCollision          = errors.New("situationhypotheses: hypothesis id collision")
	ErrContributionIDCollision        = errors.New("situationhypotheses: contribution id collision")
	ErrSourceRevisionConflict         = errors.New("situationhypotheses: source revision conflict")
	ErrStaleFactSet                   = errors.New("situationhypotheses: stale fact set")
	ErrFingerprintMismatch            = errors.New("situationhypotheses: fingerprint mismatch")
	ErrInvalidTransition              = errors.New("situationhypotheses: invalid transition")
	ErrInvalidPlan                    = errors.New("situationhypotheses: invalid plan")
	ErrAmbiguousLeadingHypothesis     = errors.New("situationhypotheses: ambiguous leading hypothesis")
)
