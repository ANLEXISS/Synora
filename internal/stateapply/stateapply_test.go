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

func TestVisionIdentitySetsLastSeen(t *testing.T) {
	store := state.NewStore()
	seenAt := time.Date(2026, 7, 11, 17, 3, 56, 742582666, time.UTC)
	event := &contract.Event{
		Type:       contract.EventVisionIdentity,
		Source:     "vision-worker",
		Identity:   "alexis",
		NodeID:     "zoneA.L1.chambre_enfant",
		Confidence: 0.9,
		Timestamp:  seenAt,
	}

	presence := stateapply.ApplyVisionIdentity(store, event)
	if presence == nil {
		t.Fatal("vision.identity should create runtime presence")
	}
	if presence.State != "present" || presence.Location != event.NodeID || presence.Confidence != 0.9 || !presence.LastSeen.Equal(seenAt) {
		t.Fatalf("unexpected runtime presence: %#v", presence)
	}

	stored, ok := store.PresenceState("alexis")
	if !ok || stored == nil || !stored.LastSeen.Equal(seenAt) {
		t.Fatalf("last_seen was not stored: %#v", stored)
	}
	if stored.ConfidenceSource != "vision-worker" {
		t.Fatalf("confidence source was not retained: %#v", stored)
	}
}

func TestResidentPresenceHysteresisAndUncertainIdentity(t *testing.T) {
	store := state.NewStore()
	residents := map[string]*topology.Resident{"alexis": {ID: "alexis", Name: "Alexis"}}
	base := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

	for _, confidence := range []float64{0.59, 0.50} {
		if got := stateapply.ApplyVisionIdentityForResidents(store, &contract.Event{
			Type: contract.EventVisionIdentity, ResidentID: "alexis", Source: "vision-worker",
			NodeID: "entry", Confidence: confidence, Timestamp: base,
		}, residents); got != nil {
			t.Fatalf("confidence %.2f entered presence below enter threshold: %#v", confidence, got)
		}
	}

	entered := stateapply.ApplyVisionIdentityForResidents(store, &contract.Event{
		Type: contract.EventVisionIdentity, ResidentID: "alexis", Source: "vision-worker",
		NodeID: "entry", Confidence: 0.60, Timestamp: base.Add(time.Second),
	}, residents)
	if entered == nil || entered.State != "present" || entered.Location != "entry" {
		t.Fatalf("confidence at enter threshold did not establish presence: %#v", entered)
	}

	if got := stateapply.ApplyVisionIdentityForResidents(store, &contract.Event{
		Type: contract.EventVisionIdentity, ResidentID: "alexis", Source: "vision-worker",
		NodeID: "hall", Confidence: 0.39, Timestamp: base.Add(2 * time.Second),
	}, residents); got != nil {
		t.Fatalf("confidence below exit threshold changed presence: %#v", got)
	}
	retained, ok := store.PresenceState("alexis")
	if !ok || retained == nil || retained.Location != "entry" || retained.Confidence != 0.60 {
		t.Fatalf("low-confidence observation changed retained presence: %#v", retained)
	}

	updated := stateapply.ApplyVisionIdentityForResidents(store, &contract.Event{
		Type: contract.EventVisionIdentity, ResidentID: "alexis", Source: "vision-worker",
		NodeID: "hall", Confidence: 0.40, Timestamp: base.Add(3 * time.Second),
	}, residents)
	if updated == nil || updated.Location != "hall" || updated.Confidence != 0.40 {
		t.Fatalf("confidence at exit threshold did not retain/update presence: %#v", updated)
	}
	if got := stateapply.ApplyVisionIdentityForResidents(store, &contract.Event{
		Type: contract.EventVisionUncertain, ResidentID: "alexis", Source: "vision-worker",
		NodeID: "hall", Confidence: 0.99, Timestamp: base.Add(4 * time.Second),
	}, residents); got != nil {
		t.Fatalf("uncertain identity acquired certain presence: %#v", got)
	}
}

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
	stateapply.ApplyVisionIdentity(store, event)

	identity, ok := store.Identity("alexis")
	if !ok || identity == nil {
		t.Fatalf("expected identity state to be applied")
	}
	if identity.LastDeviceID != "camera-1" || identity.LastNodeID != "entry" {
		t.Fatalf("unexpected identity state: %#v", identity)
	}
	presence, ok := store.PresenceState("alexis")
	if !ok || presence == nil {
		t.Fatal("expected runtime resident presence to be applied")
	}
	if presence.State != "present" || presence.Location != "entry" || presence.Confidence != 0.91 || !presence.LastSeen.Equal(event.Timestamp) {
		t.Fatalf("unexpected runtime resident presence: %#v", presence)
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
