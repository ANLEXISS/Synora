package cognitivecore

import (
	"context"
	"encoding/json"
	"strings"
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
	if len(core.Store.Journal()) != 1 || len(bus.sent) != 4 {
		t.Fatalf("commit/dispatch order incomplete: journal=%d sent=%d", len(core.Store.Journal()), len(bus.sent))
	}
	if bus.sent[0].Type != "core.decision" || bus.sent[1].Type != "core.decision" || bus.sent[2].Type != "core.snapshot" || bus.sent[3].Type != "action.request" {
		t.Fatalf("unexpected service messages: %#v", bus.sent)
	}
	if bus.sent[1].Target != "api" {
		t.Fatalf("decision trace was not targeted to API: %#v", bus.sent[1])
	}
	if string(bus.sent[3].Payload) == "" || string(bus.sent[3].Payload) == "null" {
		t.Fatal("empty action request")
	}
}

func TestEventFromMessageDoesNotInventPayloadFacts(t *testing.T) {
	event := EventFromMessage(contract.Message{ID: "m", Type: "sensor.normal", Source: "discovery", Payload: json.RawMessage(`{"movement":true}`)})
	if event.ID != "m" || event.Payload["movement"] != true {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestServiceSystemStateResetErasesDurableStateAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	core := durableTestCore(store)
	if _, err := core.Process(context.Background(), contract.Event{
		ID: "before-reset", Type: "sensor.motion", Source: "discovery",
		Timestamp: time.Unix(100, 0).UTC(), Payload: map[string]any{"movement": true},
	}); err != nil {
		t.Fatal(err)
	}
	bus := &serviceBus{}
	service := &Service{Bus: bus, Core: core, Name: "core", Now: func() time.Time { return time.Unix(101, 0).UTC() }}
	payload, err := json.Marshal(contract.SystemStateResetRequest{TargetState: "empty", Reason: "operator requested erasure", CreatedBy: "admin-1"})
	if err != nil {
		t.Fatal(err)
	}
	request := contract.Message{ID: "reset-1", Type: contract.RPCSystemResetState, Kind: contract.KindRPC, Source: "api", Target: "core", Payload: payload}
	if err := service.Handle(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	erasedRevision := store.Revision()
	if len(store.Journal()) != 0 || erasedRevision == 0 {
		t.Fatalf("reset left durable state: journal=%d revision=%d", len(store.Journal()), store.Revision())
	}
	if len(bus.sent) != 1 {
		t.Fatalf("reset response count=%d", len(bus.sent))
	}
	var result contract.SystemStateResetResult
	if err := json.Unmarshal(bus.sent[0].Payload, &result); err != nil || result.Status != "erased" {
		t.Fatalf("unexpected reset response: %#v err=%v", bus.sent[0], err)
	}
	if err := service.Handle(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.Journal()) != 0 || restarted.Revision() != erasedRevision {
		t.Fatalf("erased state reappeared after restart: journal=%d revision=%d", len(restarted.Journal()), restarted.Revision())
	}
}

func TestServiceSystemStateResetRejectsInvalidEnvelopeWithoutErasing(t *testing.T) {
	store := NewUniversalStore()
	if _, err := durableTestCore(store).Process(context.Background(), contract.Event{ID: "keep", Type: "sensor.motion", Source: "discovery"}); err != nil {
		t.Fatal(err)
	}
	bus := &serviceBus{}
	service := &Service{Bus: bus, Core: durableTestCore(store), Name: "core"}
	payload, _ := json.Marshal(contract.SystemStateResetRequest{TargetState: "everything", Reason: "too broad", CreatedBy: "admin-1"})
	if err := service.Handle(context.Background(), contract.Message{ID: "bad-reset", Type: contract.RPCSystemResetState, Kind: contract.KindRPC, Source: "api", Target: "core", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if len(store.Journal()) != 1 || len(bus.sent) != 1 || !strings.Contains(string(bus.sent[0].Payload), `"status":"error"`) {
		t.Fatalf("invalid reset changed state or response: journal=%d sent=%v", len(store.Journal()), bus.sent)
	}
}
