package context

import (
	"errors"
	"testing"
	"time"
)

func testTopology() TopologySnapshot {
	return TopologySnapshot{
		Revision: "topology-1", CapturedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		Nodes: []Node{
			{ID: "entry", Kind: NodeEntrance, EntryPoint: true},
			{ID: "hall", Kind: NodeCorridor, ZoneID: "ground"},
			{ID: "room", ParentID: "hall", ZoneID: "ground", Kind: NodeRoom},
			{ID: "yard", Kind: NodeExterior, Exterior: true},
		},
		Edges: []Edge{
			{From: "entry", To: "hall", TraversalKind: TraversalDoor},
			{From: "hall", To: "room", TraversalKind: TraversalWalk},
			{From: "hall", To: "yard", TraversalKind: TraversalExterior},
		},
	}
}

func TestTopologyValidationAndDefensiveCopy(t *testing.T) {
	topology := testTopology()
	if err := topology.Validate(); err != nil {
		t.Fatal(err)
	}
	copy := topology.Clone()
	copy.Nodes[0].ID = "changed"
	if topology.Nodes[0].ID == copy.Nodes[0].ID {
		t.Fatal("topology clone aliases nodes")
	}
	bad := topology
	bad.Nodes = append([]Node(nil), topology.Nodes...)
	bad.Nodes[1].ParentID = "missing"
	if err := bad.Validate(); !errors.Is(err, ErrInvalidTopology) {
		t.Fatalf("missing parent error = %v", err)
	}
	bad = topology
	bad.Nodes = append([]Node(nil), topology.Nodes...)
	bad.Nodes[1].ParentID = "room"
	bad.Nodes[2].ParentID = "hall"
	if err := bad.Validate(); !errors.Is(err, ErrInvalidTopology) {
		t.Fatalf("cycle error = %v", err)
	}
	bad = topology
	bad.Edges = append([]Edge(nil), topology.Edges...)
	bad.Edges = append(bad.Edges, Edge{From: "entry", To: "entry", TraversalKind: TraversalWalk})
	if err := bad.Validate(); !errors.Is(err, ErrInvalidTopology) {
		t.Fatalf("self edge error = %v", err)
	}
	bad = topology
	bad.Edges = []Edge{{From: "room", To: "hall", TraversalKind: TraversalWalk}, topology.Edges[0], topology.Edges[2]}
	if err := bad.Validate(); !errors.Is(err, ErrInvalidTopology) {
		t.Fatalf("canonical order error = %v", err)
	}
}

func TestTopologyAllowsMovementCyclesAndDisconnectedNodes(t *testing.T) {
	topology := TopologySnapshot{Revision: "cycle", CapturedAt: time.Now().UTC(), Nodes: []Node{{ID: "a", Kind: NodeRoom}, {ID: "b", Kind: NodeRoom}, {ID: "z", Kind: NodeRoom}}, Edges: []Edge{{From: "a", To: "b", TraversalKind: TraversalWalk}, {From: "b", To: "a", Directed: true, TraversalKind: TraversalWalk}}}
	if err := topology.Validate(); err != nil {
		t.Fatal(err)
	}
}
