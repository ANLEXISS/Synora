package calibrationanalytics

import "errors"

var (
	ErrInvalidAnalyticsPolicy         = errors.New("calibration analytics: invalid policy")
	ErrInvalidAnalyticsInput          = errors.New("calibration analytics: invalid input")
	ErrInvalidAnalyticsMarkers        = errors.New("calibration analytics: invalid markers")
	ErrInvalidPolicyEvaluationMarkers = errors.New("calibration analytics: invalid policy evaluation markers")
	ErrInvalidReport                  = errors.New("calibration analytics: invalid report")
	ErrInsufficientRecords            = errors.New("calibration analytics: insufficient records")
	ErrInsufficientComparableRecords  = errors.New("calibration analytics: insufficient comparable records")
	ErrInsufficientWindows            = errors.New("calibration analytics: insufficient windows")
	ErrInsufficientCohorts            = errors.New("calibration analytics: insufficient cohorts")
	ErrTooManyRecords                 = errors.New("calibration analytics: too many records")
	ErrTooManyWindows                 = errors.New("calibration analytics: too many windows")
	ErrTooManyCohorts                 = errors.New("calibration analytics: too many cohorts")
	ErrTooManyCategories              = errors.New("calibration analytics: too many categories")
	ErrInvalidPermille                = errors.New("calibration analytics: invalid permille")
	ErrInvalidSequenceRange           = errors.New("calibration analytics: invalid sequence range")
	ErrInvalidCohort                  = errors.New("calibration analytics: invalid cohort")
	ErrInvalidWindow                  = errors.New("calibration analytics: invalid window")
	ErrLedgerUnavailable              = errors.New("calibration analytics: ledger unavailable")
	ErrAnalyticsDisabled              = errors.New("calibration analytics: disabled")
	ErrAnalysisFailed                 = errors.New("calibration analytics: analysis failed")
	ErrReportUnavailable              = errors.New("calibration analytics: report unavailable")
	ErrReportFingerprintMismatch      = errors.New("calibration analytics: report fingerprint mismatch")
	ErrUnsupportedSchema              = errors.New("calibration analytics: unsupported schema")
	ErrUnsupportedCategory            = errors.New("calibration analytics: unsupported category")
	ErrInsufficientData               = errors.New("calibration analytics: insufficient data")
	ErrLimitExceeded                  = errors.New("calibration analytics: limit exceeded")

	// Compatibility names retained for the first internal implementation.
	ErrInvalidPolicy  = ErrInvalidAnalyticsPolicy
	ErrInvalidInput   = ErrInvalidAnalyticsInput
	ErrInvalidMarkers = ErrInvalidAnalyticsMarkers
)

func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrInvalidAnalyticsPolicy):
		return "invalid_policy"
	case errors.Is(err, ErrInvalidAnalyticsInput):
		return "invalid_input"
	case errors.Is(err, ErrInvalidPermille):
		return "invalid_permille"
	case errors.Is(err, ErrInvalidSequenceRange):
		return "invalid_sequence_range"
	case errors.Is(err, ErrInvalidCohort):
		return "invalid_cohort"
	case errors.Is(err, ErrInvalidWindow):
		return "invalid_window"
	case errors.Is(err, ErrInvalidAnalyticsMarkers), errors.Is(err, ErrInvalidPolicyEvaluationMarkers):
		return "invalid_markers"
	case errors.Is(err, ErrReportFingerprintMismatch):
		return "report_fingerprint_mismatch"
	case errors.Is(err, ErrUnsupportedSchema):
		return "unsupported_schema"
	case errors.Is(err, ErrTooManyRecords), errors.Is(err, ErrTooManyWindows), errors.Is(err, ErrTooManyCohorts), errors.Is(err, ErrTooManyCategories), errors.Is(err, ErrLimitExceeded):
		return "limit_exceeded"
	case errors.Is(err, ErrInsufficientRecords), errors.Is(err, ErrInsufficientComparableRecords), errors.Is(err, ErrInsufficientWindows), errors.Is(err, ErrInsufficientCohorts), errors.Is(err, ErrInsufficientData):
		return "insufficient_data"
	case errors.Is(err, ErrLedgerUnavailable):
		return "ledger_unavailable"
	case errors.Is(err, ErrAnalysisFailed):
		return "analysis_failed"
	case errors.Is(err, ErrReportUnavailable):
		return "report_unavailable"
	case errors.Is(err, ErrAnalyticsDisabled):
		return "disabled"
	default:
		return "analytics_error"
	}
}
