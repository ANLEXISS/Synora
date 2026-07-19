package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synora/internal/cge/demo"
)

func TestStaticExportIsSelfContainedAndDataDriven(t *testing.T) {
	result, err := demo.Run(context.Background(), demo.Options{Scenario: "investor-core", Seed: 3501, Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := exportStatic(dir, result, "fr", true); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "app.js", "live.js", "live.css", "live-extra.css", "styles.css", "scenario.json", "claims.json", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing export file %s: %v", name, err)
		}
	}
	var scenario demo.RunResult
	data, err := os.ReadFile(filepath.Join(dir, "scenario.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &scenario); err != nil {
		t.Fatal(err)
	}
	if scenario.Manifest.SyntheticWarning != "synthetic_episode_not_separated" || !scenario.Manifest.SyntheticScenario {
		t.Fatal("synthetic provenance warning missing")
	}
	index, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "demo-investor-core-001") || !strings.Contains(string(index), "security-authority") {
		t.Fatal("static export did not embed scenario and claims")
	}
	if !strings.Contains(string(index), "presentationMode = true") {
		t.Fatal("static export did not select the offline guided presentation")
	}
	app, err := os.ReadFile(filepath.Join(dir, "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(app), "score = 850") || strings.Contains(string(app), "routines = 12") {
		t.Fatal("cognitive values were hardcoded in frontend")
	}
	if strings.Contains(string(app), "https://") || strings.Contains(string(app), "http://") {
		t.Fatal("frontend contains an external URL")
	}
	live, err := os.ReadFile(filepath.Join(dir, "live.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(live), "score = 850") || strings.Contains(string(live), "routines = 12") || strings.Contains(string(live), "https://") {
		t.Fatal("live frontend contains hardcoded cognitive values or an external URL")
	}
}

func TestClaimsReferenceOnlyKnownStatuses(t *testing.T) {
	for _, claim := range demo.Claims().Claims {
		switch claim.Status {
		case "proven", "demonstrated", "experimental", "future":
		default:
			t.Fatalf("unknown claim status %q", claim.Status)
		}
	}
}
