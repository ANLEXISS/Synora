package stateapply_test

import (
	"encoding/json"
	"testing"
	"time"

	"synora/internal/device"
	"synora/internal/engine"
	cgecontracts "synora/internal/engine/contracts"
	"synora/internal/ingest"
	"synora/internal/state"
	"synora/internal/stateapply"
	"synora/internal/topology"
	"synora/pkg/contract"
)

func TestEventAnalyzeThenStateApply(t *testing.T) {
	devices := device.NewRegistry()
	devices.Register([]device.DeviceConfig{{
		ID:     "camera-1",
		Type:   "camera",
		Room:   "entry",
		NodeID: "entry",
	}})
	topo := &topology.Topology{
		Nodes: map[string]*topology.Node{
			"entry": {ID: "entry", Name: "Entry", Type: topology.NodeRoom},
		},
	}
	store := state.NewStore()
	engineInstance := engine.NewEngine(topo, devices, map[string]*topology.Resident{
		"alexis": {ID: "alexis", Name: "Alexis"},
	})

	payload, err := json.Marshal(map[string]any{
		"identity":   "alexis",
		"confidence": 0.91,
		"clip_path":  "/tmp/clip.mp4",
	})
	if err != nil {
		t.Fatal(err)
	}
	parser := ingest.Parser{
		Devices: devices,
		Now:     func() time.Time { return time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC) },
	}
	event, err := parser.Parse(contract.Message{
		Type:    contract.EventVisionIdentity,
		Kind:    contract.KindEvent,
		Source:  "camera-1",
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("parse event: %v", err)
	}

	stateapply.TouchDeviceState(store, devices, event)
	result := engineInstance.Analyze(event, store)
	if result == nil || result.Decision == nil {
		t.Fatalf("expected engine result with decision, got %#v", result)
	}
	stateapply.Apply(store, result, stateapply.Callbacks{})

	identity, ok := store.Identity("alexis")
	if !ok || identity == nil {
		t.Fatalf("expected identity state to be applied")
	}
	if identity.LastDeviceID != "camera-1" || identity.LastNodeID != "entry" {
		t.Fatalf("unexpected identity state: %#v", identity)
	}
	clip, ok := store.Clip(result.Clip.ID)
	if !ok || clip == nil || clip.CameraID != "camera-1" {
		t.Fatalf("expected clip state to be applied, got %#v", clip)
	}
	deviceState, ok := store.DeviceState("camera-1")
	if !ok || deviceState == nil || !deviceState.Online {
		t.Fatalf("expected camera device state online, got %#v", deviceState)
	}
}

func TestApplyCreatesPendingValidationRequest(t *testing.T) {
	store := state.NewStore()
	at := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

	stateapply.Apply(store, &engine.Result{
		Decision: &contract.Decision{
			ID:                 "dec-1",
			EventID:            "evt-1",
			Timestamp:          at,
			Reason:             "rapid_novel_transition",
			ValidationRequired: true,
			ValidationReason:   "rapid_novel_transition",
			NodeID:             "entry",
			ClipID:             "clip-1",
		},
		Situations: []cgecontracts.Situation{{
			ID:       "sit-1",
			Evidence: []string{"event:evt-1"},
		}},
		Identity: &state.IdentityState{
			ID: "alexis",
		},
	}, stateapply.Callbacks{})

	validation, ok := store.Validation("validation-dec-1")
	if !ok || validation == nil {
		t.Fatalf("expected validation request in store")
	}
	if validation.Status != contract.ValidationStatusPending {
		t.Fatalf("validation should be pending: %#v", validation)
	}
	if validation.DecisionID != "dec-1" || validation.EventID != "evt-1" || validation.SituationID != "sit-1" {
		t.Fatalf("validation links mismatch: %#v", validation)
	}
	if validation.ProposedIdentity != "alexis" || validation.NodeID != "entry" || validation.ClipID != "clip-1" {
		t.Fatalf("validation context mismatch: %#v", validation)
	}
	if len(validation.Evidence) == 0 {
		t.Fatalf("validation evidence should not be empty: %#v", validation)
	}
}
