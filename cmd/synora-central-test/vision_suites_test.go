package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synora/internal/visionsuite"
)

func TestVisionRunDoesNotTreatMissingSlotsAsSuccess(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	manifestPath := filepath.Join("..", "..", defaultVisionSuiteManifest)
	registryPath := filepath.Join("..", "..", defaultVisionModuleRegistry)
	err := runVisionSuiteCommand(
		"run", "face_known", "face_known_01",
		manifestPath, registryPath,
		t.TempDir(), out, nil, nil,
	)
	if err == nil {
		t.Fatal("run returned success for an absent placeholder and unconfigured plugin")
	}
	body, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatalf("expected explicit redacted run report on failure: %v", readErr)
	}
	var report visionsuite.Report
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	if report.QualificationStatus != "not_qualified" || report.RegistrySHA256 == "" || report.ExecutionMode != visionsuite.ExecutionReplay || len(report.Cases) != 1 || report.Cases[0].ExecutionMode != visionsuite.ExecutionReplay || report.Cases[0].Status != "media_absent" || report.Cases[0].InferenceExecuted || report.Executed != 0 || report.Qualified != 0 {
		t.Fatalf("missing slot was not explicitly fail-closed: %+v", report)
	}
}

func TestVisionCLIRejectsManifestNotPinnedByRegistry(t *testing.T) {
	sourceManifestPath := filepath.Join("..", "..", defaultVisionSuiteManifest)
	registryPath := filepath.Join("..", "..", defaultVisionModuleRegistry)
	body, err := os.ReadFile(sourceManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "modified-manifest.json")
	if err := os.WriteFile(manifestPath, append(body, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	err = runVisionSuiteCommand("list", "", "", manifestPath, registryPath, "", filepath.Join(t.TempDir(), "unused.json"), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not pinned") {
		t.Fatalf("unreviewed manifest was not rejected by its digest pin: %v", err)
	}
}
