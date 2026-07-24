package evidencediscrimination

import (
	"testing"
	"time"

	"synora/internal/cge/episodes"
	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

func benchmarkSet(b *testing.B, count int, id string, revision uint64) (situationfacts.FactSet, situationhypotheses.CompetingHypothesisSet) {
	b.Helper()
	values := make([]episodes.ObservationRef, count)
	for i := range values {
		values[i] = evidenceObservation("bench-"+id+"-"+string(rune('a'+i%26)), evidenceBase.Add(time.Duration(i)*time.Second), unknownEvidence(), "node", "track")
		values[i].EventID = "bench-" + id + "-" + formatInt(i)
	}
	set := evidenceSet(b, "episode-"+id, values, nil, revision)
	return set, evaluateEvidence(b, set)
}

func formatInt(value int) string {
	if value == 0 {
		return "0"
	}
	buf := [24]byte{}
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}

func BenchmarkAnalyzeSmall(b *testing.B)   { benchmarkAnalyze(b, 2) }
func BenchmarkAnalyzeMedium(b *testing.B)  { benchmarkAnalyze(b, 8) }
func BenchmarkAnalyzeMaximal(b *testing.B) { benchmarkAnalyze(b, 32) }
func benchmarkAnalyze(b *testing.B, observations int) {
	set, hypotheses := benchmarkSet(b, observations, "analyze-"+formatInt(observations), 1)
	catalog, policy := Catalog(), DefaultPolicy()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: hypotheses, HypothesisSchema: situationhypotheses.Schema()}, catalog, policy); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReevaluateFromDiffAdded(b *testing.B) {
	previous, previousHyp := benchmarkSet(b, 8, "diff-before", 1)
	values := make([]episodes.ObservationRef, 8)
	for i := range values {
		values[i] = evidenceObservation("diff-"+formatInt(i), evidenceBase.Add(time.Duration(i)*time.Second), unknownEvidence(), "node", "track")
	}
	values = append(values, evidenceObservation("diff-8", evidenceBase.Add(8*time.Second), unknownEvidence(), "node", "track"))
	current := evidenceSet(b, "episode-diff-before", values, nil, 2)
	currentHyp := evaluateEvidence(b, current)
	factDiff, err := situationfacts.Diff(previous, current)
	if err != nil {
		b.Fatal(err)
	}
	assessment, err := Analyze(AnalysisInput{FactSet: previous, HypothesisSet: previousHyp, HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ReevaluateFromDiff(previous, current, factDiff, previousHyp, currentHyp, assessment, Catalog(), DefaultPolicy()); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkFullAnalysisEquivalent(b *testing.B) {
	set, hypotheses := benchmarkSet(b, 9, "full", 2)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: hypotheses, HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy()); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkBuildCandidates(b *testing.B) {
	set, hypotheses := benchmarkSet(b, 8, "build", 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: hypotheses, HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy()); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkScoreCandidate(b *testing.B) { BenchmarkBuildCandidates(b) }
func BenchmarkRankCandidates(b *testing.B) { BenchmarkBuildCandidates(b) }

func benchmarkRegistry(b *testing.B, count int) *Registry {
	r := NewRegistry()
	for i := 0; i < count; i++ {
		id := "registry-" + formatInt(i)
		set, hypotheses := benchmarkSet(b, 2, id, 1)
		plan, err := Plan(set, hypotheses, r.Snapshot(), Catalog(), DefaultPolicy())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := r.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
	return r
}
func BenchmarkApplyPlan(b *testing.B) {
	set, hypotheses := benchmarkSet(b, 2, "apply", 1)
	r := NewRegistry()
	for i := 0; i < b.N; i++ {
		snapshot := r.Snapshot()
		plan, err := Plan(set, hypotheses, snapshot, Catalog(), DefaultPolicy())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := r.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkExplain(b *testing.B) {
	set, hypotheses := benchmarkSet(b, 8, "explain", 1)
	a, err := Analyze(AnalysisInput{FactSet: set, HypothesisSet: hypotheses, HypothesisSchema: situationhypotheses.Schema()}, Catalog(), DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	if len(a.Candidates) == 0 {
		b.Skip("no descriptive candidate")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Explain(a.Candidates[0]); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkPublicSnapshot10(b *testing.B) {
	r := benchmarkRegistry(b, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Snapshot()
	}
}
func BenchmarkPublicSnapshot100(b *testing.B) {
	r := benchmarkRegistry(b, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Snapshot()
	}
}
func BenchmarkRegistryDigest(b *testing.B) {
	r := benchmarkRegistry(b, 10)
	snapshot := r.Snapshot()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RegistryDigest(snapshot)
	}
}
