package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"synora/internal/oemmodel"
	"synora/internal/oemprofile"
)

type oemModelPreflightReport struct {
	Modules           map[string]oemmodel.PreflightResult `json:"modules"`
	Counts            map[string]map[string]int           `json:"capability_counts"`
	StatusCounts      map[string]int                      `json:"status_counts"`
	RuntimeLoaded     bool                                `json:"runtime_loaded"`
	InferenceExecuted bool                                `json:"inference_executed"`
}

func collectOEMModelPreflight(root string) map[string]oemModelPreflightReport {
	out := map[string]oemModelPreflightReport{}
	for _, name := range []string{"synora-rk3588-central-v1", "synora-generic-cpu-development-v1"} {
		p, _, err := oemprofile.Load(filepath.Join(root, "configs/oem", name+".yaml"))
		if err != nil {
			continue
		}
		report := oemModelPreflightReport{Modules: map[string]oemmodel.PreflightResult{}, Counts: map[string]map[string]int{"requested": {}, "observed": {}, "effective": {}}, StatusCounts: map[string]int{"requested": 0, "observed": 0, "effective": 0, "blocked": 0, "unavailable": 0, "prepared": 0, "qualified": 0}, RuntimeLoaded: false, InferenceExecuted: false}
		for module, selection := range p.ModelSelection {
			report.StatusCounts["requested"]++
			report.Counts["requested"][selection.RequestedMode]++
			manifestPath := filepath.Join(root, "configs/oem/model-packages", selection.PackageID+".json")
			pkg, _, loadErr := oemmodel.Load(manifestPath)
			var result oemmodel.PreflightResult
			if loadErr != nil || pkg.ModuleID != module || pkg.PackageID != selection.PackageID || pkg.RuntimeAdapterID != selection.RuntimeAdapterID || pkg.PostprocessAdapterID != selection.PostprocessAdapterID {
				result = oemmodel.PreflightResult{RequestedState: selection.RequestedMode, ModuleID: module, CompatibilityStatus: "blocked", QualificationStatus: "prepared", ArtifactStatus: "absent", ObservedAdapterState: "unavailable", EffectiveState: "unavailable", ErrorCategory: "package_missing"}
			} else {
				allowTest := p.HardwareClass == "generic-cpu" && selection.RequestedMode == "test_only"
				result = oemmodel.Preflight(pkg, os.Getenv("SYNORA_MODEL_ROOT"), runtime.GOARCH, "1.0.0", allowTest, nil)
				result.RequestedState = selection.RequestedMode
			}
			report.Modules[module] = result
			report.Counts["observed"][result.ObservedAdapterState]++
			report.Counts["effective"][result.EffectiveState]++
			report.StatusCounts["observed"]++
			report.StatusCounts["effective"]++
			if result.EffectiveState == "blocked" || result.CompatibilityStatus == "blocked" {
				report.StatusCounts["blocked"]++
			}
			if result.EffectiveState == "unavailable" {
				report.StatusCounts["unavailable"]++
			}
			if result.EffectiveState == "prepared" || result.QualificationStatus == "prepared" {
				report.StatusCounts["prepared"]++
			}
			if result.QualificationStatus == "qualified" {
				report.StatusCounts["qualified"]++
			}
		}
		for _, module := range []string{"vision-person", "vision-pose", "vision-face", "vision-vehicle", "vision-plate", "vision-animal", "camera-health"} {
			if _, exists := report.Modules[module]; !exists {
				result := oemmodel.PreflightResult{RequestedState: "not_configured", ModuleID: module, CompatibilityStatus: "not_requested", QualificationStatus: "not_applicable", ArtifactStatus: "not_applicable", ObservedAdapterState: "not_requested", EffectiveState: "not_configured"}
				report.Modules[module] = result
				report.Counts["requested"]["not_configured"]++
				report.Counts["observed"]["not_requested"]++
				report.Counts["effective"]["not_configured"]++
				report.StatusCounts["requested"]++
				report.StatusCounts["observed"]++
				report.StatusCounts["effective"]++
			}
		}
		out[p.ProfileID] = report
	}
	return out
}

type oemModelPackageE2ECase struct {
	oemmodel.ScenarioResult
	RuntimePipelineCaseID string         `json:"runtime_pipeline_case_id"`
	RuntimePipelinePassed bool           `json:"runtime_pipeline_passed"`
	Journey               []journeyEvent `json:"journey"`
}

func runOEMModelPackageE2E(root string) []oemModelPackageE2ECase {
	results := oemmodel.RunScenarios()
	base, err := loadFixture(filepath.Join(root, "testdata/central-e2e-v1/cases/human-public-outdoor.json"))
	if err != nil {
		return []oemModelPackageE2ECase{{ScenarioResult: oemmodel.ScenarioResult{ID: "oem_model_pipeline_setup", Family: "oem_model_packages_v1", Expected: "fixture_available", Actual: "unavailable", Passed: false, ErrorCategory: "fixture_unavailable"}}}
	}
	out := make([]oemModelPackageE2ECase, 0, len(results))
	for _, result := range results {
		fixture := base
		fixture.ID = "model-package-" + result.ID
		pipeline := runFixture(root, fixture)
		item := oemModelPackageE2ECase{ScenarioResult: result, RuntimePipelineCaseID: pipeline.ID, RuntimePipelinePassed: pipeline.Passed, Journey: pipeline.Journey}
		item.Passed = item.Passed && pipeline.Passed
		if !pipeline.Passed && item.ErrorCategory == "" {
			item.ErrorCategory = "central_pipeline_failed"
		}
		out = append(out, item)
	}
	return out
}

func oemModelPackageJourneyCheck(cases []oemModelPackageE2ECase) error {
	for _, c := range cases {
		complete, reason, missing := assessJourney(c.Journey)
		if !complete {
			return fmt.Errorf("OEM model scenario %s pipeline incomplete: %s (missing=%v)", c.ID, reason, missing)
		}
	}
	return nil
}
