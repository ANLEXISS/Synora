package demo

import (
	"testing"
	"time"

	cgecontext "synora/internal/cge/context"
)

func TestDemoTopologyResolvesAllNodes(t *testing.T) {
	top := topology(time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	if err := top.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, node := range top.Nodes {
		frame, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: "demo-test", ObservedAt: top.CapturedAt, NodeID: node.ID, Timezone: "Europe/Paris", Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome, Topology: top, AllowPartial: true})
		if err != nil || frame.Quality != cgecontext.QualityComplete {
			t.Fatalf("node=%s quality=%s err=%v", node.ID, frame.Quality, err)
		}
	}
}
