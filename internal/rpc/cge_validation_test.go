package rpc

import (
	"encoding/json"
	"testing"

	"synora/internal/event"
	"synora/internal/state"
	"synora/pkg/contract"
)

func TestCGEValidationEventKeepsControlledTrace(t *testing.T) {
	store := state.NewStore()
	chains := event.NewChainManager(event.DefaultChainConfig())
	var queued []*contract.Event
	server := NewServer(Config{
		State:  store,
		Chains: chains,
		IngestEvent: func(value *contract.Event) {
			queued = append(queued, value)
		},
	})

	result, err := server.Handler(contract.RPCCGEValidationEvent)(contract.Message{Payload: json.RawMessage(`{
		"event_type":"camera.tampered","device_id":"cam_03","node_id":"zoneA.L0.entree",
		"confidence":0.78,"danger_level_hint":"high","learn":false,"reason":"validation_tamper"
	}`)})
	if err != nil {
		t.Fatalf("inject validation event: %v", err)
	}
	if len(queued) != 1 {
		t.Fatalf("expected one queued event, got %d", len(queued))
	}
	if queued[0].Type != contract.EventVisionTamper || queued[0].Source != contract.SourceValidation {
		t.Fatalf("unexpected normalized event: %#v", queued[0])
	}
	metadata, _ := queued[0].Payload["metadata"].(map[string]any)
	if metadata["validation"] != true || metadata["learn"] != false || metadata["test_mode"] != contract.ValidationTestModeControlledReal {
		t.Fatalf("controlled validation metadata missing: %#v", metadata)
	}
	if queued[0].ID == "" || queued[0].Payload["event_id"] != queued[0].ID {
		t.Fatalf("event identity was not preserved: %#v", queued[0])
	}
	if response, ok := result.(map[string]any); !ok || response["status"] != "queued" {
		t.Fatalf("unexpected injection response: %#v", result)
	}
}

func TestCGEValidationHistoryDoesNotRequireChains(t *testing.T) {
	store := state.NewStore()
	server := NewServer(Config{State: store})
	result, err := server.Handler(contract.RPCCGEValidationHistory)(contract.Message{})
	if err != nil {
		t.Fatalf("list empty validation history: %v", err)
	}
	items, ok := result.([]contract.CGEValidationHistoryItem)
	if !ok || len(items) != 0 {
		t.Fatalf("unexpected empty validation history: %#v", result)
	}
}

func TestCGEValidationSequenceNormalizesAliasesAndSharesChainIdentity(t *testing.T) {
	store := state.NewStore()
	chains := event.NewChainManager(event.DefaultChainConfig())
	var queued []*contract.Event
	server := NewServer(Config{
		State:  store,
		Chains: chains,
		IngestEvent: func(value *contract.Event) {
			queued = append(queued, value)
			chains.Process(value, nil)
		},
	})

	result, err := server.Handler(contract.RPCCGEValidationSequence)(contract.Message{Payload: json.RawMessage(`{
		"learn":true,"reason":"sequence-test","events":[
			{"event_type":"vision.unknown","device_id":"cam_03","node_id":"zoneA.L0.entree","confidence":0.82,"danger_level_hint":"medium"},
			{"event_type":"motion.detected","device_id":"cam_03","node_id":"zoneA.L0.entree"},
			{"event_type":"weapon.detected","device_id":"cam_03","node_id":"zoneA.L0.salon","confidence":0.91,"danger_level_hint":"critical"}
		]}`)})
	if err != nil {
		t.Fatalf("queue validation sequence: %v", err)
	}
	if len(queued) != 3 {
		t.Fatalf("expected three queued events, got %d", len(queued))
	}
	response := result.(map[string]any)
	validationID := response["validation_id"].(string)
	if response["activation_id"] != validationID || response["sequence_key"] != validationID {
		t.Fatalf("sequence identity mismatch: %#v", response)
	}
	for index, value := range queued {
		if value.Type == "motion.detected" || value.Type == "weapon.detected" {
			t.Fatalf("alias was not normalized: %s", value.Type)
		}
		if value.ActivationID != validationID || value.SequenceKey != validationID || value.ClipIndex != index {
			t.Fatalf("event %d identity/index mismatch: %#v", index, value)
		}
		metadata := value.Payload["metadata"].(map[string]any)
		if metadata["validation"] != true || metadata["learn"] != true || metadata["test_mode"] != contract.ValidationTestModeControlledReal {
			t.Fatalf("event %d metadata mismatch: %#v", index, metadata)
		}
	}
	items := chains.List(event.ChainFilter{Status: "all"})
	if len(items) != 1 || items[0].EventsCount != 3 || items[0].ContextualEventsCount != 1 || items[0].ValidationID != validationID {
		t.Fatalf("expected one coherent chain, got %#v", items)
	}
}

func TestCGEValidationSequenceRejectsUnsupportedEventWithoutQueueing(t *testing.T) {
	store := state.NewStore()
	queued := 0
	server := NewServer(Config{State: store, IngestEvent: func(*contract.Event) { queued++ }})
	_, err := server.Handler(contract.RPCCGEValidationSequence)(contract.Message{Payload: json.RawMessage(`{
		"events":[{"event_type":"totally.unsupported"}]
	}`)})
	if contract.APIErrorCode(err) != contract.ErrorValidationFailed {
		t.Fatalf("error code=%s err=%v", contract.APIErrorCode(err), err)
	}
	if err == nil || err.Error() != `unsupported event_type "totally.unsupported" at events[0]` {
		t.Fatalf("unexpected error=%v", err)
	}
	if queued != 0 || len(store.ValidationEventsList()) != 0 {
		t.Fatalf("invalid sequence was partially queued: queued=%d history=%d", queued, len(store.ValidationEventsList()))
	}
	typed := err.(*contract.APIError)
	if typed.Details["event_index"] != 0 {
		t.Fatalf("missing error details: %#v", typed.Details)
	}
}

func TestCGEValidationLearnFlagControlsCriticalMemory(t *testing.T) {
	for _, learn := range []bool{false, true} {
		t.Run(map[bool]string{false: "excluded", true: "included"}[learn], func(t *testing.T) {
			store := state.NewStore()
			chains := event.NewChainManager(event.DefaultChainConfig())
			server := NewServer(Config{State: store, Chains: chains, IngestEvent: func(value *contract.Event) {
				chains.Process(value, nil)
			}})
			payload := `{"learn":` + map[bool]string{false: "false", true: "true"}[learn] + `,"events":[{"event_type":"vision.weapon","device_id":"cam_03","node_id":"zoneA.L0.salon"}]}`
			if _, err := server.Handler(contract.RPCCGEValidationSequence)(contract.Message{Payload: []byte(payload)}); err != nil {
				t.Fatal(err)
			}
			memories := chains.CriticalMemories(10)
			if (len(memories) > 0) != learn {
				t.Fatalf("learn=%t critical memories=%#v", learn, memories)
			}
		})
	}
}
