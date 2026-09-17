package cognitive

import (
	"context"
	"encoding/json"
	"testing"

	"synora/pkg/contract"
)

type testBus struct {
	messages []contract.Message
	incoming chan contract.Message
}

func (b *testBus) Send(message contract.Message) error {
	b.messages = append(b.messages, message)
	return nil
}

func (b *testBus) SubscribeChannel(string) <-chan contract.Message { return b.incoming }

func TestServicePublishesStatusAndAdvisoryOutput(t *testing.T) {
	bus := &testBus{incoming: make(chan contract.Message, 1)}
	service := &Service{Bus: bus, Scheduler: NewScheduler(DefaultRegistry()), Name: "cognitive"}
	if err := service.PublishStatus(); err != nil {
		t.Fatal(err)
	}
	encoded, catalog := validInputParts(t)
	payload, _ := json.Marshal(CognitiveInput{SchemaVersion: SchemaVersion, RequestID: "req", Task: Task{ID: "task", Kind: "observe", RequestedCapabilities: []string{CapabilityEventReasoning}}, EncodedState: encoded, ActionCatalog: catalog})
	if err := service.Handle(context.Background(), contract.Message{ID: "msg", Type: MessageTypeTask, Kind: contract.KindCommand, Source: "tester", Target: "cognitive", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if len(bus.messages) != 2 || bus.messages[0].Type != MessageTypeStatus || bus.messages[1].Type != MessageTypeOutput {
		t.Fatalf("unexpected messages: %+v", bus.messages)
	}
	var output CognitiveOutput
	if err := json.Unmarshal(bus.messages[1].Payload, &output); err != nil {
		t.Fatal(err)
	}
	if !output.AdvisoryOnly || len(output.ExecutableActions) != 0 || bus.messages[1].Target != "tester" {
		t.Fatalf("unsafe or misrouted output: %+v", output)
	}
}
