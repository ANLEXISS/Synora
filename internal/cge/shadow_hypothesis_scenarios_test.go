package cge

import (
	"context"
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/hypotheses"
)

func TestShadowHypothesisScenariosRemainAppendOnly(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	engine, err := NewShadowEngineWithConfig(context.Background(), cognitiveShadowConfig(t.TempDir()), fixedShadowClock{now: base.Add(time.Hour)}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	associationPlan := association.Plan{PolicyVersion: "association-v1", PlannedAt: base, Decision: association.DecisionAmbiguous, Observation: chains.ObservationRef{ID: "ambiguous-association", EventType: "vision.identity", Timestamp: base}, BestScore: 80, ReasonCode: association.ReasonAmbiguous, Reason: "two candidates", RankedCandidates: []association.CandidateScore{{ChainID: "chain-a", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: 80, Facts: []association.ScoreFact{{Code: "entity.same", Score: 80}}}, {ChainID: "chain-b", SourceRevision: 1, Status: chains.StatusActive, Eligible: true, Score: 80, Facts: []association.ScoreFact{{Code: "sequence.same", Score: 80}}}}}
	action, err := engine.orchestrator.processAssociationAmbiguity(context.Background(), associationPlan, base)
	if err != nil || action != ShadowHypothesisOpened {
		t.Fatalf("association open: %s %v", action, err)
	}
	action, err = engine.orchestrator.processAssociationAmbiguity(context.Background(), associationPlan, base.Add(time.Second))
	if err != nil || action != ShadowHypothesisUnchanged {
		t.Fatalf("association idempotence: %s %v", action, err)
	}
	associationPlan.PolicyVersion = "association-v2"
	action, err = engine.orchestrator.processAssociationAmbiguity(context.Background(), associationPlan, base.Add(2*time.Second))
	if err != nil || action != ShadowHypothesisRebased {
		t.Fatalf("association rebase: %s %v", action, err)
	}

	evaluation := shadowAmbiguousEvaluation()
	action, _, _, err = engine.orchestrator.processEvidenceEvaluation(context.Background(), evaluation, base)
	if err != nil || action != ShadowHypothesisOpened {
		t.Fatalf("evidence open: %s %v", action, err)
	}
	action, _, idempotent, err := engine.orchestrator.processEvidenceEvaluation(context.Background(), evaluation, base.Add(time.Second))
	if err != nil || action != ShadowHypothesisUnchanged || !idempotent {
		t.Fatalf("evidence idempotence: %s idempotent=%t err=%v", action, idempotent, err)
	}
	updated := evaluation
	updated.Facts = append([]evidence.EvidenceFact(nil), evaluation.Facts...)
	updated.Facts[0].Score++
	action, _, _, err = engine.orchestrator.processEvidenceEvaluation(context.Background(), updated, base.Add(2*time.Second))
	if err != nil || action != ShadowHypothesisRebased {
		t.Fatalf("evidence rebase: %s %v", action, err)
	}
	updated.EvidenceFingerprint = "sha256:new-fingerprint"
	action, _, _, err = engine.orchestrator.processEvidenceEvaluation(context.Background(), updated, base.Add(3*time.Second))
	if err != nil || action != ShadowHypothesisSuperseded {
		t.Fatalf("evidence supersession: %s %v", action, err)
	}

	current, found, err := engine.coordinator.FindCurrentEvidenceSubject(evaluation.ChainID, evaluation.TargetObservationID)
	if err != nil || !found {
		t.Fatalf("current evidence dossier: %#v %v", current, err)
	}
	if _, err := engine.coordinator.SetHypothesisStatus(context.Background(), hypotheses.SetStatusCommand{SetID: current.ID, SourceRevision: current.Revision, Target: hypotheses.StatusInvalidated, Mutation: chains.MutationContext{At: base.Add(4 * time.Second), Actor: DefaultShadowActor, Reason: "explicit test terminal", CorrelationID: "terminal-test"}}, base.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	action, _, _, err = engine.orchestrator.processEvidenceEvaluation(context.Background(), updated, base.Add(5*time.Second))
	if err != nil || action != ShadowHypothesisTerminalPreserved {
		t.Fatalf("terminal preservation: %s %v", action, err)
	}

	decisive := updated
	decisive.Decision = evidence.DecisionProposeSupport
	action, _, _, err = engine.orchestrator.processEvidenceEvaluation(context.Background(), decisive, base.Add(6*time.Second))
	if err != nil || action != ShadowHypothesisTerminalPreserved {
		t.Fatalf("terminal decisive preservation: %s %v", action, err)
	}
}

func TestCognitiveShadowStopsMutationsWhenCoordinatorIsDegraded(t *testing.T) {
	config := cognitiveShadowConfig(t.TempDir())
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: time.Now().UTC()}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	engine.coordinator.SetQualificationPublicationHook(func() error { return errors.New("synthetic publication failure") })
	_, err = engine.Observe(context.Background(), shadowEvent("degraded-cognitive", "vision.identity", time.Now().UTC()))
	if err == nil || engine.Metrics().OrchestrationDegraded == 0 {
		t.Fatalf("degraded cognitive observation was not contained: err=%v metrics=%#v", err, engine.Metrics())
	}
}

func shadowAmbiguousEvaluation() evidence.EvidenceEvaluation {
	return evidence.EvidenceEvaluation{ChainID: "cge-shadow-hyp-chain", SourceRevision: 1, TargetObservationID: "shadow-evidence-target", EvaluatedAt: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC), PolicyNamespace: "synora.cge.evidence", PolicyVersion: "evidence-v1", EvidenceFingerprint: "sha256:shadow-fingerprint", ResolutionValues: evidence.ResolutionValues{SupportValue: 0.10, ContradictionValue: 0.15}, Decision: evidence.DecisionAmbiguous, SupportScore: 80, ContradictionScore: 70, DecisionMargin: 10, ContextObservationIDs: []string{"context-a", "context-b"}, Facts: []evidence.EvidenceFact{{Code: "entity.context_same", Side: evidence.EvidenceSupport, Score: 60, ObservationIDs: []string{"shadow-evidence-target", "context-a"}}, {Code: "entity.context_conflict", Side: evidence.EvidenceContradiction, Score: 70, ObservationIDs: []string{"shadow-evidence-target", "context-b"}}}, ReasonCode: "evidence.ambiguous", Reason: "support and contradiction remain plausible"}
}
