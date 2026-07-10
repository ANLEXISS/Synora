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
