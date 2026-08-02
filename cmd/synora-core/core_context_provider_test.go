package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	cgecontext "synora/internal/cge/context"
	"synora/internal/device"
	"synora/internal/state"
	"synora/internal/topology"
)

func TestBuildCoreContextSnapshotRedactsAndClassifiesLiveFacts(t *testing.T) {
	at := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	source := state.ContextSourceSnapshot{
		Presence: []state.PresenceState{
			{ID: "presence-sensitive", ResidentID: "SENSITIVE-RESIDENT-ID", State: "present", Location: "entry", Confidence: .91, LastSeen: at},
			{ID: "presence-unknown", ResidentID: "resident-unknown", State: "", LastSeen: time.Time{}},
		},
		Devices: []state.DeviceState{
			{ID: "SENSITIVE-DEVICE-ID", Type: "sensor", NodeID: "entry", Online: true, LastSeen: at},
			{ID: "device-stale", Type: "sensor", NodeID: "hall", Online: false, LastSeen: at.Add(-20 * time.Minute)},
		},
		Cameras: []state.CameraState{{ID: "SENSITIVE-CAMERA-ID", NodeID: "entry", Online: true, LastSeen: at}},
		System:  state.ContextSystemState{LastState: "idle", SecurityMode: "home"},
	}
	topology := cgecontext.CoreTopologyContext{
		Revision: "topology-revision",
		Nodes:    []cgecontext.Node{{ID: "hall", Kind: cgecontext.NodeRoom}, {ID: "entry", Kind: cgecontext.NodeEntrance, EntryPoint: true}},
		Edges:    []cgecontext.Edge{{From: "hall", To: "entry", Directed: false, TraversalKind: cgecontext.TraversalUnknown}},
	}
	snapshot := buildCoreContextSnapshot(at, source, []string{"resident-present", "resident-unknown"}, []device.DeviceConfig{{ID: "SENSITIVE-DEVICE-ID", Type: "sensor", NodeID: "entry", Secret: "SENSITIVE-TOKEN", Network: map[string]any{"ip": "SENSITIVE-IP"}}}, topology, cgecontext.DefaultContextFreshnessPolicy())
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("snapshot validation: %v", err)
	}
	if snapshot.Freshness.Overall != cgecontext.FreshnessStale || snapshot.Freshness.Residents != cgecontext.FreshnessUnknown || snapshot.Freshness.Devices != cgecontext.FreshnessStale {
		t.Fatalf("freshness did not preserve unknown/stale distinction: %+v", snapshot.Freshness)
	}
	encoded, _ := json.Marshal(snapshot)
	for _, sentinel := range []string{"SENSITIVE-RESIDENT-ID", "SENSITIVE-DEVICE-ID", "SENSITIVE-CAMERA-ID", "SENSITIVE-IP", "SENSITIVE-TOKEN"} {
		if strings.Contains(string(encoded), sentinel) {
			t.Fatalf("context snapshot leaked %q: %s", sentinel, encoded)
		}
	}
	for _, resident := range snapshot.Residents {
		if resident.ResidentFingerprint == "SENSITIVE-RESIDENT-ID" || strings.Contains(resident.ResidentFingerprint, "SENSITIVE") {
			t.Fatalf("resident redaction failed: %+v", resident)
		}
	}
	if snapshot.Topology.Fingerprint == "" || snapshot.Topology.Revision == "" {
		t.Fatalf("topology fingerprint missing: %+v", snapshot.Topology)
	}
}

func TestCoreTopologySnapshotIsCanonicalAndRequestNeighborsAreBounded(t *testing.T) {
	topology := testCoreTopology()
	value := coreTopologySnapshot(topology)
	value.ObservationNode = "entry"
	value.ImmediateNeighbors = topologyNeighbors(value, "entry")
	if len(value.Nodes) != 3 || len(value.Edges) != 2 || len(value.ImmediateNeighbors) != 2 {
		t.Fatalf("unexpected topology projection: %+v", value)
	}
	if value.Nodes[0].ID > value.Nodes[1].ID || value.Nodes[1].ID > value.Nodes[2].ID {
		t.Fatalf("topology nodes are not canonical: %+v", value.Nodes)
	}
	if value.Fingerprint == "" || value.Revision == "" {
		t.Fatalf("topology identity missing: %+v", value)
	}
}

func testCoreTopology() *topology.Topology {
	entry := &topology.Node{ID: "entry", Type: topology.NodeRoom, Connect: []string{"hall", "kitchen"}}
	hall := &topology.Node{ID: "hall", Type: topology.NodeRoom, Connect: []string{"entry"}}
	kitchen := &topology.Node{ID: "kitchen", Type: topology.NodeRoom, Connect: []string{"entry"}}
	return &topology.Topology{Nodes: map[string]*topology.Node{entry.ID: entry, hall.ID: hall, kitchen.ID: kitchen}}
}
