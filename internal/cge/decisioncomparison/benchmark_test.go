package decisioncomparison

import (
	"testing"

	"synora/internal/cge/cognitivesituation"
)

func BenchmarkCompareContinuityAligned(b *testing.B) {
	situation := comparisonSituation(b, cognitivesituation.PhaseObserving)
	set := comparisonRecommendations(b, situation, nil)
	input := CompareInput{Historical: historicalRef(false), Situation: situation, Recommendations: set}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compare(input, DefaultPolicy())
	}
}

func BenchmarkCompareTransitionAligned(b *testing.B) {
	situation := comparisonSituation(b, cognitivesituation.PhaseObserving)
	set := comparisonRecommendations(b, situation, nil)
	input := CompareInput{Historical: historicalRef(true), Situation: situation, Recommendations: set}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compare(input, DefaultPolicy())
	}
}

func BenchmarkCompareCognitiveConservative(b *testing.B) {
	situation := comparisonSituation(b, cognitivesituation.PhaseAmbiguous)
	set := comparisonRecommendations(b, situation, nil)
	input := CompareInput{Historical: historicalRef(false), Situation: situation, Recommendations: set}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compare(input, DefaultPolicy())
	}
}

func BenchmarkCompareIncomparable(b *testing.B) {
	situation := comparisonSituation(b, cognitivesituation.PhaseObserving)
	set := comparisonRecommendations(b, situation, nil)
	historical := historicalRef(false)
	historical.CurrentStateCode = ""
	historical.Fingerprint = HistoricalDecisionFingerprint(historical)
	input := CompareInput{Historical: historical, Situation: situation, Recommendations: set}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compare(input, DefaultPolicy())
	}
}

func BenchmarkExplain(b *testing.B) {
	situation := comparisonSituation(b, cognitivesituation.PhaseObserving)
	set := comparisonRecommendations(b, situation, nil)
	value, _ := Compare(CompareInput{Historical: historicalRef(false), Situation: situation, Recommendations: set}, DefaultPolicy())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Explain(value, DefaultPolicy())
	}
}
