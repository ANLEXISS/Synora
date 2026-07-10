package automation

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"synora/pkg/contract"
)

var automationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

const topologyNodeMissing = "topology_node_missing"

func ValidateRule(input Rule) error {
	return validateRule(input, true)
}

func validateStoredRule(input Rule) error {
	return validateRule(input, false)
}

func validateRule(input Rule, strictID bool) error {
	rule := normalizeRule(input)
	id := strings.TrimSpace(rule.ID)
	if id == "" || len(id) > 128 || (strictID && !automationIDPattern.MatchString(id)) {
		return contract.NewAPIError(contract.ErrorValidationFailed, "automation id is required and must be a stable identifier")
	}
	if strings.TrimSpace(rule.Trigger.EventType) == "" {
		return contract.NewAPIError(contract.ErrorValidationFailed, "automation trigger.event_type is required")
	}
	if rule.Trigger.MinScore < 0 || rule.Trigger.MinScore > 1 || rule.MinScore < 0 || rule.MinScore > 1 {
		return contract.NewAPIError(contract.ErrorValidationFailed, "automation min_score must be between 0 and 1")
	}
	logic := strings.ToLower(strings.TrimSpace(rule.ConditionLogic))
	if logic != "all" && logic != "any" {
		return contract.NewAPIError(contract.ErrorValidationFailed, "automation condition_logic must be all or any")
	}
	if rule.CooldownMs < 0 || rule.TimeoutMs < 0 || rule.RetryCount < 0 {
		return contract.NewAPIError(contract.ErrorValidationFailed, "automation timing and retry values cannot be negative")
	}
	if len(rule.Actions) == 0 {
		return contract.NewAPIError(contract.ErrorValidationFailed, "automation requires at least one action")
	}
	for _, condition := range rule.Conditions {
		if strings.TrimSpace(condition.Field) == "" {
			return contract.NewAPIError(contract.ErrorValidationFailed, "automation condition field is required")
		}
	}
	for _, action := range rule.Actions {
		if err := validateAction(rule, action, strictID); err != nil {
			return err
		}
	}
	return nil
}

func validateAction(rule Rule, action AutomationAction, strictAPI bool) error {
	if strings.TrimSpace(action.Type) == "" && strings.TrimSpace(action.Device) == "" &&
		strings.TrimSpace(action.Channel) == "" && strings.TrimSpace(action.Target) == "" {
		return contract.NewAPIError(contract.ErrorValidationFailed, "automation action type or target is required")
	}
	if action.TimeoutMs < 0 || action.RetryCount < 0 || action.Retry < 0 {
		return contract.NewAPIError(contract.ErrorValidationFailed, "automation action timing and retry values cannot be negative")
	}
	name := normalizedActionName(action)
	switch name {
	case "emergency_call", "emergency.call", "disable_camera", "camera.disable":
		return contract.NewAPIError(contract.ErrorForbiddenAction, "automatic action %q is forbidden", name)
	case "door.unlock", "siren.turn_on":
		approved := strings.EqualFold(strings.TrimSpace(rule.Status), contract.AutomationStatusApproved)
		if strictAPI && rule.Enabled && (!approved || !rule.RequiresValidation) {
			return contract.NewAPIError(contract.ErrorUnsafeAutomation,
				"action %q requires an approved automation with requires_validation=true", name)
		}
	case "siren.turn_on_without_policy":
		return contract.NewAPIError(contract.ErrorForbiddenAction, "automatic action %q is forbidden", name)
	}
	return nil
}

func containsSensitiveAction(actions []AutomationAction) bool {
	for _, action := range actions {
		switch normalizedActionName(action) {
		case "door.unlock", "siren.turn_on", "siren.turn_on_without_policy", "emergency_call", "emergency.call", "disable_camera", "camera.disable":
			return true
		}
	}
	return false
}

func normalizedActionName(action AutomationAction) string {
	name := normalizeActionToken(action.Type)
	command := normalizeActionToken(action.Command)
	if command == "" && action.Data != nil {
		command = normalizeActionToken(fmt.Sprint(action.Data["command"]))
	}
	switch {
	case name == "door.unlock", name == "unlock", command == "unlock":
		return "door.unlock"
	case name == "siren.turn_on", name == "siren.on":
		return "siren.turn_on"
	case command == "on" && strings.Contains(strings.ToLower(action.Target+" "+action.Device), "siren"):
		return "siren.turn_on"
	case name == "siren.turn_on_without_policy":
		return name
	case name == "emergency_call", name == "emergency.call", command == "emergency_call":
		return "emergency_call"
	case name == "disable_camera", name == "camera.disable", command == "disable_camera":
		return "disable_camera"
	case command == "disable" && strings.Contains(strings.ToLower(action.Target+" "+action.Device), "camera"):
		return "disable_camera"
	default:
		return name
	}
}

func normalizeActionToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func applyAutomationPatch(value *Rule, patch AutomationPatch, now time.Time) {
	if patch.Name != nil {
		value.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Enabled != nil {
		value.Enabled = *patch.Enabled
		value.enabledSet = true
		if value.Enabled {
			value.DeletedAt = nil
		}
	}
	if patch.Trigger != nil {
		value.Trigger = *patch.Trigger
		value.EventType = value.Trigger.EventType
		value.State = value.Trigger.State
		value.Node = value.Trigger.NodeID
		value.MinScore = value.Trigger.MinScore
	}
	if patch.Conditions != nil {
		value.Conditions = append([]Condition(nil), (*patch.Conditions)...)
	}
	if patch.ConditionLogic != nil {
		value.ConditionLogic = strings.ToLower(strings.TrimSpace(*patch.ConditionLogic))
	}
	if patch.Actions != nil {
		value.Actions = append([]AutomationAction(nil), (*patch.Actions)...)
	}
	if patch.CooldownMs != nil {
		value.CooldownMs = *patch.CooldownMs
	}
	if patch.TimeoutMs != nil {
		value.TimeoutMs = *patch.TimeoutMs
	}
	if patch.RetryCount != nil {
		value.RetryCount = *patch.RetryCount
	}
	if patch.DryRun != nil {
		value.DryRun = *patch.DryRun
	}
	if patch.RequiresValidation != nil {
		value.RequiresValidation = *patch.RequiresValidation
	}
	if patch.Status != nil {
		value.Status = strings.ToLower(strings.TrimSpace(*patch.Status))
	}
	value.UpdatedAt = now.UTC()
	*value = normalizeRule(*value)
}
