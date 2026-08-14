package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	corerpc "synora/internal/rpc"
	"synora/internal/state"
	"synora/pkg/contract"
)

func TestIncidentIntegrationRealIntrusionPersistsAndSurvivesReset(t *testing.T) {
	app, bus := newTestCoreApp(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	app.state.SetPersistence(state.NewFilePersistence(statePath))
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)
	first := &contract.Event{
		ID: "evt-weapon-1", Type: contract.EventVisionWeapon, Source: "vision-worker",
		Timestamp: now, DeviceID: "cam_01", NodeID: "entry", TrackID: "track-unknown",
		ClipID: "clip-weapon-1", SequenceKey: "seq-weapon", ActivationID: "activation-1",
		Confidence: 0.93, Payload: map[string]any{"clip_path": "/var/lib/synora/clips/clip-weapon-1.mp4", "weapon_type": "knife"},
	}
	app.processEvent(first)

	system := app.state.SystemState()
	if system.LastState != "intrusion" || !system.IntrusionActive {
		t.Fatalf("real weapon decision should apply intrusion: %#v", system)
	}
	items := app.state.IncidentsList(10)
	if len(items) != 1 {
		t.Fatalf("expected one incident after real intrusion, got %#v", items)
	}
	incident := items[0]
	if incident.Status != contract.IncidentStatusNew || incident.Cause.EventType != contract.EventVisionWeapon || incident.CameraID != "cam_01" || incident.NodeID != "entry" {
		t.Fatalf("incident lost decision context: %#v", incident)
	}
	if incident.IdentityKind != contract.IncidentIdentityNone || incident.ResidentID != "" {
		t.Fatalf("incident should not invent resident identity: %#v", incident)
	}
	if len(incident.EventIDs) != 1 || len(incident.ClipIDs) != 1 || len(incident.Timeline) != 1 || incident.ClipIDs[0] != "clip-weapon-1" {
		t.Fatalf("incident evidence references are incomplete: %#v", incident)
	}
	if len(bus.messagesOfType("incident.created")) != 1 {
		t.Fatalf("Core should publish one incident.created event, messages=%#v", bus.messages)
	}
	createdMessage := bus.messagesOfType("incident.created")[0]
	if createdMessage.Version != contract.RealtimeSchemaVersion || createdMessage.Epoch == "" || createdMessage.Sequence == 0 || createdMessage.Revision == 0 {
		t.Fatalf("incident publication must carry realtime cursor metadata: %#v", createdMessage)
	}

	second := *first
	second.ID = "evt-weapon-2"
	second.Timestamp = now.Add(10 * time.Second)
	app.processEvent(&second)
	if got := len(app.state.IncidentsList(10)); got != 1 {
		t.Fatalf("same intrusion sequence should remain one incident, got %d", got)
	}
	incident = app.state.IncidentsList(10)[0]
	if len(incident.EventIDs) != 2 || len(incident.Timeline) != 2 {
		t.Fatalf("same sequence should enrich incident once per event: %#v", incident)
	}
	updates := bus.messagesOfType("incident.updated")
	if len(updates) != 1 || updates[0].Version != contract.RealtimeSchemaVersion {
		t.Fatalf("Core should publish one realtime enrichment: %#v", updates)
	}
	var update contract.RealtimeIncidentUpdatedPayload
	if err := json.Unmarshal(updates[0].Payload, &update); err != nil || update.Reason != "enriched" || update.IncidentID != incident.ID || update.Revision != incident.Revision {
		t.Fatalf("unexpected realtime enrichment payload: %#v err=%v", update, err)
	}

	server := corerpc.NewServer(corerpc.Config{State: app.state})
	ackAny, err := server.Handler("incidents.acknowledge")(contract.Message{Payload: []byte(`{"id":"` + incident.ID + `"}`)})
	if err != nil || ackAny.(contract.Incident).Status != contract.IncidentStatusAcknowledged {
		t.Fatalf("incident acknowledgement failed: value=%#v err=%v", ackAny, err)
	}
	if _, err := server.Handler("system.reset_intrusion")(contract.Message{Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("intrusion reset failed: %v", err)
	}
	if after := app.state.SystemState(); after.LastState != "idle" || after.IntrusionActive {
		t.Fatalf("reset should clear operational intrusion state: %#v", after)
	}
	restored, ok := app.state.Incident(incident.ID)
	if !ok || restored.Status != contract.IncidentStatusAcknowledged || len(restored.EventIDs) != 2 {
		t.Fatalf("reset must retain acknowledged evidence: %#v ok=%t", restored, ok)
	}
	reloaded := state.NewStore(state.WithPersistencePath(statePath))
	if _, err := reloaded.LoadPersisted(); err != nil {
		t.Fatalf("reload incident state: %v", err)
	}
	reloadedIncident, ok := reloaded.Incident(incident.ID)
	if !ok || reloadedIncident.Status != contract.IncidentStatusAcknowledged || len(reloadedIncident.EventIDs) != 2 {
		t.Fatalf("persisted incident not restored after integration path: %#v ok=%t", reloadedIncident, ok)
	}
}

func TestIncidentIdentityClassificationDoesNotInventResidents(t *testing.T) {
	app, _ := newTestCoreApp(t)
	resident := &contract.Event{Type: contract.EventVisionIdentity, Identity: "alexis", Confidence: 0.96}
	kind, residentID := app.incidentIdentity(resident)
	if kind != contract.IncidentIdentityResident || residentID != "alexis" {
		t.Fatalf("known high-confidence resident classification=%s id=%q", kind, residentID)
	}

	unknown := &contract.Event{Type: contract.EventVisionUnknown, Identity: ""}
	kind, residentID = app.incidentIdentity(unknown)
	if kind != contract.IncidentIdentityUnknown || residentID != "" {
		t.Fatalf("unknown classification=%s id=%q", kind, residentID)
	}

	uncertain := &contract.Event{Type: contract.EventVisionIdentity, Identity: "alexis", Confidence: 0.40}
	kind, residentID = app.incidentIdentity(uncertain)
	if kind != contract.IncidentIdentityUncertain || residentID != "" {
		t.Fatalf("low-confidence classification=%s id=%q", kind, residentID)
	}
}
