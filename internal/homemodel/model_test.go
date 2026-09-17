package homemodel

import (
	"encoding/json"
	"testing"
	"time"

	"synora/internal/topology"
	"synora/pkg/contract"
)

var modelBase = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

func testHomeTopology() *topology.Topology {
	return &topology.Topology{Nodes: map[string]*topology.Node{
		"driveway":    {ID: "driveway", Name: "Driveway", Type: topology.NodeZone, Metadata: map[string]any{"exterior": true}, Connect: []string{"front_door"}},
		"front_door":  {ID: "front_door", Name: "Front door", Type: topology.NodeRoom, Connect: []string{"driveway", "hallway"}},
		"hallway":     {ID: "hallway", Name: "Hallway", Type: topology.NodeRoom, Connect: []string{"front_door", "living_room"}},
		"living_room": {ID: "living_room", Name: "Living room", Type: topology.NodeRoom, Connect: []string{"hallway"}},
		"isolated":    {ID: "isolated", Name: "Isolated", Type: topology.NodeRoom},
	}}
}

func observationEvent(id, source, track, zone string, at time.Time, confidence float64) *contract.Event {
	return &contract.Event{ID: id, Type: contract.EventVisionUnknown, Source: source, DeviceID: source, TrackID: track, NodeID: zone, Timestamp: at, ReceivedAt: at.Add(2 * time.Second), Confidence: confidence, Payload: map[string]any{"entity_class": "person", "features": map[string]any{"coat": "blue"}}}
}

func TestTopologyClassificationIsConfigurableAndDeterministic(t *testing.T) {
	model := New(testHomeTopology(), DefaultConfig())
	model.SetSensorCoverage("cam-wide", "driveway", "front_door")
	topology := model.Topology()
	covered := 0
	for _, relation := range topology.Relations {
		if relation.Type == "covers" && relation.From == "cam-wide" {
			covered++
		}
	}
	if covered != 2 {
		t.Fatalf("multi-zone source coverage was not projected: %+v", topology.Relations)
	}
	if got := model.TransitionStatus("driveway", "front_door"); got != TransitionExpected {
		t.Fatalf("expected adjacent transition, got %s", got)
	}
	if got := model.TransitionStatus("driveway", "living_room"); got != TransitionUnlikely {
		t.Fatalf("expected reachable long transition, got %s", got)
	}
	if got := model.TransitionStatus("driveway", "isolated"); got != TransitionImpossible {
		t.Fatalf("expected impossible transition, got %s", got)
	}
	if got := model.TransitionStatus("missing", "driveway"); got != TransitionUnknown {
		t.Fatalf("expected unknown endpoint to remain non-reachable, got %s", got)
	}
}

func TestObservationIsIdempotentAndRetainsEventTime(t *testing.T) {
	model := New(testHomeTopology(), DefaultConfig())
	event := observationEvent("obs-1", "cam-a", "12", "driveway", modelBase, .82)
	first, _ := model.Observe(event)
	second, _ := model.Observe(event)
	if len(first.Entities) != 1 || len(second.Entities) != 1 || second.WorkingSet.ObservationCount != 1 {
		t.Fatalf("duplicate observation changed model: first=%+v second=%+v", first, second)
	}
	canonical := NormalizeObservation(event)
	if !canonical.Timestamp.Equal(modelBase) || !canonical.ReceivedAt.Equal(modelBase.Add(2*time.Second)) {
		t.Fatalf("event/received time was not preserved: %+v", canonical)
	}
	if canonical.SourceID != "cam-a" || canonical.LocalTrackID != "12" {
		t.Fatalf("canonical source/track mismatch: %+v", canonical)
	}
}

func TestCrossCameraContinuityAndSemanticTrajectory(t *testing.T) {
	model := New(testHomeTopology(), DefaultConfig())
	first, _ := model.Observe(observationEvent("obs-a", "cam-a", "12", "driveway", modelBase, .82))
	second, _ := model.Observe(observationEvent("obs-b", "cam-b", "8", "front_door", modelBase.Add(8*time.Second), .91))
	if len(first.Entities) != 1 || len(second.Entities) != 1 {
		t.Fatalf("cross-camera observations duplicated entity: %+v", second.Entities)
	}
	entity := second.Entities[0]
	if len(entity.Tracks) != 2 || entity.Tracks[0].SourceID != "cam-a" || entity.Tracks[1].SourceID != "cam-b" {
		t.Fatalf("tracks were not joined deterministically: %+v", entity.Tracks)
	}
	if len(entity.Trajectory) != 2 || entity.Trajectory[0].Zone != "driveway" || entity.Trajectory[1].Zone != "front_door" {
		t.Fatalf("unexpected semantic trajectory: %+v", entity.Trajectory)
	}
	approaching := false
	for _, situation := range second.Situations {
		if situation.Type == "unknown_approaching" {
			approaching = true
		}
	}
	if !approaching {
		t.Fatalf("expected derived unknown approach situation: %+v", second.Situations)
	}
}

func TestSimultaneousTargetsRemainSeparateAndUnknownGapsRemainUnknown(t *testing.T) {
	model := New(testHomeTopology(), DefaultConfig())
	model.Observe(observationEvent("obs-a", "cam-a", "1", "driveway", modelBase, .9))
	model.Observe(observationEvent("obs-b", "cam-b", "2", "driveway", modelBase, .9))
	snapshot, _ := model.Observe(observationEvent("obs-c", "cam-b", "2", "hallway", modelBase.Add(5*time.Second), .9))
	if len(snapshot.Entities) != 2 {
		t.Fatalf("simultaneous targets were merged: %+v", snapshot.Entities)
	}
	var moved contract.HomeEntity
	for _, entity := range snapshot.Entities {
		for _, track := range entity.Tracks {
			if track.LocalTrackID == "2" {
				moved = entity
			}
		}
	}
	if len(moved.Trajectory) != 2 {
		t.Fatalf("expected second target trajectory: %+v", moved.Trajectory)
	}
	gapModel := New(testHomeTopology(), DefaultConfig())
	gapModel.Observe(observationEvent("gap-a", "cam-a", "3", "driveway", modelBase, .9))
	gapSnapshot, _ := gapModel.Observe(observationEvent("gap-b", "cam-b", "4", "", modelBase.Add(6*time.Second), .9))
	if len(gapSnapshot.Entities) != 1 || len(gapSnapshot.Entities[0].Trajectory) != 2 || gapSnapshot.Entities[0].Trajectory[1].Zone != "UNKNOWN" {
		t.Fatalf("missing zone was invented or split: %+v", gapSnapshot.Entities)
	}
}

func TestLowConfidenceIdentityIsHypothesisNotFactAndStateRecovers(t *testing.T) {
	model := New(testHomeTopology(), DefaultConfig())
	event := observationEvent("obs-id", "cam-a", "1", "front_door", modelBase, .61)
	event.Type = contract.EventVisionUncertain
	event.Identity = "resident-1"
	event.Payload["identity_candidate"] = "resident-1"
	event.Payload["identity_confidence"] = .61
	snapshot, raw := model.Observe(event)
	if snapshot.Entities[0].Identity != "" || len(snapshot.Hypotheses) == 0 || snapshot.Hypotheses[0].Type != "identity_candidate" {
		t.Fatalf("low-confidence identity became certain: %+v", snapshot)
	}
	if len(raw) == 0 || !json.Valid(raw) {
		t.Fatalf("model state is not persistable: %s", raw)
	}
	restored := New(testHomeTopology(), DefaultConfig())
	if err := restored.Load(raw); err != nil {
		t.Fatal(err)
	}
	recovered := restored.Snapshot()
	if len(recovered.Entities) != 1 || recovered.Entities[0].Identity != "" || len(recovered.Hypotheses) == 0 {
		t.Fatalf("model did not recover context: %+v", recovered)
	}
}

func TestDelayedUnknownSourceAndWorkingSetBounds(t *testing.T) {
	config := DefaultConfig()
	config.MaxObservations = 2
	model := New(testHomeTopology(), config)
	latest, _ := model.Observe(observationEvent("late-new", "", "", "driveway", modelBase.Add(10*time.Second), .7))
	if latest.GeneratedAt != modelBase.Add(10*time.Second) {
		t.Fatalf("unexpected latest event time: %s", latest.GeneratedAt)
	}
	older, _ := model.Observe(observationEvent("late-old", "unknown-source", "", "front_door", modelBase.Add(5*time.Second), .7))
	if older.GeneratedAt != modelBase.Add(10*time.Second) || older.WorkingSet.ObservationCount != 2 {
		t.Fatalf("delayed event rewound model or was lost: %+v", older)
	}
	newest, _ := model.Observe(observationEvent("late-newest", "unknown-source", "", "hallway", modelBase.Add(20*time.Second), .7))
	if newest.WorkingSet.ObservationCount != 2 {
		t.Fatalf("working set exceeded configured bound: %+v", newest.WorkingSet)
	}
	if NormalizeObservation(&contract.Event{ID: "unknown-source", Type: contract.EventVisionMotion, Timestamp: modelBase}).SourceID != "" {
		t.Fatal("source fallback should remain unknown when event has no source")
	}
}
