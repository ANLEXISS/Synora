package episodes

import (
	"fmt"
	"testing"
	"time"
)

func benchmarkRegistry(b *testing.B, count int) (*Registry, Policy) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	for i := 0; i < count; i++ {
		value := observation(fmt.Sprintf("bench-%d", i), testBase.Add(time.Duration(i)*time.Second), known(fmt.Sprintf("resident-%d", i)), fmt.Sprintf("node-%d", i), "zone", fmt.Sprintf("track-%d", i))
		ingest(b, registry, value, nil, policy)
	}
	return registry, policy
}

func BenchmarkPlanIngest10(b *testing.B)  { benchmarkPlan(b, 10) }
func BenchmarkPlanIngest100(b *testing.B) { benchmarkPlan(b, 100) }
func benchmarkPlan(b *testing.B, count int) {
	registry, policy := benchmarkRegistry(b, count)
	value := observation("bench-target", testBase.Add(time.Minute), known("resident-target"), "node-target", "zone", "track-target")
	snapshot := registry.Snapshot()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = PlanIngest(snapshot, value, nil, policy)
	}
}

func BenchmarkApplyIngestPlan(b *testing.B) {
	policy := DefaultPolicy()
	registry := NewRegistryWithPolicy(policy)
	for i := 0; i < b.N; i++ {
		value := observation(fmt.Sprintf("apply-%d", i), testBase.Add(time.Duration(i)*time.Second), known("resident-a"), "entry", "ground", "track-a")
		plan, _ := PlanIngest(registry.Snapshot(), value, nil, policy)
		_, _ = registry.ApplyIngestPlan(plan, value, value.ObservedAt)
	}
}
func BenchmarkSnapshot(b *testing.B) {
	registry, _ := benchmarkRegistry(b, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry.Snapshot()
	}
}
func BenchmarkRegistryDigest(b *testing.B) {
	registry, _ := benchmarkRegistry(b, 100)
	snapshot := registry.Snapshot()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RegistryDigest(snapshot)
	}
}
func BenchmarkEvaluateLifecycle(b *testing.B) {
	registry, policy := benchmarkRegistry(b, 100)
	snapshot := registry.Snapshot()
	at := testBase.Add(time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EvaluateLifecycle(snapshot, at, policy)
	}
}
