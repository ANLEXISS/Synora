package event

import (
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestRateControllerDedupesRepeatedProductionEvent(t *testing.T) {
	controller := NewRateController(2*time.Second, 750*time.Millisecond)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	first := &contract.Event{
		Type:      contract.EventVisionUnknown,
		Source:    "vision",
		Timestamp: now,
		DeviceID:  "cam_01",
		NodeID:    "entry",
		Identity:  "unknown",
		Priority:  contract.PriorityNormal,
		GroupKey:  "vision.unknown|vision|cam_01|entry|unknown",
		Payload:   map[string]any{},
	}
	second := *first
	second.Timestamp = now.Add(time.Second)
	second.Payload = map[string]any{}

	if !controller.Accept(first) {
		t.Fatal("first production event should be accepted")
	}
	if controller.Accept(&second) {
		t.Fatal("second identical production event should be deduped")
	}
}

func TestRateControllerAcceptsDistinctSimulatedSteps(t *testing.T) {
	controller := NewRateController(2*time.Second, 750*time.Millisecond)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	first := simulatedUnknownEvent(now, "run-1:unknown_first")
	second := simulatedUnknownEvent(now.Add(time.Second), "run-1:unknown_confirmed")

	if !controller.Accept(first) {
		t.Fatal("first simulated event should be accepted")
	}
	if !controller.Accept(second) {
		t.Fatal("second simulated event with distinct instance should be accepted")
	}
}

func TestRateControllerLeavesExplicitIDsToCoreIdempotenceGate(t *testing.T) {
	controller := NewRateController(2*time.Second, 750*time.Millisecond)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	first := &contract.Event{ID: "same-id", Type: contract.EventVisionUnknown, Source: "vision", Timestamp: now, Priority: contract.PriorityCritical, GroupKey: "first"}
	second := *first
	second.Payload = map[string]any{"confidence": .1}
	second.Timestamp = now.Add(time.Second)
	if !controller.Accept(first) || !controller.Accept(&second) {
		t.Fatal("explicitly identified events must reach Core for idempotence/collision validation")
	}
}

func TestRateControllerEvictsOldStateAtBoundedCapacity(t *testing.T) {
	controller := NewRateControllerWithLimit(time.Minute, time.Minute, 2)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	for _, source := range []string{"camera-a", "camera-b", "camera-c"} {
		if !controller.Accept(&contract.Event{
			Type:      contract.EventVisionMotion,
			Source:    source,
			Timestamp: now,
			DeviceID:  source,
			Priority:  contract.PriorityNormal,
			Payload:   map[string]any{},
		}) {
			t.Fatalf("event from %s was rejected", source)
		}
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if len(controller.fingerprints) > 2 || len(controller.groups) > 2 {
		t.Fatalf("rate controller state exceeded bound: fingerprints=%d groups=%d", len(controller.fingerprints), len(controller.groups))
	}
}

func simulatedUnknownEvent(at time.Time, instanceID string) *contract.Event {
	return &contract.Event{
		Type:      contract.EventVisionUnknown,
		Source:    "lab",
		Timestamp: at,
		DeviceID:  "cam_01",
		NodeID:    "entry",
		Identity:  "unknown",
		Priority:  contract.PriorityNormal,
		GroupKey:  "vision.unknown|lab|cam_01|entry|unknown|" + instanceID,
		Payload: map[string]any{
			"metadata": map[string]any{
				"simulated":         true,
				"event_instance_id": instanceID,
			},
		},
	}
}
