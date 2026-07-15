package coreclient

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"synora/internal/bus"
	"synora/internal/security"
	"synora/pkg/contract"
)

type Client struct {
	bus requester
}

type requester interface {
	Request(msgType string, source string, payload []byte, target string) (*contract.Message, error)
}

type boundedRequester interface {
	RequestWithTimeout(msgType string, source string, payload []byte, target string, timeout time.Duration) (*contract.Message, error)
}

const coreRPCTimeout = 2 * time.Second

func New(
	busClient *bus.Client,
) *Client {

	return &Client{
		bus: busClient,
	}
}

func (c *Client) Snapshot() (
	*contract.PublicSnapshot,
	error,
) {
	state, err := c.coreState()
	if err != nil {
		return nil, err
	}

	snapshot := contract.PublicSnapshotFromCoreState(state)
	return &snapshot, nil
}

func (c *Client) State() (*contract.PublicSnapshot, error) {
	state, err := c.coreState()
	if err != nil {
		return nil, err
	}

	snapshot := contract.PublicSnapshotFromCoreState(state)
	return &snapshot, nil
}

func (c *Client) coreState() (map[string]any, error) {
	var state map[string]any
	if err := c.call("core.state", nil, &state); err != nil {
		return nil, err
	}
	return state, nil
}

func (c *Client) Devices() ([]map[string]any, error) {
	var devices []map[string]any
	if err := c.call("device.list", nil, &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

func (c *Client) EventChains(filter map[string]any) (map[string]any, error) {
	var result map[string]any
	if err := c.call("event.chains", filter, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Events() ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("event.list", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) EventChain(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("event.chain", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CriticalChains(filter map[string]any) ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("cge.critical_chains", filter, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CriticalChain(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.critical_chain", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGESecurityProfile() (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.security_profile", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateCGESecurityProfile(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("cge.security_profile.update", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ActionPolicy() (map[string]any, error) {
	var result map[string]any
	if err := c.call("actions.policy", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateActionPolicy(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("actions.policy.update", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ResetActionPolicy() (map[string]any, error) {
	var result map[string]any
	if err := c.call("actions.policy.reset", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ActionCatalog() ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("actions.catalog", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) TestAction(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("actions.test", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) SubmitCgeEvaluationFeedback(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("cge.feedback.evaluation", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) SubmitCgeChainFeedback(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("cge.feedback.chain", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CgeFeedbackList(filter map[string]any) ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("cge.feedback.list", filter, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) InjectCGEValidationEvent(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw(contract.RPCCGEValidationEvent, data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) InjectCGEValidationSequence(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw(contract.RPCCGEValidationSequence, data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGEValidationHistory() ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call(contract.RPCCGEValidationHistory, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ClearCGEValidationHistory() (map[string]any, error) {
	var result map[string]any
	if err := c.call(contract.RPCCGEValidationHistoryClear, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Device(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("device.get", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CreateDevice(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("device.create", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateDevice(id string, data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.call("device.update", mutationPayload(id, data), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Topology() (map[string]any, error) {
	var topology map[string]any
	if err := c.call("topology.snapshot", nil, &topology); err != nil {
		return nil, err
	}
	return topology, nil
}

func (c *Client) ReplaceTopology(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("topology.replace", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DeleteTopology() (map[string]any, error) {
	var result map[string]any
	if err := c.call("topology.delete", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) SystemHealth() (*contract.RuntimeHealth, error) {
	var health contract.RuntimeHealth
	if err := c.call("system.health", nil, &health); err != nil {
		return nil, err
	}
	return &health, nil
}

func (c *Client) ResetIntrusion(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("system.reset_intrusion", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ResetSystemState(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw(contract.RPCSystemResetState, data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ManualRisk(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw(contract.RPCManualRisk, data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ClearManualRisk(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw(contract.RPCManualRiskClear, data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) SecurityMode() (contract.SecurityModeState, error) {
	var result contract.SecurityModeState
	if err := c.call(contract.RPCSecurityMode, nil, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) SetSecurityMode(data json.RawMessage) (contract.SecurityModeState, error) {
	var result contract.SecurityModeState
	if err := c.callRaw(contract.RPCSecurityModeUpdate, data, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) ArmSecurity(data json.RawMessage) (contract.SecurityModeState, error) {
	var result contract.SecurityModeState
	if err := c.callRaw(contract.RPCSecurityArm, data, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) DisarmSecurity(data json.RawMessage) (contract.SecurityModeState, error) {
	var result contract.SecurityModeState
	if err := c.callRaw(contract.RPCSecurityDisarm, data, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c *Client) Validations() ([]contract.ValidationRequest, error) {
	var validations []contract.ValidationRequest
	if err := c.call("validations.list", nil, &validations); err != nil {
		return nil, err
	}
	return validations, nil
}

func (c *Client) Validation(id string) (*contract.ValidationRequest, error) {
	var result contract.ValidationRequest
	if err := c.call("validations.get", idPayload(id), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) CreateValidation(data json.RawMessage) (*contract.ValidationRequest, error) {
	var result contract.ValidationRequest
	if err := c.callRaw("validations.create", data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) UpdateValidation(id string, data json.RawMessage) (*contract.ValidationRequest, error) {
	var result contract.ValidationRequest
	if err := c.call("validations.update", mutationPayload(id, data), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) DeleteValidation(id string) (*contract.ValidationRequest, error) {
	var result contract.ValidationRequest
	if err := c.call("validations.delete", idPayload(id), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ResolveValidation(
	id string,
	data json.RawMessage,
) (*contract.ValidationRequest, error) {
	var req contract.ValidationResolveRequest
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid validation resolution payload")
		}
	}
	req.ID = strings.TrimSpace(id)

	var validation contract.ValidationRequest
	if err := c.call("validations.resolve", req, &validation); err != nil {
		return nil, err
	}
	return &validation, nil
}

func (c *Client) CGESummary() (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.summary", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGESequences(params map[string]any) ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("cge.sequences", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGETransitions(params map[string]any) ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("cge.transitions", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGELearnedBehaviors(params map[string]any) ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("cge.learned_behaviors", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGESequence(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.sequence", map[string]any{"id": strings.TrimSpace(id)}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGELearnedBehavior(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.learned_behavior", map[string]any{"id": strings.TrimSpace(id)}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGECriticalSeeds(params map[string]any) ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("cge.critical_seeds", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGECriticalSeed(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.critical_seed", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CreateCGECriticalSeed(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("cge.critical_seed.create", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateCGECriticalSeed(id string, data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.critical_seed.update", mutationPayload(id, data), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DeleteCGECriticalSeed(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.critical_seed.delete", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGEDangerAssessments(params map[string]any) ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("cge.danger_assessments", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CGEDangerAssessment(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.danger_assessment", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateCGELearnedBehavior(id string, data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.learned_behavior.update", mutationPayload(id, data), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DeleteCGELearnedBehavior(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("cge.learned_behavior.delete", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ActOnCGELearnedBehavior(id string, action string, data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	payload := map[string]any{
		"id":     strings.TrimSpace(id),
		"action": strings.TrimSpace(action),
		"data":   normalizedRawMessage(data),
	}
	if err := c.call("cge.learned_behavior.action", payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DeleteDevice(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("device.delete", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Residents() ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("residents.list", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Resident(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("resident.get", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CreateResident(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("residents.create", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateResident(id string, data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.call("resident.update", mutationPayload(id, data), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DeleteResident(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("resident.delete", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Automations() ([]map[string]any, error) {
	var result []map[string]any
	if err := c.call("automation.list", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Automation(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("automation.get", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CreateAutomation(data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.callRaw("automation.create", data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) UpdateAutomation(id string, data json.RawMessage) (map[string]any, error) {
	var result map[string]any
	if err := c.call("automation.update", mutationPayload(id, data), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DeleteAutomation(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("automation.delete", idPayload(id), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) StartPairing() (*security.PairingStartResponse, error) {
	var result security.PairingStartResponse
	if err := c.call("devices.pairing.start", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) CompletePairing(
	data json.RawMessage,
) (*security.PairingCompleteResponse, error) {
	var req security.PairingCompleteRequest
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid pairing payload")
		}
	}

	var result security.PairingCompleteResponse
	if err := c.call("devices.pairing.complete", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ResetTopology(
	data json.RawMessage,
) ([]map[string]any, error) {

	var topology []map[string]any
	if err := c.callRaw("topology.reset", data, &topology); err != nil {
		return nil, err
	}
	return topology, nil
}

func idPayload(id string) map[string]any {
	return map[string]any{"id": strings.TrimSpace(id)}
}

func mutationPayload(id string, data json.RawMessage) map[string]any {
	return map[string]any{
		"id":   strings.TrimSpace(id),
		"data": normalizedRawMessage(data),
	}
}

func normalizedRawMessage(data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return json.RawMessage(`{}`)
	}
	return data
}

func (c *Client) call(
	msgType string,
	payload any,
	out any,
) error {

	var body []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = encoded
	}

	return c.callRaw(msgType, body, out)
}

func (c *Client) callRaw(
	msgType string,
	body []byte,
	out any,
) error {

	var response *contract.Message
	var err error
	if bounded, ok := c.bus.(boundedRequester); ok {
		response, err = bounded.RequestWithTimeout(msgType, "api", body, "core", coreRPCTimeout)
	} else {
		response, err = c.bus.Request(msgType, "api", body, "core")
	}
	if err != nil {
		return err
	}

	if response == nil {
		return errors.New("empty bus response")
	}

	if rpcErr := responseError(response.Payload); rpcErr != nil {
		return rpcErr
	}

	if out == nil || len(response.Payload) == 0 {
		return nil
	}

	return json.Unmarshal(response.Payload, out)
}

func responseError(payload []byte) error {
	if len(payload) == 0 {
		return nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil
	}
	raw, ok := fields["error"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var code string
	if err := json.Unmarshal(raw, &code); err == nil {
		code = strings.TrimSpace(code)
		if code == "" {
			return nil
		}
		var message string
		_ = json.Unmarshal(fields["message"], &message)
		if isStableAPIErrorCode(code) || strings.TrimSpace(message) != "" {
			var details map[string]any
			_ = json.Unmarshal(fields["details"], &details)
			return &contract.APIError{Code: code, Message: strings.TrimSpace(message), Details: details}
		}
		// Compatibility with the historical RPC envelope {"error":"message"}.
		if len(fields) == 1 {
			return errors.New(code)
		}
		return nil
	}

	var typed contract.APIError
	if err := json.Unmarshal(raw, &typed); err == nil && strings.TrimSpace(typed.Code) != "" {
		_ = json.Unmarshal(fields["details"], &typed.Details)
		return &typed
	}
	return nil
}

func isStableAPIErrorCode(code string) bool {
	switch strings.TrimSpace(code) {
	case contract.ErrorInvalidJSON,
		contract.ErrorInvalidRequest,
		contract.ErrorNotFound,
		contract.ErrorDuplicateID,
		contract.ErrorValidationFailed,
		contract.ErrorForbiddenAction,
		contract.ErrorTopologyRequired,
		contract.ErrorUnsafeAutomation,
		contract.ErrorInternal:
		return true
	default:
		return false
	}
}
