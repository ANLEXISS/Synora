package cognitive

import (
	"errors"
	"strings"
)

const CognitiveDryRunEnv = "SYNORA_COGNITIVE_DRY_RUN"

var ErrPhysicalExecutionDisabled = errors.New("cognitive physical execution is disabled; proposal remains dry-run")

// SafetyDecision is a report-only result. AllowedActions means "allowed to
// remain an advisory proposal"; it never means dispatched or executed.
type SafetyDecision struct {
	AdvisoryOnly   bool             `json:"advisory_only"`
	AllowedActions []string         `json:"allowed_actions"`
	BlockedActions []FilteredAction `json:"blocked_actions"`
	ExecutorCalled bool             `json:"executor_called"`
}

// ApplySafetyGate is intentionally the final boundary exposed to the MLP
// shadow harness. The cognitive runtime has no executor reference, and every
// high/critical proposal is blocked even as an advisory suggestion.
func ApplySafetyGate(output CognitiveOutput, catalog ActionCatalog) SafetyDecision {
	decision := SafetyDecision{AdvisoryOnly: true, AllowedActions: []string{}, BlockedActions: append([]FilteredAction{}, output.FilteredActions...), ExecutorCalled: false}
	for _, id := range output.ProposedActionIDs {
		risk := "unknown"
		for _, action := range catalog.Actions {
			if actionSlot(action) == id {
				risk = strings.ToLower(strings.TrimSpace(action.RiskClass))
				break
			}
		}
		switch risk {
		case "high", "critical", "teacher_gated", "unknown":
			decision.BlockedActions = append(decision.BlockedActions, FilteredAction{ID: id, Reason: "safety_gate_advisory_only"})
		default:
			decision.AllowedActions = append(decision.AllowedActions, id)
		}
	}
	return decision
}

// DryRunPolicy fails closed. Even when the environment variable is omitted,
// cognitive output cannot reach an actuator. Setting SYNORA_COGNITIVE_DRY_RUN=1
// makes the intended mode explicit in deployments and demonstrations.
type DryRunPolicy struct {
	Enabled bool
}

func NewDryRunPolicy(getenv func(string) string) DryRunPolicy {
	if getenv == nil {
		return DryRunPolicy{Enabled: true}
	}
	value := strings.ToLower(strings.TrimSpace(getenv(CognitiveDryRunEnv)))
	return DryRunPolicy{Enabled: value == "" || value == "1" || value == "true" || value == "yes" || value == "on"}
}

func (p DryRunPolicy) PublishOnly() bool { return p.Enabled }

func (p DryRunPolicy) Execute(_ string) error {
	return ErrPhysicalExecutionDisabled
}
