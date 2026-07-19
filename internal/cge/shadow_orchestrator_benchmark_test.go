package cge

import (
	"context"
	"fmt"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

func BenchmarkShadowProcessObservation(b *testing.B) {
	for _, chainsCount := range []int{50, 200, 500, 1000} {
		for _, maxReevaluations := range []int{0, 4, 8} {
			for _, autoEvidence := range []bool{false, true} {
				name := fmt.Sprintf("chains=%d/reevaluations=%d/auto_evidence=%t", chainsCount, maxReevaluations, autoEvidence)
				b.Run(name, func(b *testing.B) {
					engine := benchmarkShadowEngine(b, chainsCount, maxReevaluations, autoEvidence)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						observation := chains.ObservationRef{ID: fmt.Sprintf("bench-event-%d", i), EventType: "vision.identity", Timestamp: benchmarkShadowBase.Add(time.Duration(i+chainsCount+1) * time.Second), EntityID: fmt.Sprintf("bench-entity-%d", i), DeviceID: "bench-device", NodeID: "bench-node", ActivationID: "bench-activation", TrackID: fmt.Sprintf("bench-track-%d", i), SequenceKey: fmt.Sprintf("bench-sequence-%d", i)}
						if _, err := engine.orchestrator.ProcessObservation(context.Background(), observation, observation.EventType); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}

var benchmarkShadowBase = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func benchmarkShadowEngine(b *testing.B, chainCount, maxReevaluations int, autoEvidence bool) *ShadowEngine {
	b.Helper()
	config := cognitiveShadowConfig(b.TempDir())
	config.Cognitive.AutoApplyDecisiveEvidence = autoEvidence
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: benchmarkShadowBase.Add(2 * time.Hour)}, quietShadowLogger())
	if err != nil {
		b.Fatal(err)
	}
	engine.orchestrator.config.MaxEvidenceReevaluationsPerObservation = maxReevaluations
	for i := 0; i < chainCount; i++ {
		observation := chains.ObservationRef{ID: fmt.Sprintf("bench-seed-%d", i), EventType: "vision.identity", Timestamp: benchmarkShadowBase.Add(time.Duration(i) * time.Second), EntityID: fmt.Sprintf("seed-entity-%d", i), DeviceID: fmt.Sprintf("seed-device-%d", i), NodeID: "seed-node", ActivationID: fmt.Sprintf("seed-activation-%d", i), TrackID: fmt.Sprintf("seed-track-%d", i), SequenceKey: fmt.Sprintf("seed-sequence-%d", i)}
		if _, err := engine.orchestrator.ProcessObservation(context.Background(), observation, observation.EventType); err != nil {
			b.Fatal(err)
		}
	}
	b.Cleanup(func() { _ = engine.Close() })
	return engine
}
