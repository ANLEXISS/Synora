package actions

import (
	"fmt"
	"strings"
)

// ExecutionMode is the single physical-execution gate for the Actions
// service. Cognitive and simulation paths never bypass it.
type ExecutionMode string

const (
	ExecutionDisabled ExecutionMode = "disabled"
	ExecutionDryRun   ExecutionMode = "dry_run"
	ExecutionArmed    ExecutionMode = "armed"
)

const (
	ExecutionModeEnv       = "SYNORA_ACTION_EXECUTION_MODE"
	ArmedConfirmationEnv   = "SYNORA_ACTIONS_ARMED_CONFIRMATION"
	ArmedConfirmationValue = "I_UNDERSTAND_PHYSICAL_ACTIONS"
)

func ResolveExecutionMode(getenv func(string) string) (ExecutionMode, error) {
	if getenv == nil {
		return ExecutionDryRun, nil
	}
	value := strings.ToLower(strings.TrimSpace(getenv(ExecutionModeEnv)))
	if value == "" {
		return ExecutionDryRun, nil
	}
	mode := ExecutionMode(value)
	switch mode {
	case ExecutionDisabled, ExecutionDryRun:
		return mode, nil
	case ExecutionArmed:
		if strings.TrimSpace(getenv(ArmedConfirmationEnv)) != ArmedConfirmationValue {
			return ExecutionDisabled, fmt.Errorf("armed execution requires %s=%s", ArmedConfirmationEnv, ArmedConfirmationValue)
		}
		return mode, nil
	default:
		return ExecutionDisabled, fmt.Errorf("invalid action execution mode %q", value)
	}
}
