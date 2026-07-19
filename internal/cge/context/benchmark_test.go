package context

import (
	"fmt"
	"testing"
	"time"
)

func benchmarkTopology(size int) TopologySnapshot {
	nodes := make([]Node, size)
	edges := make([]Edge, 0, size-1)
	for i := range nodes {
		id := fmt.Sprintf("node-%04d", i)
		nodes[i] = Node{ID: id, Kind: NodeRoom, ZoneID: "zone-1"}
		if i > 0 {
			edges = append(edges, Edge{From: fmt.Sprintf("node-%04d", i-1), To: id, TraversalKind: TraversalWalk})
		}
	}
	return TopologySnapshot{Revision: fmt.Sprintf("benchmark-%d", size), CapturedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC), Nodes: nodes, Edges: edges}
}

func BenchmarkResolveFrame(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500} {
		b.Run(fmt.Sprintf("nodes_%d", size), func(b *testing.B) {
			input := ResolveInput{ObservationID: "benchmark-observation", ObservedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC), NodeID: fmt.Sprintf("node-%04d", size/2), Timezone: "Europe/Paris", Occupancy: OccupancyUnknown, HouseMode: HouseModeUnknown, Topology: benchmarkTopology(size)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = ResolveFrame(input)
			}
		})
	}
}

func BenchmarkFrameSignature(b *testing.B) {
	frame, err := ResolveFrame(ResolveInput{ObservationID: "benchmark-observation", ObservedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC), NodeID: "node-0005", Timezone: "UTC", Topology: benchmarkTopology(10)})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = FrameSignature(frame)
	}
}

func BenchmarkEvaluateTransition(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500} {
		b.Run(fmt.Sprintf("nodes_%d", size), func(b *testing.B) {
			topology := benchmarkTopology(size)
			previous, _ := ResolveFrame(ResolveInput{ObservationID: "previous", ObservedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC), NodeID: "node-0000", Timezone: "UTC", Topology: topology})
			current, _ := ResolveFrame(ResolveInput{ObservationID: "current", ObservedAt: time.Date(2026, 7, 18, 12, 1, 0, 0, time.UTC), NodeID: fmt.Sprintf("node-%04d", size-1), Timezone: "UTC", Topology: topology})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = EvaluateTransition(previous, current, topology)
			}
		})
	}
}

func BenchmarkShortestPath(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500} {
		b.Run(fmt.Sprintf("nodes_%d", size), func(b *testing.B) {
			topology := benchmarkTopology(size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, _ = ShortestPath(topology, "node-0000", fmt.Sprintf("node-%04d", size-1))
			}
		})
	}
}
