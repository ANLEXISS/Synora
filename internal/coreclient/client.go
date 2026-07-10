package coreclient

import (
	"encoding/json"
	"errors"
	"strings"

	"synora/internal/bus"
	"synora/internal/security"
	"synora/pkg/contract"
)

type Client struct {
	bus *bus.Client
}

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

func (c *Client) Topology() ([]map[string]any, error) {
	var topology []map[string]any
	if err := c.call("topology.snapshot", nil, &topology); err != nil {
		return nil, err
	}
	return topology, nil
}

func (c *Client) SystemHealth() (*contract.RuntimeHealth, error) {
	var health contract.RuntimeHealth
	if err := c.call("system.health", nil, &health); err != nil {
		return nil, err
	}
	return &health, nil
}

func (c *Client) Validations() ([]contract.ValidationRequest, error) {
	var validations []contract.ValidationRequest
	if err := c.call("validations.list", nil, &validations); err != nil {
		return nil, err
	}
	return validations, nil
}

func (c *Client) ResolveValidation(
	id string,
	data json.RawMessage,
) (*contract.ValidationRequest, error) {
	var req contract.ValidationResolveRequest
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, err
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

func (c *Client) UpdateDevice(
	id string,
	data json.RawMessage,
) ([]map[string]any, error) {

	var devices []map[string]any
	err := c.call(
		"device.update",
		map[string]any{
			"id":   strings.TrimSpace(id),
			"data": data,
		},
		&devices,
	)
	if err != nil {
		return nil, err
	}
	return devices, nil
}

func (c *Client) DeleteDevice(id string) (map[string]any, error) {
	var result map[string]any
	if err := c.call("device.delete", map[string]any{"id": strings.TrimSpace(id)}, &result); err != nil {
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
			return nil, err
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

	response, err := c.bus.Request(
		msgType,
		"api",
		body,
		"core",
	)
	if err != nil {
		return err
	}

	if response == nil {
		return errors.New("empty bus response")
	}

	if len(response.Payload) > 0 {
		var errPayload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(response.Payload, &errPayload); err == nil && errPayload.Error != "" {
			return errors.New(errPayload.Error)
		}
	}

	if out == nil || len(response.Payload) == 0 {
		return nil
	}

	return json.Unmarshal(response.Payload, out)
}
