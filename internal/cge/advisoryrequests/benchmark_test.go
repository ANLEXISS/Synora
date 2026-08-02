package advisoryrequests

import (
	"testing"
	"time"

	"synora/internal/cge/evidencediscrimination"
)

func benchmarkAssessment(count int) evidencediscrimination.DiscriminationAssessment {
	candidates := make([]evidencediscrimination.EvidenceCandidate, 0, count)
	kinds := evidencediscrimination.AllCandidateKinds()
	for i := 0; i < count; i++ {
		candidate := testCandidate("benchmark-candidate-"+itoa(i), kinds[i%len(kinds)], 800-i%200, 700-i%150, 400-i%100, i%500, evidencediscrimination.SensitivityLow)
		candidates = append(candidates, candidate)
	}
	return testAssessment(candidates, true, true, 1)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	if value < 0 {
		return "-" + itoa(-value)
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}

func BenchmarkPlan1Candidate(b *testing.B)   { benchmarkPlan(b, 1) }
func BenchmarkPlan8Candidates(b *testing.B)  { benchmarkPlan(b, 8) }
func BenchmarkPlan32Candidates(b *testing.B) { benchmarkPlan(b, 32) }

func benchmarkPlan(b *testing.B, count int) {
	a := benchmarkAssessment(count)
	p := DefaultPolicy()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Plan(PlanInput{Assessment: a, EvaluatedAt: testTime}, p); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUpdateExistingRequest(b *testing.B) {
	r := NewRegistry()
	a := testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("update", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1)
	plan, _ := Plan(PlanInput{Assessment: a, EvaluatedAt: testTime}, DefaultPolicy())
	_, _ = r.ApplyPlan(plan)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		candidate := a.Candidates[0]
		candidate.UtilityPermille = 800 + i%100
		candidate.Fingerprint = evidencediscrimination.CandidateFingerprint(candidate)
		assessment := testAssessment([]evidencediscrimination.EvidenceCandidate{candidate}, true, true, uint64(i+2))
		if _, err := Plan(PlanInput{Assessment: assessment, RegistrySnapshot: r.Snapshot(), EvaluatedAt: testTime.Add(time.Duration(i+1) * time.Second)}, DefaultPolicy()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyPlan(b *testing.B) {
	a := testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("apply", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1)
	p := DefaultPolicy()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := NewRegistry()
		plan, _ := Plan(PlanInput{Assessment: a, EvaluatedAt: testTime}, p)
		if _, err := r.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSatisfyRequest(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := NewRegistry()
		a := testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("satisfy", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1)
		plan, _ := Plan(PlanInput{Assessment: a, EvaluatedAt: testTime}, DefaultPolicy())
		_, _ = r.ApplyPlan(plan)
		missing := testAssessment(nil, false, false, 2)
		plan, _ = Plan(PlanInput{Assessment: missing, RegistrySnapshot: r.Snapshot(), EvaluatedAt: testTime.Add(time.Minute)}, DefaultPolicy())
		if _, err := r.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExpireRequest(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := NewRegistry()
		a := testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("expire", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1)
		plan, _ := Plan(PlanInput{Assessment: a, EvaluatedAt: testTime}, DefaultPolicy())
		_, _ = r.ApplyPlan(plan)
		missing := testAssessment(nil, true, true, 2)
		plan, _ = Plan(PlanInput{Assessment: missing, RegistrySnapshot: r.Snapshot(), EvaluatedAt: testTime.Add(16 * time.Minute)}, DefaultPolicy())
		if _, err := r.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCreateNewGeneration(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := NewRegistry()
		a := testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("generation", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1)
		plan, _ := Plan(PlanInput{Assessment: a, EvaluatedAt: testTime}, DefaultPolicy())
		_, _ = r.ApplyPlan(plan)
		missing := testAssessment(nil, true, true, 2)
		plan, _ = Plan(PlanInput{Assessment: missing, RegistrySnapshot: r.Snapshot(), EvaluatedAt: testTime.Add(16 * time.Minute)}, DefaultPolicy())
		_, _ = r.ApplyPlan(plan)
		plan, _ = Plan(PlanInput{Assessment: a, RegistrySnapshot: r.Snapshot(), EvaluatedAt: testTime.Add(17 * time.Minute)}, DefaultPolicy())
		if _, err := r.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyDisposition(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := NewRegistry()
		a := testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("disposition", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1)
		plan, _ := Plan(PlanInput{Assessment: a, EvaluatedAt: testTime}, DefaultPolicy())
		_, _ = r.ApplyPlan(plan)
		request := r.GetByEpisode("episode-test")[0]
		if _, err := r.ApplyDisposition(AdvisoryDisposition{RequestID: request.ID, Kind: DispositionAcknowledge, Actor: "benchmark", At: testTime.Add(time.Second), SourceRevision: request.Revision}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublicSnapshot100(b *testing.B)  { benchmarkSnapshot(b, 100) }
func BenchmarkPublicSnapshot1000(b *testing.B) { benchmarkSnapshot(b, 1000) }

func benchmarkSnapshot(b *testing.B, count int) {
	r := NewRegistry()
	for i := 0; i < count; i++ {
		candidate := testCandidate("snapshot-"+itoa(i), evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)
		candidate.EpisodeID = "episode-" + itoa(i)
		candidate.Dimension = evidencediscrimination.EvidenceDimension("dimension-" + itoa(i))
		candidate.Fingerprint = evidencediscrimination.CandidateFingerprint(candidate)
		a := testAssessment([]evidencediscrimination.EvidenceCandidate{candidate}, true, true, uint64(i+1))
		a.EpisodeID = candidate.EpisodeID
		a.Fingerprint = evidencediscrimination.AssessmentFingerprint(a)
		plan, _ := Plan(PlanInput{Assessment: a, RegistrySnapshot: r.Snapshot(), EvaluatedAt: testTime}, DefaultPolicy())
		if _, err := r.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Snapshot()
	}
}

func BenchmarkRegistryDigest(b *testing.B) {
	r := NewRegistry()
	plan, _ := Plan(PlanInput{Assessment: testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("digest", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1), EvaluatedAt: testTime}, DefaultPolicy())
	_, _ = r.ApplyPlan(plan)
	s := r.Snapshot()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = RegistryDigest(s)
	}
}

func BenchmarkExplain(b *testing.B) {
	plan, _ := Plan(PlanInput{Assessment: testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("explain", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1), EvaluatedAt: testTime}, DefaultPolicy())
	r := plan.ResultingRequests[0]
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = Explain(r)
	}
}
