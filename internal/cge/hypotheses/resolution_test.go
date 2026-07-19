package hypotheses

import (
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
)

func TestAssociationAlternativesCarryImmutableAttachEffects(t *testing.T) {
	plan := ambiguousAssociationPlan()
	set, err := FromAmbiguousAssociation(plan, hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := set.Snapshot()
	if snapshot.Assessments[0].ResolutionSchemaVersion != ResolutionSchemaV1 {
		t.Fatalf("expected resolution schema 1, got %d", snapshot.Assessments[0].ResolutionSchemaVersion)
	}
	for _, alternative := range snapshot.Alternatives {
		if alternative.ResolutionEffect == nil || alternative.ResolutionEffect.Kind != ResolutionEffectAttachObservation {
			t.Fatalf("missing attach effect: %+v", alternative)
		}
		effect := alternative.ResolutionEffect.AttachObservation
		if effect.ChainID != alternative.ChainID || effect.SourceRevision != alternative.SourceRevision || effect.Observation.ID != snapshot.Subject.ObservationID {
			t.Fatalf("effect does not match alternative: %+v", effect)
		}
	}
	copy := snapshot
	copy.Alternatives[0].ResolutionEffect.AttachObservation.Observation.ID = "mutated"
	if set.Snapshot().Alternatives[0].ResolutionEffect.AttachObservation.Observation.ID == "mutated" {
		t.Fatal("snapshot exposed mutable effect state")
	}
}

func TestEvidenceAlternativesCarryFixedContributionEffects(t *testing.T) {
	set, err := FromAmbiguousEvidence(ambiguousEvidenceEvaluation(), hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := set.Snapshot()
	if snapshot.Assessments[0].ResolutionSchemaVersion != ResolutionSchemaV1 {
		t.Fatalf("expected schema 1, got %d", snapshot.Assessments[0].ResolutionSchemaVersion)
	}
	for _, alternative := range snapshot.Alternatives {
		if alternative.ResolutionEffect == nil {
			t.Fatalf("alternative has no effect: %+v", alternative)
		}
		switch alternative.Kind {
		case AlternativeSupport, AlternativeContradiction:
			effect := alternative.ResolutionEffect.AddContribution
			if effect == nil || effect.ContributionTemplate.ID != alternative.ContributionID || effect.ContributionTemplate.Value == 0 {
				t.Fatalf("invalid contribution effect: %+v", effect)
			}
		case AlternativeInsufficient:
			if alternative.ResolutionEffect.NoChainEffect == nil {
				t.Fatal("insufficient alternative must carry an explicit no-op")
			}
		}
	}
}

func TestResolutionReadinessAndExplicitPlanning(t *testing.T) {
	set, err := FromAmbiguousEvidence(ambiguousEvidenceEvaluation(), hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := set.Snapshot()
	readiness := snapshot.ResolutionReadiness()
	if !readiness.Ready || readiness.ResolvableAlternativeCount != len(snapshot.Alternatives) {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
	plan, err := PlanResolution(snapshot, snapshot.Alternatives[1].ID, hypothesisTestBase.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if plan.AlternativeID != snapshot.Alternatives[1].ID || plan.SourceRevision != snapshot.Revision || plan.AssessmentVersion != 1 {
		t.Fatalf("unexpected resolution plan: %+v", plan)
	}
	if plan.Effect.Kind != snapshot.Alternatives[1].ResolutionEffect.Kind {
		t.Fatal("plan did not preserve the selected effect")
	}
	if _, err := PlanResolution(snapshot, "missing-alternative", hypothesisTestBase.Add(time.Minute)); !errors.Is(err, ErrAlternativeNotFound) {
		t.Fatalf("expected missing alternative, got %v", err)
	}
}

func TestLegacyAssessmentIsNotResolvable(t *testing.T) {
	set, err := FromAmbiguousAssociation(ambiguousAssociationPlan(), hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	legacy := set.Snapshot()
	legacy.Assessments = nil
	legacy.CurrentAssessmentVersion = 0
	for i := range legacy.Alternatives {
		legacy.Alternatives[i].ResolutionEffect = nil
	}
	restored, err := Restore(legacy)
	if err != nil {
		t.Fatal(err)
	}
	readiness := restored.Snapshot().ResolutionReadiness()
	if readiness.Ready || readiness.SchemaVersion != ResolutionSchemaLegacy || readiness.ReasonCode != ErrHypothesisResolutionMaterialMissing.Error() {
		t.Fatalf("unexpected legacy readiness: %+v", readiness)
	}
	if _, err := PlanResolution(restored.Snapshot(), restored.Snapshot().Alternatives[0].ID, hypothesisTestBase.Add(time.Minute)); !errors.Is(err, ErrHypothesisResolutionMaterialMissing) {
		t.Fatalf("expected legacy material error, got %v", err)
	}
}

func TestResolutionEffectChangesAssessmentFingerprint(t *testing.T) {
	set, err := FromAmbiguousAssociation(ambiguousAssociationPlan(), hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := set.Snapshot()
	old := snapshot.Assessments[0].Fingerprint
	modified := snapshot.Alternatives
	modified[0].ResolutionEffect.AttachObservation.SourceRevision++
	newFingerprint, err := DeriveAssessmentFingerprint(snapshot.Family, snapshot.Subject, modified, snapshot.Provenance)
	if err != nil {
		t.Fatal(err)
	}
	if newFingerprint == old {
		t.Fatal("effect change did not change assessment fingerprint")
	}
}

func TestEffectHelpersRemainPure(t *testing.T) {
	set, err := FromAmbiguousEvidence(ambiguousEvidenceEvaluation(), hypothesisTestBase, conversionMutation(hypothesisTestBase))
	if err != nil {
		t.Fatal(err)
	}
	alternative := set.Snapshot().Alternatives[0]
	effect := alternative.ResolutionEffect.AddContribution
	command, err := effect.Command(chains.MutationContext{At: hypothesisTestBase.Add(time.Minute), Actor: "test", Reason: "prepare", CorrelationID: "prepare-1"})
	if err != nil {
		t.Fatal(err)
	}
	if command.ChainID != set.Snapshot().Subject.ChainID || command.Contribution.CreatedAt.IsZero() {
		t.Fatalf("unexpected prepared command: %+v", command)
	}
	if set.Snapshot().Revision != 1 || len(set.Snapshot().History) != 1 {
		t.Fatal("effect helper mutated the hypothesis")
	}
}
