package engine

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/device"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

func TestEngineHomeModelIsAdditiveAndRecoversFromStateStore(t *testing.T) {
	topo := &topology.Topology{Nodes: map[string]*topology.Node{
		"outside": {ID: "outside", Type: topology.NodeZone, Metadata: map[string]any{"exterior": true}, Connect: []string{"door"}},
		"door":    {ID: "door", Type: topology.NodeRoom, Connect: []string{"outside", "hall"}},
		"hall":    {ID: "hall", Type: topology.NodeRoom, Connect: []string{"door"}},
	}}
	devices := device.NewRegistry()
	devices.Register([]device.DeviceConfig{{ID: "cam-a", Type: "camera", NodeID: "outside"}, {ID: "cam-b", Type: "camera", NodeID: "door"}})
	path := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(state.WithPersistencePath(path))
	engineInstance := NewEngine(topo, devices, nil)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	first := engineInstance.Analyze(&contract.Event{ID: "home-a", Type: contract.EventVisionUnknown, Source: "vision", DeviceID: "cam-a", TrackID: "12", NodeID: "outside", Timestamp: base, ReceivedAt: base.Add(3 * time.Second), Confidence: .82}, store)
	second := engineInstance.Analyze(&contract.Event{ID: "home-b", Type: contract.EventVisionUnknown, Source: "vision", DeviceID: "cam-b", TrackID: "8", NodeID: "door", Timestamp: base.Add(8 * time.Second), ReceivedAt: base.Add(9 * time.Second), Confidence: .9}, store)
	if first == nil || first.Decision == nil || first.HomeModel == nil || second.HomeModel == nil {
		t.Fatalf("Home Model was not additive to the existing decision result: first=%+v second=%+v", first, second)
	}
	if len(second.HomeModel.Entities) != 1 || len(second.HomeModel.Entities[0].Tracks) != 2 {
		t.Fatalf("expected cross-camera HomeEntity, got %+v", second.HomeModel.Entities)
	}
	raw := store.HomeModelRaw()
	if !json.Valid(raw) {
		t.Fatalf("invalid persisted Home Model: %s", raw)
	}
	reloadedStore := state.NewStore(state.WithPersistencePath(path))
	if _, err := reloadedStore.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	reloadedEngine := NewEngine(topo, devices, nil)
	if err := reloadedEngine.LoadHomeModel(reloadedStore.HomeModelRaw()); err != nil {
		t.Fatal(err)
	}
	recovered := reloadedEngine.HomeModelSnapshot()
	if recovered == nil || len(recovered.Entities) != 1 || len(recovered.Entities[0].Trajectory) != 2 {
		t.Fatalf("Home Model did not recover without replay: %+v", recovered)
	}
}
