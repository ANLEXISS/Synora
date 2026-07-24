package decisioncomparison

import "errors"

var (
	ErrInvalidPolicy                = errors.New("invalid decision comparison policy")
	ErrInvalidHistoricalDecisionRef = errors.New("invalid historical decision reference")
	ErrInvalidCompareInput          = errors.New("invalid decision comparison input")
	ErrMissingHistoricalDecision    = errors.New("missing historical decision")
	ErrInvalidSituation             = errors.New("invalid cognitive situation")
	ErrInvalidRecommendationSet     = errors.New("invalid cognitive recommendation set")
	ErrSourceFingerprintMismatch    = errors.New("decision comparison source fingerprint mismatch")
	ErrSourceRevisionConflict       = errors.New("decision comparison source revision conflict")
	ErrIncomparableSources          = errors.New("incomparable decision comparison sources")
	ErrInvalidDimension             = errors.New("invalid comparison dimension")
	ErrInvalidCategory              = errors.New("invalid comparison category")
	ErrInvalidComparison            = errors.New("invalid historical decision comparison")
	ErrInvalidExplanation           = errors.New("invalid comparison explanation")
	ErrProjectionSnapshotConflict   = errors.New("decision comparison projection snapshot conflict")
	ErrHistoricalIsolationViolation = errors.New("historical decision isolation violation")
)
