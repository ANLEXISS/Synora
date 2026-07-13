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
