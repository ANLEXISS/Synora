package main

import (
	"fmt"
	"path/filepath"

	"synora/internal/oemprofile"
)

type oemProfileE2ECase struct {
	oemprofile.ScenarioResult
	RuntimePipelineCaseID string         `json:"runtime_pipeline_case_id"`
	RuntimePipelinePassed bool           `json:"runtime_pipeline_passed"`
	Journey               []journeyEvent `json:"journey"`
}

func runOEMProfileE2E(root string) []oemProfileE2ECase {
	results := oemprofile.RunScenarios(filepath.Join(root, "configs/oem/synora-rk3588-central-v1.yaml"), filepath.Join(root, "configs/oem/synora-generic-cpu-development-v1.yaml"), oemprofile.ObserveHost())
	base, err := loadFixture(filepath.Join(root, "testdata/central-e2e-v1/cases/human-public-outdoor.json"))
	if err != nil {
		return []oemProfileE2ECase{{ScenarioResult: oemprofile.ScenarioResult{ID: "oem_profile_pipeline_setup", Family: "oem_profile_v1", Expected: "runtime_pipeline_available", Actual: "unavailable", Passed: false, ErrorCategory: "fixture_unavailable"}}}
	}
	out := make([]oemProfileE2ECase, 0, len(results))
	for _, result := range results {
		pipelineFixture := base
		pipelineFixture.ID = "oem-" + result.ID
		pipeline := runFixture(root, pipelineFixture)
		out = append(out, oemProfileE2ECase{ScenarioResult: result, RuntimePipelineCaseID: pipeline.ID, RuntimePipelinePassed: pipeline.Passed, Journey: pipeline.Journey})
		out[len(out)-1].Passed = out[len(out)-1].Passed && pipeline.Passed
		if !pipeline.Passed && out[len(out)-1].ErrorCategory == "" {
			out[len(out)-1].ErrorCategory = "central_pipeline_failed"
		}
	}
	return out
}

func oemProfileJourneyCheck(cases []oemProfileE2ECase) error {
	for _, c := range cases {
		complete, reason, missing := assessJourney(c.Journey)
		if !complete {
			return fmt.Errorf("OEM scenario %s central pipeline incomplete: %s (missing=%v)", c.ID, reason, missing)
		}
	}
	return nil
}
