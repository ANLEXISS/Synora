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

func TestParserPreservesTransportIdentityAndCaptureTimestamp(t *testing.T) {
	capture := time.Date(2026, 8, 11, 10, 11, 12, 123000000, time.UTC)
	received := capture.Add(30 * time.Second)
	body, err := json.Marshal(map[string]any{
		"device_id": "cam_01", "resident_id": "resident-1", "timestamp": capture.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := (Parser{Now: func() time.Time { return received }}).Parse(contract.Message{
		ID: "clip-1:event:0:vision.identity", Type: contract.EventVisionIdentity, Source: "discovery",
		Timestamp: received, Payload: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != "clip-1:event:0:vision.identity" || !event.Timestamp.Equal(capture) {
		t.Fatalf("transport identity/capture timestamp lost: %#v", event)
	}
	// ReceivedAt is deliberately generated at the ingest boundary; it must not
	// replace the capture timestamp.
	if !event.ReceivedAt.After(event.Timestamp) {
		t.Fatalf("received timestamp should be after capture timestamp: %#v", event)
	}
}

func TestQueueRejectsFutureAndTooOldEventsWithDeterministicClock(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	policy := TimestampPolicy{
		Now:           func() time.Time { return now },
		MaxFutureSkew: 5 * time.Minute,
		MaxPastAge:    10 * time.Minute,
	}
	high := make(chan *contract.Event, 2)
	normal := make(chan *contract.Event, 2)
	queue := &Queue{Parser: Parser{Now: func() time.Time { return now }}, Timestamp: policy, High: high, Normal: normal}

	for name, timestamp := range map[string]time.Time{
		"future": now.Add(6 * time.Minute),
		"old":    now.Add(-11 * time.Minute),
	} {
		_, accepted := queue.Ingest(contract.Message{
			ID: "event-" + name, Type: contract.EventVisionMotion, Kind: contract.KindEvent,
			Source: "vision", Timestamp: timestamp,
		})
		if accepted {
			t.Fatalf("%s event should be rejected", name)
		}
	}
	if len(normal) != 0 || len(high) != 0 {
		t.Fatal("timestamp-rejected events must not reach a processing queue")
	}

	_, accepted := queue.Ingest(contract.Message{
		ID: "event-current", Type: contract.EventVisionMotion, Kind: contract.KindEvent,
		Source: "vision", Timestamp: now,
	})
	if !accepted {
		t.Fatal("current event should be accepted")
	}
}

func TestQueueAllowsExplicitlyHistoricalSimulation(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	queue := &Queue{
		Parser:    Parser{Now: func() time.Time { return now }},
		Timestamp: TimestampPolicy{Now: func() time.Time { return now }, MaxPastAge: time.Minute},
		Normal:    make(chan *contract.Event, 1),
	}
	_, accepted := queue.Ingest(contract.Message{
		ID: "historical-simulation", Type: contract.EventVisionMotion, Kind: contract.KindEvent,
		Source: "lab", SourceType: contract.SourceSimulator, Timestamp: now.Add(-24 * time.Hour),
	})
	if !accepted {
		t.Fatal("explicitly simulated historical event should be accepted")
	}
}
