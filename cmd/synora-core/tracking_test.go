package main

import (
	"testing"
	"time"

	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

func TestVisionIdentityUsesResidentIDAndUpdatesResidentTrack(t *testing.T) {
	app, _ := newTestCoreApp(t)
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	app.processEvent(&contract.Event{ID: "identity-resident", Type: contract.EventVisionIdentity, Source: "vision-worker", ResidentID: "alexis", Identity: "Alexis display name", DeviceID: "cam_01", NodeID: "entry", TrackID: "track-a", SequenceKey: "seq-a", ActivationID: "activation-a", Confidence: .91, Timestamp: at})
	presence, ok := app.state.PresenceState("alexis")
	if !ok || presence == nil || presence.State != "present" {
		t.Fatalf("resident presence missing: %#v", presence)
	}
	track, ok := app.state.ResidentTrack("alexis")
	if !ok || track == nil || track.LastNodeID != "entry" || track.LastTrackID != "track-a" {
		t.Fatalf("resident track missing stable identifiers: %#v", track)
	}
	if _, ok := app.state.PresenceState("Alexis display name"); ok {
		t.Fatal("display_name was used as a runtime identifier")
	}
	if app.state.SystemState().IntrusionActive {
		t.Fatal("identity alone must not create intrusion")
	}
}

func TestUnknownResidentIDIsNotSilentlyAccepted(t *testing.T) {
	app, _ := newTestCoreApp(t)
	app.processEvent(&contract.Event{ID: "identity-unknown-resident", Type: contract.EventVisionIdentity, Source: "vision-worker", ResidentID: "missing-resident", DeviceID: "cam_01", NodeID: "entry", Confidence: .99, Timestamp: time.Now().UTC()})
	if _, ok := app.state.PresenceState("missing-resident"); ok {
		t.Fatal("unknown resident_id became present")
	}
	if _, ok := app.state.ResidentTrack("missing-resident"); ok {
		t.Fatal("unknown resident_id created ResidentTrack")
	}
}

func TestUncertainCreatesOneAnonymousEntityAndNeverPresence(t *testing.T) {
	app, _ := newTestCoreApp(t)
	at := time.Date(2026, 8, 11, 12, 1, 0, 0, time.UTC)
	first := &contract.Event{ID: "uncertain-1", Type: contract.EventVisionUncertain, Source: "vision-worker", DeviceID: "cam_01", NodeID: "entry", TrackID: "track-anon", SequenceKey: "seq-anon", ActivationID: "activation-anon", ResidentID: "alexis", Confidence: .44, Timestamp: at}
	second := *first
	second.ID = "uncertain-2"
	second.Timestamp = at.Add(time.Second)
	app.processEvent(first)
	app.processEvent(&second)
	entityID := state.EntityTrackID("track-anon", "seq-anon", "activation-anon", "cam_01", "entry")
	entity, ok := app.state.EntityTrack(entityID)
	if !ok || entity == nil || entity.Kind != "uncertain" || entity.TrackID != "track-anon" {
		t.Fatalf("anonymous entity was not reused: %#v", entity)
	}
	if entity.ResidentID != "" {
		t.Fatalf("uncertain event affirmed resident: %#v", entity)
	}
	if _, ok := app.state.PresenceState("alexis"); ok {
		t.Fatal("uncertain event marked resident present")
	}
}

func TestUnknownCreatesAnonymousEntityClusterAndEndIsIdempotent(t *testing.T) {
	app, _ := newTestCoreApp(t)
	at := time.Date(2026, 8, 11, 12, 2, 0, 0, time.UTC)
	app.processEvent(&contract.Event{ID: "unknown-1", Type: contract.EventVisionUnknown, Source: "vision-worker", DeviceID: "cam_01", NodeID: "entry", TrackID: "track-unknown", SequenceKey: "seq-unknown", ActivationID: "activation-unknown", Confidence: .9, Timestamp: at})
	id := state.EntityTrackID("track-unknown", "seq-unknown", "activation-unknown", "cam_01", "entry")
	if entity, ok := app.state.EntityTrack(id); !ok || entity == nil || entity.Kind != "unknown" {
		t.Fatalf("unknown entity missing: %#v", entity)
	}
	if cluster, ok := app.state.Cluster("unknown_presence:" + id); !ok || cluster == nil || len(cluster.EventIDs) != 1 {
		t.Fatalf("unknown cluster missing: %#v", cluster)
	}
	incidents := app.state.IncidentsList(10)
	if len(incidents) != 1 || incidents[0].SecurityState != "intrusion" || incidents[0].EntityID != id {
		t.Fatalf("unknown security observation was not durably incidented: %#v", incidents)
	}
	end := &contract.Event{ID: "end-1", Type: contract.EventVisionEnd, Source: "vision-worker", ActivationID: "activation-unknown", Timestamp: at.Add(time.Second)}
	app.processEvent(end)
	revision := app.state.Revision()
	app.processEvent(end)
	if app.state.Revision() != revision {
		t.Fatal("duplicate vision.end mutated StateStore")
	}
	if _, ok := app.state.EntityTrack(id); ok {
		t.Fatal("vision.end did not finalize anonymous activation")
	}
}

func TestDuplicateObservationIsAppliedOnce(t *testing.T) {
	app, _ := newTestCoreApp(t)
	event := &contract.Event{ID: "duplicate-observation", Type: contract.EventVisionUnknown, Source: "vision-worker", DeviceID: "cam_01", NodeID: "entry", TrackID: "duplicate-track", ActivationID: "duplicate-activation", Confidence: .9, Timestamp: time.Now().UTC()}
	app.processEvent(event)
	revision := app.state.Revision()
	app.processEvent(event)
	if app.state.Revision() != revision {
		t.Fatal("duplicate observation mutated StateStore")
	}
	if len(app.state.IncidentsList(10)) > 1 {
		t.Fatal("duplicate observation created multiple incidents")
	}
}

func TestUncertainThenIdentityBindsOneEntity(t *testing.T) {
	app, _ := newTestCoreApp(t)
	at := time.Now().UTC()
	app.processEvent(&contract.Event{ID: "bind-uncertain", Type: contract.EventVisionUncertain, Source: "vision-worker", DeviceID: "cam_01", NodeID: "entry", TrackID: "bind-track", ActivationID: "bind-activation", SequenceKey: "bind-sequence", ResidentID: "alexis", Confidence: .42, Timestamp: at})
	app.processEvent(&contract.Event{ID: "bind-identity", Type: contract.EventVisionIdentity, Source: "vision-worker", DeviceID: "cam_01", NodeID: "entry", TrackID: "bind-track", ActivationID: "bind-activation", SequenceKey: "bind-sequence", ResidentID: "alexis", Confidence: .92, Timestamp: at.Add(time.Second)})
	entityID := state.EntityTrackID("bind-track", "bind-sequence", "bind-activation", "cam_01", "entry")
	entity, ok := app.state.EntityTrack(entityID)
	if !ok || entity == nil || entity.ResidentID != "alexis" || entity.Kind != "resident" {
		t.Fatalf("anonymous entity was not bound to resident: %#v", entity)
	}
}

func TestCoreRejectsOldEpochAndOutOfOrderSequence(t *testing.T) {
	app, _ := newTestCoreApp(t)
	base := time.Date(2026, 8, 11, 12, 3, 0, 0, time.UTC)
	app.processEvent(&contract.Event{ID: "epoch-2", Type: contract.EventVisionIdentity, Source: "vision-worker", ResidentID: "alexis", DeviceID: "cam_01", NodeID: "entry", Confidence: .9, Epoch: "worker-epoch-1", Sequence: 2, ReceivedAt: base, Timestamp: base})
	before := app.state.Revision()
	app.processEvent(&contract.Event{ID: "epoch-1", Type: contract.EventVisionIdentity, Source: "vision-worker", ResidentID: "alexis", DeviceID: "cam_01", NodeID: "salon", Confidence: .9, Epoch: "worker-epoch-1", Sequence: 1, ReceivedAt: base.Add(time.Second), Timestamp: base.Add(time.Second)})
	if app.state.Revision() != before {
		t.Fatal("out-of-order event changed StateStore")
	}
	app.processEvent(&contract.Event{ID: "epoch-old", Type: contract.EventVisionIdentity, Source: "vision-worker", ResidentID: "alexis", DeviceID: "cam_01", NodeID: "salon", Confidence: .9, Epoch: "worker-epoch-old", Sequence: 2, ReceivedAt: base.Add(2 * time.Second), Timestamp: base.Add(2 * time.Second)})
	if presence, _ := app.state.PresenceState("alexis"); presence == nil || presence.Location != "entry" {
		t.Fatalf("old epoch overwrote current presence: %#v", presence)
	}
}

func TestResidentTrackRejectsImpossibleTopologyMovement(t *testing.T) {
	app, _ := newTestCoreApp(t)
	entry := &topology.Node{ID: "entry", Type: topology.NodeRoom, Connect: []string{"hall"}}
	hall := &topology.Node{ID: "hall", Type: topology.NodeRoom, Connect: []string{"entry"}}
	remote := &topology.Node{ID: "remote", Type: topology.NodeRoom}
	app.topology = &topology.Topology{Nodes: map[string]*topology.Node{"entry": entry, "hall": hall, "remote": remote}}
	app.engine.Topology = app.topology
	app.processEvent(&contract.Event{ID: "move-1", Type: contract.EventVisionIdentity, Source: "vision-worker", ResidentID: "alexis", DeviceID: "cam_01", NodeID: "entry", TrackID: "track-move", Confidence: .9, Timestamp: time.Now().UTC()})
	app.processEvent(&contract.Event{ID: "move-2", Type: contract.EventVisionIdentity, Source: "vision-worker", ResidentID: "alexis", DeviceID: "cam_01", NodeID: "hall", TrackID: "track-move", Confidence: .9, Timestamp: time.Now().Add(time.Second)})
	track, _ := app.state.ResidentTrack("alexis")
	if track == nil || track.LastNodeID != "hall" {
		t.Fatalf("connected topology movement was rejected: %#v", track)
	}
	app.processEvent(&contract.Event{ID: "move-3", Type: contract.EventVisionIdentity, Source: "vision-worker", ResidentID: "alexis", DeviceID: "cam_02", NodeID: "remote", TrackID: "track-move", Confidence: .9, Timestamp: time.Now().Add(2 * time.Second)})
	track, _ = app.state.ResidentTrack("alexis")
	if track == nil || track.LastNodeID != "hall" {
		t.Fatalf("impossible movement changed ResidentTrack: %#v", track)
	}
}
