package main

import (
	"context"
	"strings"
	"testing"

	"synora/internal/cge/demo"
)

func TestEmbeddedScenarioCatalogIsValid(t *testing.T) {
	library, err := embeddedScenarioLibrary()
	if err != nil {
		t.Fatal(err)
	}
	items := library.List()
	if len(items) != 13 {
		t.Fatalf("expected thirteen versioned scenarios, got %d", len(items))
	}
	for _, scenario := range items {
		if err := scenario.Validate(); err != nil {
			t.Fatalf("%s: %v", scenario.ID, err)
		}
	}
}

func TestEmbeddedScenariosUseRealLiveSession(t *testing.T) {
	library, err := embeddedScenarioLibrary()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range library.List() {
		if scenario.UnsupportedReason != nil {
			continue
		}
		session, err := demo.NewLiveSession(context.Background(), demo.LiveOptions{Seed: scenario.Seed})
		if err != nil {
			t.Fatal(scenario.ID, err)
		}
		session.SetScenarioLibrary(library)
		_, err = session.StartScenario(context.Background(), scenario.ID)
		if err != nil {
			session.Close()
			t.Fatalf("%s start: %v", scenario.ID, err)
		}
		err = session.ScenarioRunToEnd(context.Background())
		report, reportErr := session.ScenarioReport()
		closeErr := session.Close()
		if err != nil {
			t.Fatalf("%s run: %v", scenario.ID, err)
		}
		if closeErr != nil {
			t.Fatalf("%s close: %v", scenario.ID, closeErr)
		}
		if reportErr != nil {
			t.Fatalf("%s report: %v", scenario.ID, reportErr)
		}
		if report.UnexpectedProperties != 0 {
			t.Fatalf("%s expected properties diverged: %#v", scenario.ID, report)
		}
		if !report.NoSecurityAuthority || report.FinalGlobalState.ActionsEnabled {
			t.Fatalf("%s exposed security authority or actions: %#v", scenario.ID, report)
		}
		if scenario.ID == "memory-field-isolation" {
			state := session.ScenarioState()
			if state == nil || state.MemoryMatrix == nil || len(state.MemoryMatrix.Rows) != 7 || state.MemoryMatrix.BaselineDays != 30 {
				t.Fatalf("memory field matrix was not produced from isolated sessions: %#v", state)
			}
			control := state.MemoryMatrix.Rows[0]
			if control.Structural.Score != 0 || control.Temporal.Score != 0 || control.Interval.Score != 0 {
				t.Fatalf("control row is not aligned with the real baseline: %#v", control)
			}
		}
	}
}

func TestScenarioCatalogUsesOnlyAssociationHypotheses(t *testing.T) {
	library, err := embeddedScenarioLibrary()
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"association-ambiguity": true}
	for _, scenario := range library.List() {
		for _, property := range append(append([]demo.ExpectedProperty{}, scenario.ExpectedProperties...), expectedFromSteps(scenario.Steps)...) {
			if strings.HasPrefix(string(property.Scope), "hypothesis.") && !allowed[scenario.ID] {
				t.Fatalf("%s claims an unauthorized hypothesis scope: %s", scenario.ID, property.Scope)
			}
		}
		if strings.Contains(strings.ToLower(string(scenario.Category)), "situation") {
			t.Fatalf("%s is categorized as a situation", scenario.ID)
		}
	}
}

func expectedFromSteps(steps []demo.ScenarioStep) []demo.ExpectedProperty {
	var out []demo.ExpectedProperty
	for _, step := range steps {
		out = append(out, step.Expected...)
	}
	return out
}

func TestMemoryFieldIsolationDefinitionIsOneFieldAtATime(t *testing.T) {
	library, err := embeddedScenarioLibrary()
	if err != nil {
		t.Fatal(err)
	}
	scenario, ok := library.Get("memory-field-isolation")
	if !ok || scenario.MemoryFieldIsolation == nil {
		t.Fatal("memory field isolation scenario is missing")
	}
	declared := map[string][]string{
		"control":   {"none"},
		"hour":      {"time"},
		"space":     {"space"},
		"mode":      {"house_mode"},
		"occupancy": {"occupancy"},
		"interval":  {"elapsed"},
		"partial":   {"house_mode", "occupancy", "context_quality"},
	}
	for _, variant := range scenario.MemoryFieldIsolation.Variants {
		if strings.Join(variant.Changes, "|") != strings.Join(declared[variant.ID], "|") {
			t.Fatalf("variant %s changes %v, want %v", variant.ID, variant.Changes, declared[variant.ID])
		}
	}
}

func TestLiveLabUIDoesNotTurnDivergenceIntoSituationOrThreat(t *testing.T) {
	data, err := webAssets.ReadFile("web/live.js")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	for _, forbidden := range []string{"situation suspecte", "intrusion probable", "visiteur attendu", "comportement malveillant", "reconnaissance des lieux", "retour pour récupérer un objet", "danger détecté", "visitor expected", "malicious behavior", "threat detected"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Live Lab UI contains interpretive or threat result text %q", forbidden)
		}
	}
	for _, required := range []string{"hypothèses d’association", "interprétation de situation : non produite dans cette version", "le cge mesure une divergence avec sa mémoire"} {
		if !strings.Contains(text, required) {
			t.Fatalf("Live Lab UI is missing boundary text %q", required)
		}
	}
}
