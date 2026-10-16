// Package oemprofile validates explicit OEM hardware profiles and derives
// requested, observed, and effective capabilities without granting
// qualification from profile declarations.
package oemprofile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = "synora.oem.profile/v1"

var validStates = map[string]bool{"disabled": true, "not_configured": true, "prepared": true, "available": true, "qualified": true, "degraded": true, "unavailable": true, "blocked": true}
var sha256Hex = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

type Profile struct {
	ProfileID                 string                              `yaml:"profile_id" json:"profile_id"`
	SchemaVersion             string                              `yaml:"schema_version" json:"schema_version"`
	ProductVariant            string                              `yaml:"product_variant" json:"product_variant"`
	TargetArchitecture        string                              `yaml:"target_architecture" json:"target_architecture"`
	MinimumRuntimeVersion     string                              `yaml:"minimum_runtime_version" json:"minimum_runtime_version"`
	HardwareClass             string                              `yaml:"hardware_class" json:"hardware_class"`
	VisionInputs              []string                            `yaml:"vision_inputs" json:"vision_inputs"`
	ComputeCapabilities       []string                            `yaml:"compute_capabilities" json:"compute_capabilities"`
	StorageCapabilities       []string                            `yaml:"storage_capabilities" json:"storage_capabilities"`
	NetworkCapabilities       []string                            `yaml:"network_capabilities" json:"network_capabilities"`
	DeviceCapabilities        []string                            `yaml:"device_capabilities" json:"device_capabilities"`
	EnabledModules            []string                            `yaml:"enabled_modules" json:"enabled_modules"`
	AdapterSelection          map[string]string                   `yaml:"adapter_selection" json:"adapter_selection"`
	SafetyDefaults            SafetyDefaults                      `yaml:"safety_defaults" json:"safety_defaults"`
	APISurface                APISurface                          `yaml:"api_surface" json:"api_surface"`
	CompatibilityPolicy       Compatibility                       `yaml:"compatibility_policy" json:"compatibility_policy"`
	ModelManifestRequirements map[string]ModelManifestRequirement `yaml:"model_manifest_requirements,omitempty" json:"model_manifest_requirements,omitempty"`
}

type ModelManifestRequirement struct {
	Path             string `yaml:"path" json:"path"`
	SHA256           string `yaml:"sha256" json:"sha256"`
	ArtifactRequired bool   `yaml:"artifact_required" json:"artifact_required"`
}

type SafetyDefaults struct {
	ActionMode        string `yaml:"action_mode" json:"action_mode"`
	PhysicalActions   bool   `yaml:"physical_actions" json:"physical_actions"`
	Audio             bool   `yaml:"audio" json:"audio"`
	ExternalNetwork   bool   `yaml:"external_network" json:"external_network"`
	RequireSafetyGate bool   `yaml:"require_safety_gate" json:"require_safety_gate"`
}
type APISurface struct {
	Redacted               bool `yaml:"redacted" json:"redacted"`
	AuthenticationRequired bool `yaml:"authentication_required" json:"authentication_required"`
}
type Compatibility struct {
	UnknownFields     string `yaml:"unknown_fields" json:"unknown_fields"`
	MissingAdapter    string `yaml:"missing_adapter" json:"missing_adapter"`
	IncompatibleModel string `yaml:"incompatible_model" json:"incompatible_model"`
}

type RuntimeObservation struct {
	Architecture       string
	RuntimeVersion     string
	RKNNAvailable      bool
	V4L2Available      bool
	RTSPConfigured     bool
	StorageWritable    bool
	APIAuthConfigured  bool
	ModelManifestValid bool
}
type Report struct {
	ProfileID             string            `json:"profile_id"`
	ProfileSchemaVersion  string            `json:"profile_schema_version"`
	RequestedCapabilities map[string]string `json:"requested_capabilities"`
	ObservedCapabilities  map[string]string `json:"observed_capabilities"`
	EffectiveCapabilities map[string]string `json:"effective_capabilities"`
	BlockedCapabilities   []string          `json:"blocked_capabilities"`
	AdapterInventory      map[string]string `json:"adapter_inventory"`
	PreflightStatus       string            `json:"preflight_status"`
	CompatibilityStatus   string            `json:"compatibility_status"`
	Architecture          string            `json:"runtime_architecture"`
	RuntimeVersion        string            `json:"runtime_version"`
	ProfileSHA256         string            `json:"profile_sha256"`
	Errors                []string          `json:"errors,omitempty"`
}

// Load rejects unknown YAML fields and multiple YAML documents.
func Load(path string) (Profile, []byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(body))
	dec.KnownFields(true)
	var p Profile
	if err := dec.Decode(&p); err != nil {
		return Profile{}, nil, fmt.Errorf("decode OEM profile: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Profile{}, nil, errors.New("OEM profile must contain exactly one document")
	}
	if err := p.Validate(); err != nil {
		return Profile{}, body, err
	}
	return p, body, nil
}

func (p Profile) Validate() error {
	if p.ProfileID == "" || !safeName(p.ProfileID) {
		return errors.New("invalid profile_id")
	}
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported profile schema_version %q", p.SchemaVersion)
	}
	if p.ProductVariant == "" || p.TargetArchitecture == "" || p.MinimumRuntimeVersion == "" || p.HardwareClass == "" {
		return errors.New("required profile metadata missing")
	}
	if !validVersion(p.MinimumRuntimeVersion) {
		return errors.New("minimum_runtime_version must be numeric major.minor.patch")
	}
	if p.HardwareClass != "rk3588" && p.HardwareClass != "generic-cpu" {
		return fmt.Errorf("unsupported hardware_class %q", p.HardwareClass)
	}
	if (p.HardwareClass == "rk3588" && p.TargetArchitecture != "arm64") || (p.HardwareClass == "generic-cpu" && p.TargetArchitecture != "any" && p.TargetArchitecture != "amd64" && p.TargetArchitecture != "arm64") {
		return errors.New("hardware class and target architecture are inconsistent")
	}
	if p.TargetArchitecture != "arm64" && p.TargetArchitecture != "amd64" && p.TargetArchitecture != "any" {
		return fmt.Errorf("unsupported target architecture %q", p.TargetArchitecture)
	}
	for module, adapter := range p.AdapterSelection {
		if !knownModule(module) || !knownAdapter(adapter) {
			return fmt.Errorf("unknown module/adapter selection %q=%q", module, adapter)
		}
	}
	for module, req := range p.ModelManifestRequirements {
		if !knownModule(module) || req.Path == "" || filepath.IsAbs(req.Path) || strings.Contains(req.Path, "..") || !sha256Hex.MatchString(req.SHA256) {
			return fmt.Errorf("invalid model manifest requirement for %q", module)
		}
	}
	for _, module := range p.EnabledModules {
		if !knownModule(module) {
			return fmt.Errorf("unknown module %q", module)
		}
		if _, ok := p.AdapterSelection[module]; !ok {
			return fmt.Errorf("enabled module %q has no explicit adapter", module)
		}
	}
	for _, item := range []struct {
		name   string
		values []string
	}{{"vision_inputs", p.VisionInputs}, {"compute_capabilities", p.ComputeCapabilities}, {"storage_capabilities", p.StorageCapabilities}, {"network_capabilities", p.NetworkCapabilities}, {"device_capabilities", p.DeviceCapabilities}} {
		for _, v := range item.values {
			if !allowedCapability(item.name, v) {
				return fmt.Errorf("invalid %s entry", item.name)
			}
		}
	}
	if p.SafetyDefaults.ActionMode != "active_dry_run" || p.SafetyDefaults.PhysicalActions || p.SafetyDefaults.Audio || p.SafetyDefaults.ExternalNetwork || !p.SafetyDefaults.RequireSafetyGate {
		return errors.New("unsafe safety_defaults; dry-run, no physical/audio/external network, and Safety Gate required")
	}
	if !p.APISurface.Redacted || !p.APISurface.AuthenticationRequired {
		return errors.New("API must be authenticated and redacted")
	}
	if p.CompatibilityPolicy.UnknownFields != "reject" || p.CompatibilityPolicy.MissingAdapter != "unavailable" || p.CompatibilityPolicy.IncompatibleModel != "blocked" {
		return errors.New("compatibility policy must fail closed")
	}
	return nil
}

func allowedCapability(group, value string) bool {
	vocab := map[string]map[string]bool{"vision_inputs": {"v4l2": true, "rtsp": true, "mock": true, "replay": true}, "compute_capabilities": {"cpu": true, "rknn": true, "onnx": true}, "storage_capabilities": {"local_store": true}, "network_capabilities": {"local_api": true}, "device_capabilities": {"dry_run_actions": true}}
	return vocab[group][value]
}
func validVersion(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func Preflight(p Profile, body []byte, obs RuntimeObservation) Report {
	h := sha256.Sum256(body)
	r := Report{ProfileID: p.ProfileID, ProfileSchemaVersion: p.SchemaVersion, RequestedCapabilities: map[string]string{}, ObservedCapabilities: map[string]string{}, EffectiveCapabilities: map[string]string{}, BlockedCapabilities: []string{}, AdapterInventory: AdapterInventory(), PreflightStatus: "ready", CompatibilityStatus: "compatible", Architecture: obs.Architecture, RuntimeVersion: obs.RuntimeVersion, ProfileSHA256: hex.EncodeToString(h[:])}
	for _, c := range append(append(append([]string{}, p.ComputeCapabilities...), p.StorageCapabilities...), append(p.NetworkCapabilities, p.DeviceCapabilities...)...) {
		r.RequestedCapabilities[c] = "prepared"
	}
	for _, module := range p.EnabledModules {
		r.RequestedCapabilities["module:"+module] = "prepared"
	}
	for _, input := range p.VisionInputs {
		r.RequestedCapabilities["vision_input:"+input] = "prepared"
	}
	for module, req := range p.ModelManifestRequirements {
		manifest, err := readManifest(req.Path)
		state := "available"
		if err != nil {
			state = "unavailable"
		} else {
			sum := sha256.Sum256(manifest)
			if hex.EncodeToString(sum[:]) != strings.ToLower(req.SHA256) {
				state = "blocked"
			}
		}
		r.ObservedCapabilities["model_manifest:"+module] = state
		if state != "available" {
			r.BlockedCapabilities = appendUnique(r.BlockedCapabilities, module)
		}
		if req.ArtifactRequired && !obs.ModelManifestValid {
			r.EffectiveCapabilities[module] = "blocked"
			r.Errors = append(r.Errors, "required model artifact compatibility unavailable")
		}
	}
	r.ObservedCapabilities["architecture"] = "available"
	if obs.Architecture == "" || (p.TargetArchitecture != "any" && obs.Architecture != p.TargetArchitecture) {
		r.ObservedCapabilities["architecture"] = "unavailable"
		r.Errors = append(r.Errors, "runtime architecture does not match profile")
	}
	if !runtimeAtLeast(obs.RuntimeVersion, p.MinimumRuntimeVersion) {
		r.Errors = append(r.Errors, "runtime version below profile minimum")
	}
	r.ObservedCapabilities["storage"] = "unavailable"
	if obs.StorageWritable {
		r.ObservedCapabilities["storage"] = "available"
	}
	r.ObservedCapabilities["api_authentication"] = "unavailable"
	if obs.APIAuthConfigured {
		r.ObservedCapabilities["api_authentication"] = "available"
	}
	for module, adapter := range p.AdapterSelection {
		key := module + "." + adapter
		state := "available"
		if !adapterSupported(module, adapter) {
			state = "unavailable"
			r.BlockedCapabilities = append(r.BlockedCapabilities, module)
		}
		if adapter == "v4l2" && !obs.V4L2Available {
			state = "unavailable"
			r.BlockedCapabilities = appendUnique(r.BlockedCapabilities, module)
		}
		if adapter == "rtsp" && !obs.RTSPConfigured {
			state = "unavailable"
			r.BlockedCapabilities = appendUnique(r.BlockedCapabilities, module)
		}
		if adapter == "rk3588-rknn" && !obs.RKNNAvailable {
			state = "unavailable"
			r.BlockedCapabilities = appendUnique(r.BlockedCapabilities, module)
		}
		r.ObservedCapabilities[key] = state
		if contains(p.EnabledModules, module) && state == "available" {
			r.EffectiveCapabilities[module] = "available"
		} else if contains(p.EnabledModules, module) {
			r.EffectiveCapabilities[module] = "unavailable"
		}
	}
	for _, c := range p.ComputeCapabilities {
		r.EffectiveCapabilities[c] = "unavailable"
		if c == "cpu" {
			r.EffectiveCapabilities[c] = "available"
		}
		if c == "rknn" && obs.RKNNAvailable {
			r.EffectiveCapabilities[c] = "available"
		}
	}
	for _, c := range p.StorageCapabilities {
		r.EffectiveCapabilities[c] = "unavailable"
		if obs.StorageWritable {
			r.EffectiveCapabilities[c] = "available"
		}
	}
	for _, c := range p.NetworkCapabilities {
		r.EffectiveCapabilities[c] = "unavailable"
		if c == "local_api" && obs.APIAuthConfigured {
			r.EffectiveCapabilities[c] = "available"
		}
	}
	for _, input := range p.VisionInputs {
		state := "unavailable"
		switch input {
		case "mock", "replay":
			state = "available"
		case "v4l2":
			if obs.V4L2Available {
				state = "available"
			}
		case "rtsp":
			if obs.RTSPConfigured {
				state = "available"
			}
		}
		r.ObservedCapabilities["vision_input:"+input] = state
		r.EffectiveCapabilities["vision_input:"+input] = state
		if state != "available" {
			r.BlockedCapabilities = appendUnique(r.BlockedCapabilities, "vision_input:"+input)
		}
	}
	for _, capability := range p.DeviceCapabilities {
		state := "unavailable"
		if capability == "dry_run_actions" && p.SafetyDefaults.ActionMode == "active_dry_run" && p.SafetyDefaults.RequireSafetyGate && !p.SafetyDefaults.PhysicalActions {
			state = "available"
		}
		r.ObservedCapabilities[capability] = state
		r.EffectiveCapabilities[capability] = state
	}
	if len(r.BlockedCapabilities) > 0 {
		r.CompatibilityStatus = "degraded"
		r.PreflightStatus = "degraded"
	}
	if len(r.Errors) > 0 {
		r.PreflightStatus = "blocked"
	}
	sort.Strings(r.BlockedCapabilities)
	return r
}

func readManifest(path string) ([]byte, error) {
	if body, err := os.ReadFile(path); err == nil {
		return body, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return os.ReadFile(filepath.Join(dir, path))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil, fmt.Errorf("manifest unavailable")
}

// ValidateStartup rejects invalid/incompatible profile contracts before
// Discovery creates directories, connects a bus, or starts adapters.
func ValidateStartup(p Profile, architecture, runtimeVersion string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.TargetArchitecture != "any" && p.TargetArchitecture != architecture {
		return fmt.Errorf("profile target architecture %q does not match runtime %q", p.TargetArchitecture, architecture)
	}
	if !runtimeAtLeast(runtimeVersion, p.MinimumRuntimeVersion) {
		return errors.New("runtime below profile minimum")
	}
	return nil
}

func ObserveHost() RuntimeObservation {
	return RuntimeObservation{Architecture: runtime.GOARCH, RuntimeVersion: "1.0.0", StorageWritable: func() bool {
		f, e := os.CreateTemp("", "synora-oem-preflight-")
		if e != nil {
			return false
		}
		n := f.Name()
		_ = f.Close()
		_ = os.Remove(n)
		return true
	}(), APIAuthConfigured: func() bool { _, e := os.Stat("configs/auth.yaml"); return e == nil }(), V4L2Available: func() bool { matches, _ := filepath.Glob("/dev/video*"); return len(matches) > 0 }(), RTSPConfigured: func() bool { _, e := os.Stat("/etc/synora/mediamtx.yml"); return e == nil }(), RKNNAvailable: func() bool {
		for _, p := range []string{"/dev/rknpu", "/dev/rknn"} {
			if _, e := os.Stat(p); e == nil {
				return true
			}
		}
		return false
	}()}
}
func ResolveProfilePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", errors.New("absolute profile paths are not accepted")
	}
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", errors.New("profile path traversal rejected")
		}
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("profile path traversal rejected")
	}
	return clean, nil
}
func AdapterInventory() map[string]string {
	return map[string]string{"vision-person.rk3588-rknn": "included_optional_runtime", "vision-person.mock": "included_test_only", "vision-pose.rk3588-rknn": "included_optional_runtime", "vision-pose.replay": "included_test_only", "camera-health.v4l2": "included_behind_discovery", "camera-health.mock": "included_test_only", "communication.mock": "included_test_only", "device-control.mock": "included_dry_run_only"}
}
func knownModule(s string) bool {
	switch s {
	case "vision-person", "vision-pose", "vision-face", "vision-vehicle", "vision-plate", "vision-animal", "camera-health", "device-control", "communication":
		return true
	}
	return false
}
func knownAdapter(s string) bool {
	switch s {
	case "rk3588-rknn", "cpu-onnx", "v4l2", "rtsp", "mock", "replay", "driver-specific":
		return true
	}
	return false
}
func adapterSupported(m, a string) bool {
	if a == "mock" || a == "replay" {
		return true
	}
	return ((m == "vision-pose" || m == "vision-person") && a == "rk3588-rknn") || (m == "camera-health" && (a == "v4l2" || a == "rtsp"))
}
func safeName(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func appendUnique(a []string, s string) []string {
	if !contains(a, s) {
		return append(a, s)
	}
	return a
}
