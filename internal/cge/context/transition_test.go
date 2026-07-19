package context

import (
	"testing"
	"time"
)

func frameForTransition(t *testing.T, id string, at time.Time, node string, topology TopologySnapshot) Frame {
	t.Helper()
	frame, err := ResolveFrame(ResolveInput{ObservationID: id, ObservedAt: at, NodeID: node, Timezone: "UTC", Occupancy: OccupancyOccupied, HouseMode: HouseModeHome, Topology: topology})
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func TestEvaluateTransitionFactsAndDistance(t *testing.T) {
	base := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	topology := testTopology()
	previous := frameForTransition(t, "previous", base, "entry", topology)
	current := frameForTransition(t, "current", base.Add(20*time.Minute), "room", topology)
	assessment, err := EvaluateTransition(previous, current, testTopology())
	if err != nil {
		t.Fatal(err)
	}
	if assessment.GraphDistance != 2 || assessment.DistanceStatus != DistanceReachable || assessment.Adjacent {
		t.Fatalf("unexpected path assessment: %#v", assessment)
	}
	if assessment.SameZone || !assessment.TemporalBucketChanged {
		t.Fatalf("unexpected transition flags: %#v", assessment)
	}
	current = frameForTransition(t, "current-2", base.Add(time.Minute), "yard", topology)
	assessment, err = EvaluateTransition(previous, current, testTopology())
	if err != nil {
		t.Fatal(err)
	}
	if !assessment.ExteriorTransition {
		t.Fatal("expected exterior transition")
	}
}

func TestEvaluateTransitionDistinguishesUnreachableAndUnknown(t *testing.T) {
	base := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	topology := testTopology()
	previous := frameForTransition(t, "previous", base, "entry", topology)
	disconnected := testTopology()
	disconnected.Nodes = append(disconnected.Nodes, Node{ID: "isolated", Kind: NodeRoom})
	disconnected.Nodes = CanonicalTopology(disconnected).Nodes
	disconnected.Edges = CanonicalTopology(disconnected).Edges
	current := frameForTransition(t, "current", base.Add(time.Minute), "isolated", disconnected)
	assessment, err := EvaluateTransition(previous, current, disconnected)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.DistanceStatus != DistanceUnreachable {
		t.Fatalf("distance status = %s", assessment.DistanceStatus)
	}
	unknown := current
	unknown.NodeID = "not-in-topology"
	unknown.Fingerprint = frameFingerprint(unknown)
	assessment, err = EvaluateTransition(previous, unknown, disconnected)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.DistanceStatus != DistanceUnknown {
		t.Fatalf("distance status = %s", assessment.DistanceStatus)
	}
}
