package hypotheses

import (
	"fmt"
	"testing"

	"synora/internal/cge/chains"
)

func benchmarkIndexedRegistry(b *testing.B, count int) *Registry {
	b.Helper()
	registry := NewRegistry()
	for i := 0; i < count; i++ {
		evaluation := ambiguousEvidenceEvaluation()
		evaluation.ChainID = chains.ChainID(fmt.Sprintf("chain-%04d", i))
		evaluation.TargetObservationID = fmt.Sprintf("observation-%04d", i)
		set, err := FromAmbiguousEvidence(evaluation, hypothesisTestBase, conversionMutation(hypothesisTestBase))
		if err != nil {
			b.Fatal(err)
		}
		if err := registry.Add(set); err != nil {
			b.Fatal(err)
		}
	}
	return registry
}

func BenchmarkRegistryFindCurrentEvidenceSubject(b *testing.B) {
	for _, size := range []int{50, 200, 500, 1000} {
		b.Run(fmt.Sprintf("hypotheses=%d", size), func(b *testing.B) {
			registry := benchmarkIndexedRegistry(b, size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, found, err := registry.FindCurrentEvidenceSubject(chains.ChainID(fmt.Sprintf("chain-%04d", i%size)), fmt.Sprintf("observation-%04d", i%size)); err != nil || !found {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRegistryListOpenEvidenceForChain(b *testing.B) {
	for _, size := range []int{50, 200, 500, 1000} {
		b.Run(fmt.Sprintf("hypotheses=%d", size), func(b *testing.B) {
			registry := benchmarkIndexedRegistry(b, size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := registry.ListOpenEvidenceForChain(chains.ChainID(fmt.Sprintf("chain-%04d", i%size))); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
