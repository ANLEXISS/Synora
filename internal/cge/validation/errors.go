package validation

import "errors"

var (
	ErrScenarioInvalid   = errors.New("validation_scenario_invalid")
	ErrScenarioFailed    = errors.New("validation_scenario_failed")
	ErrScenarioCancelled = errors.New("validation_scenario_cancelled")
)
