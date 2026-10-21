package discovery

import (
	"os"
	"path/filepath"
	"runtime"

	"synora/internal/oemmodel"
	"synora/internal/oemprofile"
)

// ModuleRuntimeCapability is the API-safe effective state. It intentionally
// excludes model paths, hashes, labels, tensors, and device identifiers.
type ModuleRuntimeCapability struct {
	ModuleID             string `json:"module_id"`
	RequestedState       string `json:"requested_state"`
	ObservedAdapterState string `json:"observed_adapter_state"`
	EffectiveState       string `json:"effective_state"`
	PackageID            string `json:"package_id,omitempty"`
	ArtifactFormat       string `json:"artifact_format,omitempty"`
	CompatibilityStatus  string `json:"compatibility_status"`
	QualificationStatus  string `json:"qualification_status"`
	ErrorCategory        string `json:"error_category,omitempty"`
}

func loadModelCapabilities() map[string]ModuleRuntimeCapability {
	profilePath := os.Getenv("SYNORA_OEM_PROFILE")
	if profilePath == "" {
		return nil
	}
	profile, _, err := oemprofile.Load(profilePath)
	if err != nil {
		return nil
	}
	root := os.Getenv("SYNORA_OEM_PACKAGE_ROOT")
	if root != "" {
		if info, e := os.Lstat(root); e != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			root = ""
		}
	}
	capabilities := make(map[string]ModuleRuntimeCapability, len(profile.ModelSelection)+7)
	for _, module := range []string{"vision-person", "vision-pose", "vision-face", "vision-vehicle", "vision-plate", "vision-animal", "camera-health"} {
		capabilities[module] = ModuleRuntimeCapability{ModuleID: module, RequestedState: "not_configured", ObservedAdapterState: "not_requested", EffectiveState: "not_configured", CompatibilityStatus: "not_requested", QualificationStatus: "not_applicable"}
	}
	for module, selection := range profile.ModelSelection {
		c := ModuleRuntimeCapability{ModuleID: module, RequestedState: selection.RequestedMode, EffectiveState: "unavailable", CompatibilityStatus: "blocked", QualificationStatus: "prepared", ErrorCategory: "package_missing"}
		if root != "" {
			path := filepath.Join(root, selection.PackageID+".json")
			if info, e := os.Lstat(path); e == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				pkg, _, e := oemmodel.Load(path)
				if e == nil && pkg.PackageID == selection.PackageID && pkg.ModuleID == module && pkg.RuntimeAdapterID == selection.RuntimeAdapterID && pkg.PostprocessAdapterID == selection.PostprocessAdapterID {
					allowTest := selection.RequestedMode == "test_only" && profile.HardwareClass == "generic-cpu" && os.Getenv("SYNORA_TEST_VISION_WORKER") == "1"
					result := oemmodel.Preflight(pkg, os.Getenv("SYNORA_MODEL_ROOT"), runtime.GOARCH, "1.0.0", allowTest, nil)
					c = ModuleRuntimeCapability{ModuleID: module, RequestedState: selection.RequestedMode, ObservedAdapterState: result.ObservedAdapterState, EffectiveState: result.EffectiveState, PackageID: selection.PackageID, ArtifactFormat: pkg.ArtifactFormat, CompatibilityStatus: result.CompatibilityStatus, QualificationStatus: result.QualificationStatus, ErrorCategory: result.ErrorCategory}
				}
			}
		}
		capabilities[module] = c
	}
	return capabilities
}

func capabilitiesPermitVision(capabilities map[string]ModuleRuntimeCapability, testWorker bool) bool {
	if capabilities == nil {
		return true
	}
	for _, module := range []string{"vision-person", "vision-pose"} {
		c, selected := capabilities[module]
		if !selected {
			continue
		}
		if c.EffectiveState == "prepared" || c.EffectiveState == "available" {
			return true
		}
		if testWorker && c.EffectiveState == "simulated_test" {
			return true
		}
	}
	return false
}
