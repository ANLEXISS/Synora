package cognitivesituation

import "errors"

var (
	ErrInvalidPolicy                  = errors.New("cognitivesituation: invalid policy")
	ErrInvalidBuildInput              = errors.New("cognitivesituation: invalid build input")
	ErrEpisodeNotFound                = errors.New("cognitivesituation: episode not found")
	ErrWorkflowInvalid                = errors.New("cognitivesituation: workflow invalid")
	ErrLayerInvalidated               = errors.New("cognitivesituation: layer invalidated")
	ErrLayerFingerprintMismatch       = errors.New("cognitivesituation: layer fingerprint mismatch")
	ErrMissingExpectedLayer           = errors.New("cognitivesituation: missing expected layer")
	ErrInvalidKnowledgeSummary        = errors.New("cognitivesituation: invalid knowledge summary")
	ErrInvalidHypothesisSummary       = errors.New("cognitivesituation: invalid hypothesis summary")
	ErrInvalidEvidenceSummary         = errors.New("cognitivesituation: invalid evidence summary")
	ErrInvalidAdvisorySummary         = errors.New("cognitivesituation: invalid advisory summary")
	ErrInvalidCapabilitySummary       = errors.New("cognitivesituation: invalid capability summary")
	ErrInvalidAuthorizationSummary    = errors.New("cognitivesituation: invalid authorization summary")
	ErrInvalidRecommendationReadiness = errors.New("cognitivesituation: invalid recommendation readiness")
	ErrInvalidSituation               = errors.New("cognitivesituation: invalid situation")
	ErrInvalidExplanation             = errors.New("cognitivesituation: invalid explanation")
	ErrInvalidDiff                    = errors.New("cognitivesituation: invalid diff")
	ErrSourceRevisionConflict         = errors.New("cognitivesituation: source revision conflict")
	ErrRecoveryRebuildFailed          = errors.New("cognitivesituation: recovery rebuild failed")
)
