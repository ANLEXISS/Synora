package cognitivecore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"synora/pkg/contract"
)

func loadedTestCore(t *testing.T) (*Core, *UniversalStore) {
	t.Helper()
	bundle, err := LoadCPUBundle("../../build/cognitive-mlp-v1")
	if err != nil {
		t.Skipf("V1 bundle not available in this checkout: %v", err)
	}
	store := NewUniversalStore()
	return &Core{Store: store, MLP: bundle, Gate: SafetyGate{DryRun: true}, Now: func() time.Time { return time.Unix(600, 0).UTC() }}, store
}

func testHarnessEvent(eventType string, inference bool) contract.Event {
	return contract.Event{ID: "test-event-1", Type: eventType, Source: "api", Timestamp: time.Unix(600, 0).UTC(), Payload: map[string]any{
		"provenance": "test-harness", "test": true, "test_inference": inference,
		"topology": contract.VisionTopologyProtectedInterior, "human_present": true, "track_count": 1, "track_confirmed": true,
		"episode_phase": "confirmed", "priority": contract.VisionPriorityP1, "real_detection": true, "observation_count": 1,
	}}
}

func TestProcessTestUsesRealBundleWithoutPersistenceOrAction(t *testing.T) {
	core, store := loadedTestCore(t)
	result, err := core.ProcessTest(context.Background(), testHarnessEvent(contract.EventVisionSegmentReadyV1, true))
	if err != nil {
		t.Fatal(err)
	}
	if result.Commit.Decision.Trace == nil || result.Commit.Decision.Trace.SchemaVersion != MLPTraceSchemaVersion {
		t.Fatalf("missing MLP trace: %#v", result.Commit.Decision)
	}
	if !result.Commit.Decision.Trace.Redacted || !result.Commit.Decision.Trace.Test || result.Commit.Decision.Trace.Provenance != "test-harness" {
		t.Fatalf("test provenance/redaction missing: %#v", result.Commit.Decision.Trace)
	}
	if result.Result.Action != nil || len(store.Journal()) != 0 || store.Revision() != 1 {
		t.Fatalf("test inference touched business state or action outbox: result=%#v journal=%d revision=%d", result.Result, len(store.Journal()), store.Revision())
	}
}

func TestProcessTestWithoutInferenceReturnsNoCalculation(t *testing.T) {
	core, store := loadedTestCore(t)
	event := testHarnessEvent(contract.EventWebCommand, false)
	result, err := core.ProcessTest(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if result.Commit.Decision.Status != "not_requested" || result.Commit.Decision.Trace != nil || len(store.Journal()) != 0 {
		t.Fatalf("non-inference scenario produced a calculation or persistence: %#v", result.Commit)
	}
}

func TestEveryInferenceCatalogCaseProducesBoundedRedactedTrace(t *testing.T) {
	for _, catalogCase := range contract.TestInferenceCatalog() {
		if !catalogCase.TriggersInference {
			continue
		}
		t.Run(catalogCase.ID, func(t *testing.T) {
			core, _ := loadedTestCore(t)
			payload := map[string]any{"provenance": "test-harness", "test": true, "test_inference": true}
			for key, value := range catalogCase.Payload {
				payload[key] = value
			}
			result, err := core.ProcessTest(context.Background(), contract.Event{ID: "catalog-" + catalogCase.ID, Type: catalogCase.EventType, Source: "api", Timestamp: time.Unix(600, 0).UTC(), Payload: payload})
			if err != nil {
				t.Fatal(err)
			}
			trace := result.Commit.Decision.Trace
			if trace == nil || trace.SchemaVersion != MLPTraceSchemaVersion || !trace.Redacted || len(trace.ActivePaths) > 64 {
				t.Fatalf("catalog case did not produce a bounded redacted trace: %#v", trace)
			}
			for _, activation := range trace.Activations {
				if len(activation.ActiveNodes) > 5 {
					t.Fatalf("catalog case exceeded active node bound: %#v", activation)
				}
			}
		})
	}
}

func TestServiceTestEnvelopePublishesOnlyRedactedDecisionToAPI(t *testing.T) {
	core, store := loadedTestCore(t)
	bus := &serviceBus{}
	service := &Service{Bus: bus, Core: core, Name: "core"}
	payload, err := json.Marshal(map[string]any{
		"event_type": contract.EventVisionSegmentReadyV1, "provenance": "test-harness", "test": true, "test_inference": true,
		"topology": contract.VisionTopologyProtectedInterior, "human_present": true, "track_count": 1, "track_confirmed": true,
		"priority": contract.VisionPriorityP1, "real_detection": true, "observation_count": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Handle(context.Background(), contract.Message{ID: "test-envelope", Type: contract.EventValidationTestInference, Kind: contract.KindEvent, Source: "api", Target: "core", Timestamp: time.Unix(600, 0).UTC(), Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if len(bus.sent) != 1 || bus.sent[0].Type != "core.decision" || bus.sent[0].Target != "api" || len(store.Journal()) != 0 {
		t.Fatalf("test envelope crossed an unsafe boundary: sent=%#v journal=%d", bus.sent, len(store.Journal()))
	}
	if string(bus.sent[0].Payload) == "" || string(bus.sent[0].Payload) == "null" {
		t.Fatal("empty test decision payload")
	}
}

func TestServiceRejectsUncataloguedTestEventType(t *testing.T) {
	core, _ := loadedTestCore(t)
	service := &Service{Bus: &serviceBus{}, Core: core}
	payload := json.RawMessage(`{"event_type":"unknown.test.event","provenance":"test-harness","test":true,"test_inference":true}`)
	if err := service.Handle(context.Background(), contract.Message{ID: "unknown-test", Type: contract.EventValidationTestInference, Kind: contract.KindEvent, Source: "api", Target: "core", Timestamp: time.Unix(600, 0).UTC(), Payload: payload}); err == nil {
		t.Fatal("uncatalogued test event type was accepted")
	}
}
