package cge

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestNoopEngineIsNeutral(t *testing.T) {
	engine := NewNoopEngine()
	event := Event{ID: "event-1", Type: "vision.motion"}

	result, err := engine.Observe(context.Background(), event)
	if err != nil {
		t.Fatalf("noop observe: %v", err)
	}
	if !reflect.DeepEqual(result, ObservationResult{}) {
		t.Fatalf("noop result = %#v, want zero result", result)
	}
	snapshot, err := engine.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("noop snapshot: %v", err)
	}
	if !reflect.DeepEqual(snapshot, Snapshot{}) {
		t.Fatalf("noop snapshot = %#v, want zero snapshot", snapshot)
	}
	explanation, err := engine.Explain(context.Background(), "situation-1")
	if err != nil || explanation.SituationID != "situation-1" || explanation.Available {
		t.Fatalf("noop explanation = %#v, err=%v", explanation, err)
	}
}

func TestShadowEngineCountsObservations(t *testing.T) {
	engine := NewShadowEngine()
	at := time.Date(2026, 7, 17, 10, 11, 12, 0, time.UTC)

	first, err := engine.Observe(context.Background(), Event{Type: "vision.motion", Timestamp: at})
	if err != nil {
		t.Fatalf("first observe: %v", err)
	}
	second, err := engine.Observe(context.Background(), Event{Type: "vision.unknown", Timestamp: at.Add(time.Second)})
	if err != nil {
		t.Fatalf("second observe: %v", err)
	}
	if first.ObservationCount != 1 || second.ObservationCount != 2 {
		t.Fatalf("observation counts = %d, %d", first.ObservationCount, second.ObservationCount)
	}

	snapshot, err := engine.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.ObservationCount != 2 || snapshot.LastEventType != "vision.unknown" || !snapshot.LastObservedAt.Equal(at.Add(time.Second)) {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestShadowEngineIsSafeForConcurrentObservation(t *testing.T) {
	engine := NewShadowEngine()
	const observations = 256

	var wg sync.WaitGroup
	for i := 0; i < observations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := engine.Observe(context.Background(), Event{Type: "vision.motion"}); err != nil {
				t.Errorf("concurrent observe: %v", err)
			}
		}()
	}
	wg.Wait()

	snapshot, err := engine.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.ObservationCount != observations {
		t.Fatalf("observation count = %d, want %d", snapshot.ObservationCount, observations)
	}
}

func TestEventFromContractDoesNotShareMutableEventData(t *testing.T) {
	original := &contract.Event{
		ID: "event-1", Type: "vision.motion", Source: "vision-worker",
		Timestamp: time.Now().UTC(), Payload: map[string]any{"secret": "not-crossed"},
	}
	boundary := EventFromContract(original)

	boundary.Type = "changed-by-observer"
	if original.Type != "vision.motion" {
		t.Fatalf("boundary mutation changed original event type: %q", original.Type)
	}
	if boundary.ID != original.ID || boundary.Source != original.Source {
		t.Fatalf("boundary event lost scalar fields: %#v", boundary)
	}
}
