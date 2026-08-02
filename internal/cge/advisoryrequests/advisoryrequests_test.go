package advisoryrequests

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"

	"synora/internal/cge/evidencediscrimination"
)

var testTime = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func testCandidate(id string, kind evidencediscrimination.EvidenceCandidateKind, utility, discrimination, coverage, redundancy int, sensitivity evidencediscrimination.EvidenceSensitivityClass) evidencediscrimination.EvidenceCandidate {
	c := evidencediscrimination.EvidenceCandidate{
		ID: evidencediscrimination.EvidenceCandidateID(id), EpisodeID: "episode-test", Kind: kind, Dimension: evidencediscrimination.DimensionDomesticContext,
		Discriminates:          []evidencediscrimination.HypothesisPair{{First: "hyp-a", Second: "hyp-b"}},
		DiscriminationPermille: discrimination, CoverageGainPermille: coverage, RedundancyPermille: redundancy, UtilityPermille: utility,
		CostClass: evidencediscrimination.CostLow, LatencyClass: evidencediscrimination.LatencyImmediate, SensitivityClass: sensitivity,
		ReasonCodes: []string{"dimension_unresolved"}, SourceFactSetFingerprint: "facts-test", SourceHypothesisSetFingerprint: "hypotheses-test",
	}
	c.Fingerprint = evidencediscrimination.CandidateFingerprint(c)
	return c
}

func testAssessment(candidates []evidencediscrimination.EvidenceCandidate, useful, ambiguous bool, revision uint64) evidencediscrimination.DiscriminationAssessment {
	a := evidencediscrimination.DiscriminationAssessment{EpisodeID: "episode-test", SourceFactSetFingerprint: "facts-test", SourceHypothesisSetFingerprint: "hypotheses-test", Candidates: candidates, EvidenceUseful: useful, AmbiguityRelevant: ambiguous, Revision: revision}
	if len(candidates) > 0 {
		a.BestCandidateID = candidates[0].ID
	}
	a.Fingerprint = evidencediscrimination.AssessmentFingerprint(a)
	return a
}

func planFor(t *testing.T, r *Registry, a evidencediscrimination.DiscriminationAssessment, at time.Time) AdvisoryPlan {
	t.Helper()
	plan, err := Plan(PlanInput{Assessment: a, RegistrySnapshot: r.Snapshot(), EvaluatedAt: at}, r.policy)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func applyFor(t *testing.T, r *Registry, plan AdvisoryPlan) ApplyResult {
	t.Helper()
	result, err := r.ApplyPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPlanUsefulCandidateAndIdempotence(t *testing.T) {
	r := NewRegistry()
	a := testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("candidate-a", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1)
	plan := planFor(t, r, a, testTime)
	if len(plan.Creates) != 1 || len(plan.ResultingRequests) != 1 {
		t.Fatalf("creates=%d resulting=%d", len(plan.Creates), len(plan.ResultingRequests))
	}
	request := plan.ResultingRequests[0]
	if request.Status != StatusProposed || request.Generation != 1 || request.AdvisoryRank != 1 {
		t.Fatalf("unexpected request: %+v", request)
	}
	if !allFlags(request) {
		t.Fatal("mandatory advisory flags are not all true")
	}
	first := applyFor(t, r, plan)
	if !first.Applied || first.RegistryRevision != 1 {
		t.Fatalf("unexpected apply result: %+v", first)
	}
	digestBefore := r.Snapshot().Digest
	repeat := planFor(t, r, a, testTime)
	second := applyFor(t, r, repeat)
	if !second.Idempotent || second.RegistryRevision != 1 || r.Snapshot().Digest != digestBefore {
		t.Fatal("identical plan was not idempotent")
	}
}

func TestMultipleCandidatesRankingLimitsAndPreferred(t *testing.T) {
	r := NewRegistry()
	a := testAssessment([]evidencediscrimination.EvidenceCandidate{
		testCandidate("candidate-a", evidencediscrimination.KindContextConfirmation, 900, 800, 500, 50, evidencediscrimination.SensitivityLow),
		testCandidate("candidate-b", evidencediscrimination.KindPatternAlignmentConfirmation, 700, 700, 400, 100, evidencediscrimination.SensitivityLow),
		testCandidate("candidate-c", evidencediscrimination.KindEntityCountConfirmation, 600, 600, 300, 100, evidencediscrimination.SensitivityLow),
	}, true, true, 1)
	plan := planFor(t, r, a, testTime)
	if len(plan.Creates) != 3 || plan.PreferredRequestID == "" || plan.PreferredMarginPermille != 200 {
		t.Fatalf("unexpected ranking: preferred=%q margin=%d creates=%d", plan.PreferredRequestID, plan.PreferredMarginPermille, len(plan.Creates))
	}
	p := DefaultPolicy()
	p.MaxActiveRequestsPerEpisode = 2
	limitedRegistry := NewRegistryWithPolicy(p)
	limited, err := Plan(PlanInput{Assessment: a, RegistrySnapshot: limitedRegistry.Snapshot(), EvaluatedAt: testTime}, p)
	if err != nil {
		t.Fatal(err)
	}
	activeCount := 0
	for _, request := range limited.ResultingRequests {
		if active(request.Status) {
			activeCount++
		}
	}
	if activeCount != 2 {
		t.Fatalf("active limit ignored: %d", activeCount)
	}
}

func TestSuppressionSensitivityAndRedundancy(t *testing.T) {
	for name, candidate := range map[string]evidencediscrimination.EvidenceCandidate{
		"threshold":   testCandidate("threshold", evidencediscrimination.KindContextConfirmation, 100, 100, 50, 100, evidencediscrimination.SensitivityLow),
		"redundancy":  testCandidate("redundancy", evidencediscrimination.KindContextConfirmation, 800, 800, 500, 900, evidencediscrimination.SensitivityLow),
		"sensitivity": testCandidate("sensitivity", evidencediscrimination.KindContextConfirmation, 800, 800, 500, 100, evidencediscrimination.SensitivityHigh),
	} {
		t.Run(name, func(t *testing.T) {
			r := NewRegistry()
			plan := planFor(t, r, testAssessment([]evidencediscrimination.EvidenceCandidate{candidate}, true, true, 1), testTime)
			if len(plan.ResultingRequests) != 1 || plan.ResultingRequests[0].Status != StatusSuppressed {
				t.Fatalf("expected suppressed: %+v", plan.ResultingRequests)
			}
			if plan.ResultingRequests[0].AdvisoryRank != 0 {
				t.Fatal("suppressed request received active rank")
			}
		})
	}
	p := DefaultPolicy()
	p.IncludeHighSensitivityRequests = true
	plan, err := Plan(PlanInput{Assessment: testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("sensitivity", evidencediscrimination.KindContextConfirmation, 800, 800, 500, 100, evidencediscrimination.SensitivityHigh)}, true, true, 1), EvaluatedAt: testTime}, p)
	if err != nil || len(plan.ResultingRequests) != 1 || plan.ResultingRequests[0].Status != StatusProposed || !plan.ResultingRequests[0].Flags.RequiresExternalAuthorization {
		t.Fatalf("high sensitivity opt-in failed: %v %+v", err, plan)
	}
}

func TestUpdateSatisfactionExpirationAndGeneration(t *testing.T) {
	r := NewRegistry()
	c := testCandidate("candidate-a", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)
	applyFor(t, r, planFor(t, r, testAssessment([]evidencediscrimination.EvidenceCandidate{c}, true, true, 1), testTime))
	c.UtilityPermille = 850
	c.Fingerprint = evidencediscrimination.CandidateFingerprint(c)
	updated := applyFor(t, r, planFor(t, r, testAssessment([]evidencediscrimination.EvidenceCandidate{c}, true, true, 2), testTime.Add(time.Minute)))
	if updated.After[0].Generation != 1 || updated.After[0].Revision != 2 || updated.After[0].UtilityPermille != 850 {
		t.Fatalf("candidate update did not preserve occurrence: %+v", updated.After)
	}
	missing := testAssessment(nil, false, false, 3)
	satisfied := applyFor(t, r, planFor(t, r, missing, testTime.Add(2*time.Minute)))
	if satisfied.After[0].Status != StatusSatisfied {
		t.Fatalf("expected satisfied: %+v", satisfied.After)
	}

	r2 := NewRegistry()
	applyFor(t, r2, planFor(t, r2, testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("candidate-a", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1), testTime))
	expired := applyFor(t, r2, planFor(t, r2, testAssessment(nil, true, true, 2), testTime.Add(16*time.Minute)))
	if expired.After[0].Status != StatusExpired {
		t.Fatalf("expected expired: %+v", expired.After)
	}
	reappeared := planFor(t, r2, testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("candidate-a", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 3), testTime.Add(17*time.Minute))
	if len(reappeared.Creates) != 1 || reappeared.Creates[0].Generation != 2 || reappeared.Creates[0].ID == expired.After[0].ID {
		t.Fatalf("generation was not advanced: %+v", reappeared.Creates)
	}
}

func TestDispositionsLifecycleAndOptimisticRevision(t *testing.T) {
	r := NewRegistry()
	applyFor(t, r, planFor(t, r, testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("candidate-a", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1), testTime))
	request, _ := r.GetByEpisode("episode-test")[0], true
	ack, err := r.ApplyDisposition(AdvisoryDisposition{RequestID: request.ID, Kind: DispositionAcknowledge, Actor: "operator", At: testTime.Add(time.Second), SourceRevision: request.Revision})
	if err != nil || ack.Request.Status != StatusAcknowledged {
		t.Fatalf("acknowledge failed: %v %+v", err, ack)
	}
	deferUntil := testTime.Add(10 * time.Minute)
	deferred, err := r.ApplyDisposition(AdvisoryDisposition{RequestID: request.ID, Kind: DispositionDefer, Actor: "operator", At: testTime.Add(2 * time.Second), DeferUntil: &deferUntil, SourceRevision: ack.Request.Revision})
	if err != nil || deferred.Request.Status != StatusDeferred {
		t.Fatalf("defer failed: %v %+v", err, deferred)
	}
	if _, err = r.ApplyDisposition(AdvisoryDisposition{RequestID: request.ID, Kind: DispositionRestoreProposal, Actor: "operator", At: testTime.Add(3 * time.Second), SourceRevision: request.Revision}); !errors.Is(err, ErrSourceRevisionConflict) {
		t.Fatalf("stale disposition error=%v", err)
	}
	restored, err := r.ApplyDisposition(AdvisoryDisposition{RequestID: request.ID, Kind: DispositionRestoreProposal, Actor: "operator", At: testTime.Add(3 * time.Second), SourceRevision: deferred.Request.Revision})
	if err != nil || restored.Request.Status != StatusProposed {
		t.Fatalf("restore failed: %v %+v", err, restored)
	}
	cancelled, err := r.ApplyDisposition(AdvisoryDisposition{RequestID: request.ID, Kind: DispositionCancel, Actor: "operator", At: testTime.Add(4 * time.Second), SourceRevision: restored.Request.Revision})
	if err != nil || cancelled.Request.Status != StatusCancelled {
		t.Fatalf("cancel failed: %v %+v", err, cancelled)
	}
	if _, err = r.ApplyDisposition(AdvisoryDisposition{RequestID: request.ID, Kind: DispositionRestoreProposal, Actor: "operator", At: testTime.Add(5 * time.Second), SourceRevision: cancelled.Request.Revision}); !errors.Is(err, ErrRequestTerminal) {
		t.Fatalf("terminal restore error=%v", err)
	}
}

func TestValidationEqualityAndNoExecutionMeaning(t *testing.T) {
	r := NewRegistry()
	a := testAssessment([]evidencediscrimination.EvidenceCandidate{
		testCandidate("candidate-a", evidencediscrimination.KindContextConfirmation, 800, 600, 300, 100, evidencediscrimination.SensitivityLow),
		testCandidate("candidate-b", evidencediscrimination.KindPatternAlignmentConfirmation, 800, 600, 300, 100, evidencediscrimination.SensitivityLow),
	}, true, true, 1)
	plan := planFor(t, r, a, testTime)
	if plan.PreferredRequestID != "" {
		t.Fatal("equal scored requests must not produce a preferred request")
	}
	e, err := Explain(plan.ResultingRequests[0])
	if err != nil || !e.NotACommand || !e.NotAProbability || !e.NoSecurityMeaning || !e.RequiresExternalMapping || !e.RequiresExternalAuthorization {
		t.Fatalf("invalid advisory explanation: %v %+v", err, e)
	}
	forged := a
	forged.Fingerprint = "forged"
	if _, err := Plan(PlanInput{Assessment: forged, EvaluatedAt: testTime}, DefaultPolicy()); !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("forged assessment error=%v", err)
	}
	bad := a
	bad.Candidates[0].ReasonCodes = []string{"execute"}
	bad.Candidates[0].Fingerprint = evidencediscrimination.CandidateFingerprint(bad.Candidates[0])
	bad.Fingerprint = evidencediscrimination.AssessmentFingerprint(bad)
	if _, err := Plan(PlanInput{Assessment: bad, EvaluatedAt: testTime}, DefaultPolicy()); !errors.Is(err, ErrInvalidAssessment) {
		t.Fatalf("forbidden code error=%v", err)
	}
}

func TestConcurrentPlansAndSnapshots(t *testing.T) {
	r := NewRegistry()
	a := testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("candidate-a", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1)
	snapshot := r.Snapshot()
	planA, err := Plan(PlanInput{Assessment: a, RegistrySnapshot: snapshot, EvaluatedAt: testTime}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	b := a
	b.Candidates[0].UtilityPermille = 900
	b.Candidates[0].Fingerprint = evidencediscrimination.CandidateFingerprint(b.Candidates[0])
	b.Fingerprint = evidencediscrimination.AssessmentFingerprint(b)
	planB, err := Plan(PlanInput{Assessment: b, RegistrySnapshot: snapshot, EvaluatedAt: testTime.Add(time.Second)}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, plan := range []AdvisoryPlan{planA, planB} {
		wg.Add(1)
		go func(p AdvisoryPlan) { defer wg.Done(); _, e := r.ApplyPlan(p); results <- e }(plan)
	}
	wg.Wait()
	close(results)
	conflicts := 0
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
		if errors.Is(err, ErrSourceRevisionConflict) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent apply successes=%d conflicts=%d", successes, conflicts)
	}
	_ = r.Snapshot().Clone()
}

func TestCompactRequestStorage(t *testing.T) {
	typeOfRequest := reflect.TypeOf(AdvisoryEvidenceRequest{})
	forbiddenFields := []string{"Assessment", "PotentialOutcome", "FactSet", "HypothesisSet", "ConflictSet", "EpisodeSnapshot"}
	for _, name := range forbiddenFields {
		if _, ok := typeOfRequest.FieldByName(name); ok {
			t.Fatalf("request embeds forbidden upstream payload %q", name)
		}
	}
	t.Logf("shallow request struct size: %d bytes; variable storage is limited to IDs, codes, pairs and reasons", unsafe.Sizeof(AdvisoryEvidenceRequest{}))
}

func TestGeneratedCanonicalAndLifecycleProperties(t *testing.T) {
	for i := 0; i < 64; i++ {
		left := requestKeyFromParts("episode", "kind", "dimension", []string{"fact-b", "fact-a", "fact-a"}, []AdvisoryHypothesisPair{{FirstID: "hyp-b", SecondID: "hyp-a"}, {FirstID: "hyp-a", SecondID: "hyp-b"}})
		right := requestKeyFromParts("episode", "kind", "dimension", []string{"fact-a", "fact-b"}, []AdvisoryHypothesisPair{{FirstID: "hyp-a", SecondID: "hyp-b"}})
		if left != right {
			t.Fatalf("canonical key changed at iteration %d", i)
		}
		if requestIDFor(left, uint64(i+1)) == requestIDFor(left, uint64(i+2)) {
			t.Fatal("generation did not change request ID")
		}
	}
	a := testAssessment([]evidencediscrimination.EvidenceCandidate{testCandidate("property", evidencediscrimination.KindContextConfirmation, 800, 700, 400, 100, evidencediscrimination.SensitivityLow)}, true, true, 1)
	assessmentFingerprint := a.Fingerprint
	plan1, err := Plan(PlanInput{Assessment: a, EvaluatedAt: testTime}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	plan2, err := Plan(PlanInput{Assessment: a, EvaluatedAt: testTime}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if plan1.Fingerprint != plan2.Fingerprint || a.Fingerprint != assessmentFingerprint {
		t.Fatal("planner is not deterministic or mutated its input")
	}
	for _, terminalStatus := range []AdvisoryRequestStatus{StatusSatisfied, StatusExpired, StatusCancelled, StatusInvalidated} {
		if EvaluateLifecycle(terminalStatus, StatusProposed).Allowed {
			t.Fatalf("terminal status %s was reactivated", terminalStatus)
		}
	}
	if EvaluateLifecycle(StatusProposed, StatusProposed).Allowed || EvaluateLifecycle(StatusAcknowledged, StatusProposed).Allowed {
		t.Fatal("forbidden lifecycle transition accepted")
	}
}
