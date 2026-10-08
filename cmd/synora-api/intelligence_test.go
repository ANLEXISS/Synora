package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"synora/internal/security"
)

func TestSanitizeIntelligenceTraceKeepsOnlyBoundedRedactedFields(t *testing.T) {
	raw := map[string]any{
		"schema_version": "synora.mlp-trace/v1", "inference_id": "evt-1", "model_version": "cognitive-v1:abc", "redacted": true,
		"duration_ms": 1.2, "proposed_output": "notify", "weights": []any{1, 2}, "embedding": []any{1, 2},
		"topology":    map[string]any{"schema_version": "synora.mlp-trace/v1", "model_version": "cognitive-v1:abc", "heads": []any{map[string]any{"name": "danger", "layers": []any{map[string]any{"id": "danger.layer-1", "input_size": 86, "output_size": 32, "activation": "relu", "weights": []any{1}}}}}},
		"activations": []any{map[string]any{"layer_id": "danger.layer-1", "count": 32, "active_nodes": []any{map[string]any{"node_id": "danger.layer-1.node-1", "activation": .8}}}},
	}
	trace := sanitizeIntelligenceTrace(raw)
	if trace == nil || trace["redacted"] != true {
		t.Fatalf("trace rejected: %#v", trace)
	}
	body, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(body))
	for _, forbidden := range []string{"\"weights\"", "\"embedding\""} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("forbidden field leaked: %s", text)
		}
	}
}

func TestWebSocketRejectsUnconfiguredOrigin(t *testing.T) {
	cfg := &security.Config{AllowedOrigins: []string{"https://synora.example"}}
	hub := newWebSocketHub(cfg)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	request.Header.Set("Origin", "https://attacker.example")
	hub.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unexpected status for unconfigured origin: %d", recorder.Code)
	}
}

func TestSanitizeIntelligenceTraceIsBounded(t *testing.T) {
	nodes := make([]any, 20)
	paths := make([]any, 100)
	for i := range nodes {
		nodes[i] = map[string]any{"node_id": "layer.node", "activation": float64(i)}
	}
	for i := range paths {
		paths[i] = map[string]any{"from": "a", "to": "b", "strength": float64(i)}
	}
	raw := map[string]any{
		"schema_version": "synora.mlp-trace/v1", "inference_id": "evt-1", "model_version": "v1", "redacted": true,
		"topology":     map[string]any{"heads": []any{map[string]any{"name": "danger", "layers": []any{map[string]any{"id": "layer", "input_size": 86, "output_size": 32, "activation": "relu"}}}}},
		"activations":  []any{map[string]any{"layer_id": "layer", "active_nodes": nodes}},
		"active_paths": paths,
	}
	trace := sanitizeIntelligenceTraceRuntime(raw, "active_dry_run", "available")
	if trace == nil || trace["live"] != false {
		t.Fatalf("dry-run trace was not marked non-live: %#v", trace)
	}
	activations := trace["activations"].([]any)
	if len(activations) != 1 || len(activations[0].(map[string]any)["active_nodes"].([]any)) != maxActiveNodesPerLayer {
		t.Fatalf("active node bound failed: %#v", trace)
	}
	if len(trace["active_paths"].([]any)) != maxActivePaths {
		t.Fatalf("active path bound failed: %#v", trace)
	}
}

func TestSanitizeIntelligenceTraceRejectsUnredactedTrace(t *testing.T) {
	if got := sanitizeIntelligenceTrace(map[string]any{"inference_id": "evt-1", "model_version": "v1", "redacted": false}); got != nil {
		t.Fatalf("unredacted trace accepted: %#v", got)
	}
}

func TestSanitizeIntelligenceTraceKeepsTestHarnessAuditMarker(t *testing.T) {
	trace := sanitizeIntelligenceTraceRuntime(map[string]any{
		"schema_version": "synora.mlp-trace/v1", "inference_id": "test-1", "model_version": "v1", "redacted": true,
		"test": true, "provenance": "test-harness",
	}, "active_dry_run", "available")
	if trace == nil || trace["test"] != true || trace["provenance"] != "test-harness" {
		t.Fatalf("test audit marker was lost: %#v", trace)
	}
	if trace["live"] != false {
		t.Fatalf("test dry-run trace was marked live: %#v", trace)
	}
}

func TestIntelligenceDecisionCreatesBoundedRecentEvent(t *testing.T) {
	hub := newWebSocketHub(&security.Config{AllowedOrigins: []string{"https://synora.example"}})
	payload, err := json.Marshal(map[string]any{"decision": map[string]any{
		"mode": "active_dry_run", "status": "available",
		"trace": map[string]any{
			"schema_version": "synora.mlp-trace/v1", "inference_id": "evt-1", "model_version": "v1", "redacted": true,
			"proposed_output": "notify", "embedding": []any{1, 2}, "topology": map[string]any{"heads": []any{}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 6, 12, 0, 0, 123000000, time.UTC)
	hub.handleIntelligenceDecisionAt(payload, when)
	events := hub.recentEventsSnapshot()
	if len(events) != 1 {
		t.Fatalf("recent event count = %d", len(events))
	}
	event := events[0]
	if event["schema_version"] != "synora.recent-event/v1" || event["event_type"] != "inference" || event["timestamp"] != "2026-10-06T12:00:00.123Z" {
		t.Fatalf("unexpected event identity: %#v", event)
	}
	if event["live"] != false || event["runtime_mode"] != "active_dry_run" || event["proposed_output"] != "notify" {
		t.Fatalf("event status projection failed: %#v", event)
	}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(body)), "embedding") || strings.Contains(strings.ToLower(string(body)), "topology") {
		t.Fatalf("event leaked trace-only fields: %s", body)
	}
}

func TestIntelligenceDecisionWithoutRedactedTraceCreatesNoRecentEvent(t *testing.T) {
	hub := newWebSocketHub(&security.Config{AllowedOrigins: []string{"https://synora.example"}})
	hub.handleIntelligenceDecision([]byte(`{"decision":{"trace":{"schema_version":"synora.mlp-trace/v1","inference_id":"evt-1","model_version":"v1","redacted":false}}}`))
	if events := hub.recentEventsSnapshot(); len(events) != 0 {
		t.Fatalf("unredacted decision created events: %#v", events)
	}
}

func TestSanitizePilotStateKeepsOnlyAggregateEvidence(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"schema_version": "core-snapshot/v1", "revision": 7,
		"snapshot": map[string]any{
			"captured_at": "2026-10-06T12:00:00.123Z", "topology": "protected_interior",
			"security":       map[string]any{"armed": true, "degraded": false, "known": true, "identity": "resident-1"},
			"presence":       map[string]any{"human_present": true, "track_count": 1, "track_confirmed": true},
			"sensors":        map[string]any{"movement": true, "confidence": .91},
			"episode":        map[string]any{"phase": "confirmed", "segment_count": 2},
			"action_results": []any{map[string]any{"status": "dry_run", "request_id": "secret-request"}},
			"embedding":      []any{1, 2}, "media": "local://private",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := sanitizePilotState(payload, "core.snapshot")
	if state == nil || state["revision"] != uint64(7) || state["topology"] != "protected_interior" {
		t.Fatalf("state was not projected: %#v", state)
	}
	presence := state["presence"].(map[string]any)
	if presence["human_present"] != true || presence["track_count"] != float64(1) {
		t.Fatalf("presence projection failed: %#v", presence)
	}
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(body))
	for _, forbidden := range []string{"identity", "embedding", "media", "request_id", "secret-request"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("forbidden state field leaked: %s", text)
		}
	}
}

func TestSanitizePilotStateIncludesOnlyValidatedVisionEvidenceV1(t *testing.T) {
	fixture, err := os.ReadFile("../../pkg/contract/testdata/v1/vision-evidence-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var evidence any
	if err := json.Unmarshal(fixture, &evidence); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": "core-snapshot/v3", "revision": 9,
		"snapshot": map[string]any{
			"captured_at": "2026-10-06T12:00:00Z", "base_v2": map[string]any{"topology": "protected_interior"},
			"vision":          map[string]any{"posture": "ground", "fall_state": "candidate", "risk_status": "confirmed"},
			"vision_evidence": evidence, "media_path": "/private/clip.mp4", "local_track_id": "track-secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := sanitizePilotState(payload, "core.snapshot.v3")
	if state == nil || state["vision_evidence"] == nil {
		t.Fatalf("validated aggregate contract missing: %#v", state)
	}
	if _, ok := state["vision"]; ok {
		t.Fatalf("legacy parallel Vision object was projected: %#v", state["vision"])
	}
	body, _ := json.Marshal(state)
	for _, forbidden := range []string{"media_path", "local_track_id", "private/clip", "bbox", "keypoints", "embedding"} {
		if strings.Contains(strings.ToLower(string(body)), forbidden) {
			t.Fatalf("raw field leaked: %s", body)
		}
	}
	var invalid map[string]any
	_ = json.Unmarshal(fixture, &invalid)
	invalid["bbox"] = []int{1, 2, 3, 4}
	badPayload, _ := json.Marshal(map[string]any{"schema_version": "core-snapshot/v3", "revision": 10, "snapshot": map[string]any{"vision_evidence": invalid}})
	badState := sanitizePilotState(badPayload, "core.snapshot.v3")
	if badState == nil {
		t.Fatal("invalid state envelope should still be safely projected")
	}
	if badState["vision_evidence"] != nil {
		t.Fatal("invalid Vision Evidence V1 was exposed")
	}
}

func TestPilotStateReturnsUnknownUntilCoreSnapshotObserved(t *testing.T) {
	hub := newWebSocketHub(&security.Config{AllowedOrigins: []string{"https://synora.example"}})
	recorder := httptest.NewRecorder()
	handlePilotState(hub)(recorder, httptest.NewRequest(http.MethodGet, "/api/system/state", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected empty state status: %d", recorder.Code)
	}
}
