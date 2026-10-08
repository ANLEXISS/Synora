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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"synora/pkg/contract"
)

const (
	ManifestSchema = "synora.vision.media-suite-manifest/v1"
	ModuleFace     = "face_recognition"
	ModuleVehicle  = "vehicle_classification"
	ModulePlate    = "plate_reading"
	ModuleAnimal   = "animal_classification"
	ModuleCamera   = "camera_health"
)

var (
	sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	opaqueSubject = regexp.MustCompile(`^resident_test_[0-9]{2}$`)
	caseIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
	refPattern    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{1,127}$`)
	errorCode     = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)
	modules       = map[string]struct{}{
		ModuleFace: {}, ModuleVehicle: {}, ModulePlate: {}, ModuleAnimal: {}, ModuleCamera: {},
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
	Name         string  `json:"name"`
	State        string  `json:"state"`
	ModelVersion *string `json:"model_version"`
	ModelSHA256  *string `json:"model_sha256"`
}

type TechnicalMetadata struct {
	Container string `json:"container"`
	Codec     string `json:"codec"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Duration  string `json:"duration"`
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
	MediaPath string
	ModelPath string
	Suite     string
	CaseID    string
	Module    string
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
	Suite             string          `json:"suite"`
	CaseID            string          `json:"case_id"`
	Module            string          `json:"module"`
	Status            string          `json:"status"`
	MediaStatus       string          `json:"media_status"`
	ModelStatus       string          `json:"model_status"`
	Reason            string          `json:"reason"`
	InferenceExecuted bool            `json:"inference_executed"`
	LatencyMS         float64         `json:"latency_ms,omitempty"`
	ConfidencePercent int             `json:"confidence_percent,omitempty"`
	SemanticMatch     *bool           `json:"semantic_match,omitempty"`
	SemanticResult    string          `json:"semantic_result,omitempty"`
	Qualified         bool            `json:"qualified"`
	Pipeline          *PipelineResult `json:"pipeline,omitempty"`
}

type Report struct {
	SchemaVersion    string                      `json:"schema_version"`
	ManifestSHA256   string                      `json:"manifest_sha256"`
	Action           string                      `json:"action"`
	SuiteCounts      map[string]int              `json:"suite_counts"`
	Modules          map[string]ModuleDescriptor `json:"modules"`
	LatencyByModule  map[string]LatencyStats     `json:"latency_by_module"`
	Cases            []CaseReport                `json:"cases"`
	PassedCount      int                         `json:"passed_count"`
	FailedCount      int                         `json:"failed_count"`
	MediaAbsent      int                         `json:"media_absent_count"`
	MediaQuarantined int                         `json:"media_quarantined_count"`
	ModelAbsent      int                         `json:"model_absent_count"`
	ModelUnavailable int                         `json:"model_unavailable_count"`
	ModelFailed      int                         `json:"model_failed_count"`
	Executed         int                         `json:"executed_count"`
	Mismatched       int                         `json:"semantic_mismatched_count"`
	Qualified        int                         `json:"qualified_count"`
	InferenceRun     bool                        `json:"inference_executed"`
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
		switch entry.State {
		case "not_configured", "unavailable", "available", "failed":
		default:
			return ModuleRegistry{}, nil, "", fmt.Errorf("invalid module state %q", entry.State)
		}
		version, hash := "", ""
		if entry.ModelVersion != nil {
			version = *entry.ModelVersion
		}
		if entry.ModelSHA256 != nil {
			hash = *entry.ModelSHA256
		}
		if entry.State == "available" && (version == "" || !sha256Pattern.MatchString(hash)) {
			return ModuleRegistry{}, nil, "", fmt.Errorf("available module %q requires a version and model SHA-256", entry.Name)
		}
		if entry.State == "not_configured" && (version != "" || hash != "") {
			return ModuleRegistry{}, nil, "", fmt.Errorf("not-configured module %q cannot declare a model", entry.Name)
		}
		states[entry.Name] = ModuleDescriptor{Name: entry.Name, State: entry.State, ModelVersion: version, ModelSHA256: hash, InputCompatible: false, OutputCompatible: true, Reason: "no_model_configured"}
	}
	for name := range modules {
		if _, ok := states[name]; !ok {
			return ModuleRegistry{}, nil, "", fmt.Errorf("module registry is missing %q", name)
		}
	}
	digest := sha256.Sum256(body)
	return registry, states, hex.EncodeToString(digest[:]), nil
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
		if slot.Technical.Width < 0 || slot.Technical.Height < 0 || slot.Technical.Container == "" || slot.Technical.Codec == "" || slot.Technical.Duration == "" {
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
			if slot.ClipSHA256 != strings.Repeat("0", 64) || slot.NotRunReason == "" {
				return fmt.Errorf("placeholder %q must have a zero hash and a not-run reason", slot.CaseID)
			}
		case "available", "quarantined":
			if !sha256Pattern.MatchString(slot.ClipSHA256) || slot.ClipSHA256 == strings.Repeat("0", 64) {
				return fmt.Errorf("invalid asset hash in slot %q", slot.CaseID)
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
		"camera_health":    {"healthy": true, "unavailable": true, "frozen": true, "obscured": true, "degraded": true, "timestamp_invalid": true},
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
		result[name] = ModuleDescriptor{Name: name, State: "not_configured", InputCompatible: false, OutputCompatible: true, Reason: "no model or executable module configured; no inference will run"}
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

func Inspect(manifest Manifest, digest, action, mediaRoot string, moduleStates map[string]ModuleDescriptor) Report {
	report := baseReport(manifest, digest, action)
	for _, slot := range manifest.Suites {
		report.SuiteCounts[slot.Suite]++
		caseReport := CaseReport{Suite: slot.Suite, CaseID: slot.CaseID, Module: slot.Module, Status: "media_absent", MediaStatus: "absent", ModelStatus: "not_configured", Reason: slot.NotRunReason, Qualified: false, InferenceExecuted: false}
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
					caseReport.Status, caseReport.MediaStatus = "model_absent", "available"
					caseReport.Reason = "media verified; module model is not configured"
				}
			} else if errors.Is(err, os.ErrNotExist) && slot.AssetStatus == "available" {
				caseReport.Status, caseReport.MediaStatus, caseReport.Reason = "media_absent", "absent", "manifested media asset is missing"
			} else if !errors.Is(err, os.ErrNotExist) {
				caseReport.Status, caseReport.MediaStatus, caseReport.Reason = "media_quarantined", "quarantined", "unsafe media path or unreadable asset"
			}
		}
		if state, ok := moduleStates[slot.Module]; ok {
			caseReport.ModelStatus = state.State
			if state.State == "unavailable" || state.State == "failed" {
				caseReport.Status, caseReport.Reason = "model_unavailable", state.Reason
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
	return report
}

// Execute is the extension point for future module plugins. Fixture expectations
// are deliberately not part of ModuleInput and therefore cannot influence inference.
func Execute(ctx context.Context, manifest Manifest, digest, mediaRoot string, modelPaths map[string]string, plugins map[string]Module, pipeline EvidencePipeline) Report {
	report := baseReport(manifest, digest, "run")
	for _, slot := range manifest.Suites {
		report.SuiteCounts[slot.Suite]++
		item := CaseReport{Suite: slot.Suite, CaseID: slot.CaseID, Module: slot.Module, Status: "media_absent", MediaStatus: "absent", ModelStatus: "not_configured", Reason: slot.NotRunReason, Qualified: false}
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
		modelPath := modelPaths[slot.Module]
		plugin := plugins[slot.Module]
		if modelPath == "" {
			item.Status, item.ModelStatus, item.Reason = "model_absent", "not_configured", "verified media is present but no compatible module/model is configured"
			report.ModelAbsent++
			report.Cases = append(report.Cases, item)
			continue
		}
		if plugin == nil {
			item.Status, item.ModelStatus, item.Reason = "model_unavailable", "unavailable", "model path is configured but no reviewed module plugin is registered"
			report.ModelUnavailable++
			report.Cases = append(report.Cases, item)
			continue
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
		if descriptor.Name != slot.Module || descriptor.State != "available" || !descriptor.InputCompatible || !descriptor.OutputCompatible || pipeline == nil {
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
		result, runErr := plugin.Run(ctx, ModuleInput{MediaPath: mediaPath, ModelPath: modelPath, Suite: slot.Suite, CaseID: slot.CaseID, Module: slot.Module})
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
	return report
}

func semanticState(e contract.VisionEvidenceV1, module string) string {
	switch module {
	case ModuleFace:
		return e.Face.Result
	case ModuleVehicle:
		return e.Presence.Vehicle.State
	case ModulePlate:
		return e.Plate.Result
	case ModuleAnimal:
		return e.Presence.Animal.State
	case ModuleCamera:
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
	if expected == "ambiguous_or_unavailable" && (actual == "ambiguous" || actual == "unavailable") {
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
	return Report{SchemaVersion: "synora.vision.suite-report/v1", ManifestSHA256: digest, Action: action, SuiteCounts: make(map[string]int), Modules: InactiveModules(), LatencyByModule: make(map[string]LatencyStats), Cases: []CaseReport{}}
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
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func SortedModuleNames() []string {
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
