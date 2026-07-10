package ingest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestParserIncludesSimulationInstanceInGroupKey(t *testing.T) {
	payload := map[string]any{
		"device_id":  "cam_01",
		"camera_id":  "cam_01",
		"node_id":    "zoneA.L0.entree",
		"identity":   "unknown",
		"confidence": 0.72,
		"metadata": map[string]any{
			"simulated":         true,
			"test_run_id":       "run-1",
			"scenario_step_id":  "unknown_confirmed",
			"event_instance_id": "run-1:unknown_confirmed",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	event, err := (Parser{}).Parse(contract.Message{
		Type:       contract.EventVisionUnknown,
		Kind:       contract.KindEvent,
		Source:     "lab",
		SourceType: contract.SourceSimulator,
		Target:     "core",
		Timestamp:  time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		Payload:    body,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if event.NodeID != "zoneA.L0.entree" {
		t.Fatalf("payload node_id should be preserved, got %#v", event)
	}
	if event.Payload["source_type"] != contract.SourceSimulator {
		t.Fatalf("source_type should be preserved in payload: %#v", event.Payload)
	}
	if !strings.HasSuffix(event.GroupKey, "|run-1:unknown_confirmed") {
		t.Fatalf("simulated group_key should include event_instance_id: %s", event.GroupKey)
	}
}

func TestParserProductionGroupKeyKeepsExistingShape(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"device_id": "cam_01",
		"node_id":   "entry",
		"identity":  "unknown",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	event, err := (Parser{}).Parse(contract.Message{
		Type:    contract.EventVisionUnknown,
		Kind:    contract.KindEvent,
		Source:  "vision",
		Target:  "core",
		Payload: body,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if event.GroupKey != "vision.unknown|vision|cam_01|entry|unknown" {
		t.Fatalf("production group_key changed unexpectedly: %s", event.GroupKey)
	}
}
