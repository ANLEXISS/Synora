// synora-oem-model-preflight validates selected local model packages. It never
// loads a runtime, allocates a device, or runs inference.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"synora/internal/oemmodel"
	"synora/internal/oemprofile"
)

type report struct {
	ProfileID         string                              `json:"profile_id"`
	SchemaVersion     string                              `json:"schema_version"`
	PackageCount      int                                 `json:"package_count"`
	CapabilityCounts  map[string]map[string]int           `json:"capability_counts"`
	StatusCounts      map[string]int                      `json:"status_counts"`
	Modules           map[string]oemmodel.PreflightResult `json:"modules"`
	RuntimeLoaded     bool                                `json:"runtime_loaded"`
	InferenceExecuted bool                                `json:"inference_executed"`
}

func main() {
	profilePath := flag.String("profile", "", "OEM profile path")
	packageRoot := flag.String("packages-root", "configs/oem/model-packages", "trusted package-manifest root")
	artifactRoot := flag.String("artifact-root", os.Getenv("SYNORA_MODEL_ROOT"), "trusted artifact root; defaults to SYNORA_MODEL_ROOT")
	flag.Parse()
	if *profilePath == "" {
		fmt.Fprintln(os.Stderr, "--profile is required")
		os.Exit(2)
	}
	for _, part := range strings.FieldsFunc(*packageRoot, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			fmt.Fprintln(os.Stderr, "package root traversal rejected")
			os.Exit(2)
		}
	}
	clean, err := oemprofile.ResolveProfilePath(*profilePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	p, _, err := oemprofile.Load(clean)
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile rejected:", err)
		os.Exit(1)
	}
	root, err := filepath.Abs(filepath.Clean(*packageRoot))
	if err != nil {
		fmt.Fprintln(os.Stderr, "package root invalid")
		os.Exit(2)
	}
	if info, e := os.Lstat(root); e != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		fmt.Fprintln(os.Stderr, "package root unavailable or unsafe")
		os.Exit(2)
	}
	r := report{ProfileID: p.ProfileID, SchemaVersion: oemmodel.SchemaVersion, Modules: map[string]oemmodel.PreflightResult{}, CapabilityCounts: map[string]map[string]int{}, StatusCounts: map[string]int{"requested": 0, "observed": 0, "effective": 0, "blocked": 0, "unavailable": 0, "prepared": 0, "qualified": 0}, InferenceExecuted: false, RuntimeLoaded: false}
	accepted := true
	for _, dimension := range []string{"requested", "observed", "effective"} {
		r.CapabilityCounts[dimension] = map[string]int{}
	}
	for module, selection := range p.ModelSelection {
		r.StatusCounts["requested"]++
		r.CapabilityCounts["requested"][selection.RequestedMode]++
		path := filepath.Join(root, selection.PackageID+".json")
		info, e := os.Lstat(path)
		if e != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			accepted = false
			r.Modules[module] = oemmodel.PreflightResult{ModuleID: module, RequestedState: selection.RequestedMode, CompatibilityStatus: "blocked", QualificationStatus: "prepared", ArtifactStatus: "absent", ObservedAdapterState: "unavailable", EffectiveState: "unavailable", ErrorCategory: "package_missing"}
			r.CapabilityCounts["observed"]["unavailable"]++
			r.CapabilityCounts["effective"]["unavailable"]++
			r.StatusCounts["observed"]++
			r.StatusCounts["effective"]++
			r.StatusCounts["unavailable"]++
			r.StatusCounts["prepared"]++
			continue
		}
		pkg, _, e := oemmodel.Load(path)
		if e != nil || pkg.PackageID != selection.PackageID || pkg.ModuleID != module || pkg.RuntimeAdapterID != selection.RuntimeAdapterID || pkg.PostprocessAdapterID != selection.PostprocessAdapterID {
			accepted = false
			r.Modules[module] = oemmodel.PreflightResult{ModuleID: module, RequestedState: selection.RequestedMode, CompatibilityStatus: "blocked", QualificationStatus: "prepared", ArtifactStatus: "absent", ObservedAdapterState: "unavailable", EffectiveState: "blocked", ErrorCategory: "package_selection_mismatch"}
			r.CapabilityCounts["observed"]["unavailable"]++
			r.CapabilityCounts["effective"]["blocked"]++
			r.StatusCounts["observed"]++
			r.StatusCounts["effective"]++
			r.StatusCounts["blocked"]++
			r.StatusCounts["prepared"]++
			continue
		}
		allowTest := selection.RequestedMode == "test_only" && p.HardwareClass == "generic-cpu"
		result := oemmodel.Preflight(pkg, *artifactRoot, runtime.GOARCH, "1.0.0", allowTest, nil)
		result.RequestedState = selection.RequestedMode
		if result.CompatibilityStatus == "blocked" || result.QualificationStatus == "blocked" {
			accepted = false
		}
		if selection.RequestedMode == "test_only" && !allowTest {
			accepted = false
			result.EffectiveState = "blocked"
			result.ErrorCategory = "test_profile_required"
			result.CompatibilityStatus = "blocked"
		}
		r.Modules[module] = result
		r.PackageCount++
		r.CapabilityCounts["observed"][result.ObservedAdapterState]++
		r.CapabilityCounts["effective"][result.EffectiveState]++
		r.StatusCounts["observed"]++
		r.StatusCounts["effective"]++
		if result.EffectiveState == "blocked" || result.CompatibilityStatus == "blocked" {
			r.StatusCounts["blocked"]++
		}
		if result.EffectiveState == "unavailable" {
			r.StatusCounts["unavailable"]++
		}
		if result.EffectiveState == "prepared" || result.QualificationStatus == "prepared" {
			r.StatusCounts["prepared"]++
		}
		if result.QualificationStatus == "qualified" {
			r.StatusCounts["qualified"]++
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !accepted {
		os.Exit(1)
	}
}
