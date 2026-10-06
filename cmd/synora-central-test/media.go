package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultVisionMediaManifest = "testdata/central-e2e-v1/vision-media-v1/manifest.json"
	mediaStatusPassed          = "passed"
	mediaStatusFailed          = "failed"
	mediaStatusNotRun          = "not_run"
	mediaStatusModelBlocked    = "blocked_model_unavailable"
	mediaStatusModelMissing    = "blocked_model_missing"
	mediaStatusMediaMissing    = "blocked_media_missing"
	mediaStatusIntegrity       = "blocked_integrity_failure"
)

type visionMediaManifest struct {
	SchemaVersion string             `json:"schema_version"`
	Version       string             `json:"version"`
	LogicalDate   string             `json:"logical_date"`
	MediaRootEnv  string             `json:"media_root_env"`
	Entries       []visionMediaEntry `json:"entries"`
}

type visionMediaEntry struct {
	ID                        string                 `json:"id"`
	Family                    string                 `json:"family"`
	RelativePath              string                 `json:"relative_path"`
	SHA256                    string                 `json:"sha256"`
	SHA256State               string                 `json:"sha256_state,omitempty"`
	DurationMS                int64                  `json:"duration_ms"`
	CameraID                  string                 `json:"camera_id"`
	TopologyZone              string                 `json:"topology_zone"`
	EdgeTrackFixture          string                 `json:"edge_track_fixture"`
	LicenceOrConsentReference string                 `json:"licence_or_consent_reference"`
	Expectation               visionMediaExpectation `json:"expectation"`
}

type visionMediaExpectation struct {
	Vision     map[string]any `json:"vision"`
	Core       map[string]any `json:"core"`
	SafetyGate map[string]any `json:"safety_gate"`
}

type mediaCaseReport struct {
	ID                           string            `json:"id"`
	Family                       string            `json:"family"`
	Category                     string            `json:"category,omitempty"`
	Status                       string            `json:"status"`
	Reason                       string            `json:"reason,omitempty"`
	PoseStatus                   string            `json:"pose_status,omitempty"`
	Posture                      string            `json:"posture,omitempty"`
	ImmobilitySeconds            float64           `json:"immobility_seconds,omitempty"`
	FallState                    string            `json:"fall_state,omitempty"`
	RapidMotionState             string            `json:"rapid_motion_state,omitempty"`
	PhysicalInteractionCandidate bool              `json:"physical_interaction_candidate"`
	Confidence                   float64           `json:"confidence,omitempty"`
	LatencyMS                    float64           `json:"latency_ms,omitempty"`
	Expected                     map[string]any    `json:"expected,omitempty"`
	SemanticStatus               string            `json:"semantic_status,omitempty"`
	CorePassed                   bool              `json:"core_passed"`
	SnapshotVersion              string            `json:"snapshot_version,omitempty"`
	SnapshotDimension            int               `json:"snapshot_dimension,omitempty"`
	MLPHeads                     []string          `json:"mlp_heads,omitempty"`
	MLPObservations              []mlpObservation  `json:"mlp_observations,omitempty"`
	SafetyGateStatuses           []string          `json:"safety_gate_statuses,omitempty"`
	SafetyGateReasons            []string          `json:"safety_gate_reasons,omitempty"`
	RawVisionForwarded           bool              `json:"raw_vision_forwarded"`
	AudioRendered                bool              `json:"audio_rendered"`
	PhysicalActionExecuted       bool              `json:"physical_action_executed"`
	NetworkAccess                bool              `json:"network_access"`
	HumanDetectorStatus          string            `json:"human_detector_status,omitempty"`
	HumanConfirmedFrameCount     int               `json:"human_confirmed_frame_count,omitempty"`
	PoseRequestCount             int               `json:"pose_request_count,omitempty"`
	PoseRequestReason            string            `json:"pose_request_reason,omitempty"`
	Journey                      []journeyEvent    `json:"journey,omitempty"`
	ActionLifecycleStatus        string            `json:"action_lifecycle_status,omitempty"`
	IdempotenceChecks            idempotenceChecks `json:"idempotence_checks"`
	RejectedActionResults        int               `json:"rejected_action_results"`
}

type mediaSuiteReport struct {
	SchemaVersion               string                    `json:"schema_version"`
	ManifestSHA256              string                    `json:"manifest_sha256"`
	SourceManifestSHA256        string                    `json:"source_manifest_sha256,omitempty"`
	Mode                        string                    `json:"mode"`
	MediaRootSet                bool                      `json:"media_root_set"`
	ModelStatus                 string                    `json:"model_status"`
	ModelReason                 string                    `json:"model_reason,omitempty"`
	ScenarioCount               int                       `json:"scenario_count"`
	PassedCount                 int                       `json:"passed_count"`
	FailedCount                 int                       `json:"failed_count"`
	StatusCounts                map[string]int            `json:"status_counts"`
	FamilyCounts                map[string]int            `json:"family_counts"`
	Passed                      bool                      `json:"passed"`
	NotRunCount                 int                       `json:"not_run_count"`
	BlockedModelMissingCount    int                       `json:"blocked_model_missing_count"`
	PoseBackendStatus           string                    `json:"pose_backend_status"`
	MediaHashVerificationPassed bool                      `json:"media_hash_verification_passed"`
	MediaByCategory             map[string]map[string]int `json:"media_by_category"`
	PoseLatencyMS               map[string]float64        `json:"pose_latency_ms"`
	PostureByCategory           map[string]map[string]int `json:"posture_by_category"`
	FallStateByCategory         map[string]map[string]int `json:"fall_state_by_category"`
	SemanticQualification       string                    `json:"semantic_qualification"`
	SemanticDebtByCategory      map[string]int            `json:"semantic_debt_by_category"`
	PipelineCompletedCount      int                       `json:"pipeline_completed_count"`
	PipelineIncompleteCount     int                       `json:"pipeline_incomplete_count"`
	ActionLifecycleByStatus     map[string]int            `json:"action_lifecycle_by_status"`
	IdempotenceChecks           map[string]int            `json:"idempotence_checks"`
	RejectedActionResults       int                       `json:"rejected_action_results"`
	Cases                       []mediaCaseReport         `json:"cases"`
}

type le2iManifest struct {
	SchemaVersion        string     `json:"schema_version"`
	Version              string     `json:"version"`
	Dataset              string     `json:"dataset"`
	Purpose              string     `json:"purpose"`
	Seed                 string     `json:"seed"`
	SourceManifestSHA256 string     `json:"source_manifest_sha256"`
	CaseCount            int        `json:"case_count"`
	MediaRootEnv         string     `json:"media_root_env"`
	Backend              string     `json:"backend"`
	Cases                []le2iCase `json:"cases"`
}

type le2iCase struct {
	ID               string         `json:"id"`
	Category         string         `json:"category"`
	ClipRelativePath string         `json:"clip_relative_path"`
	ClipSHA256       string         `json:"clip_sha256"`
	ClipCodec        string         `json:"clip_codec"`
	ClipWidth        int            `json:"clip_width"`
	ClipHeight       int            `json:"clip_height"`
	ClipFPS          string         `json:"clip_fps"`
	ClipFrameCount   int            `json:"clip_frame_count"`
	Expectation      map[string]any `json:"expectation"`
}

type poseModelDiagnostic struct {
	Status string
	Reason string
}

type poseAggregateResult struct {
	PoseStatus                   string  `json:"pose_status"`
	Posture                      string  `json:"posture"`
	ImmobilitySeconds            float64 `json:"immobility_seconds"`
	FallState                    string  `json:"fall_state"`
	RapidMotionState             string  `json:"rapid_motion_state"`
	PhysicalInteractionCandidate bool    `json:"physical_interaction_candidate"`
	Confidence                   float64 `json:"confidence"`
	LatencyMS                    float64 `json:"latency_ms"`
	LatencyP50MS                 float64 `json:"latency_p50_ms"`
	LatencyP95MS                 float64 `json:"latency_p95_ms"`
	LatencyMaxMS                 float64 `json:"latency_max_ms"`
	FrameCount                   int     `json:"frame_count"`
	PoseFrameCount               int     `json:"pose_frame_count"`
	HumanDetectorStatus          string  `json:"human_detector_status"`
	HumanConfirmedFrameCount     int     `json:"human_confirmed_frame_count"`
	PoseRequestCount             int     `json:"pose_request_count"`
	PoseRequestReason            string  `json:"pose_request_reason"`
}

func loadVisionMediaManifest(path string) (visionMediaManifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return visionMediaManifest{}, err
	}
	var manifest visionMediaManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return visionMediaManifest{}, err
	}
	if manifest.SchemaVersion != "synora.vision-test-media-manifest/v1" || manifest.Version == "" || manifest.MediaRootEnv != "SYNORA_VISION_MEDIA_ROOT" || len(manifest.Entries) == 0 {
		return visionMediaManifest{}, errors.New("invalid vision media manifest")
	}
	seen := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if entry.ID == "" || entry.Family == "" || entry.RelativePath == "" || entry.CameraID == "" || entry.TopologyZone == "" || entry.EdgeTrackFixture == "" || entry.LicenceOrConsentReference == "" || entry.DurationMS < 0 {
			return visionMediaManifest{}, fmt.Errorf("invalid vision media entry %q", entry.ID)
		}
		if _, ok := seen[entry.ID]; ok {
			return visionMediaManifest{}, fmt.Errorf("duplicate vision media entry %q", entry.ID)
		}
		seen[entry.ID] = struct{}{}
		if filepath.IsAbs(entry.RelativePath) || filepath.Clean(entry.RelativePath) == "." || strings.HasPrefix(filepath.ToSlash(filepath.Clean(entry.RelativePath)), "../") {
			return visionMediaManifest{}, fmt.Errorf("vision media path escapes root: %q", entry.RelativePath)
		}
		if entry.SHA256 != "" && entry.SHA256 != strings.Repeat("0", 64) && !isSHA256(entry.SHA256) {
			return visionMediaManifest{}, fmt.Errorf("invalid vision media sha256 for %q", entry.ID)
		}
	}
	return manifest, nil
}

func isSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func runVisionMediaSuite(repoRoot, manifestPath, mediaRoot, modelPath string) mediaSuiteReport {
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		return mediaManifestFailure(manifestPath, err)
	}
	var header struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(body, &header); err != nil {
		return mediaManifestFailure(manifestPath, err)
	}
	if header.SchemaVersion == "synora.vision.media-manifest/v1" {
		return runLe2iSuite(repoRoot, manifestPath, mediaRoot, modelPath)
	}
	return runLegacyVisionMediaSuite(repoRoot, manifestPath, mediaRoot, modelPath)
}

func mediaManifestFailure(path string, err error) mediaSuiteReport {
	return mediaSuiteReport{SchemaVersion: "synora.central-media-e2e/v1", ManifestSHA256: fileSHA256(path), Mode: "media", ModelStatus: "unavailable", ModelReason: err.Error(), StatusCounts: map[string]int{mediaStatusIntegrity: 1}, FailedCount: 1, ScenarioCount: 1, Passed: false, Cases: []mediaCaseReport{{ID: "manifest", Family: "manifest", Status: mediaStatusIntegrity, Reason: err.Error()}}}
}

func runLegacyVisionMediaSuite(repoRoot, manifestPath, mediaRoot, modelPath string) mediaSuiteReport {
	manifest, err := loadVisionMediaManifest(manifestPath)
	if err != nil {
		return mediaManifestFailure(manifestPath, err)
	}
	report := mediaSuiteReport{
		SchemaVersion:  "synora.central-media-e2e/v1",
		ManifestSHA256: fileSHA256(manifestPath),
		Mode:           "media",
		MediaRootSet:   mediaRoot != "",
		StatusCounts:   make(map[string]int),
		FamilyCounts:   make(map[string]int),
		Cases:          make([]mediaCaseReport, 0, len(manifest.Entries)),
	}
	model := diagnosePoseModel(repoRoot, modelPath)
	report.ModelStatus, report.ModelReason = model.Status, model.Reason
	for _, entry := range manifest.Entries {
		item := evaluateVisionMediaEntry(repoRoot, entry, mediaRoot, model)
		report.Cases = append(report.Cases, item)
		report.StatusCounts[item.Status]++
		report.FamilyCounts[item.Family]++
		if item.Status == mediaStatusPassed {
			report.PassedCount++
		}
	}
	report.ScenarioCount = len(report.Cases)
	report.FailedCount = report.ScenarioCount - report.PassedCount
	report.Passed = report.ScenarioCount > 0 && report.FailedCount == 0
	return report
}

func loadLe2iManifest(path string) (le2iManifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return le2iManifest{}, err
	}
	var manifest le2iManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return le2iManifest{}, err
	}
	if manifest.SchemaVersion != "synora.vision.media-manifest/v1" || manifest.Version == "" || manifest.Dataset == "" || manifest.MediaRootEnv != "SYNORA_VISION_MEDIA_ROOT" || manifest.Backend != "yolov8n_pose_rknn/v1" || manifest.CaseCount != 48 || len(manifest.Cases) != 48 {
		return le2iManifest{}, errors.New("invalid Le2i V1 media manifest")
	}
	seen := make(map[string]struct{}, len(manifest.Cases))
	counts := map[string]int{"Fall": 10, "Lie": 10, "Likefall": 8, "Stand": 10, "Blank": 10}
	actualCounts := make(map[string]int)
	for _, item := range manifest.Cases {
		if item.ID == "" || item.ClipRelativePath == "" || !isSHA256(item.ClipSHA256) || item.ClipCodec != "h264" || item.ClipWidth != 320 || (item.ClipHeight != 180 && item.ClipHeight != 240) || item.ClipFPS != "25/1" || item.ClipFrameCount != 16 {
			return le2iManifest{}, fmt.Errorf("invalid Le2i case %q", item.ID)
		}
		if _, ok := seen[item.ID]; ok {
			return le2iManifest{}, fmt.Errorf("duplicate Le2i case %q", item.ID)
		}
		seen[item.ID] = struct{}{}
		if filepath.IsAbs(item.ClipRelativePath) || filepath.Clean(item.ClipRelativePath) == "." || strings.HasPrefix(filepath.ToSlash(filepath.Clean(item.ClipRelativePath)), "../") {
			return le2iManifest{}, fmt.Errorf("Le2i path escapes root: %q", item.ClipRelativePath)
		}
		actualCounts[item.Category]++
	}
	if len(actualCounts) != len(counts) {
		return le2iManifest{}, fmt.Errorf("invalid Le2i categories: %v", actualCounts)
	}
	for category, want := range counts {
		if actualCounts[category] != want {
			return le2iManifest{}, fmt.Errorf("Le2i category %s has %d cases, want %d", category, actualCounts[category], want)
		}
	}
	return manifest, nil
}

func runLe2iSuite(repoRoot, manifestPath, mediaRoot, modelPath string) mediaSuiteReport {
	manifest, err := loadLe2iManifest(manifestPath)
	if err != nil {
		return mediaManifestFailure(manifestPath, err)
	}
	report := mediaSuiteReport{
		SchemaVersion: "synora.central-media-e2e/v1", ManifestSHA256: fileSHA256(manifestPath), SourceManifestSHA256: manifest.SourceManifestSHA256,
		Mode: "le2i", MediaRootSet: mediaRoot != "", StatusCounts: make(map[string]int), FamilyCounts: make(map[string]int),
		MediaByCategory: make(map[string]map[string]int), PoseLatencyMS: map[string]float64{"p50_ms": 0, "p95_ms": 0, "max_ms": 0},
		PostureByCategory: make(map[string]map[string]int), FallStateByCategory: make(map[string]map[string]int), SemanticQualification: "not_qualified",
		SemanticDebtByCategory: make(map[string]int), ActionLifecycleByStatus: make(map[string]int), IdempotenceChecks: make(map[string]int), Cases: make([]mediaCaseReport, 0, len(manifest.Cases)),
	}
	model := diagnosePoseModel(repoRoot, modelPath)
	report.ModelStatus, report.ModelReason = le2iModelStatus(modelPath, model), model.Reason
	report.PoseBackendStatus = report.ModelStatus
	allHashesVerified := mediaRoot != ""
	var latencyValues []float64
	for _, entry := range manifest.Cases {
		item := evaluateLe2iCase(repoRoot, entry, mediaRoot, model)
		report.Cases = append(report.Cases, item)
		report.StatusCounts[item.Status]++
		report.FamilyCounts[item.Category]++
		if _, ok := report.MediaByCategory[item.Category]; !ok {
			report.MediaByCategory[item.Category] = make(map[string]int)
		}
		report.MediaByCategory[item.Category][item.Status]++
		if item.Status == mediaStatusPassed {
			report.PassedCount++
		} else if item.Status == mediaStatusNotRun || item.Status == mediaStatusModelMissing {
			report.NotRunCount++
		}
		if item.Status == mediaStatusModelMissing {
			report.BlockedModelMissingCount++
		}
		if item.Status == mediaStatusIntegrity || item.Status == mediaStatusMediaMissing {
			allHashesVerified = false
		}
		if item.LatencyMS > 0 {
			latencyValues = append(latencyValues, item.LatencyMS)
		}
		if item.Posture != "" {
			incrementNested(report.PostureByCategory, item.Category, item.Posture)
		}
		if item.FallState != "" {
			incrementNested(report.FallStateByCategory, item.Category, item.FallState)
		}
		if item.SemanticStatus == "observed_mismatch" {
			report.SemanticDebtByCategory[item.Category]++
		}
		report.ActionLifecycleByStatus[item.ActionLifecycleStatus]++
		if item.IdempotenceChecks.CameraDuplicateNoSecondAction {
			report.IdempotenceChecks["camera_duplicate_no_second_action"]++
		}
		if item.IdempotenceChecks.ActionResultDuplicateSafe {
			report.IdempotenceChecks["action_result_duplicate_safe"]++
		}
		if item.IdempotenceChecks.OrphanActionResultRejected {
			report.IdempotenceChecks["orphan_action_result_rejected"]++
		}
		if !item.IdempotenceChecks.DirectExecutorCalls {
			report.IdempotenceChecks["no_direct_executor_calls"]++
		}
		if journeyComplete(item.Journey) {
			report.PipelineCompletedCount++
		} else {
			report.PipelineIncompleteCount++
		}
		report.RejectedActionResults += item.RejectedActionResults
	}
	report.ScenarioCount = len(report.Cases)
	report.FailedCount = report.ScenarioCount - report.PassedCount
	report.Passed = report.ScenarioCount > 0 && report.FailedCount == 0 && report.MediaHashVerificationPassed
	report.MediaHashVerificationPassed = allHashesVerified && report.ScenarioCount == 48
	if len(latencyValues) > 0 {
		report.PoseLatencyMS["p50_ms"] = percentileFloat(latencyValues, 50)
		report.PoseLatencyMS["p95_ms"] = percentileFloat(latencyValues, 95)
		report.PoseLatencyMS["max_ms"] = maxFloat(latencyValues)
	}
	// Hash verification is an integrity gate; calculate Passed after its final value.
	report.Passed = report.ScenarioCount == 48 && report.FailedCount == 0 && report.MediaHashVerificationPassed
	return report
}

func le2iModelStatus(modelPath string, diagnostic poseModelDiagnostic) string {
	if diagnostic.Status == "available" {
		return "available"
	}
	if modelPath == "" {
		return mediaStatusModelMissing
	}
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return mediaStatusModelMissing
	}
	return diagnostic.Status
}

func incrementNested(values map[string]map[string]int, category, value string) {
	if values[category] == nil {
		values[category] = make(map[string]int)
	}
	values[category][value]++
}

func percentileFloat(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	position := percentile / 100 * float64(len(sorted)-1)
	lower := int(position)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[lower]
	}
	return sorted[lower] + (sorted[upper]-sorted[lower])*(position-float64(lower))
}

func maxFloat(values []float64) float64 {
	maximum := 0.0
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func evaluateLe2iCase(repoRoot string, entry le2iCase, mediaRoot string, model poseModelDiagnostic) mediaCaseReport {
	item := mediaCaseReport{ID: entry.ID, Family: entry.Category, Category: entry.Category, Status: mediaStatusMediaMissing, Expected: entry.Expectation, SemanticStatus: "not_qualified"}
	if mediaRoot == "" {
		item.Reason = "SYNORA_VISION_MEDIA_ROOT is not configured"
		return item
	}
	path, err := safeMediaPath(mediaRoot, entry.ClipRelativePath)
	if err != nil {
		item.Status, item.Reason = mediaStatusIntegrity, err.Error()
		return item
	}
	if _, err := os.Stat(path); err != nil {
		item.Status, item.Reason = mediaStatusMediaMissing, "Le2i clip is missing"
		return item
	}
	actualHash, err := sha256File(path)
	if err != nil || !strings.EqualFold(actualHash, entry.ClipSHA256) {
		item.Status, item.Reason = mediaStatusIntegrity, "Le2i clip SHA-256 does not match the manifest"
		return item
	}
	if err := validateLe2iClip(path, entry); err != nil {
		item.Status, item.Reason = mediaStatusIntegrity, err.Error()
		return item
	}
	if model.Status != "available" {
		blockStatus := mediaStatusModelMissing
		poseStatus := "blocked_model_missing"
		if modelPath := modelPathFromEnvironment(); modelPath != "" {
			if _, statErr := os.Stat(modelPath); statErr == nil {
				blockStatus, poseStatus = mediaStatusModelBlocked, "unavailable"
			}
		}
		item.Status, item.PoseStatus, item.Reason = blockStatus, poseStatus, model.Reason
		return item
	}
	result, err := runPoseMediaCase(repoRoot, path, modelPathFromEnvironment())
	if err != nil {
		item.Status, item.Reason, item.PoseStatus = mediaStatusFailed, err.Error(), "unavailable"
		return item
	}
	item.PoseStatus, item.Posture, item.ImmobilitySeconds = result.PoseStatus, result.Posture, result.ImmobilitySeconds
	item.FallState, item.RapidMotionState = result.FallState, result.RapidMotionState
	item.PhysicalInteractionCandidate, item.Confidence, item.LatencyMS = result.PhysicalInteractionCandidate, result.Confidence, result.LatencyMS
	item.HumanDetectorStatus, item.HumanConfirmedFrameCount = result.HumanDetectorStatus, result.HumanConfirmedFrameCount
	item.PoseRequestCount, item.PoseRequestReason = result.PoseRequestCount, result.PoseRequestReason
	coreReport := runLe2iAggregateThroughCore(repoRoot, entry, result)
	item.CorePassed = coreReport.Passed
	item.SnapshotVersion, item.SnapshotDimension = coreReport.SnapshotVersion, coreReport.SnapshotDimension
	item.MLPHeads, item.MLPObservations = coreReport.MLPHeads, coreReport.MLPObservations
	item.SafetyGateStatuses, item.SafetyGateReasons = coreReport.SafetyGateStatuses, coreReport.SafetyGateReasons
	item.RawVisionForwarded, item.AudioRendered, item.PhysicalActionExecuted, item.NetworkAccess = coreReport.RawVisionForwarded, coreReport.AudioRendered, coreReport.PhysicalAction, coreReport.NetworkAccess
	item.Journey, item.ActionLifecycleStatus, item.IdempotenceChecks, item.RejectedActionResults = coreReport.Journey, coreReport.ActionLifecycleStatus, coreReport.IdempotenceChecks, coreReport.RejectedActionResults
	item.Status = mediaStatusPassed
	if !item.CorePassed || item.RawVisionForwarded || item.AudioRendered || item.PhysicalActionExecuted || item.NetworkAccess || item.FallState == "confirmed" {
		item.Status, item.Reason = mediaStatusFailed, "central safety or redaction gate failed"
	}
	if le2iSemanticMatch(entry.Category, result) {
		item.SemanticStatus = "observed_match"
	} else {
		item.SemanticStatus = "observed_mismatch"
	}
	if reason := le2iSafetySemanticMismatch(entry.Category, result); reason != "" {
		item.Status, item.Reason = mediaStatusFailed, reason
	}
	return item
}

func le2iSafetySemanticMismatch(category string, result poseAggregateResult) string {
	if result.FallState == "confirmed" {
		return "confirmed fall state is forbidden in this harness"
	}
	switch category {
	case "Blank":
		if result.PoseStatus == "available" || result.FallState == "candidate" {
			return "Blank media must never activate pose or a fall candidate"
		}
		if result.Posture != "unavailable" || result.FallState != "none" {
			return "Blank media must remain unavailable with no fall state"
		}
	case "Stand":
		if result.FallState == "candidate" {
			return "Stand media cannot emit a fall candidate"
		}
	case "Lie":
		if result.FallState == "candidate" {
			return "Lie media cannot emit a fall candidate without an upright-to-ground transition"
		}
	case "Likefall":
		if result.FallState == "confirmed" {
			return "Likefall media cannot emit a confirmed fall"
		}
	}
	return ""
}

func validateLe2iClip(path string, entry le2iCase) error {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return errors.New("ffprobe is unavailable; Le2i metadata cannot be verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name,width,height,r_frame_rate,nb_frames", "-of", "json", path)
	body, err := cmd.Output()
	if err != nil {
		return errors.New("Le2i clip decode failed")
	}
	var probe struct {
		Streams []struct {
			Codec  string `json:"codec_name"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			FPS    string `json:"r_frame_rate"`
			Frames string `json:"nb_frames"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || len(probe.Streams) != 1 {
		return errors.New("Le2i clip decoder metadata is invalid")
	}
	stream := probe.Streams[0]
	if stream.Codec != entry.ClipCodec || stream.Width != entry.ClipWidth || stream.Height != entry.ClipHeight || stream.FPS != entry.ClipFPS || stream.Frames != fmt.Sprint(entry.ClipFrameCount) {
		return fmt.Errorf("Le2i clip metadata mismatch: codec=%s size=%dx%d fps=%s frames=%s", stream.Codec, stream.Width, stream.Height, stream.FPS, stream.Frames)
	}
	return nil
}

func le2iSemanticMatch(category string, result poseAggregateResult) bool {
	switch category {
	case "Fall":
		return result.FallState == "candidate"
	case "Lie":
		return result.Posture == "ground" && result.FallState == "none"
	case "Likefall":
		return result.Posture == "ambiguous" || result.FallState == "candidate"
	case "Stand":
		return result.Posture == "upright" && result.FallState == "none"
	case "Blank":
		return result.PoseFrameCount == 0 && result.FallState == "none"
	default:
		return false
	}
}

func runLe2iAggregateThroughCore(repoRoot string, entry le2iCase, result poseAggregateResult) caseReport {
	value := generatedFixture(generatedSuite{IDPrefix: "le2i", Suite: "le2i_media", Family: "le2i_media", Count: 1, Seed: 997, Bundle: "v3"}, 0)
	value.ID, value.Suite = entry.ID, "le2i_media"
	var envelope map[string]any
	if err := json.Unmarshal(value.Messages[0].Payload, &envelope); err != nil {
		return caseReport{ID: entry.ID, Suite: value.Suite, Bundle: "v3", Passed: false, Error: err.Error()}
	}
	snapshot := envelope["snapshot"].(map[string]any)
	vision := snapshot["vision"].(map[string]any)
	posture := result.Posture
	if posture == "ambiguous" || posture == "unavailable" {
		posture = "unknown"
	}
	vision["pose_status"] = result.PoseStatus
	vision["posture"] = posture
	vision["pose_quality"] = result.Confidence
	vision["pose_sampled"] = result.PoseStatus == "available"
	vision["pose_observation_count"] = result.PoseFrameCount
	vision["fall_state"] = result.FallState
	vision["transition_to_ground"] = result.FallState == "candidate"
	vision["immobility_seconds"] = result.ImmobilitySeconds
	vision["motion_tier"] = map[string]string{"rapid": "rapid", "none": "still"}[result.RapidMotionState]
	if vision["motion_tier"] == "" {
		vision["motion_tier"] = "unknown"
	}
	vision["physical_interaction_candidate"] = false
	vision["real_detection"] = true
	vision["replay_simulation"] = false
	envelope["provenance"] = "vision-media-harness/le2i-v1"
	value.Messages[0].Payload, _ = json.Marshal(envelope)
	return runFixture(repoRoot, value)
}

func diagnosePoseModel(repoRoot, modelPath string) poseModelDiagnostic {
	if modelPath == "" {
		return poseModelDiagnostic{Status: "unavailable", Reason: "SYNORA_POSE_RKNN_MODEL is not configured"}
	}
	info, err := os.Stat(modelPath)
	if err != nil {
		return poseModelDiagnostic{Status: "unavailable", Reason: "pose model is unavailable: " + err.Error()}
	}
	if !info.Mode().IsRegular() || !strings.HasSuffix(strings.ToLower(modelPath), ".rknn") {
		return poseModelDiagnostic{Status: "unavailable", Reason: "pose model must be a regular .rknn file"}
	}
	python := os.Getenv("PYTHON")
	if python == "" {
		python = "python3"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "tools/central_yolov8_pose.py", "--diagnostic", "--model", modelPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(cmd.Dir, "services/vision-worker"))
	out, err := cmd.Output()
	if err != nil {
		return poseModelDiagnostic{Status: "unavailable", Reason: "YOLOv8-pose RKNN runtime could not load the model: " + trimCommandError(err, out)}
	}
	var result struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := unmarshalAggregateJSON(out, &result); err != nil || result.Status != "available" {
		if result.Reason == "" {
			result.Reason = "YOLOv8-pose RKNN diagnostic returned no available status"
		}
		return poseModelDiagnostic{Status: "unavailable", Reason: result.Reason}
	}
	return poseModelDiagnostic{Status: "available", Reason: result.Reason}
}

func evaluateVisionMediaEntry(repoRoot string, entry visionMediaEntry, mediaRoot string, model poseModelDiagnostic) mediaCaseReport {
	item := mediaCaseReport{ID: entry.ID, Family: entry.Family, Status: mediaStatusMediaMissing, PhysicalInteractionCandidate: false, Expected: expectedMediaValues(entry.Expectation)}
	if mediaRoot == "" {
		item.Reason = "SYNORA_VISION_MEDIA_ROOT is not configured; media mode is blocked"
		return item
	}
	path, err := safeMediaPath(mediaRoot, entry.RelativePath)
	if err != nil {
		item.Status, item.Reason = mediaStatusIntegrity, err.Error()
		return item
	}
	if _, err := os.Stat(path); err != nil {
		item.Status, item.Reason = mediaStatusMediaMissing, "media file is missing"
		return item
	}
	if entry.SHA256 == "" || entry.SHA256 == strings.Repeat("0", 64) {
		item.Status, item.Reason = mediaStatusIntegrity, "manifest entry has no deposited media hash"
		return item
	}
	actualHash, err := sha256File(path)
	if err != nil {
		item.Status, item.Reason = mediaStatusIntegrity, "media hash failed: "+err.Error()
		return item
	}
	if !strings.EqualFold(actualHash, entry.SHA256) {
		item.Status, item.Reason = mediaStatusIntegrity, "media SHA-256 does not match the manifest"
		return item
	}
	if err := validateMediaDecode(path, entry.DurationMS); err != nil {
		item.Status, item.Reason = mediaStatusIntegrity, err.Error()
		return item
	}
	if model.Status != "available" && requiresPose(entry.Family) {
		item.Status, item.Reason, item.PoseStatus = mediaStatusModelBlocked, model.Reason, "unavailable"
		return item
	}
	if !requiresPose(entry.Family) {
		item.Status, item.Reason = mediaStatusModelBlocked, "face backend is intentionally outside this task"
		return item
	}
	result, err := runPoseMediaCase(repoRoot, path, modelPathFromEnvironment())
	if err != nil {
		item.Status, item.Reason, item.PoseStatus = mediaStatusFailed, err.Error(), "unavailable"
		return item
	}
	item.Status = mediaStatusPassed
	item.PoseStatus = result.PoseStatus
	item.Posture = result.Posture
	item.ImmobilitySeconds = result.ImmobilitySeconds
	item.FallState = result.FallState
	item.RapidMotionState = result.RapidMotionState
	item.PhysicalInteractionCandidate = result.PhysicalInteractionCandidate
	item.Confidence = result.Confidence
	item.LatencyMS = result.LatencyMS
	if reason := mediaExpectationMismatch(entry.Expectation, result); reason != "" {
		item.Status, item.Reason = mediaStatusFailed, reason
	}
	return item
}

func mediaExpectationMismatch(expectation visionMediaExpectation, actual poseAggregateResult) string {
	if expected, ok := expectation.Vision["posture"].(string); ok && expected != "" && expected != "unknown" && actual.Posture != expected {
		return fmt.Sprintf("vision posture mismatch: expected %s, got %s", expected, actual.Posture)
	}
	if expected, ok := expectation.Vision["fall_state"].(string); ok && expected != "" {
		if expected == "uncertain" {
			if actual.FallState != "uncertain" && actual.FallState != "none" && actual.FallState != "candidate" {
				return fmt.Sprintf("vision fall state mismatch: expected uncertain-compatible state, got %s", actual.FallState)
			}
		} else if actual.FallState != expected {
			return fmt.Sprintf("vision fall state mismatch: expected %s, got %s", expected, actual.FallState)
		}
	}
	if actual.FallState == "confirmed" {
		return "confirmed fall state is forbidden in this task"
	}
	return ""
}

func expectedMediaValues(expectation visionMediaExpectation) map[string]any {
	result := make(map[string]any)
	if expectation.Vision != nil {
		result["vision"] = expectation.Vision
	}
	if expectation.Core != nil {
		result["core"] = expectation.Core
	}
	if expectation.SafetyGate != nil {
		result["safety_gate"] = expectation.SafetyGate
	}
	return result
}

func requiresPose(family string) bool {
	switch family {
	case "fall", "lie_down", "sit_down", "near_fall_or_bend", "no_person_or_occluded", "multi_person":
		return true
	default:
		return false
	}
}

func safeMediaPath(root, relative string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(filepath.Join(rootAbs, relative))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("media path escapes SYNORA_VISION_MEDIA_ROOT")
	}
	return pathAbs, nil
}

func sha256File(path string) (string, error) {
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

func validateMediaDecode(path string, expectedDurationMS int64) error {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return errors.New("ffprobe is unavailable; media decode cannot be verified")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=duration:stream=codec_type", "-of", "json", path)
	body, err := cmd.Output()
	if err != nil {
		return errors.New("media decode failed: " + trimCommandError(err, body))
	}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return errors.New("media decoder returned invalid metadata")
	}
	if len(probe.Streams) == 0 {
		return errors.New("media has no decodable stream")
	}
	if expectedDurationMS > 0 {
		duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
		if err != nil || duration <= 0 {
			return errors.New("media duration is unavailable")
		}
		if diff := duration*1000 - float64(expectedDurationMS); diff > 100 || diff < -100 {
			return errors.New("media duration does not match the manifest")
		}
	}
	return nil
}

func runPoseMediaCase(repoRoot, path, modelPath string) (poseAggregateResult, error) {
	python := os.Getenv("PYTHON")
	if python == "" {
		python = "python3"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "tools/central_yolov8_pose.py", "--video", path, "--model", modelPath, "--max-frames", "32")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(repoRoot, "services/vision-worker"))
	body, err := cmd.Output()
	if err != nil {
		return poseAggregateResult{}, fmt.Errorf("YOLOv8-pose media execution failed: %s", trimCommandError(err, body))
	}
	var result poseAggregateResult
	if err := unmarshalAggregateJSON(body, &result); err != nil {
		return poseAggregateResult{}, errors.New("YOLOv8-pose media output was not an aggregate contract")
	}
	if result.FallState == "confirmed" {
		return poseAggregateResult{}, errors.New("YOLOv8-pose backend returned forbidden confirmed fall state")
	}
	return result, nil
}

func unmarshalAggregateJSON(body []byte, target any) error {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
			if err := json.Unmarshal([]byte(line), target); err == nil {
				return nil
			}
		}
	}
	return errors.New("aggregate JSON line not found")
}

func modelPathFromEnvironment() string {
	return os.Getenv("SYNORA_POSE_RKNN_MODEL")
}

func trimCommandError(err error, output []byte) string {
	if len(output) > 0 {
		return strings.TrimSpace(string(output))
	}
	return err.Error()
}
