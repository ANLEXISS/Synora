package cge

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// BenchmarkShadowObserveContext measures the boundary adapter plus the
// existing orchestrator. It intentionally uses a partial static provider and
// never invokes resolution, lifecycle, actions or the historical engine.
func BenchmarkShadowObserveContext(b *testing.B) {
	for _, chainCount := range []int{50, 200, 500, 1000} {
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("chains=%d/context=%t", chainCount, enabled), func(b *testing.B) {
				config := cognitiveShadowConfig(b.TempDir())
				config.Context.Enabled = enabled
				engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: benchmarkShadowBase.Add(time.Hour)}, quietShadowLogger())
				if err != nil {
					b.Fatal(err)
				}
				for i := 0; i < chainCount; i++ {
					adapted, err := AdaptEventWithAllowlist(shadowEvent(fmt.Sprintf("seed-%d", i), "vision.identity", benchmarkShadowBase.Add(time.Duration(i)*time.Second)), DefaultEligibleEventTypes())
					if err != nil {
						b.Fatal(err)
					}
					adapted.Input.Observation.SequenceKey = fmt.Sprintf("seed-sequence-%d", i)
					adapted.Input.Observation.TrackID = fmt.Sprintf("seed-track-%d", i)
					if _, err := engine.orchestrator.ProcessObservation(context.Background(), adapted.Input.Observation, "vision.identity"); err != nil {
						b.Fatal(err)
					}
				}
				b.Cleanup(func() { _ = engine.Close() })
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					event := shadowEvent(fmt.Sprintf("context-bench-%d", i), "vision.identity", benchmarkShadowBase.Add(time.Duration(chainCount+i+1)*time.Second))
					if _, err := engine.Observe(context.Background(), event); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
