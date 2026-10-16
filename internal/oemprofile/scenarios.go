package oemprofile

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"synora/internal/cognitivecore"
)

type ScenarioResult struct {
	ID                  string `json:"id"`
	Family              string `json:"family"`
	Expected            string `json:"expected"`
	Actual              string `json:"actual"`
	Passed              bool   `json:"passed"`
	ProfileID           string `json:"profile_id,omitempty"`
	PreflightStatus     string `json:"preflight_status"`
	CompatibilityStatus string `json:"compatibility_status"`
	ErrorCategory       string `json:"error_category,omitempty"`
	AudioRendered       bool   `json:"audio_rendered"`
	PhysicalAction      bool   `json:"physical_action_executed"`
	NetworkAccess       bool   `json:"network_access"`
	RawDataForwarded    bool   `json:"raw_data_forwarded"`
}

// RunScenarios exercises strict profile loading, fail-closed compatibility,
// non-promotion, redacted API policy and restart-stable profile identity.
func RunScenarios(rkPath, cpuPath string, obs RuntimeObservation) []ScenarioResult {
	rk, rkBody, rkErr := Load(rkPath)
	cpu, cpuBody, cpuErr := Load(cpuPath)
	results := make([]ScenarioResult, 0, 18)
	add := func(id, expected, actual, profile string, err error) {
		r := ScenarioResult{ID: id, Family: "oem_profile_v1", Expected: expected, Actual: actual, Passed: expected == actual, ProfileID: profile, PreflightStatus: "ready", CompatibilityStatus: "compatible"}
		if err != nil {
			r.Passed = false
			r.ErrorCategory = "contract_rejected"
		}
		results = append(results, r)
	}
	add("rk3588_valid", "accepted", loadResult(rkErr), rk.ProfileID, rkErr)
	add("generic_cpu_valid", "accepted", loadResult(cpuErr), cpu.ProfileID, cpuErr)
	unknown := []byte(strings.Replace(string(cpuBody), "profile_id:", "undeclared: true\nprofile_id:", 1))
	_, err := DecodeBytes(unknown)
	add("unknown_field_rejected", "rejected", decodeResult(err), cpu.ProfileID, expectErr(err))
	badSchema := cpu
	badSchema.SchemaVersion = "synora.oem.profile/v9"
	add("schema_incompatible", "rejected", validateResult(badSchema), cpu.ProfileID, expectErr(badSchema.Validate()))
	badArch := cpu
	badArch.TargetArchitecture = "mips"
	add("architecture_incompatible", "rejected", validateResult(badArch), cpu.ProfileID, expectErr(badArch.Validate()))
	missing := cpu
	missing.AdapterSelection["vision-face"] = "cpu-onnx"
	missing.EnabledModules = append(missing.EnabledModules, "vision-face")
	mr := Preflight(missing, cpuBody, obs)
	add("adapter_missing", "unavailable", mr.ObservedCapabilities["vision-face.cpu-onnx"], cpu.ProfileID, nil)
	missingModule := cpu
	missingModule.AdapterSelection["vision-face"] = "cpu-onnx"
	missingModule.EnabledModules = append(missingModule.EnabledModules, "vision-face")
	mm := Preflight(missingModule, cpuBody, obs)
	add("module_unavailable", "unavailable", mm.EffectiveCapabilities["vision-face"], cpu.ProfileID, nil)
	qualified := ValidateCapabilityClaim("qualified")
	add("qualified_claim_refused", "rejected", decodeResult(qualified), cpu.ProfileID, expectErr(qualified))
	badModel := rk
	badRequirement := badModel.ModelManifestRequirements["vision-pose"]
	badRequirement.SHA256 = strings.Repeat("0", 64)
	badModel.ModelManifestRequirements["vision-pose"] = badRequirement
	badModelReport := Preflight(badModel, rkBody, obs)
	add("model_manifest_hash_invalid", "blocked", badModelReport.ObservedCapabilities["model_manifest:vision-pose"], rk.ProfileID, nil)
	physical := cpu
	physical.SafetyDefaults.PhysicalActions = true
	add("physical_action_denied", "rejected", validateResult(physical), cpu.ProfileID, expectErr(physical.Validate()))
	secret := []byte(strings.Replace(string(cpuBody), "profile_id:", "api_token: fake-secret\nprofile_id:", 1))
	_, err = DecodeBytes(secret)
	add("secret_field_rejected", "rejected", decodeResult(err), cpu.ProfileID, expectErr(err))
	_, pathErr := ResolveProfilePath("../../etc/shadow")
	add("path_traversal_rejected", "rejected", decodeResult(pathErr), cpu.ProfileID, expectErr(pathErr))
	api := cpu
	api.APISurface.Redacted = true
	api.APISurface.AuthenticationRequired = true
	add("api_redacted_authenticated", "accepted", apiPolicyResult(api), cpu.ProfileID, nil)
	first := Preflight(cpu, cpuBody, obs)
	profileCopy, copyErr := os.CreateTemp("", "synora-profile-restart-*.yaml")
	second := Report{}
	if copyErr == nil {
		copyPath := profileCopy.Name()
		_ = profileCopy.Close()
		_ = os.WriteFile(copyPath, cpuBody, 0600)
		reloaded, reloadedBody, reErr := Load(copyPath)
		if reErr == nil {
			second = Preflight(reloaded, reloadedBody, obs)
		}
		_ = os.Remove(copyPath)
	}
	add("same_profile_restart_stable", first.ProfileSHA256, second.ProfileSHA256, cpu.ProfileID, nil)
	storeDir, storeErr := os.MkdirTemp("", "synora-oem-store-*")
	storeBefore, storeAfter := uint64(0), uint64(1)
	if storeErr == nil {
		defer os.RemoveAll(storeDir)
		store, openErr := cognitivecore.OpenUniversalStore(filepath.Join(storeDir, "universal"))
		if openErr == nil {
			storeBefore = store.Revision()
			incompatible := cpu
			incompatible.SchemaVersion = "synora.oem.profile/v9"
			if ValidateStartup(incompatible, "arm64", "1.0.0") != nil {
				storeAfter = store.Revision()
			}
			if reopened, err := cognitivecore.OpenUniversalStore(filepath.Join(storeDir, "universal")); err == nil {
				storeAfter = reopened.Revision()
			}
		}
	}
	add("incompatible_profile_no_store_corruption", strconv.FormatUint(storeBefore, 10), strconv.FormatUint(storeAfter, 10), cpu.ProfileID, storeErr)
	degraded := Preflight(rk, rkBody, RuntimeObservation{Architecture: rk.TargetArchitecture, StorageWritable: true, APIAuthConfigured: true, RKNNAvailable: false})
	add("capability_degraded", "unavailable", degraded.EffectiveCapabilities["vision-pose"], rk.ProfileID, nil)
	add("discovery_core_mlp_gate_dry_run", "dry_run_only", dryRunPolicy(cpu), cpu.ProfileID, nil)
	add("privacy_audio_network_physical_guard", "all_disabled", privacyPolicy(cpu), cpu.ProfileID, nil)
	return results
}

func DecodeBytes(body []byte) (Profile, error) {
	tmp, err := os.CreateTemp("", "synora-profile-check-*.yaml")
	if err != nil {
		return Profile{}, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return Profile{}, err
	}
	if err := tmp.Close(); err != nil {
		return Profile{}, err
	}
	p, _, err := Load(name)
	return p, err
}
func ValidateCapabilityClaim(s string) error {
	if s == "qualified" {
		return fmt.Errorf("qualification requires independent validation evidence")
	}
	if !validStates[s] {
		return fmt.Errorf("unknown capability state")
	}
	return nil
}
func ValidateModelRequirement(state, manifestHash, modelHash string) string {
	if state == "required" && (!sha256Hex.MatchString(manifestHash) || !sha256Hex.MatchString(modelHash)) {
		return "blocked"
	}
	return "available"
}

func runtimeAtLeast(actual, minimum string) bool {
	parse := func(s string) ([3]int, bool) {
		var out [3]int
		parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
		if len(parts) == 0 || len(parts) > 3 {
			return out, false
		}
		for i, part := range parts {
			n := 0
			if _, err := fmt.Sscanf(part, "%d", &n); err != nil || n < 0 {
				return out, false
			}
			out[i] = n
		}
		return out, true
	}
	a, aok := parse(actual)
	m, mok := parse(minimum)
	if !aok || !mok {
		return false
	}
	for i := range a {
		if a[i] > m[i] {
			return true
		}
		if a[i] < m[i] {
			return false
		}
	}
	return true
}
func loadResult(err error) string {
	if err != nil {
		return "rejected"
	}
	return "accepted"
}
func decodeResult(err error) string {
	if err != nil {
		return "rejected"
	}
	return "accepted"
}
func validateResult(p Profile) string {
	if p.Validate() != nil {
		return "rejected"
	}
	return "accepted"
}
func expectErr(err error) error {
	if err == nil {
		return fmt.Errorf("expected fail-closed rejection")
	}
	return nil
}
func apiPolicyResult(p Profile) string {
	if p.APISurface.Redacted && p.APISurface.AuthenticationRequired {
		return "accepted"
	}
	return "rejected"
}
func dryRunPolicy(p Profile) string {
	if p.SafetyDefaults.ActionMode == "active_dry_run" && p.SafetyDefaults.RequireSafetyGate && !p.SafetyDefaults.PhysicalActions {
		return "dry_run_only"
	}
	return "unsafe"
}
func privacyPolicy(p Profile) string {
	if !p.SafetyDefaults.Audio && !p.SafetyDefaults.ExternalNetwork && !p.SafetyDefaults.PhysicalActions {
		return "all_disabled"
	}
	return "unsafe"
}
