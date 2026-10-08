package visionsuite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"synora/pkg/contract"
)

const (
	ManifestSchema         = "synora.vision.media-suite-manifest/v1"
	ExecutionReplay        = "replay"
	ExecutionSimulatedTest = "simulated_test"
	ModuleFace             = "face_recognition"
	ModuleVehicle          = "vehicle_presence"
	ModulePlate            = "plate_reading"
	ModuleAnimal           = "animal_presence"
	ModuleCamera           = "camera_health"
	ModulePose             = "human_pose"
)

var (
	sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	opaqueSubject = regexp.MustCompile(`^resident_test_[0-9]{2}$`)
	caseIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
	refPattern    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{1,127}$`)
	errorCode     = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)
	envName       = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
	modules       = map[string]struct{}{
		ModuleFace: {}, ModuleVehicle: {}, ModulePlate: {}, ModuleAnimal: {}, ModuleCamera: {}, ModulePose: {},
	}
	conditionTagVocabulary = map[string]struct{}{
		"indoor": {}, "outdoor": {}, "day": {}, "night": {}, "low_light": {},
		"frontal": {}, "profile": {}, "occluded": {}, "distant": {},
		"single_subject": {}, "multi_subject": {}, "empty_scene": {}, "stationary": {}, "moving": {},
		"clear": {}, "degraded": {}, "resident_known": {}, "identity_unknown": {}, "identity_ambiguous": {},
		"vehicle_present": {}, "animal_present": {}, "no_target": {}, "healthy_camera": {},
		"stream_missing": {}, "frozen_frame": {}, "tamper_suspected": {},
	}
)

type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	Version       string `json:"version"`
	MediaRootEnv  string `json:"media_root_env"`
	Suites        []Slot `json:"slots"`
}

type ModuleRegistry struct {
	SchemaVersion string          `json:"schema_version"`
	Modules       []RegistryEntry `json:"modules"`
}

type RegistryEntry struct {
	Name                  string          `json:"name"`
	Role                  string          `json:"role"`
	InitialStatus         string          `json:"initial_status"`
	InputContract         string          `json:"input_contract"`
	OutputContract        string          `json:"output_contract"`
	Preconditions         []string        `json:"preconditions"`
	ForbiddenData         []string        `json:"forbidden_data"`
	ModelState            string          `json:"model_state"`
	StateReason           string          `json:"state_reason"`
	ModelVersion          *string         `json:"model_version"`
	ModelSHA256           *string         `json:"model_sha256"`
	ModelPathEnv          string          `json:"model_path_env"`
	MediaRootEnv          string          `json:"media_root_env"`
	ManifestPath          string          `json:"manifest_path"`
	ManifestSHA256        string          `json:"manifest_sha256"`
	GalleryPathEnv        string          `json:"gallery_path_env"`
	QualityMetrics        []string        `json:"quality_metrics"`
	LatencyP95TargetMS    *float64        `json:"latency_p95_target_ms"`
	QualificationCriteria []string        `json:"qualification_criteria"`
	MissingMediaState     string          `json:"missing_media_state"`
	MissingModelState     string          `json:"missing_model_state"`
	SnapshotV3            SnapshotV3Scope `json:"snapshot_v3"`
}

type SnapshotV3Scope struct {
	Encoded    []string `json:"encoded"`
	NotEncoded []string `json:"not_encoded"`
}

type TechnicalMetadata struct {
	Container string `json:"container"`
	Codec     string `json:"codec"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Frames    int    `json:"frame_count"`
	Duration  string `json:"duration"`
}

type MediaProbe struct {
	Container string  `json:"container"`
	Codec     string  `json:"codec"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Frames    int     `json:"frame_count"`
	Duration  float64 `json:"duration_seconds"`
}

type Expectation struct {
	State             string `json:"state"`
	Presence          string `json:"presence,omitempty"`
	ConfidenceMinimum *int   `json:"confidence_minimum_percent,omitempty"`
	SubjectRef        string `json:"subject_ref,omitempty"`
}

type Slot struct {
	Suite            string            `json:"suite"`
	CaseID           string            `json:"case_id"`
	ClipRelativePath string            `json:"clip_relative_path"`
	ClipSHA256       string            `json:"clip_sha256"`
	ConditionTags    []string          `json:"condition_tags"`
	Technical        TechnicalMetadata `json:"technical_metadata"`
	Module           string            `json:"module"`
	Expected         Expectation       `json:"expected"`
	Provenance       string            `json:"provenance"`
	License          string            `json:"license"`
	AssetStatus      string            `json:"asset_status"`
	NotRunReason     string            `json:"not_run_reason,omitempty"`
}

type ModuleDescriptor struct {
	Name             string `json:"name"`
	State            string `json:"state"`
	ModelVersion     string `json:"model_version,omitempty"`
	ModelSHA256      string `json:"model_sha256,omitempty"`
	InputCompatible  bool   `json:"input_compatible"`
	OutputCompatible bool   `json:"output_compatible"`
	Reason           string `json:"reason"`
}

type ModuleInput struct {
	MediaPath     string
	ModelPath     string
	GalleryPath   string
	ExecutionMode string
	Suite         string
	CaseID        string
	Module        string
}

type ModuleResult struct {
	Evidence          *contract.VisionEvidenceV1
	Latency           time.Duration
	StructuredError   string
	InferenceExecuted bool
}

type Module interface {
	Descriptor() ModuleDescriptor
	Run(context.Context, ModuleInput) (ModuleResult, error)
}

type PipelineResult struct {
	CoreReached        bool `json:"core_reached"`
	StoreWritten       bool `json:"store_written"`
	SnapshotEncoded    bool `json:"snapshot_encoded"`
	MLPExecuted        bool `json:"mlp_executed"`
	SafetyGateChecked  bool `json:"safety_gate_checked"`
	DryRunResult       bool `json:"dry_run_result"`
	PhysicalAction     bool `json:"physical_action_executed"`
	AudioRendered      bool `json:"audio_rendered"`
	NetworkAccess      bool `json:"network_access"`
	RawVisionForwarded bool `json:"raw_vision_forwarded"`
}

type EvidencePipeline func(context.Context, contract.VisionEvidenceV1) (PipelineResult, error)

type LatencyStats struct {
	Count  int     `json:"count"`
	MinMS  float64 `json:"min_ms"`
	MeanMS float64 `json:"mean_ms"`
	P95MS  float64 `json:"p95_ms"`
	MaxMS  float64 `json:"max_ms"`
}

type CaseReport struct {
	Suite                 string          `json:"suite"`
	CaseID                string          `json:"case_id"`
	Module                string          `json:"module"`
	ExecutionMode         string          `json:"execution_mode"`
	Status                string          `json:"status"`
	ReadinessState        string          `json:"readiness_state"`
	MediaStatus           string          `json:"media_status"`
	MediaMetadataVerified bool            `json:"media_metadata_verified"`
	MediaMetadata         *MediaProbe     `json:"media_metadata,omitempty"`
	ModelStatus           string          `json:"model_status"`
	Reason                string          `json:"reason"`
	InferenceExecuted     bool            `json:"inference_executed"`
	LatencyMS             float64         `json:"latency_ms,omitempty"`
	ConfidencePercent     int             `json:"confidence_percent,omitempty"`
	SemanticMatch         *bool           `json:"semantic_match,omitempty"`
	SemanticResult        string          `json:"semantic_result,omitempty"`
	Qualified             bool            `json:"qualified"`
	Pipeline              *PipelineResult `json:"pipeline,omitempty"`
}

type Report struct {
	SchemaVersion       string                      `json:"schema_version"`
	ManifestSHA256      string                      `json:"manifest_sha256"`
	RegistrySHA256      string                      `json:"module_registry_sha256,omitempty"`
	ExecutionMode       string                      `json:"execution_mode"`
	Action              string                      `json:"action"`
	SuiteCounts         map[string]int              `json:"suite_counts"`
	Modules             map[string]ModuleDescriptor `json:"modules"`
	LatencyByModule     map[string]LatencyStats     `json:"latency_by_module"`
	Cases               []CaseReport                `json:"cases"`
	PassedCount         int                         `json:"passed_count"`
	FailedCount         int                         `json:"failed_count"`
	MediaAbsent         int                         `json:"media_absent_count"`
	MediaQuarantined    int                         `json:"media_quarantined_count"`
	ModelAbsent         int                         `json:"model_absent_count"`
	ModelUnavailable    int                         `json:"model_unavailable_count"`
	ModelFailed         int                         `json:"model_failed_count"`
	Executed            int                         `json:"executed_count"`
	Mismatched          int                         `json:"semantic_mismatched_count"`
	Qualified           int                         `json:"qualified_count"`
	InferenceRun        bool                        `json:"inference_executed"`
	QualificationStatus string                      `json:"qualification_status"`
}

func LoadManifest(path string) (Manifest, string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, "", err
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, "", err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Manifest{}, "", errors.New("manifest contains trailing JSON")
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, "", err
	}
	digest := sha256.Sum256(body)
	return manifest, hex.EncodeToString(digest[:]), nil
}

func LoadRegistry(path string) (ModuleRegistry, map[string]ModuleDescriptor, string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return ModuleRegistry{}, nil, "", err
	}
	var registry ModuleRegistry
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return ModuleRegistry{}, nil, "", err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ModuleRegistry{}, nil, "", errors.New("module registry contains trailing JSON")
	}
	if registry.SchemaVersion != "synora.vision.module-registry/v1" || len(registry.Modules) != len(modules) {
		return ModuleRegistry{}, nil, "", errors.New("invalid Vision module registry header or inventory")
	}
	states := make(map[string]ModuleDescriptor, len(registry.Modules))
	for _, entry := range registry.Modules {
		if _, ok := modules[entry.Name]; !ok || states[entry.Name].Name != "" {
			return ModuleRegistry{}, nil, "", fmt.Errorf("unknown or duplicate module registry entry %q", entry.Name)
		}
		switch entry.InitialStatus {
		case "not_configured", "unavailable", "simulated_test", "dry_run", "available", "failed":
		default:
			return ModuleRegistry{}, nil, "", fmt.Errorf("invalid initial module status %q", entry.InitialStatus)
		}
		if entry.ModelState != "not_configured" && entry.ModelState != "unavailable" && entry.ModelState != "available" && entry.ModelState != "failed" {
			return ModuleRegistry{}, nil, "", fmt.Errorf("invalid model state for %q", entry.Name)
		}
		if entry.Role == "" || !errorCode.MatchString(entry.StateReason) || entry.InputContract != "external_media_slot/v1" || entry.OutputContract != contract.EventVisionEvidenceV1 || len(entry.Preconditions) == 0 || len(entry.ForbiddenData) == 0 || len(entry.QualityMetrics) == 0 || len(entry.QualificationCriteria) == 0 {
			return ModuleRegistry{}, nil, "", fmt.Errorf("module %q lacks its frozen contract metadata", entry.Name)
		}
		if !envName.MatchString(entry.MediaRootEnv) || !safeRelativePath(entry.ManifestPath) || entry.ManifestPath == "" || !sha256Pattern.MatchString(entry.ManifestSHA256) || (entry.ModelPathEnv != "" && !envName.MatchString(entry.ModelPathEnv)) || (entry.GalleryPathEnv != "" && !envName.MatchString(entry.GalleryPathEnv)) {
			return ModuleRegistry{}, nil, "", fmt.Errorf("module %q contains invalid external path configuration", entry.Name)
		}
		if entry.Name == ModuleFace && entry.GalleryPathEnv == "" || entry.Name != ModuleFace && entry.GalleryPathEnv != "" {
			return ModuleRegistry{}, nil, "", fmt.Errorf("module %q has invalid gallery path declaration", entry.Name)
		}
		if entry.ModelState == "available" && (entry.ModelVersion == nil || entry.ModelSHA256 == nil || entry.ModelPathEnv == "") {
			return ModuleRegistry{}, nil, "", fmt.Errorf("module %q available model must have version, hash and external path variable", entry.Name)
		}
		if entry.LatencyP95TargetMS != nil && (math.IsNaN(*entry.LatencyP95TargetMS) || math.IsInf(*entry.LatencyP95TargetMS, 0) || *entry.LatencyP95TargetMS < 0.001 || *entry.LatencyP95TargetMS > 3600000) {
			return ModuleRegistry{}, nil, "", fmt.Errorf("module %q has invalid latency target", entry.Name)
		}
		if entry.MissingMediaState != "not_run" && entry.MissingMediaState != "unavailable" || entry.MissingModelState != "not_configured" && entry.MissingModelState != "unavailable" {
			return ModuleRegistry{}, nil, "", fmt.Errorf("module %q has unsafe missing-input behavior", entry.Name)
		}
		if len(entry.SnapshotV3.Encoded) == 0 && len(entry.SnapshotV3.NotEncoded) == 0 {
			return ModuleRegistry{}, nil, "", fmt.Errorf("module %q lacks a V3 projection declaration", entry.Name)
		}
		for _, value := range append(append([]string{}, entry.Preconditions...), append(entry.ForbiddenData, append(entry.QualityMetrics, entry.QualificationCriteria...)...)...) {
			if !errorCode.MatchString(value) {
				return ModuleRegistry{}, nil, "", fmt.Errorf("module %q contains invalid metadata token", entry.Name)
			}
		}
		version, hash := "", ""
		if entry.ModelVersion != nil {
			version = *entry.ModelVersion
		}
		if entry.ModelSHA256 != nil {
			hash = *entry.ModelSHA256
		}
		if entry.ModelState == "available" && (version == "" || !sha256Pattern.MatchString(hash)) {
			return ModuleRegistry{}, nil, "", fmt.Errorf("available module %q requires a version and model SHA-256", entry.Name)
		}
		if entry.ModelState != "available" && (version != "" || hash != "") {
			return ModuleRegistry{}, nil, "", fmt.Errorf("module %q cannot declare model identity unless model is available", entry.Name)
		}
		states[entry.Name] = ModuleDescriptor{Name: entry.Name, State: entry.ModelState, ModelVersion: version, ModelSHA256: hash, InputCompatible: false, OutputCompatible: true, Reason: firstNonEmpty(entry.StateReason, "model_not_configured")}
	}
	for name := range modules {
		if _, ok := states[name]; !ok {
			return ModuleRegistry{}, nil, "", fmt.Errorf("module registry is missing %q", name)
		}
	}
	digest := sha256.Sum256(body)
	return registry, states, hex.EncodeToString(digest[:]), nil
}

// ValidateManifestPin binds every selected suite to the reviewed manifest
// digest in the canonical module registry before media paths are inspected.
func ValidateManifestPin(manifest Manifest, digest string, registry ModuleRegistry) error {
	if !sha256Pattern.MatchString(digest) {
		return errors.New("suite manifest SHA-256 is missing or invalid")
	}
	entries := make(map[string]RegistryEntry, len(registry.Modules))
	for _, entry := range registry.Modules {
		entries[entry.Name] = entry
	}
	for _, slot := range manifest.Suites {
		entry, ok := entries[slot.Module]
		if !ok || entry.ManifestSHA256 != digest {
			return fmt.Errorf("module %q manifest is not pinned by the canonical registry", slot.Module)
		}
	}
	return nil
}

func ValidateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != ManifestSchema || manifest.Version == "" || manifest.MediaRootEnv == "" || len(manifest.Suites) == 0 {
		return errors.New("invalid vision media suite manifest header")
	}
	seen := make(map[string]struct{}, len(manifest.Suites))
	for _, slot := range manifest.Suites {
		if slot.Suite == "" || !caseIDPattern.MatchString(slot.CaseID) || slot.Module == "" || !refPattern.MatchString(slot.Provenance) || !refPattern.MatchString(slot.License) {
			return fmt.Errorf("incomplete slot %q", slot.CaseID)
		}
		if slot.Technical.Width < 0 || slot.Technical.Height < 0 || slot.Technical.Frames < 0 || slot.Technical.Container == "" || slot.Technical.Codec == "" || slot.Technical.Duration == "" {
			return fmt.Errorf("invalid technical metadata in slot %q", slot.CaseID)
		}
		if len(slot.ConditionTags) == 0 {
			return fmt.Errorf("slot %q requires condition_tags", slot.CaseID)
		}
		seenTags := make(map[string]bool, len(slot.ConditionTags))
		for _, tag := range slot.ConditionTags {
			if _, ok := conditionTagVocabulary[tag]; !ok || seenTags[tag] {
				return fmt.Errorf("unknown or duplicate condition tag %q in slot %q", tag, slot.CaseID)
			}
			seenTags[tag] = true
		}
		if !conditionTagsRelevant(slot) {
			return fmt.Errorf("condition_tags do not describe suite %q in slot %q", slot.Suite, slot.CaseID)
		}
		if _, ok := modules[slot.Module]; !ok {
			return fmt.Errorf("unknown module %q", slot.Module)
		}
		if expectedModule := moduleForSuite(slot.Suite); expectedModule != slot.Module {
			return fmt.Errorf("suite %q requires module %q", slot.Suite, expectedModule)
		}
		key := slot.Suite + "/" + slot.CaseID
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate slot %q", key)
		}
		seen[key] = struct{}{}
		if !safeRelativePath(slot.ClipRelativePath) {
			return fmt.Errorf("unsafe clip_relative_path in slot %q", slot.CaseID)
		}
		if strings.Contains(slot.Provenance, "://") || strings.Contains(slot.License, "://") || strings.Contains(slot.NotRunReason, "://") {
			return fmt.Errorf("URLs are not allowed in slot metadata %q", slot.CaseID)
		}
		switch slot.AssetStatus {
		case "placeholder":
			if slot.ClipSHA256 != strings.Repeat("0", 64) || slot.NotRunReason == "" || slot.Technical.Frames != 0 {
				return fmt.Errorf("placeholder %q must have a zero hash and a not-run reason", slot.CaseID)
			}
		case "available", "quarantined":
			if !sha256Pattern.MatchString(slot.ClipSHA256) || slot.ClipSHA256 == strings.Repeat("0", 64) {
				return fmt.Errorf("invalid asset hash in slot %q", slot.CaseID)
			}
			if slot.AssetStatus == "available" && (slot.Technical.Width <= 0 || slot.Technical.Height <= 0 || slot.Technical.Frames <= 0 || strings.EqualFold(slot.Technical.Codec, "pending")) {
				return fmt.Errorf("available media %q requires codec, dimensions, frame count and hash", slot.CaseID)
			}
			if slot.AssetStatus == "available" {
				duration, err := time.ParseDuration(slot.Technical.Duration)
				if err != nil || duration <= 0 {
					return fmt.Errorf("available media %q requires a positive parseable duration", slot.CaseID)
				}
			}
		default:
			return fmt.Errorf("invalid asset status in slot %q", slot.CaseID)
		}
		if err := validateExpected(slot.Expected); err != nil {
			return fmt.Errorf("slot %q: %w", slot.CaseID, err)
		}
		if slot.Expected.SubjectRef != "" && slot.Suite != "face_known" {
			return fmt.Errorf("subject_ref is permitted only in the external face_known expectation")
		}
		if !validExpectedState(slot.Suite, slot.Expected.State) {
			return fmt.Errorf("invalid expected state %q for suite %q", slot.Expected.State, slot.Suite)
		}
	}
	counts := make(map[string]int)
	for _, slot := range manifest.Suites {
		counts[slot.Suite]++
	}
	for suite, minimum := range map[string]int{"face_known": 10, "face_unknown": 10, "face_ambiguous": 5, "vehicle_presence": 10, "plate_reading": 10, "animal_presence": 10, "camera_health": 10} {
		if counts[suite] < minimum {
			return fmt.Errorf("suite %q has %d slots; minimum is %d", suite, counts[suite], minimum)
		}
	}
	return nil
}

func conditionTagsRelevant(slot Slot) bool {
	tags := make(map[string]bool, len(slot.ConditionTags))
	for _, tag := range slot.ConditionTags {
		tags[tag] = true
	}
	switch slot.Suite {
	case "face_known":
		return tags["resident_known"]
	case "face_unknown":
		return tags["identity_unknown"]
	case "face_ambiguous":
		return tags["identity_ambiguous"]
	case "vehicle_presence", "plate_reading":
		return tags["vehicle_present"] || tags["no_target"]
	case "animal_presence":
		return tags["animal_present"] || tags["no_target"]
	case "camera_health":
		return tags["healthy_camera"] || tags["stream_missing"] || tags["frozen_frame"] || tags["tamper_suspected"]
	default:
		return false
	}
}

func moduleForSuite(suite string) string {
	switch suite {
	case "face_known", "face_unknown", "face_ambiguous":
		return ModuleFace
	case "vehicle_presence":
		return ModuleVehicle
	case "plate_reading":
		return ModulePlate
	case "animal_presence":
		return ModuleAnimal
	case "camera_health":
		return ModuleCamera
	default:
		return ""
	}
}

func validExpectedState(suite, state string) bool {
	allowed := map[string]map[string]bool{
		"face_known":       {"recognized": true},
		"face_unknown":     {"unknown": true},
		"face_ambiguous":   {"ambiguous_or_unavailable": true},
		"vehicle_presence": {"present": true, "absent": true, "ambiguous": true},
		"plate_reading":    {"recognized": true, "unknown": true, "ambiguous": true, "unavailable": true},
		"animal_presence":  {"present": true, "absent": true, "ambiguous": true},
		"camera_health":    {"healthy": true, "unavailable": true, "degraded": true, "tamper_suspected": true},
	}
	return allowed[suite][state]
}

func validateExpected(expected Expectation) error {
	if expected.State == "" {
		return errors.New("semantic expected state is required")
	}
	if expected.ConfidenceMinimum != nil && (*expected.ConfidenceMinimum < 0 || *expected.ConfidenceMinimum > 100) {
		return errors.New("confidence minimum must be in [0,100]")
	}
	if expected.SubjectRef != "" && !opaqueSubject.MatchString(expected.SubjectRef) {
		return errors.New("subject_ref must be an opaque resident_test_NN reference")
	}
	return nil
}

func safeRelativePath(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.Contains(value, ":") || strings.Contains(value, "\\") {
		return false
	}
	clean := filepath.Clean(value)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func InactiveModules() map[string]ModuleDescriptor {
	result := make(map[string]ModuleDescriptor, len(modules))
	for name := range modules {
		state := "not_configured"
		reason := "no_model_or_plugin_configured"
		if name == ModulePose {
			state, reason = "unavailable", "runtime_or_model_not_proven_available"
		}
		result[name] = ModuleDescriptor{Name: name, State: state, InputCompatible: false, OutputCompatible: true, Reason: reason}
	}
	return result
}

func List(manifest Manifest, digest string) Report {
	report := baseReport(manifest, digest, "list")
	for _, slot := range manifest.Suites {
		report.SuiteCounts[slot.Suite]++
	}
	return report
}

func Filter(manifest Manifest, suite string) (Manifest, error) {
	if suite == "" {
		return manifest, nil
	}
	filtered := manifest
	filtered.Suites = make([]Slot, 0, len(manifest.Suites))
	for _, slot := range manifest.Suites {
		if slot.Suite == suite {
			filtered.Suites = append(filtered.Suites, slot)
		}
	}
	if len(filtered.Suites) == 0 {
		return Manifest{}, fmt.Errorf("suite %q is not declared", suite)
	}
	return filtered, nil
}

func FilterCase(manifest Manifest, caseID string) (Manifest, error) {
	if caseID == "" {
		return manifest, nil
	}
	filtered := manifest
	filtered.Suites = make([]Slot, 0, 1)
	for _, slot := range manifest.Suites {
		if slot.CaseID == caseID {
			filtered.Suites = append(filtered.Suites, slot)
		}
	}
	if len(filtered.Suites) == 0 {
		return Manifest{}, fmt.Errorf("case %q is not declared", caseID)
	}
	return filtered, nil
}

func Inspect(manifest Manifest, digest, action, mediaRoot string, moduleStates map[string]ModuleDescriptor) Report {
	return InspectWithProbe(context.Background(), manifest, digest, action, mediaRoot, moduleStates, ProbeMediaMetadata)
}

func InspectWithProbe(ctx context.Context, manifest Manifest, digest, action, mediaRoot string, moduleStates map[string]ModuleDescriptor, probe func(context.Context, string) (MediaProbe, error)) Report {
	report := baseReport(manifest, digest, action)
	if moduleStates == nil {
		moduleStates = InactiveModules()
	}
	if probe == nil {
		probe = ProbeMediaMetadata
	}
	report.Modules = moduleStates
	for _, slot := range manifest.Suites {
		report.SuiteCounts[slot.Suite]++
		state := moduleStates[slot.Module]
		caseReport := CaseReport{Suite: slot.Suite, CaseID: slot.CaseID, Module: slot.Module, ExecutionMode: "not_run", Status: "media_absent", MediaStatus: "absent", ModelStatus: state.State, Reason: slot.NotRunReason, Qualified: false, InferenceExecuted: false}
		if slot.AssetStatus == "quarantined" {
			caseReport.Status, caseReport.MediaStatus, caseReport.Reason = "quarantined", "quarantined", "asset is explicitly quarantined"
		} else if mediaRoot != "" {
			mediaPath, err := containedFile(mediaRoot, slot.ClipRelativePath)
			if err == nil {
				if slot.AssetStatus == "placeholder" {
					caseReport.Status, caseReport.MediaStatus, caseReport.Reason = "media_absent", "absent", "manifest slot is a placeholder; deposited file is not implicitly trusted"
				} else if actualHash, hashErr := fileSHA256(mediaPath); hashErr != nil || actualHash != slot.ClipSHA256 {
					caseReport.Status, caseReport.MediaStatus, caseReport.Reason = "media_quarantined", "quarantined", "asset is missing or SHA-256 verification failed"
				} else {
					metadata, probeErr := probe(ctx, mediaPath)
					if probeErr != nil || !metadataMatches(slot.Technical, metadata) {
						caseReport.Status, caseReport.MediaStatus, caseReport.Reason = "media_quarantined", "quarantined", "media_metadata_mismatch_or_probe_unavailable"
					} else {
						caseReport.MediaMetadataVerified, caseReport.MediaMetadata = true, &metadata
						caseReport.MediaStatus = "available"
						caseReport.Status, caseReport.Reason = "model_unavailable", firstNonEmpty(state.Reason, "module_not_available")
					}
				}
			} else if errors.Is(err, os.ErrNotExist) && slot.AssetStatus == "available" {
				caseReport.Status, caseReport.MediaStatus, caseReport.Reason = "media_absent", "absent", "manifested media asset is missing"
			} else if !errors.Is(err, os.ErrNotExist) {
				caseReport.Status, caseReport.MediaStatus, caseReport.Reason = "media_quarantined", "quarantined", "unsafe media path or unreadable asset"
			}
		}
		switch caseReport.MediaStatus {
		case "absent":
			report.MediaAbsent++
		case "quarantined":
			report.MediaQuarantined++
		}
		switch caseReport.Status {
		case "executed":
			report.Executed++
		case "semantic_mismatched":
			report.Mismatched++
		case "qualified":
			report.Qualified++
		case "failed":
			report.FailedCount++
		}
		switch caseReport.ModelStatus {
		case "not_configured":
			report.ModelAbsent++
		case "unavailable":
			report.ModelUnavailable++
		case "failed":
			report.ModelFailed++
		}
		report.Cases = append(report.Cases, caseReport)
	}
	applyReadinessStates(&report)
	return report
}

// Execute is the extension point for future module plugins. Fixture expectations
// are deliberately not part of ModuleInput and therefore cannot influence inference.
func Execute(ctx context.Context, manifest Manifest, digest, mediaRoot string, modelPaths map[string]string, plugins map[string]Module, pipeline EvidencePipeline) Report {
	return ExecuteWithStates(ctx, manifest, digest, mediaRoot, InactiveModules(), modelPaths, plugins, pipeline)
}

func ExecuteWithStates(ctx context.Context, manifest Manifest, digest, mediaRoot string, moduleStates map[string]ModuleDescriptor, modelPaths map[string]string, plugins map[string]Module, pipeline EvidencePipeline) Report {
	return ExecuteWithStatesAndProbe(ctx, manifest, digest, mediaRoot, moduleStates, modelPaths, plugins, pipeline, ProbeMediaMetadata)
}

func ExecuteWithStatesAndProbe(ctx context.Context, manifest Manifest, digest, mediaRoot string, moduleStates map[string]ModuleDescriptor, modelPaths map[string]string, plugins map[string]Module, pipeline EvidencePipeline, probe func(context.Context, string) (MediaProbe, error)) Report {
	return ExecuteWithInputsAndProbe(ctx, manifest, digest, mediaRoot, moduleStates, modelPaths, nil, plugins, pipeline, probe)
}

func ExecuteWithInputsAndProbe(ctx context.Context, manifest Manifest, digest, mediaRoot string, moduleStates map[string]ModuleDescriptor, modelPaths, galleryPaths map[string]string, plugins map[string]Module, pipeline EvidencePipeline, probe func(context.Context, string) (MediaProbe, error)) Report {
	return ExecuteWithModeAndProbe(ctx, manifest, digest, mediaRoot, moduleStates, modelPaths, galleryPaths, plugins, pipeline, ExecutionReplay, probe)
}

func ExecuteWithModeAndProbe(ctx context.Context, manifest Manifest, digest, mediaRoot string, moduleStates map[string]ModuleDescriptor, modelPaths, galleryPaths map[string]string, plugins map[string]Module, pipeline EvidencePipeline, executionMode string, probe func(context.Context, string) (MediaProbe, error)) Report {
	report := baseReport(manifest, digest, "run")
	if executionMode != ExecutionReplay && executionMode != ExecutionSimulatedTest {
		report.ExecutionMode = "blocked"
		for _, slot := range manifest.Suites {
			report.Cases = append(report.Cases, CaseReport{Suite: slot.Suite, CaseID: slot.CaseID, Module: slot.Module, ExecutionMode: "blocked", Status: "model_unavailable", ReadinessState: "blocked", MediaStatus: "not_run", ModelStatus: "not_configured", Reason: "execution_mode_not_supported"})
		}
		return report
	}
	report.ExecutionMode = executionMode
	if moduleStates == nil {
		moduleStates = InactiveModules()
	}
	if probe == nil {
		probe = ProbeMediaMetadata
	}
	report.Modules = moduleStates
	for _, slot := range manifest.Suites {
		report.SuiteCounts[slot.Suite]++
		moduleState := moduleStates[slot.Module]
		item := CaseReport{Suite: slot.Suite, CaseID: slot.CaseID, Module: slot.Module, ExecutionMode: executionMode, Status: "media_absent", MediaStatus: "absent", ModelStatus: moduleState.State, Reason: slot.NotRunReason, Qualified: false}
		if slot.AssetStatus == "quarantined" {
			item.Status, item.MediaStatus, item.Reason = "quarantined", "quarantined", "asset is explicitly quarantined"
			report.MediaQuarantined++
			report.ModelAbsent++
			report.Cases = append(report.Cases, item)
			continue
		}
		mediaPath, err := containedFile(mediaRoot, slot.ClipRelativePath)
		if err != nil || slot.AssetStatus == "placeholder" {
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				item.Status, item.MediaStatus, item.Reason = "media_quarantined", "quarantined", "unsafe media path or unreadable asset"
				report.MediaQuarantined++
			} else {
				item.Status, item.MediaStatus, item.Reason = "media_absent", "absent", firstNonEmpty(slot.NotRunReason, "media asset is absent")
				report.MediaAbsent++
			}
			report.ModelAbsent++
			report.Cases = append(report.Cases, item)
			continue
		}
		actualHash, hashErr := fileSHA256(mediaPath)
		if hashErr != nil || actualHash != slot.ClipSHA256 {
			item.Status, item.MediaStatus, item.Reason = "media_quarantined", "quarantined", "media SHA-256 verification failed"
			report.FailedCount++
			report.MediaQuarantined++
			report.ModelAbsent++
			report.Cases = append(report.Cases, item)
			continue
		}
		item.MediaStatus = "available"
		metadata, metadataErr := probe(ctx, mediaPath)
		if metadataErr != nil || !metadataMatches(slot.Technical, metadata) {
			item.Status, item.MediaStatus, item.Reason = "media_quarantined", "quarantined", "media_metadata_mismatch_or_probe_unavailable"
			report.FailedCount++
			report.MediaQuarantined++
			report.Cases = append(report.Cases, item)
			continue
		}
		item.MediaMetadataVerified, item.MediaMetadata = true, &metadata
		modelPath := modelPaths[slot.Module]
		plugin := plugins[slot.Module]
		if moduleState.State != "available" {
			item.Status, item.Reason = "model_unavailable", firstNonEmpty(moduleState.Reason, "module_not_available")
			if moduleState.State == "not_configured" {
				item.Status, item.ModelStatus, item.Reason = "model_absent", "not_configured", "module_not_configured"
				report.ModelAbsent++
			} else if moduleState.State == "failed" {
				item.ModelStatus, item.Reason = "failed", "module_failed_before_run"
				report.ModelFailed++
			} else {
				item.ModelStatus = "unavailable"
				report.ModelUnavailable++
			}
			report.Cases = append(report.Cases, item)
			continue
		}
		if modelPath == "" {
			item.Status, item.ModelStatus, item.Reason = "model_unavailable", "unavailable", "module is marked available but external model path is not configured"
			report.ModelUnavailable++
			report.Cases = append(report.Cases, item)
			continue
		}
		if plugin == nil {
			item.Status, item.ModelStatus, item.Reason = "model_unavailable", "unavailable", "model path is configured but no reviewed module plugin is registered"
			report.ModelUnavailable++
			report.Cases = append(report.Cases, item)
			continue
		}
		galleryPath := ""
		if slot.Module == ModuleFace {
			galleryPath = galleryPaths[ModuleFace]
			galleryInfo, galleryErr := os.Stat(galleryPath)
			if galleryPath == "" || galleryErr != nil || !galleryInfo.IsDir() {
				item.Status, item.ModelStatus, item.Reason = "model_unavailable", "unavailable", "external_face_gallery_not_configured"
				report.ModelUnavailable++
				report.Cases = append(report.Cases, item)
				continue
			}
		}
		descriptor := plugin.Descriptor()
		reportedDescriptor := descriptor
		reportedDescriptor.Name = slot.Module
		if !refPattern.MatchString(reportedDescriptor.Reason) {
			reportedDescriptor.Reason = "module_reason_redacted"
		}
		if reportedDescriptor.ModelVersion != "" && !refPattern.MatchString(reportedDescriptor.ModelVersion) {
			reportedDescriptor.ModelVersion = ""
		}
		if reportedDescriptor.ModelSHA256 != "" && !sha256Pattern.MatchString(reportedDescriptor.ModelSHA256) {
			reportedDescriptor.ModelSHA256 = ""
		}
		report.Modules[slot.Module] = reportedDescriptor
		item.ModelStatus = descriptor.State
		if descriptor.Name != slot.Module || descriptor.State != "available" || descriptor.ModelVersion != moduleState.ModelVersion || descriptor.ModelSHA256 != moduleState.ModelSHA256 || !descriptor.InputCompatible || !descriptor.OutputCompatible || pipeline == nil {
			item.Status, item.Reason = "model_unavailable", "module state, compatibility or Evidence V1 pipeline is unavailable"
			switch descriptor.State {
			case "not_configured":
				report.ModelAbsent++
			case "failed":
				report.ModelFailed++
			case "available":
				report.ModelUnavailable++
			default:
				report.ModelUnavailable++
			}
			report.Cases = append(report.Cases, item)
			continue
		}
		if descriptor.ModelVersion == "" || !sha256Pattern.MatchString(descriptor.ModelSHA256) {
			item.Status, item.ModelStatus, item.Reason = "model_unavailable", "unavailable", "available module lacks version and SHA-256 identity"
			report.ModelUnavailable++
			report.Cases = append(report.Cases, item)
			continue
		}
		modelHash, modelErr := fileSHA256(modelPath)
		modelInfo, statErr := os.Stat(modelPath)
		if modelErr != nil || statErr != nil || !modelInfo.Mode().IsRegular() || modelHash != descriptor.ModelSHA256 {
			item.Status, item.ModelStatus, item.Reason = "model_unavailable", "unavailable", "configured model file is unavailable or hash-mismatched"
			report.ModelUnavailable++
			report.Cases = append(report.Cases, item)
			continue
		}
		result, runErr := plugin.Run(ctx, ModuleInput{MediaPath: mediaPath, ModelPath: modelPath, GalleryPath: galleryPath, ExecutionMode: executionMode, Suite: slot.Suite, CaseID: slot.CaseID, Module: slot.Module})
		item.InferenceExecuted = result.InferenceExecuted
		report.InferenceRun = report.InferenceRun || result.InferenceExecuted
		item.LatencyMS = float64(result.Latency.Microseconds()) / 1000
		if runErr != nil || result.StructuredError != "" || !result.InferenceExecuted || result.Evidence == nil {
			item.Status, item.ModelStatus = "failed", "failed"
			item.Reason = "module_execution_failed"
			if errorCode.MatchString(result.StructuredError) {
				item.Reason = result.StructuredError
			}
			report.FailedCount++
			report.ModelFailed++
			report.Cases = append(report.Cases, item)
			continue
		}
		if err := result.Evidence.Validate(); err != nil || result.Evidence.SchemaVersion != contract.EventVisionEvidenceV1 {
			item.Status, item.ModelStatus, item.Reason = "failed", "failed", "module result failed Evidence V1 validation"
			report.FailedCount++
			report.ModelFailed++
			report.Cases = append(report.Cases, item)
			continue
		}
		if result.Evidence.Provenance != executionMode || (executionMode == ExecutionReplay && result.Evidence.SimulatedCamera) || (executionMode == ExecutionSimulatedTest && !result.Evidence.SimulatedCamera) {
			item.Status, item.ModelStatus, item.Reason = "failed", "failed", "evidence_provenance_does_not_match_execution_mode"
			report.FailedCount++
			report.ModelFailed++
			report.Cases = append(report.Cases, item)
			continue
		}
		pipelineResult, pipeErr := pipeline(ctx, *result.Evidence)
		item.Pipeline = &pipelineResult
		if pipeErr != nil || !pipelineResult.CoreReached || !pipelineResult.StoreWritten || !pipelineResult.SnapshotEncoded || !pipelineResult.MLPExecuted || !pipelineResult.SafetyGateChecked || !pipelineResult.DryRunResult || pipelineResult.PhysicalAction || pipelineResult.AudioRendered || pipelineResult.NetworkAccess || pipelineResult.RawVisionForwarded {
			item.Status, item.Reason = "failed", "evidence_v1_pipeline_incomplete_or_unsafe"
			report.FailedCount++
			report.Cases = append(report.Cases, item)
			continue
		}
		actual := semanticState(*result.Evidence, slot.Module)
		item.SemanticResult = actual
		item.ConfidencePercent = int(math.Round(semanticConfidence(*result.Evidence, slot.Module) * 100))
		matched := matchesExpected(slot.Expected.State, actual) && (slot.Expected.ConfidenceMinimum == nil || item.ConfidencePercent >= *slot.Expected.ConfidenceMinimum)
		item.SemanticMatch = &matched
		if matched {
			item.Status, item.Reason = "executed", "module and redacted Evidence V1 pipeline completed; qualification is separate"
			report.Executed++
		} else {
			item.Status, item.Reason = "semantic_mismatched", "actual redacted semantic state did not match the external fixture expectation"
			report.Mismatched++
		}
		report.Cases = append(report.Cases, item)
	}
	report.LatencyByModule = summarizeLatency(report.Cases)
	applyReadinessStates(&report)
	return report
}

func applyReadinessStates(report *Report) {
	for index := range report.Cases {
		item := &report.Cases[index]
		switch item.Status {
		case "media_absent":
			item.ReadinessState = "not_run"
		case "quarantined", "media_quarantined":
			item.ReadinessState = "blocked"
		case "model_absent":
			item.ReadinessState = "not_configured"
		case "model_unavailable":
			item.ReadinessState = "unavailable"
		case "failed":
			item.ReadinessState = "failed"
		case "executed", "semantic_mismatched", "qualified":
			item.ReadinessState = "available"
		default:
			item.ReadinessState = "not_run"
		}
	}
}

func semanticState(e contract.VisionEvidenceV1, module string) string {
	switch module {
	case ModuleFace:
		if e.Face.Availability != contract.VisionEvaluated {
			if e.RuntimeAggregate != nil && e.RuntimeAggregate.FaceStatus == "low_quality" {
				return "low_quality"
			}
			return "unavailable"
		}
		return e.Face.Result
	case ModuleVehicle:
		if e.Presence.Vehicle.Availability != contract.VisionEvaluated {
			return "unavailable"
		}
		return e.Presence.Vehicle.State
	case ModulePlate:
		if e.Plate.Availability != contract.VisionEvaluated {
			return "unavailable"
		}
		return e.Plate.Result
	case ModuleAnimal:
		if e.Presence.Animal.Availability != contract.VisionEvaluated {
			return "unavailable"
		}
		return e.Presence.Animal.State
	case ModuleCamera:
		if e.CameraHealth.Availability != contract.VisionEvaluated {
			return "unavailable"
		}
		return e.CameraHealth.State
	default:
		return "unknown"
	}
}

func semanticConfidence(e contract.VisionEvidenceV1, module string) float64 {
	switch module {
	case ModuleFace:
		return e.Face.Confidence
	case ModuleVehicle:
		return e.Presence.Vehicle.Confidence
	case ModulePlate:
		return e.Plate.Confidence
	case ModuleAnimal:
		return e.Presence.Animal.Confidence
	case ModuleCamera:
		return e.CameraHealth.Confidence
	default:
		return 0
	}
}

func summarizeLatency(cases []CaseReport) map[string]LatencyStats {
	values := make(map[string][]float64)
	for _, item := range cases {
		if item.InferenceExecuted {
			values[item.Module] = append(values[item.Module], item.LatencyMS)
		}
	}
	result := make(map[string]LatencyStats, len(values))
	for module, samples := range values {
		sort.Float64s(samples)
		total := 0.0
		for _, value := range samples {
			total += value
		}
		p95 := int(math.Ceil(float64(len(samples))*0.95)) - 1
		result[module] = LatencyStats{Count: len(samples), MinMS: samples[0], MeanMS: total / float64(len(samples)), P95MS: samples[p95], MaxMS: samples[len(samples)-1]}
	}
	return result
}

func matchesExpected(expected, actual string) bool {
	if expected == "ambiguous_or_unavailable" && (actual == "ambiguous" || actual == "unavailable" || actual == "low_quality") {
		return true
	}
	for _, allowed := range strings.Split(expected, "|") {
		if strings.TrimSpace(allowed) == actual {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unspecified"
}

func baseReport(manifest Manifest, digest, action string) Report {
	return Report{SchemaVersion: "synora.vision.suite-report/v1", ManifestSHA256: digest, Action: action, ExecutionMode: "not_run", QualificationStatus: "not_qualified", SuiteCounts: make(map[string]int), Modules: InactiveModules(), LatencyByModule: make(map[string]LatencyStats), Cases: []CaseReport{}}
}

func containedFile(root, relative string) (string, error) {
	if !safeRelativePath(relative) {
		return "", errors.New("unsafe relative media path")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(absRoot, relative))
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("media path escapes root")
	}
	info, err := os.Stat(realPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("media asset is not a regular file")
	}
	return realPath, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ProbeMediaMetadata invokes ffprobe only for a hash-verified local media file.
// The input path never leaves this process and ffprobe is restricted to the
// local-file protocol; no frame pixels are returned or retained.
func ProbeMediaMetadata(ctx context.Context, mediaPath string) (MediaProbe, error) {
	binary, err := exec.LookPath("ffprobe")
	if err != nil {
		return MediaProbe{}, errors.New("ffprobe_unavailable")
	}
	command := exec.CommandContext(ctx, binary, "-v", "error", "-protocol_whitelist", "file", "-count_frames", "-select_streams", "v:0", "-show_entries", "stream=codec_name,width,height,nb_read_frames:format=format_name,duration", "-of", "json", mediaPath)
	body, err := command.Output()
	if err != nil {
		return MediaProbe{}, errors.New("ffprobe_failed")
	}
	var decoded struct {
		Streams []struct {
			Codec  string `json:"codec_name"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Frames string `json:"nb_read_frames"`
		} `json:"streams"`
		Format struct {
			Container string `json:"format_name"`
			Duration  string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil || len(decoded.Streams) != 1 {
		return MediaProbe{}, errors.New("ffprobe_invalid_video_metadata")
	}
	frames, frameErr := strconv.Atoi(decoded.Streams[0].Frames)
	duration, durationErr := strconv.ParseFloat(decoded.Format.Duration, 64)
	if frameErr != nil || durationErr != nil || frames <= 0 || duration <= 0 {
		return MediaProbe{}, errors.New("ffprobe_incomplete_video_metadata")
	}
	return MediaProbe{Container: decoded.Format.Container, Codec: decoded.Streams[0].Codec, Width: decoded.Streams[0].Width, Height: decoded.Streams[0].Height, Frames: frames, Duration: duration}, nil
}

func metadataMatches(expected TechnicalMetadata, actual MediaProbe) bool {
	containerMatch := false
	for _, item := range strings.Split(actual.Container, ",") {
		if strings.EqualFold(strings.TrimSpace(item), expected.Container) {
			containerMatch = true
			break
		}
	}
	expectedDuration, err := time.ParseDuration(expected.Duration)
	if err != nil {
		return false
	}
	tolerance := math.Max(expectedDuration.Seconds()*0.01, 0.05)
	return containerMatch && strings.EqualFold(expected.Codec, actual.Codec) && expected.Width == actual.Width && expected.Height == actual.Height && expected.Frames == actual.Frames && math.Abs(expectedDuration.Seconds()-actual.Duration) <= tolerance
}

func SortedModuleNames() []string {
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
