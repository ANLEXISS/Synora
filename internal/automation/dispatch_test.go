package automation

import (
	"encoding/json"
	"testing"
	"time"

	"synora/pkg/contract"
)

type recordingSender struct {
	messages []contract.Message
}

func (s *recordingSender) Send(msg contract.Message) error {
	s.messages = append(s.messages, msg)
	return nil
}

func TestEventAutomationDispatchesActionRequest(t *testing.T) {
	engine := NewEngine(t.TempDir() + "/automations.yaml")
	err := engine.Add(Rule{
		ID:        "rule-1",
		EventType: contract.EventVisionUnknown,
		State:     "suspicious",
		Node:      "entry",
		MinScore:  0.5,
		Actions: []contract.Action{{
			Type:      "device.command",
			Device:    "siren-1",
			Command:   "on",
			Value:     true,
			TimeoutMs: 250,
			Retry:     1,
		}},
	})
	if err != nil {
		t.Fatalf("add rule: %v", err)
	}

	actions := engine.Evaluate(&contract.Event{
		Type:   contract.EventVisionUnknown,
		NodeID: "entry",
	}, &contract.Decision{
		State:          "suspicious",
		NodeID:         "entry",
		EffectiveScore: 0.8,
	})
	if len(actions) != 1 {
		t.Fatalf("expected one action, got %d", len(actions))
	}

	sender := &recordingSender{}
	dispatcher := Dispatcher{
		Bus:    sender,
		Source: "core",
		Target: "actions",
		Now:    func() time.Time { return time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC) },
		NewID:  func() string { return "msg-test" },
	}
	if err := dispatcher.Dispatch(actions[0], ActionContext{
		SourceEventID: "evt-1",
		DecisionID:    "dec-1",
	}); err != nil {
		t.Fatalf("dispatch action: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected one bus message, got %d", len(sender.messages))
	}

	msg := sender.messages[0]
	if msg.Type != contract.EventActionRequest || msg.Kind != contract.KindCommand || msg.Target != "actions" {
		t.Fatalf("unexpected action request message: %#v", msg)
	}
	var payload contract.ActionRequest
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal action payload: %v", err)
	}
	if payload.Type != "device.command" ||
		payload.Target != "siren-1" ||
		payload.Data["command"] != "on" ||
		payload.SourceEventID != "evt-1" ||
		payload.DecisionID != "dec-1" ||
		payload.TimeoutMs != 250 ||
		payload.Retry != 1 ||
		payload.Action.Device != "siren-1" ||
		payload.Action.Command != "on" ||
		payload.IdempotencyKey == "" {
		t.Fatalf("unexpected action payload: %#v", payload)
	}
}
