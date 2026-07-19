package cge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func cognitiveShadowConfig(root string) ShadowConfig {
	config := enabledShadowConfig(root, true)
	config.Cognitive.Enabled = true
	config.Cognitive.AutoApplyDecisiveEvidence = true
	return config
}

func TestCognitiveShadowOrchestratesTargetedEvidenceWithoutResolution(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	config := cognitiveShadowConfig(root)
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: base.Add(10 * time.Minute)}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	first := shadowEvent("cognitive-1", "vision.identity", base)
	adapted, err := AdaptEventWithAllowlist(first, DefaultEligibleEventTypes())
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.orchestrator.ProcessObservation(context.Background(), adapted.Input.Observation, adapted.Input.SituationKind)
	if err != nil {
		t.Fatalf("first observation: %v", err)
	}
	if result.AssociationDecision != "create_candidate" || result.ChainID == "" {
		t.Fatalf("unexpected first orchestration: %#v", result)
	}

	second := shadowEvent("cognitive-2", "vision.identity", base.Add(time.Second))
	adapted, err = AdaptEventWithAllowlist(second, DefaultEligibleEventTypes())
	if err != nil {
		t.Fatal(err)
	}
	result, err = engine.orchestrator.ProcessObservation(context.Background(), adapted.Input.Observation, adapted.Input.SituationKind)
	if err != nil {
		t.Fatalf("second observation: %v", err)
	}
	if result.ChainID == "" || result.AssociationDecision == "ambiguous" {
		t.Fatalf("second observation was not non-ambiguous: %#v", result)
	}
	chain, err := engine.coordinator.Get(result.ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if chain.Status == "resolved" || chain.Status == "invalidated" || len(chain.History) == 0 {
		t.Fatalf("cognitive orchestration changed lifecycle: %#v", chain)
	}
	for _, hypothesis := range engine.coordinator.ListHypotheses() {
		if hypothesis.Status == "resolved" {
			t.Fatalf("shadow resolved hypothesis: %#v", hypothesis)
		}
	}
	if got := engine.Metrics(); got.EvidenceEvaluated == 0 {
		t.Fatalf("targeted evidence was not evaluated: %#v", got)
	}
}

func TestCognitiveShadowConfigurationDefaultsAndEnvironment(t *testing.T) {
	defaults, err := LoadShadowConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Cognitive.Enabled || defaults.Cognitive.AutoApplyDecisiveEvidence || defaults.Cognitive.MaxEvidenceReevaluationsPerObservation != 8 {
		t.Fatalf("unsafe cognitive defaults: %#v", defaults.Cognitive)
	}
	values := map[string]string{
		ShadowCognitiveEnabledEnv: "true", ShadowAutoEvidenceEnv: "true", ShadowMaxReevaluationsEnv: "4",
	}
	configured, err := LoadShadowConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !configured.Cognitive.Enabled || !configured.Cognitive.AutoApplyDecisiveEvidence || configured.Cognitive.MaxEvidenceReevaluationsPerObservation != 4 {
		t.Fatalf("environment settings not loaded: %#v", configured.Cognitive)
	}
	bad := configured
	bad.Enabled = true
	bad.Cognitive.MaxEvidenceReevaluationsPerObservation = 0
	if err := bad.Validate(); err == nil || !errors.Is(err, ErrInvalidShadowConfig) {
		t.Fatalf("invalid reevaluation bound accepted: %v", err)
	}
	if _, err := NewShadowEngineWithConfig(context.Background(), DefaultShadowConfig(), fixedShadowClock{now: time.Now().UTC()}, quietShadowLogger()); err != nil {
		t.Fatal(err)
	}
}

func TestCognitiveShadowConcurrentObservationsRemainContained(t *testing.T) {
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	config := cognitiveShadowConfig(t.TempDir())
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: base.Add(time.Hour)}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			event := shadowEvent(fmt.Sprintf("concurrent-%d", index), "vision.identity", base.Add(time.Duration(index)*time.Second))
			_, _ = engine.Observe(context.Background(), event)
		}(i)
	}
	group.Wait()
	if got := engine.Metrics(); got.EventsObserved != 8 || got.OrchestrationPanics != 0 {
		t.Fatalf("concurrent cognitive observations were not contained: %#v", got)
	}
}
