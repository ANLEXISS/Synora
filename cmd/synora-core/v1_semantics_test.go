package main

import (
	"testing"
	"time"

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
