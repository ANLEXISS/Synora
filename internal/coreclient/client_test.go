package coreclient

import (
	"encoding/json"
	"errors"
	"testing"

	"synora/pkg/contract"
)

type recordingRequester struct {
	msgType string
	payload json.RawMessage
	result  json.RawMessage
}

func (r *recordingRequester) Request(msgType string, _ string, payload []byte, _ string) (*contract.Message, error) {
	r.msgType = msgType
	r.payload = append(json.RawMessage(nil), payload...)
	return &contract.Message{Payload: r.result}, nil
}

func TestResponseErrorDecodesStableAPIError(t *testing.T) {
	err := responseError([]byte(`{"error":"not_found","message":"device not found"}`))
	var apiErr *contract.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T %v", err, err)
	}
	if apiErr.Code != contract.ErrorNotFound || apiErr.Message != "device not found" {
		t.Fatalf("unexpected APIError: %#v", apiErr)
	}
}

func TestResponseErrorKeepsLegacyEnvelopeCompatibility(t *testing.T) {
	err := responseError([]byte(`{"error":"legacy failure"}`))
	if err == nil || err.Error() != "legacy failure" {
		t.Fatalf("unexpected legacy error: %v", err)
	}
}

func TestResponseErrorDoesNotMistakeResourceFieldForRPCError(t *testing.T) {
	err := responseError([]byte(`{"id":"automation-1","error":"topology_node_missing","enabled":false}`))
	if err != nil {
		t.Fatalf("resource payload was mistaken for RPC error: %v", err)
	}
}

func TestConfigurationMethodsUseExpectedRPCEnvelope(t *testing.T) {
	requester := &recordingRequester{result: json.RawMessage(`{"id":"cam-1","enabled":false}`)}
	client := &Client{bus: requester}
	if _, err := client.UpdateDevice(" cam-1 ", json.RawMessage(`{"enabled":false}`)); err != nil {
		t.Fatal(err)
	}
	if requester.msgType != "device.update" {
		t.Fatalf("msgType=%q", requester.msgType)
	}
	var payload struct {
		ID   string         `json:"id"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(requester.payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != "cam-1" || payload.Data["enabled"] != false {
		t.Fatalf("payload=%s", requester.payload)
	}
}

func TestLearnedBehaviorActionUsesActionRPC(t *testing.T) {
	requester := &recordingRequester{result: json.RawMessage(`{"id":"beh-1","status":"approved"}`)}
	client := &Client{bus: requester}
	if _, err := client.ActOnCGELearnedBehavior("beh-1", "approve", json.RawMessage(`{"requires_validation":true}`)); err != nil {
		t.Fatal(err)
	}
	if requester.msgType != "cge.learned_behavior.action" {
		t.Fatalf("msgType=%q", requester.msgType)
	}
	var payload map[string]any
	if err := json.Unmarshal(requester.payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["id"] != "beh-1" || payload["action"] != "approve" {
		t.Fatalf("payload=%s", requester.payload)
	}
	data, _ := payload["data"].(map[string]any)
	if data["requires_validation"] != true {
		t.Fatalf("payload=%s", requester.payload)
	}
}

func TestTopologyMethodsDecodeCanonicalObject(t *testing.T) {
	requester := &recordingRequester{result: json.RawMessage(`{"version":1,"locked":false,"nodes":[],"links":[]}`)}
	client := &Client{bus: requester}
	result, err := client.Topology()
	if err != nil {
		t.Fatal(err)
	}
	if requester.msgType != "topology.snapshot" || result["version"] != float64(1) {
		t.Fatalf("msgType=%q result=%#v", requester.msgType, result)
	}
}
