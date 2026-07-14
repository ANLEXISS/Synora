package rpc

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"synora/internal/actionpolicy"
	"synora/internal/actions/whatsapp"
	"synora/internal/idgen"
	"synora/pkg/contract"
)

type actionTestRequest struct {
	Command  string         `json:"command"`
	Target   string         `json:"target,omitempty"`
	Message  string         `json:"message,omitempty"`
	Template string         `json:"template,omitempty"`
	DryRun   bool           `json:"dry_run"`
	Severity string         `json:"severity,omitempty"`
	ChainID  string         `json:"chain_id,omitempty"`
	EventID  string         `json:"event_id,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

func (s *Server) actionPolicyGet(_ contract.Message) (any, error) {
	if s == nil || s.actionPolicy == nil {
		return actionPolicyResponse(actionpolicy.Defaults()), nil
	}
	return actionPolicyResponse(s.actionPolicy.Get()), nil
}

func actionPolicyResponse(policy contract.ActionPolicy) map[string]any {
	config := whatsapp.ConfigFromEnv()
	return map[string]any{
		"levels": policy.Levels,
		"notifications": map[string]any{
			"whatsapp": map[string]any{
				"enabled": config.Enabled, "dry_run": config.DryRun,
				"provider": "whatsapp_cloud", "phone_number_id_configured": strings.TrimSpace(config.PhoneNumberID) != "",
				"default_to_configured": strings.TrimSpace(config.DefaultTo) != "", "default_template": config.DefaultTemplate,
			},
		},
	}
}

func (s *Server) actionPolicyUpdate(msg contract.Message) (any, error) {
	if s == nil || s.actionPolicy == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "action policy unavailable")
	}
	policy, err := s.actionPolicy.Update(msg.Payload)
	if err != nil {
		return nil, contract.NewAPIError(contract.ErrorValidationFailed, "%s", err.Error())
	}
	s.notifyMutation("actions.policy.updated", "action_policy")
	return actionPolicyResponse(policy), nil
}

func (s *Server) actionPolicyReset(_ contract.Message) (any, error) {
	if s == nil || s.actionPolicy == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "action policy unavailable")
	}
	policy, err := s.actionPolicy.Reset()
	if err != nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "%s", err.Error())
	}
	s.notifyMutation("actions.policy.reset", "action_policy")
	return actionPolicyResponse(policy), nil
}

func (s *Server) actionCatalog(_ contract.Message) (any, error) {
	return actionpolicy.Catalog(), nil
}

func (s *Server) actionTest(msg contract.Message) (any, error) {
	var input actionTestRequest
	if err := json.Unmarshal(msg.Payload, &input); err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid action test payload")
	}
	input.Command = strings.ToLower(strings.TrimSpace(input.Command))
	if input.Command == "" {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "command is required")
	}
	known := false
	for _, item := range actionpolicy.Catalog() {
		if item.Command == input.Command || item.ID == input.Command {
			known = true
			break
		}
	}
	if !known {
		return nil, contract.NewAPIError(contract.ErrorValidationFailed, "unknown action command %q", input.Command)
	}
	if input.Target == "" {
		input.Target = "owner"
	}
	data := map[string]any{"message": input.Message, "template": input.Template, "severity": input.Severity, "chain_id": input.ChainID, "event_id": input.EventID}
	for key, value := range input.Data {
		data[key] = value
	}
	requestID := idgen.New("action-test")
	request := contract.ActionRequest{
		ID: requestID, RequestID: requestID, CorrelationID: requestID, Source: "core",
		Target: input.Target, Type: input.Command, Timestamp: time.Now().UTC(), CreatedAt: time.Now().UTC(),
		Action: contract.Action{Type: input.Command, Data: data}, Data: data,
		Metadata:       map[string]any{"dry_run": input.DryRun, "test": true},
		IdempotencyKey: requestID,
	}
	if input.DryRun {
		return map[string]any{"status": "dry_run", "provider": providerForCommand(input.Command), "to": maskedRecipient(input.Target), "prepared_request": request}, nil
	}
	if s.actionDispatcher == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "action dispatcher unavailable")
	}
	if err := s.actionDispatcher.DispatchRequest(request); err != nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "action dispatch failed: %s", err)
	}
	return map[string]any{"status": "queued", "provider": providerForCommand(input.Command), "request_id": requestID, "action_result": map[string]any{"status": "pending", "request_id": requestID}}, nil
}

func providerForCommand(command string) string {
	if command == "notify.whatsapp" || command == "notify_owner_whatsapp" {
		return "whatsapp_cloud"
	}
	return "synora-actions"
}

func maskedRecipient(target string) string {
	value := strings.TrimSpace(os.Getenv("SYNORA_WHATSAPP_DEFAULT_TO"))
	if target != "owner" && target != "" {
		value = target
	}
	if value == "" {
		return "not_configured"
	}
	if len(value) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(value)-4) + value[len(value)-4:]
}
