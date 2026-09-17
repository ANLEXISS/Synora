package homemodel

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkHomeModelObserveSynthetic(b *testing.B) {
	scenario := ReferenceScenarios()[0]
	model := New(scenario.Topology, DefaultConfig())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		at := time.Date(2026, 9, 1, 18, 0, 0, 0, time.UTC).Add(time.Duration(i%120) * time.Second)
		_, _ = model.Observe(observationEvent(fmt.Sprintf("bench-%d", i), "cam-bench", fmt.Sprintf("track-%d", i%8), "front_door", at, .8))
	}
}
