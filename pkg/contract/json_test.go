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
		TrackID:    "track-1",
		ClipID:     "clip-1",

		ValidationRequired: true,
		ValidationReason:   "low_confidence_identity",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	assertJSONField(t, data, "device_id")
	assertJSONField(t, data, "node_id")
	assertJSONField(t, data, "group_key")
	assertJSONField(t, data, "track_id")
	assertJSONField(t, data, "clip_id")
	assertJSONField(t, data, "validation_required")

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if decoded.DeviceID != event.DeviceID || decoded.NodeID != event.NodeID || decoded.GroupKey != event.GroupKey || decoded.ClipID != event.ClipID || !decoded.ValidationRequired {
		t.Fatalf("decoded event mismatch: %#v", decoded)
	}

	legacy := []byte(`{"ID":"evt-2","Type":"vision.motion","Source":"camera-2","DeviceID":"device-2","NodeID":"hall","GroupKey":"legacy","ClipID":"legacy-clip","ValidationRequired":true}`)
	if err := json.Unmarshal(legacy, &decoded); err != nil {
		t.Fatalf("unmarshal legacy event: %v", err)
	}
	if decoded.ID != "evt-2" || decoded.DeviceID != "device-2" || decoded.NodeID != "hall" || decoded.GroupKey != "legacy" || decoded.ClipID != "legacy-clip" || !decoded.ValidationRequired {
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
		ClipID:         "clip-1",
		TrackID:        "track-1",
		GroupKey:       "vision.unknown|camera-1",
		SequenceKey:    "resident:alexis",
		GraphUsed:      true,

		ValidationRequired: true,
		ValidationReason:   "rapid_novel_transition",
	}

	data, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	assertJSONField(t, data, "event_id")
	assertJSONField(t, data, "effective_score")
	assertJSONField(t, data, "node_id")
	assertJSONField(t, data, "clip_id")
	assertJSONField(t, data, "sequence_key")
	assertJSONField(t, data, "graph_used")
	assertJSONField(t, data, "validation_required")

	var decoded Decision
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal decision: %v", err)
	}
	if decoded.EventID != decision.EventID || decoded.EffectiveScore != decision.EffectiveScore || decoded.NodeID != decision.NodeID || decoded.ClipID != decision.ClipID || !decoded.GraphUsed || !decoded.ValidationRequired {
		t.Fatalf("decoded decision mismatch: %#v", decoded)
	}

	legacy := []byte(`{"ID":"dec-2","Type":"intrusion","EventID":"evt-2","EffectiveScore":0.42,"NodeID":"kitchen","ClipID":"legacy-clip","GraphUsed":true,"ValidationRequired":true}`)
	if err := json.Unmarshal(legacy, &decoded); err != nil {
		t.Fatalf("unmarshal legacy decision: %v", err)
	}
	if decoded.ID != "dec-2" || decoded.EventID != "evt-2" || decoded.EffectiveScore != 0.42 || decoded.NodeID != "kitchen" || decoded.ClipID != "legacy-clip" || !decoded.GraphUsed || !decoded.ValidationRequired {
		t.Fatalf("legacy decision mismatch: %#v", decoded)
	}
}

func TestValidationRequestJSONRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	resolvedAt := createdAt.Add(time.Minute)
	validation := ValidationRequest{
		ID:               "validation-1",
		DecisionID:       "dec-1",
		EventID:          "evt-1",
		SituationID:      "sit-1",
		Reason:           "rapid_novel_transition",
		Evidence:         []string{"event:evt-1"},
		ProposedIdentity: "alexis",
		NodeID:           "entry",
		ClipID:           "clip-1",
		Status:           ValidationStatusAccepted,
		CreatedAt:        createdAt,
		ResolvedAt:       &resolvedAt,
	}

	data, err := json.Marshal(validation)
	if err != nil {
		t.Fatalf("marshal validation request: %v", err)
	}
	for _, field := range []string{"decision_id", "event_id", "situation_id", "proposed_identity", "resolved_at"} {
		assertJSONField(t, data, field)
	}

	var decoded ValidationRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal validation request: %v", err)
	}
	if decoded.ID != validation.ID || decoded.DecisionID != "dec-1" || decoded.Status != ValidationStatusAccepted || decoded.ResolvedAt == nil {
		t.Fatalf("decoded validation mismatch: %#v", decoded)
	}
}

func TestValidationResolveRequestJSONRoundTrip(t *testing.T) {
	req := ValidationResolveRequest{
		ID:               "validation-1",
		Action:           ValidationActionAssignIdentity,
		ProposedIdentity: "camille",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal validation resolve request: %v", err)
	}
	assertJSONField(t, data, "proposed_identity")

	var decoded ValidationResolveRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal validation resolve request: %v", err)
	}
	if decoded.Action != ValidationActionAssignIdentity || decoded.ProposedIdentity != "camille" {
		t.Fatalf("decoded validation resolve mismatch: %#v", decoded)
	}
}

func TestDangerAssessmentJSONRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	assessment := DangerAssessment{
		ID:          "danger-1",
		EventID:     "evt-1",
		Level:       3,
		Score:       0.62,
		Category:    DangerCategorySecurity,
		Title:       "Unknown presence at entrance",
		Explanation: "An unknown subject was detected near an access-control area.",
		Reasons:     []string{"unknown_identity", "access_control_zone"},
		Evidence:    []string{"event:evt-1"},
		RecommendedSystemActions: []SystemActionRecommendation{{
			Type:      SystemActionCreateValidation,
			Priority:  PriorityHigh,
			Reason:    "unknown_at_access_point",
			Target:    "entry",
			DryRun:    true,
			Simulated: true,
		}},
		ValidationRequired: true,
		ValidationReason:   "unknown_at_access_point",
		CreatedAt:          createdAt,
		Simulated:          true,
	}

	data, err := json.Marshal(assessment)
	if err != nil {
		t.Fatalf("marshal danger assessment: %v", err)
	}
	for _, field := range []string{"event_id", "sequence_signature", "recommended_system_actions", "validation_required", "validation_reason", "created_at", "simulated"} {
		if field == "sequence_signature" {
			continue
		}
		assertJSONField(t, data, field)
	}

	var decoded DangerAssessment
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal danger assessment: %v", err)
	}
	if decoded.ID != assessment.ID || decoded.Level != 3 || decoded.Category != DangerCategorySecurity || !decoded.ValidationRequired || len(decoded.RecommendedSystemActions) != 1 || decoded.RecommendedSystemActions[0].Type != SystemActionCreateValidation {
		t.Fatalf("decoded danger assessment mismatch: %#v", decoded)
	}
}

func TestActionContractsJSONRoundTrip(t *testing.T) {
	request := ActionRequest{
		ID:             "act-1",
		Type:           "device.command",
		Version:        "v1",
		RequestID:      "req-1",
		CorrelationID:  "corr-1",
		Source:         "core",
		Target:         "light-1",
		CreatedAt:      time.Date(2026, 7, 4, 10, 14, 59, 0, time.UTC),
		IdempotencyKey: "key-1",
		Data:           map[string]any{"command": "set", "value": true},
		SourceEventID:  "evt-1",
		DecisionID:     "dec-1",
		TimeoutMs:      500,
		Retry:          1,
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
	assertJSONField(t, data, "source_event_id")
	assertJSONField(t, data, "decision_id")
	assertJSONField(t, data, "timeout_ms")
	assertJSONField(t, data, "created_at")

	var decoded ActionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal action request: %v", err)
	}
	if decoded.Action.Device != request.Action.Device ||
		decoded.IdempotencyKey != request.IdempotencyKey ||
		decoded.SourceEventID != "evt-1" ||
		decoded.DecisionID != "dec-1" ||
		decoded.TimeoutMs != 500 ||
		decoded.Retry != 1 {
		t.Fatalf("decoded action request mismatch: %#v", decoded)
	}

	startedAt := time.Date(2026, 7, 4, 10, 15, 1, 0, time.UTC)
	finishedAt := startedAt.Add(time.Second)
	result := ActionResult{
		ID:         "res-1",
		ActionID:   "act-1",
		RequestID:  "req-1",
		Status:     "success",
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Details:    map[string]any{"adapter": "fake"},
	}
	data, err = json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal action result: %v", err)
	}
	assertJSONField(t, data, "action_id")
	assertJSONField(t, data, "started_at")
	assertJSONField(t, data, "finished_at")

	var decodedResult ActionResult
	if err := json.Unmarshal(data, &decodedResult); err != nil {
		t.Fatalf("unmarshal action result: %v", err)
	}
	if decodedResult.ActionID != result.ActionID || decodedResult.Status != result.Status || !decodedResult.StartedAt.Equal(startedAt) {
		t.Fatalf("decoded action result mismatch: %#v", decodedResult)
	}
}

func TestActionRequestJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 15, 0, 0, time.UTC)
	request := ActionRequest{
		ID:             "act-1",
		AutomationID:   "auto-1",
		ActionID:       "action-1",
		Type:           "device.command",
		Version:        "v1",
		RequestID:      "req-1",
		CorrelationID:  "corr-1",
		Source:         "core",
		Target:         "light-1",
		Timestamp:      now,
		CreatedAt:      now,
		IdempotencyKey: "idem-1",
		SourceEventID:  "evt-1",
		DecisionID:     "dec-1",
		SituationID:    "sit-1",
		ClipID:         "clip-1",
		NodeID:         "entry",
		DeviceID:       "cam-1",
		TimeoutMs:      1000,
		RetryCount:     2,
		CooldownKey:    "auto-1:action-1",
		Metadata:       map[string]any{"origin": "automation"},
		Action: Action{
			Device:  "light-1",
			Command: "on",
			Value:   true,
		},
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal action request: %v", err)
	}
	assertJSONField(t, data, "idempotency_key")
	assertJSONField(t, data, "automation_id")
	assertJSONField(t, data, "action_id")
	assertJSONField(t, data, "source_event_id")
	assertJSONField(t, data, "decision_id")
	assertJSONField(t, data, "retry_count")
	assertJSONField(t, data, "cooldown_key")

	var decoded ActionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal action request: %v", err)
	}
	if decoded.ID != request.ID || decoded.AutomationID != "auto-1" || decoded.ActionID != "action-1" || decoded.Action.Device != "light-1" || !decoded.Timestamp.Equal(now) || decoded.TimeoutMs != 1000 || decoded.RetryCount != 2 || decoded.ClipID != "clip-1" {
		t.Fatalf("decoded action request mismatch: %#v", decoded)
	}
}

func TestActionResultJSONRoundTrip(t *testing.T) {
	startedAt := time.Date(2026, 7, 4, 10, 15, 1, 0, time.UTC)
	finishedAt := startedAt.Add(125 * time.Millisecond)
	result := ActionResult{
		ID:           "ares-1",
		RequestID:    "areq-1",
		AutomationID: "auto-1",
		ActionID:     "action-1",
		Type:         "device.command",
		Target:       "light-1",
		Status:       ActionStatusSuccess,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		DurationMs:   125,
		Attempts:     2,
		Data:         map[string]any{"adapter": "fake"},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal action result: %v", err)
	}
	for _, field := range []string{"request_id", "automation_id", "action_id", "duration_ms", "attempts", "data"} {
		assertJSONField(t, data, field)
	}

	var decoded ActionResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal action result: %v", err)
	}
	if decoded.RequestID != "areq-1" || decoded.AutomationID != "auto-1" || decoded.Attempts != 2 || decoded.DurationMs != 125 || decoded.Data["adapter"] != "fake" {
		t.Fatalf("decoded action result mismatch: %#v", decoded)
	}
}

func TestAutomationJSONRoundTripMultipleConditionsAndActions(t *testing.T) {
	rule := Automation{
		ID:             "auto-1",
		Name:           "Entry activity",
		Enabled:        true,
		Description:    "Turn on entry lights for high confidence motion.",
		Priority:       10,
		Trigger:        AutomationTrigger{EventType: EventVisionMotion, State: "active"},
		ConditionLogic: "all",
		Conditions: []Condition{
			{ID: "c1", Field: "node", Op: "==", Value: "entry"},
			{ID: "c2", Field: "score", Op: ">=", Value: 0.8, ValueType: "number"},
		},
		Actions: []AutomationAction{
			{ID: "a1", Type: "device.command", Target: "light-1", Data: map[string]any{"command": "on"}, TimeoutMs: 250, RetryCount: 1, Enabled: true, Order: 1},
			{ID: "a2", Type: "mqtt.publish", Target: "synora/events", Data: map[string]any{"payload": "motion"}, Enabled: true, Order: 2},
		},
		CooldownMs: 5000,
	}

	data, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal automation: %v", err)
	}
	for _, field := range []string{"condition_logic", "conditions", "actions", "cooldown_ms"} {
		assertJSONField(t, data, field)
	}

	var decoded Automation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal automation: %v", err)
	}
	if decoded.ID != "auto-1" || !decoded.Enabled || len(decoded.Conditions) != 2 || len(decoded.Actions) != 2 || decoded.Actions[0].RetryCount != 1 {
		t.Fatalf("decoded automation mismatch: %#v", decoded)
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
					"EventID":   "evt-clip",
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
			"validations": map[string]any{
				"validation-1": map[string]any{
					"DecisionID":       "dec-1",
					"EventID":          "evt-1",
					"SituationID":      "sit-1",
					"ProposedIdentity": "alexis",
					"Status":           ValidationStatusPending,
					"CreatedAt":        "0001-01-01T00:00:00Z",
				},
			},
			"action_results": map[string]any{
				"action-result-1": map[string]any{
					"RequestID":  "act-1",
					"ActionID":   "act-1",
					"Status":     "success",
					"StartedAt":  "0001-01-01T00:00:00Z",
					"FinishedAt": "0001-01-01T00:00:00Z",
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
	for _, key := range []string{"devices", "events", "automations", "clips", "presence", "identities", "validations", "action_results"} {
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
	if snapshot.Clips[0]["event_id"] != "evt-clip" {
		t.Fatalf("clip event_id should be exposed: %#v", snapshot.Clips[0])
	}
	if snapshot.Validations[0]["decision_id"] != "dec-1" || snapshot.Validations[0]["proposed_identity"] != "alexis" {
		t.Fatalf("validation should be exposed cleanly: %#v", snapshot.Validations[0])
	}
	if snapshot.ActionResults[0]["request_id"] != "act-1" || snapshot.ActionResults[0]["status"] != "success" {
		t.Fatalf("action result should be exposed cleanly: %#v", snapshot.ActionResults[0])
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

func TestPublicSnapshotJSONRoundTrip(t *testing.T) {
	snapshot := PublicSnapshot{
		System:        map[string]any{"last_state": "idle"},
		Devices:       []map[string]any{{"id": "cam_01", "last_seen": nil}},
		Residents:     []map[string]any{{"id": "alexis", "state": "present"}},
		Nodes:         []map[string]any{{"id": "entry"}},
		Events:        []map[string]any{{"id": "evt-1", "type": EventVisionIdentity}},
		Automations:   []map[string]any{{"id": "auto-1", "event_type": EventVisionIdentity}},
		Cameras:       []map[string]any{},
		Tracks:        []map[string]any{},
		Clusters:      []map[string]any{},
		Clips:         []map[string]any{},
		Presence:      []map[string]any{},
		Identities:    []map[string]any{},
		Validations:   []map[string]any{{"id": "validation-1", "status": ValidationStatusPending}},
		ActionResults: []map[string]any{{"id": "action-result-1", "status": "success"}},
		Metrics:       map[string]any{"state_size": float64(1)},
		CGE: map[string]any{
			"sequences":          []any{map[string]any{"signature": "vision.unknown > vision.motion", "count": float64(2)}},
			"danger_assessments": []any{map[string]any{"id": "danger-1", "level": float64(3), "category": DangerCategorySecurity}},
		},
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal public snapshot: %v", err)
	}
	for _, field := range []string{"devices", "events", "automations", "validations", "action_results", "metrics", "cge"} {
		assertJSONField(t, data, field)
	}

	var decoded PublicSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal public snapshot: %v", err)
	}
	if decoded.Devices[0]["id"] != "cam_01" || decoded.Events[0]["type"] != EventVisionIdentity || decoded.Validations[0]["status"] != ValidationStatusPending || decoded.ActionResults[0]["status"] != "success" || decoded.CGE["sequences"] == nil || decoded.CGE["danger_assessments"] == nil {
		t.Fatalf("decoded public snapshot mismatch: %#v", decoded)
	}
}

func TestPublicSnapshotProvidesNonNullSecurityContext(t *testing.T) {
	snapshot := PublicSnapshotFromCoreState(map[string]any{"system": map[string]any{"last_state": "idle"}})
	if snapshot.System["security_mode"] != string(SecurityModeHome) || snapshot.System["security_armed"] != false || snapshot.System["expected_occupancy"] != string(ExpectedOccupancyUnknown) {
		t.Fatalf("security context defaults=%#v", snapshot.System)
	}
	security, ok := snapshot.System["security"].(map[string]any)
	if !ok || security["mode"] != string(SecurityModeHome) || security["armed"] != false {
		t.Fatalf("security projection=%#v", snapshot.System["security"])
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
