package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessageJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 11, 12, 13, time.UTC)
	msg := Message{
		ID:            "msg-1",
		Version:       "v1",
		Type:          EventVisionIdentity,
		Kind:          KindEvent,
		Source:        "camera-1",
		Target:        "core",
		SourceType:    SourceDevice,
		Timestamp:     now,
		Priority:      PriorityHigh,
		TrackID:       "track-1",
		CorrelationID: "corr-1",
		RequestID:     "req-1",
		Payload:       json.RawMessage(`{"identity":"alexis"}`),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	assertJSONField(t, data, "correlation_id")
	assertJSONField(t, data, "request_id")
	assertJSONField(t, data, "source_type")
	assertJSONField(t, data, "track_id")

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if decoded.ID != msg.ID || decoded.Type != msg.Type || decoded.CorrelationID != msg.CorrelationID {
		t.Fatalf("decoded message mismatch: %#v", decoded)
	}
	if !decoded.Timestamp.Equal(now) {
		t.Fatalf("timestamp mismatch: got %s want %s", decoded.Timestamp, now)
	}
}

func TestEventJSONRoundTripAndLegacyInput(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 12, 0, 0, time.UTC)
	event := Event{
		ID:         "evt-1",
		Type:       EventVisionUnknown,
		Source:     "camera-1",
		Timestamp:  now,
		Payload:    map[string]any{"confidence": 0.73},
		DeviceID:   "device-1",
		NodeID:     "entry",
		Identity:   "unknown",
		Confidence: 0.73,
		Priority:   PriorityNormal,
		GroupKey:   "vision.unknown|camera-1",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	assertJSONField(t, data, "device_id")
	assertJSONField(t, data, "node_id")
	assertJSONField(t, data, "group_key")

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if decoded.DeviceID != event.DeviceID || decoded.NodeID != event.NodeID || decoded.GroupKey != event.GroupKey {
		t.Fatalf("decoded event mismatch: %#v", decoded)
	}

	legacy := []byte(`{"ID":"evt-2","Type":"vision.motion","Source":"camera-2","DeviceID":"device-2","NodeID":"hall","GroupKey":"legacy"}`)
	if err := json.Unmarshal(legacy, &decoded); err != nil {
		t.Fatalf("unmarshal legacy event: %v", err)
	}
	if decoded.ID != "evt-2" || decoded.DeviceID != "device-2" || decoded.NodeID != "hall" || decoded.GroupKey != "legacy" {
		t.Fatalf("legacy event mismatch: %#v", decoded)
	}
}

func TestDecisionJSONRoundTripAndLegacyInput(t *testing.T) {
	decision := Decision{
		ID:             "dec-1",
		Type:           "intrusion.suspicious",
		Source:         "core",
		Priority:       PriorityHigh,
		EventID:        "evt-1",
		Score:          0.8,
		EffectiveScore: 0.9,
		Alert:          true,
		Reason:         "unknown identity",
		State:          "suspicious",
		NodeID:         "entry",
	}

	data, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	assertJSONField(t, data, "event_id")
	assertJSONField(t, data, "effective_score")
	assertJSONField(t, data, "node_id")

	var decoded Decision
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal decision: %v", err)
	}
	if decoded.EventID != decision.EventID || decoded.EffectiveScore != decision.EffectiveScore || decoded.NodeID != decision.NodeID {
		t.Fatalf("decoded decision mismatch: %#v", decoded)
	}

	legacy := []byte(`{"ID":"dec-2","Type":"intrusion","EventID":"evt-2","EffectiveScore":0.42,"NodeID":"kitchen"}`)
	if err := json.Unmarshal(legacy, &decoded); err != nil {
		t.Fatalf("unmarshal legacy decision: %v", err)
	}
	if decoded.ID != "dec-2" || decoded.EventID != "evt-2" || decoded.EffectiveScore != 0.42 || decoded.NodeID != "kitchen" {
		t.Fatalf("legacy decision mismatch: %#v", decoded)
	}
}

func TestActionContractsJSONRoundTrip(t *testing.T) {
	request := ActionRequest{
		ID:             "act-1",
		Version:        "v1",
		RequestID:      "req-1",
		CorrelationID:  "corr-1",
		Source:         "core",
		Target:         "actions",
		IdempotencyKey: "key-1",
		Action: Action{
			Device:  "light-1",
			Command: "set",
			Value:   true,
		},
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal action request: %v", err)
	}
	assertJSONField(t, data, "idempotency_key")

	var decoded ActionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal action request: %v", err)
	}
	if decoded.Action.Device != request.Action.Device || decoded.IdempotencyKey != request.IdempotencyKey {
		t.Fatalf("decoded action request mismatch: %#v", decoded)
	}

	result := ActionResult{
		ID:       "res-1",
		ActionID: "act-1",
		Status:   "accepted",
		Details:  map[string]any{"adapter": "fake"},
	}
	data, err = json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal action result: %v", err)
	}
	assertJSONField(t, data, "action_id")

	var decodedResult ActionResult
	if err := json.Unmarshal(data, &decodedResult); err != nil {
		t.Fatalf("unmarshal action result: %v", err)
	}
	if decodedResult.ActionID != result.ActionID || decodedResult.Status != result.Status {
		t.Fatalf("decoded action result mismatch: %#v", decodedResult)
	}
}

func TestRPCContractsJSONRoundTrip(t *testing.T) {
	request := RPCRequest{
		ID:            "rpc-1",
		Version:       "v1",
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		Method:        "state.snapshot",
		Source:        "api",
		Target:        "core",
		Params:        json.RawMessage(`{"include":"all"}`),
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal rpc request: %v", err)
	}
	assertJSONField(t, data, "request_id")
	assertJSONField(t, data, "correlation_id")

	var decoded RPCRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal rpc request: %v", err)
	}
	if decoded.Method != request.Method || string(decoded.Params) != string(request.Params) {
		t.Fatalf("decoded rpc request mismatch: %#v", decoded)
	}

	response := RPCResponse{
		ID:            "rpc-1",
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		Method:        "state.snapshot",
		Result:        json.RawMessage(`{"system":{"state":"normal"}}`),
	}
	data, err = json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal rpc response: %v", err)
	}
	assertJSONField(t, data, "result")

	var decodedResponse RPCResponse
	if err := json.Unmarshal(data, &decodedResponse); err != nil {
		t.Fatalf("unmarshal rpc response: %v", err)
	}
	if decodedResponse.Method != response.Method || string(decodedResponse.Result) != string(response.Result) {
		t.Fatalf("decoded rpc response mismatch: %#v", decodedResponse)
	}

	errorResponse := RPCResponse{
		ID:     "rpc-2",
		Method: "device.update",
		Error:  &RPCError{Code: "not_found", Message: "device not found"},
	}
	data, err = json.Marshal(errorResponse)
	if err != nil {
		t.Fatalf("marshal rpc error response: %v", err)
	}
	var decodedError RPCResponse
	if err := json.Unmarshal(data, &decodedError); err != nil {
		t.Fatalf("unmarshal rpc error response: %v", err)
	}
	if decodedError.Error == nil || decodedError.Error.Code != "not_found" {
		t.Fatalf("decoded rpc error mismatch: %#v", decodedError)
	}
}

func TestStateSnapshotJSONRoundTrip(t *testing.T) {
	snapshot := StateSnapshot{
		Version:   "v1",
		System:    map[string]any{"state": "normal"},
		Metrics:   map[string]any{"events": float64(2)},
		Devices:   map[string]any{"camera-1": map[string]any{"online": true}},
		Topology:  []map[string]any{{"id": "home"}},
		Residents: []map[string]any{{"id": "alexis"}},
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal state snapshot: %v", err)
	}
	assertJSONField(t, data, "system")
	assertJSONField(t, data, "devices")
	assertJSONField(t, data, "topology")

	var decoded StateSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal state snapshot: %v", err)
	}
	if decoded.System["state"] != "normal" || decoded.Devices["camera-1"] == nil {
		t.Fatalf("decoded state snapshot mismatch: %#v", decoded)
	}
}

func TestPublicSnapshotFromCoreStateHidesInternalAndLegacyKeys(t *testing.T) {
	coreState := map[string]any{
		"system": map[string]any{
			"LastState":       "idle",
			"IntrusionTime":   time.Time{},
			"LastStateTime":   "0001-01-01T00:00:00Z",
			"IntrusionActive": false,
		},
		"devices": []any{
			map[string]any{
				"ID":       "camera-1",
				"Type":     "camera",
				"NodeID":   "entry",
				"LastSeen": "0001-01-01T00:00:00Z",
			},
		},
		"device": []any{
			map[string]any{"ID": "legacy-camera"},
		},
		"events": []any{
			map[string]any{"id": "evt-1", "timestamp": "0001-01-01T00:00:00Z"},
		},
		"event": []any{
			map[string]any{"id": "legacy-event"},
		},
		"automations": []any{
			map[string]any{
				"id":      "auto-1",
				"event":   "vision.motion",
				"actions": []any{map[string]any{"device": "light-1", "command": "on"}},
			},
		},
		"automation": []any{
			map[string]any{"id": "legacy-automation"},
		},
		"state_store": map[string]any{
			"clips": map[string]any{
				"clip-1": map[string]any{
					"CameraID":  "camera-1",
					"CreatedAt": "0001-01-01T00:00:00Z",
				},
			},
			"presence": map[string]any{
				"alexis": map[string]any{
					"ResidentID": "alexis",
					"LastSeen":   time.Time{},
				},
			},
			"identities": map[string]any{
				"alexis": map[string]any{
					"LastNodeID": "entry",
					"LastSeen":   "0001-01-01T00:00:00Z",
				},
			},
		},
		"metrics": map[string]any{
			"events_processed": float64(1),
			"state_store_size": float64(3),
		},
	}

	snapshot := PublicSnapshotFromCoreState(coreState)
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal public snapshot: %v", err)
	}

	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("unmarshal public snapshot: %v", err)
	}
	for _, key := range []string{"state_store", "device", "event", "automation"} {
		if _, ok := object[key]; ok {
			t.Fatalf("public snapshot exposes legacy/internal key %q in %s", key, string(data))
		}
	}
	for _, key := range []string{"devices", "events", "automations", "clips", "presence", "identities"} {
		if _, ok := object[key]; !ok {
			t.Fatalf("public snapshot missing key %q in %s", key, string(data))
		}
	}
	if got := snapshot.Devices[0]["last_seen"]; got != nil {
		t.Fatalf("zero last_seen should be nil, got %#v", got)
	}
	if got := snapshot.System["intrusion_time"]; got != nil {
		t.Fatalf("zero intrusion_time should be nil, got %#v", got)
	}
	if got := snapshot.System["last_state_time"]; got != nil {
		t.Fatalf("zero last_state_time should be nil, got %#v", got)
	}
	if _, ok := snapshot.Automations[0]["event"]; ok {
		t.Fatalf("automation event key should be normalized: %#v", snapshot.Automations[0])
	}
	if snapshot.Automations[0]["event_type"] != "vision.motion" {
		t.Fatalf("automation event_type mismatch: %#v", snapshot.Automations[0])
	}
	actions := snapshot.Automations[0]["actions"].([]any)
	action := actions[0].(map[string]any)
	if _, ok := action["device"]; ok {
		t.Fatalf("automation action device key should be normalized: %#v", action)
	}
	if action["device_id"] != "light-1" {
		t.Fatalf("automation action device_id mismatch: %#v", action)
	}
	if _, ok := snapshot.Metrics["state_store_size"]; ok {
		t.Fatalf("metric state_store_size should be normalized: %#v", snapshot.Metrics)
	}
	if snapshot.Metrics["state_size"] != float64(3) {
		t.Fatalf("metric state_size mismatch: %#v", snapshot.Metrics)
	}
}

func TestDocumentedEventTypesNormalize(t *testing.T) {
	documented := []string{
		EventVisionIdentity,
		EventVisionUnknown,
		EventVisionUncertain,
		EventVisionMotion,
		EventVisionWeapon,
		EventVisionFall,
		EventVisionTamper,
		EventDeviceOffline,
		EventSystemStateChanged,
		EventActionRequest,
		EventActionResult,
	}

	for _, eventType := range documented {
		if got := NormalizeEventType(eventType); got != eventType {
			t.Fatalf("NormalizeEventType(%q)=%q", eventType, got)
		}
	}
}

func assertJSONField(t *testing.T, data []byte, field string) {
	t.Helper()

	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	if _, ok := object[field]; !ok {
		t.Fatalf("missing JSON field %q in %s", field, string(data))
	}
}
