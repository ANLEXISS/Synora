package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"synora/internal/visionsuite"
)

const defaultVisionSuiteManifest = "testdata/vision-v1/suites/manifest.json"
const defaultVisionModuleRegistry = "testdata/vision-v1/modules.json"

func runVisionSuiteCommand(action, suite, manifestPath, registryPath, mediaRoot, outPath string, modelPaths map[string]string) error {
	if action != "list" && action != "verify" && action != "run" {
		return errors.New("--vision-suites must be list, verify, or run")
	}
	manifest, digest, err := visionsuite.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("vision suite manifest: %w", err)
	}
	_, moduleStates, _, err := visionsuite.LoadRegistry(registryPath)
	if err != nil {
		return fmt.Errorf("vision module registry: %w", err)
	}
	manifest, err = visionsuite.Filter(manifest, suite)
	if err != nil {
		return err
	}
	var report visionsuite.Report
	switch action {
	case "list":
		report = visionsuite.List(manifest, digest)
		report.Modules = moduleStates
	case "verify":
		report = visionsuite.Inspect(manifest, digest, action, mediaRoot, moduleStates)
	case "run":
		// Plugins are injected here when a module implementation is separately
		// reviewed and qualified. No inactive module is implicitly activated.
		report = visionsuite.Execute(context.Background(), manifest, digest, mediaRoot, modelPaths, nil, nil)
		report.Modules = moduleStates
	}
	if err := writeJSON(outPath, report); err != nil {
		return err
	}
	total := 0
	if action == "list" {
		names := make([]string, 0, len(report.SuiteCounts))
		for name := range report.SuiteCounts {
			names = append(names, name)
			total += report.SuiteCounts[name]
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("%s slots=%d\n", name, report.SuiteCounts[name])
		}
	}
	slotCount := len(report.Cases)
	if action == "list" {
		slotCount = total
	}
	fmt.Printf("vision suites action=%s report=%s slots=%d media_absent=%d media_quarantined=%d model_absent=%d model_unavailable=%d model_failed=%d executed=%d mismatched=%d qualified=%d inference_executed=%t manifest_sha256=%s\n", action, outPath, slotCount, report.MediaAbsent, report.MediaQuarantined, report.ModelAbsent, report.ModelUnavailable, report.ModelFailed, report.Executed, report.Mismatched, report.Qualified, report.InferenceRun, report.ManifestSHA256)
	if action == "run" && (report.MediaAbsent+report.ModelAbsent+report.FailedCount+report.Mismatched > 0 || len(report.Cases) == 0) {
		return errors.New("vision suite is incomplete; see explicit per-slot statuses in the report")
	}
	if _, err := os.Stat(outPath); err != nil {
		return err
	}
	return nil
}
