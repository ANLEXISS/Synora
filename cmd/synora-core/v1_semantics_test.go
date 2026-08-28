package main

import (
	"path/filepath"
	"testing"
	"time"

	"synora/internal/state"
	"synora/pkg/contract"
)

func TestV1UnknownIsImmediateIntrusionAndSingleIncident(t *testing.T) {
	app, _ := newTestCoreApp(t)
	at := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	event := &contract.Event{ID: "unknown-v1", Type: contract.EventVisionUnknown, Source: "vision-worker", DeviceID: "cam_01", NodeID: "entry", TrackID: "track-v1", ActivationID: "activation-v1", SequenceKey: "sequence-v1", Timestamp: at, Confidence: .9}
	app.processEvent(event)
	state := app.state.SystemState()
	if state.LastState != "intrusion" || !state.IntrusionActive {
		t.Fatalf("unknown did not immediately enter intrusion: %#v", state)
	}
	if incidents := app.state.IncidentsList(10); len(incidents) != 1 || incidents[0].SecurityState != "intrusion" {
		t.Fatalf("unknown incident projection=%#v", incidents)
	}
	app.processEvent(&contract.Event{ID: event.ID, Type: event.Type, Source: event.Source, DeviceID: event.DeviceID, NodeID: event.NodeID, TrackID: event.TrackID, ActivationID: event.ActivationID, SequenceKey: event.SequenceKey, Timestamp: at, ReceivedAt: at.Add(time.Minute), Confidence: event.Confidence})
	if got := len(app.state.IncidentsList(10)); got != 1 {
		t.Fatalf("duplicate delivery created %d incidents", got)
	}
}

func TestV1FallbackKeyIgnoresReceiveTimeButSeparatesObservations(t *testing.T) {
	at := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	first := &contract.Event{Type: contract.EventVisionUnknown, ActivationID: "activation", TrackID: "track", DeviceID: "cam_01", NodeID: "entry", SequenceKey: "sequence", Epoch: "epoch", Timestamp: at, ReceivedAt: at}
	retry := *first
	retry.ReceivedAt = at.Add(5 * time.Minute)
	second := *first
	second.ClipIndex = 1
	if canonicalEventKey(first) != canonicalEventKey(&retry) {
		t.Fatal("receive-time change altered the fallback idempotence key")
	}
	if canonicalEventKey(first) == canonicalEventKey(&second) {
		t.Fatal("distinct captured observations were merged")
	}
}

func TestV1RejectsEventIDCollisionBeforeMutation(t *testing.T) {
	app, _ := newTestCoreApp(t)
	at := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	app.processEvent(&contract.Event{
		ID: "collision", Type: contract.EventVisionMotion, Source: "vision-worker",
		DeviceID: "cam_01", Timestamp: at, Payload: map[string]any{"motion": true},
	})
	app.processEvent(&contract.Event{
		ID: "collision", Type: contract.EventVisionMotion, Source: "vision-worker",
		DeviceID: "cam_01", Timestamp: at, Payload: map[string]any{"motion": false},
	})
	if events := app.state.RecentEventsList(); len(events) != 1 {
		t.Fatalf("ID collision must not apply a second event: %#v", events)
	}
	if app.metrics.metricsSnapshotEventProcessed() != 1 {
		t.Fatal("ID collision must not increment processed-event metric")
	}
}

func TestV1RejectsPoisonAndMissingClipBeforeMutation(t *testing.T) {
	app, _ := newTestCoreApp(t)
	at := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	app.processEvent(&contract.Event{
		ID: "poison", Type: contract.EventActionResult, Source: "actions", Timestamp: at,
		Payload: map[string]any{"status": ""},
	})
	app.processEvent(&contract.Event{
		ID: "missing-clip", Type: contract.EventClipProcessed, Source: "discovery",
		DeviceID: "cam_01", ClipID: "clip-does-not-exist", Timestamp: at,
	})
	if events := app.state.RecentEventsList(); len(events) != 0 {
		t.Fatalf("invalid ingress events must not mutate the state journal: %#v", events)
	}
}

func TestV1ReplayAfterRestartIsDeduplicatedFromDurableRecentEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	first, _ := newTestCoreApp(t)
	first.state.SetPersistence(state.NewFilePersistence(path))
	at := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	event := &contract.Event{
		ID: "durable-replay", Type: contract.EventVisionMotion, Source: "vision-worker",
		DeviceID: "cam_01", Timestamp: at, Payload: map[string]any{"motion": true},
	}
	first.processEvent(event)

	restarted, _ := newTestCoreApp(t)
	restarted.state.SetPersistence(state.NewFilePersistence(path))
	if _, err := restarted.state.LoadPersisted(); err != nil {
		t.Fatalf("restore persisted recent events: %v", err)
	}
	restarted.eventStore.Load(restarted.state.RecentEventsList())
	restarted.processEvent(&contract.Event{
		ID: "durable-replay", Type: contract.EventVisionMotion, Source: "vision-worker",
		DeviceID: "cam_01", Timestamp: at, Payload: map[string]any{"motion": true},
	})
	if len(restarted.state.RecentEventsList()) != 1 || restarted.metrics.metricsSnapshotEventProcessed() != 0 {
		t.Fatal("durable replay should not reapply or count the event")
	}
}

func (m *coreMetrics) metricsSnapshotEventProcessed() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.eventProcessed
}
