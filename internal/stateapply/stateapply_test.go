package stateapply_test

import (
	"encoding/json"
	"testing"
	"time"

	"synora/internal/device"
	"synora/internal/engine"
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
