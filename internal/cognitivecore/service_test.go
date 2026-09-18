package cognitivecore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"synora/pkg/contract"
)

type serviceBus struct {
	sent     []contract.Message
	incoming chan contract.Message
}

func (b *serviceBus) Send(message contract.Message) error {
	b.sent = append(b.sent, message)
	return nil
}
func (b *serviceBus) SubscribeChannel(string) <-chan contract.Message { return b.incoming }

func TestServiceCommitsBeforeEmittingDiscoveryMessages(t *testing.T) {
	bus := &serviceBus{}
	core := &Core{Store: NewUniversalStore(), MLP: fakeMLP{output: MLPOutput{DangerLabel: "medium", Action: ActionIntent{Action: "notify", Capability: "notify", Topology: contract.VisionTopologyUnknown}}}, Gate: SafetyGate{DryRun: true}, Now: func() time.Time { return time.Unix(50, 0).UTC() }}
	service := &Service{Bus: bus, Core: core}
	if err := service.Handle(context.Background(), contract.Message{ID: "sensor-1", Type: "sensor.normal", Kind: contract.KindEvent, Source: "discovery", Timestamp: time.Unix(50, 0).UTC(), Payload: json.RawMessage(`{"movement":true}`)}); err != nil {
		t.Fatal(err)
	}
	if len(core.Store.Journal()) != 1 || len(bus.sent) != 2 {
		t.Fatalf("commit/dispatch order incomplete: journal=%d sent=%d", len(core.Store.Journal()), len(bus.sent))
	}
	if bus.sent[0].Type != "core.decision" || bus.sent[1].Type != "action.request" {
		t.Fatalf("unexpected service messages: %#v", bus.sent)
	}
	if string(bus.sent[1].Payload) == "" || string(bus.sent[1].Payload) == "null" {
		t.Fatal("empty action request")
	}
}

func TestEventFromMessageDoesNotInventPayloadFacts(t *testing.T) {
	event := EventFromMessage(contract.Message{ID: "m", Type: "sensor.normal", Source: "discovery", Payload: json.RawMessage(`{"movement":true}`)})
	if event.ID != "m" || event.Payload["movement"] != true {
		t.Fatalf("unexpected event: %#v", event)
	}
}
