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
	if len(core.Store.Journal()) != 1 || len(bus.sent) != 5 {
		t.Fatalf("commit/dispatch order incomplete: journal=%d sent=%d", len(core.Store.Journal()), len(bus.sent))
	}
	if bus.sent[0].Type != "core.decision" || bus.sent[1].Type != "core.decision" || bus.sent[2].Type != "core.snapshot" || bus.sent[3].Type != "core.snapshot" || bus.sent[4].Type != "action.request" {
		t.Fatalf("unexpected service messages: %#v", bus.sent)
	}
	if bus.sent[1].Target != "api" {
		t.Fatalf("decision trace was not targeted to API: %#v", bus.sent[1])
	}
	if bus.sent[3].Target != "api" {
		t.Fatalf("snapshot state was not targeted to API: %#v", bus.sent[3])
	}
	if string(bus.sent[4].Payload) == "" || string(bus.sent[4].Payload) == "null" {
		t.Fatal("empty action request")
	}
}

func TestEventFromMessageDoesNotInventPayloadFacts(t *testing.T) {
	event := EventFromMessage(contract.Message{ID: "m", Type: "sensor.normal", Source: "discovery", Payload: json.RawMessage(`{"movement":true}`)})
	if event.ID != "m" || event.Payload["movement"] != true {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestResidentGalleryRPCIsCoreOwnedRedactedAndIdempotent(t *testing.T) {
	now := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	store := NewUniversalStore()
	bus := &serviceBus{}
	service := &Service{Bus: bus, Core: &Core{Store: store, Now: func() time.Time { return now }}, Name: "core"}
	ref := "res_0123456789abcdef0123456789abcdef"
	request := ResidentGalleryRPCRequest{Operation: "create", ResidentRef: ref, IdempotencyKey: "resident-create-0001"}
	body, _ := json.Marshal(request)
	message := contract.Message{ID: "rpc-create", Type: RPCResidentGallery, Kind: contract.KindRPC, Source: "discovery", Target: "core", Payload: body}
	if err := service.Handle(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(bus.sent) != 1 || bus.sent[0].Kind != contract.KindRPC || bus.sent[0].Target != "discovery" || store.Revision() != 1 {
		t.Fatalf("Core did not persist and respond to resident RPC: sent=%+v rev=%d", bus.sent, store.Revision())
	}
	if strings.Contains(string(bus.sent[0].Payload), "name") || strings.Contains(string(bus.sent[0].Payload), "embedding") || strings.Contains(string(bus.sent[0].Payload), "path") {
		t.Fatalf("resident RPC response not redacted: %s", bus.sent[0].Payload)
	}
	bus.sent = nil
	message.ID = "rpc-retry"
	if err := service.Handle(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if len(bus.sent) != 1 || json.Unmarshal(bus.sent[0].Payload, &response) != nil || response["status"] != "ok" {
		t.Fatalf("idempotent RPC retry failed: %+v", bus.sent)
	}
	result := response["result"].(map[string]any)
	if result["duplicate"] != true || store.Revision() != 1 {
		t.Fatalf("retry created another resident: result=%v revision=%d", result, store.Revision())
	}
}

func TestResidentGalleryRPCRejectsNonDiscoveryAndInvalidRequestsWithoutDetails(t *testing.T) {
	bus := &serviceBus{}
	store := NewUniversalStore()
	service := &Service{Bus: bus, Core: &Core{Store: store}, Name: "core"}
	for _, source := range []string{"api", "unknown"} {
		message := contract.Message{ID: "bad-source", Type: RPCResidentGallery, Kind: contract.KindRPC, Source: source, Target: "core", Payload: json.RawMessage(`{"operation":"create"}`)}
		if err := service.Handle(context.Background(), message); err != nil {
			t.Fatal(err)
		}
		if len(bus.sent) == 0 || strings.Contains(string(bus.sent[len(bus.sent)-1].Payload), "scope") {
			t.Fatal("RPC rejection was missing or overly descriptive")
		}
	}
	unknownField := contract.Message{ID: "raw-field", Type: RPCResidentGallery, Kind: contract.KindRPC, Source: "discovery", Target: "core", Payload: json.RawMessage(`{"operation":"create","resident_ref":"res_0123456789abcdef0123456789abcdef","idempotency_key":"resident-create-9999","name":"private"}`)}
	if err := service.Handle(context.Background(), unknownField); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.ResidentGallery("res_0123456789abcdef0123456789abcdef"); exists {
		t.Fatal("Core stored a resident RPC containing an unknown identity field")
	}
}

func TestServicesStructurallyRejectLegacyVisionWithoutStoreMutation(t *testing.T) {
	legacyTypes := []string{
		contract.EventVisionSegmentReadyV1,
		contract.EventVisionClipObservationV1,
		contract.EventVisionClipSummaryV1,
		contract.EventVisionEnrichmentV3,
		contract.EventClipReady,
		contract.EventClipFailed,
	}
	for _, eventType := range legacyTypes {
		t.Run(eventType+"/v1", func(t *testing.T) {
			store := NewUniversalStore()
			before := store.Revision()
			bus := &serviceBus{}
			service := &Service{Bus: bus, Core: &Core{Store: store}, Name: "core"}
			message := contract.Message{ID: "legacy", Type: eventType, Kind: contract.KindEvent, Source: "discovery", Target: "core"}
			if err := service.Handle(context.Background(), message); err != nil {
				t.Fatal(err)
			}
			if store.Revision() != before || len(bus.sent) != 1 || bus.sent[0].Type != "core.vision_rejected" {
				t.Fatalf("legacy input mutated Store or lacked structured rejection: revision=%d sent=%+v", store.Revision(), bus.sent)
			}
		})
		t.Run(eventType+"/v3", func(t *testing.T) {
			store := NewUniversalStore()
			before := store.Revision()
			bus := &serviceBus{}
			service := &ServiceV3{Bus: bus, Core: &CoreV3{Store: store, ActiveDryRun: true}, Name: "core"}
			message := contract.Message{ID: "legacy", Type: eventType, Kind: contract.KindEvent, Source: "discovery", Target: "core"}
			if err := service.Handle(context.Background(), message); err != nil {
				t.Fatal(err)
			}
			if store.Revision() != before || len(bus.sent) != 1 || bus.sent[0].Type != "core.vision_rejected" || !strings.Contains(string(bus.sent[0].Payload), `"status":"rejected"`) {
				t.Fatalf("legacy input mutated Store or lacked structured rejection: revision=%d sent=%+v", store.Revision(), bus.sent)
			}
		})
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
