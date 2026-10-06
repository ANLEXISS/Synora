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
	ID                           string         `json:"id"`
	Family                       string         `json:"family"`
	Status                       string         `json:"status"`
	Reason                       string         `json:"reason,omitempty"`
	PoseStatus                   string         `json:"pose_status,omitempty"`
	Posture                      string         `json:"posture,omitempty"`
	ImmobilitySeconds            float64        `json:"immobility_seconds,omitempty"`
	FallState                    string         `json:"fall_state,omitempty"`
	RapidMotionState             string         `json:"rapid_motion_state,omitempty"`
	PhysicalInteractionCandidate bool           `json:"physical_interaction_candidate"`
	Confidence                   float64        `json:"confidence,omitempty"`
	LatencyMS                    float64        `json:"latency_ms,omitempty"`
	Expected                     map[string]any `json:"expected,omitempty"`
}

type mediaSuiteReport struct {
	SchemaVersion  string            `json:"schema_version"`
	ManifestSHA256 string            `json:"manifest_sha256"`
	Mode           string            `json:"mode"`
	MediaRootSet   bool              `json:"media_root_set"`
	ModelStatus    string            `json:"model_status"`
	ModelReason    string            `json:"model_reason,omitempty"`
	ScenarioCount  int               `json:"scenario_count"`
	PassedCount    int               `json:"passed_count"`
	FailedCount    int               `json:"failed_count"`
	StatusCounts   map[string]int    `json:"status_counts"`
	FamilyCounts   map[string]int    `json:"family_counts"`
	Passed         bool              `json:"passed"`
	Cases          []mediaCaseReport `json:"cases"`
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
	manifest, err := loadVisionMediaManifest(manifestPath)
	if err != nil {
		return mediaSuiteReport{SchemaVersion: "synora.central-media-e2e/v1", ManifestSHA256: fileSHA256(manifestPath), Mode: "media", MediaRootSet: mediaRoot != "", ModelStatus: "unavailable", ModelReason: err.Error(), StatusCounts: map[string]int{mediaStatusIntegrity: 1}, FailedCount: 1, ScenarioCount: 1, Passed: false, Cases: []mediaCaseReport{{ID: "manifest", Family: "manifest", Status: mediaStatusIntegrity, Reason: err.Error()}}}
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
	if err := json.Unmarshal(out, &result); err != nil || result.Status != "available" {
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
	result, err := runPoseMediaCase(repoRoot, path, modelPathFromEnvironment(), entry)
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

func runPoseMediaCase(repoRoot, path, modelPath string, entry visionMediaEntry) (poseAggregateResult, error) {
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
	if err := json.Unmarshal(body, &result); err != nil {
		return poseAggregateResult{}, errors.New("YOLOv8-pose media output was not an aggregate contract")
	}
	if result.FallState == "confirmed" {
		return poseAggregateResult{}, errors.New("YOLOv8-pose backend returned forbidden confirmed fall state")
	}
	return result, nil
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
