package fieldtrial

import "errors"

var (
	ErrInvalidConfig       = errors.New("invalid_field_trial_config")
	ErrInvalidSessionID    = errors.New("invalid_field_trial_session_id")
	ErrSessionClosed       = errors.New("field_trial_session_closed")
	ErrSessionDegraded     = errors.New("field_trial_session_degraded")
	ErrPartialRecord       = errors.New("field_trial_partial_record")
	ErrTelemetryCorrupt    = errors.New("field_trial_telemetry_corrupt")
	ErrQuotaExceeded       = errors.New("field_trial_quota_exceeded")
	ErrAnnotationInvalid   = errors.New("invalid_field_trial_annotation")
	ErrExportInvalid       = errors.New("invalid_field_trial_export")
	ErrKeyUnavailable      = errors.New("field_trial_key_unavailable")
	ErrUnsupportedRecovery = errors.New("unsupported_field_trial_recovery")
	ErrConfigurationDrift  = errors.New("field_trial_configuration_drift")
)
