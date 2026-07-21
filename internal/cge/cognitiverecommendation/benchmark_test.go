package cognitiverecommendation

import (
	"testing"

	"synora/internal/cge/cognitivesituation"
)

func BenchmarkPlanObserving(b *testing.B) {
	situation := observingSituation(b)
	policy := DefaultPolicy()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Plan(PlanInput{Situation: situation}, policy)
	}
}

func BenchmarkPlanCoherent(b *testing.B) {
	situation := coherentSituation(b)
	policy := DefaultPolicy()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Plan(PlanInput{Situation: situation}, policy)
	}
}

func BenchmarkPlanAmbiguous(b *testing.B) {
	situation := coherentSituation(b)
	situation.Phase = cognitivesituation.PhaseAmbiguous
	situation.Hypotheses.Ambiguous = true
	situation.Fingerprint = cognitivesituation.SituationFingerprint(situation)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Plan(PlanInput{Situation: situation}, DefaultPolicy())
	}
}

func BenchmarkCompareIdentical(b *testing.B) {
	situation := coherentSituation(b)
	set, _ := Plan(PlanInput{Situation: situation}, DefaultPolicy())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Compare(set, set)
	}
}

func BenchmarkExplain(b *testing.B) {
	situation := observingSituation(b)
	set, _ := Plan(PlanInput{Situation: situation}, DefaultPolicy())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Explain(set.Recommendations[0], DefaultPolicy())
	}
}
