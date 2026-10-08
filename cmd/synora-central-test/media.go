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
	"regexp"
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
	IngressHTTPStatus            int               `json:"ingress_http_status,omitempty"`
	IngressLifecycleEvents       int               `json:"ingress_lifecycle_events,omitempty"`
	PoseStatus                   string            `json:"pose_status,omitempty"`
	VisionEvidenceV1Status       string            `json:"vision_evidence_v1_status,omitempty"`
	Posture                      string            `json:"posture,omitempty"`
	ImmobilitySeconds            float64           `json:"immobility_seconds,omitempty"`
	FallState                    string            `json:"fall_state,omitempty"`
	RapidMotionState             string            `json:"rapid_motion_state,omitempty"`
	PhysicalInteractionCandidate bool              `json:"physical_interaction_candidate"`
	Confidence                   float64           `json:"confidence,omitempty"`
	LatencyMS                    float64           `json:"latency_ms,omitempty"`
	PoseInitializationMS         float64           `json:"pose_initialization_ms,omitempty"`
	HumanGateInitializationMS    float64           `json:"human_gate_initialization_ms,omitempty"`
	InferenceLatencyP50MS        float64           `json:"inference_latency_p50_ms,omitempty"`
	InferenceLatencyP95MS        float64           `json:"inference_latency_p95_ms,omitempty"`
	InferenceLatencyMaxMS        float64           `json:"inference_latency_max_ms,omitempty"`
	MaxFrames                    int               `json:"max_frames,omitempty"`
	MaxPoseROIs                  int               `json:"max_pose_rois,omitempty"`
	HumanGateRejectedFrames      int               `json:"human_gate_rejected_frames,omitempty"`
	ValidPoseResults             int               `json:"valid_pose_results,omitempty"`
	Expected                     map[string]any    `json:"expected,omitempty"`
	SemanticStatus               string            `json:"semantic_status,omitempty"`
	SemanticMismatchReason       string            `json:"semantic_mismatch_reason,omitempty"`
	CorePassed                   bool              `json:"core_passed"`
	StoreRevision                uint64            `json:"store_revision"`
	APISnapshotObserved          bool              `json:"api_snapshot_observed"`
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
	InferenceLatencySamplesMS    []float64         `json:"-"`
	Journey                      []journeyEvent    `json:"journey,omitempty"`
	PipelineComplete             *bool             `json:"pipeline_complete,omitempty"`
	PipelineIncompleteReason     string            `json:"pipeline_incomplete_reason,omitempty"`
	MissingStages                *[]string         `json:"missing_stages,omitempty"`
	LastObservedStage            *string           `json:"last_observed_stage,omitempty"`
	ActionLifecycleStatus        string            `json:"action_lifecycle_status,omitempty"`
	IdempotenceChecks            idempotenceChecks `json:"idempotence_checks"`
	RejectedActionResults        int               `json:"rejected_action_results"`
}

type mediaSuiteReport struct {
	SchemaVersion                string                    `json:"schema_version"`
	ManifestSHA256               string                    `json:"manifest_sha256"`
	SourceManifestSHA256         string                    `json:"source_manifest_sha256,omitempty"`
	Mode                         string                    `json:"mode"`
	VisionBackend                string                    `json:"vision_backend"`
	MediaRootSet                 bool                      `json:"media_root_set"`
	ModelStatus                  string                    `json:"model_status"`
	ModelReason                  string                    `json:"model_reason,omitempty"`
	PoseModelSHA256              string                    `json:"pose_model_sha256,omitempty"`
	RKNNRuntimeVersion           string                    `json:"rknn_runtime_version,omitempty"`
	RKNNDriverVersion            string                    `json:"rknn_driver_version,omitempty"`
	ModelToolkitVersion          string                    `json:"model_toolkit_version,omitempty"`
	OutputShapes                 []string                  `json:"output_shapes,omitempty"`
	PoseInitializationMS         float64                   `json:"pose_initialization_ms"`
	PoseInitializationLatencyMS  map[string]float64        `json:"pose_initialization_latency_ms"`
	HumanGateRejectedFrames      int                       `json:"human_gate_rejected_frames"`
	PoseRequestedFrames          int                       `json:"pose_requested_frames"`
	ValidPoseResults             int                       `json:"valid_pose_results"`
	MaxFramesPerClip             int                       `json:"max_frames_per_clip"`
	MaxPoseROIsPerClip           int                       `json:"max_pose_rois_per_clip"`
	PhysicalActionExecuted       bool                      `json:"physical_action_executed"`
	AudioRendered                bool                      `json:"audio_rendered"`
	RawVisionForwarded           bool                      `json:"raw_vision_forwarded"`
	NetworkAccess                bool                      `json:"network_access"`
	ScenarioCount                int                       `json:"scenario_count"`
	PassedCount                  int                       `json:"passed_count"`
	FailedCount                  int                       `json:"failed_count"`
	StatusCounts                 map[string]int            `json:"status_counts"`
	FamilyCounts                 map[string]int            `json:"family_counts"`
	Passed                       bool                      `json:"passed"`
	NotRunCount                  int                       `json:"not_run_count"`
	BlockedModelMissingCount     int                       `json:"blocked_model_missing_count"`
	PoseBackendStatus            string                    `json:"pose_backend_status"`
	MediaHashVerificationPassed  bool                      `json:"media_hash_verification_passed"`
	MediaByCategory              map[string]map[string]int `json:"media_by_category"`
	PoseLatencyMS                map[string]float64        `json:"pose_latency_ms"`
	NonFallFalsePositives        map[string]int            `json:"non_fall_false_positives"`
	FallRecallObserved           map[string]float64        `json:"fall_recall_observed"`
	SemanticMismatchCases        []semanticMismatchReport  `json:"semantic_mismatch_cases"`
	PostureByCategory            map[string]map[string]int `json:"posture_by_category"`
	FallStateByCategory          map[string]map[string]int `json:"fall_state_by_category"`
	SemanticQualification        string                    `json:"semantic_qualification"`
	SemanticDebtByCategory       map[string]int            `json:"semantic_debt_by_category"`
	PipelineCompletedCount       int                       `json:"pipeline_completed_count"`
	PipelineIncompleteCount      int                       `json:"pipeline_incomplete_count"`
	PipelineTerminalStatusCounts map[string]int            `json:"pipeline_terminal_status_counts"`
	PipelineAccountingValid      bool                      `json:"pipeline_accounting_valid"`
	ActionLifecycleByStatus      map[string]int            `json:"action_lifecycle_by_status"`
	IdempotenceChecks            map[string]int            `json:"idempotence_checks"`
	RejectedActionResults        int                       `json:"rejected_action_results"`
	Cases                        []mediaCaseReport         `json:"cases"`
}

type semanticMismatchReport struct {
	CaseID    string `json:"case_id"`
	Category  string `json:"category"`
	Reason    string `json:"reason"`
	Posture   string `json:"posture"`
	FallState string `json:"fall_state"`
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
	Status           string   `json:"status"`
	Reason           string   `json:"reason"`
	RuntimeVersion   string   `json:"runtime_version"`
	DriverVersion    string   `json:"driver_version"`
	ToolkitVersion   string   `json:"toolkit_version"`
	InitializationMS float64  `json:"initialization_ms"`
	OutputShapes     []string `json:"output_shapes"`
	ModelSHA256      string   `json:"model_sha256"`
}

type poseAggregateResult struct {
	PoseStatus                   string    `json:"pose_status"`
	Posture                      string    `json:"posture"`
	ImmobilitySeconds            float64   `json:"immobility_seconds"`
	FallState                    string    `json:"fall_state"`
	RapidMotionState             string    `json:"rapid_motion_state"`
	PhysicalInteractionCandidate bool      `json:"physical_interaction_candidate"`
	Confidence                   float64   `json:"confidence"`
	LatencyMS                    float64   `json:"latency_ms"`
	LatencyP50MS                 float64   `json:"latency_p50_ms"`
	LatencyP95MS                 float64   `json:"latency_p95_ms"`
	LatencyMaxMS                 float64   `json:"latency_max_ms"`
	InferenceLatencyP50MS        float64   `json:"inference_latency_p50_ms"`
	InferenceLatencyP95MS        float64   `json:"inference_latency_p95_ms"`
	InferenceLatencyMaxMS        float64   `json:"inference_latency_max_ms"`
	FrameCount                   int       `json:"frame_count"`
	PoseFrameCount               int       `json:"pose_frame_count"`
	HumanDetectorStatus          string    `json:"human_detector_status"`
	HumanConfirmedFrameCount     int       `json:"human_confirmed_frame_count"`
	PoseRequestCount             int       `json:"pose_request_count"`
	PoseRequestReason            string    `json:"pose_request_reason"`
	PoseInitializationMS         float64   `json:"pose_initialization_ms"`
	HumanGateInitializationMS    float64   `json:"human_gate_initialization_ms"`
	MaxFrames                    int       `json:"max_frames"`
	MaxPoseROIs                  int       `json:"max_pose_rois"`
	HumanGateRejectedFrames      int       `json:"human_gate_rejected_frames"`
	InferenceLatencySamplesMS    []float64 `json:"inference_latency_samples_ms"`
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
		Mode: "le2i", VisionBackend: "unavailable", MediaRootSet: mediaRoot != "", StatusCounts: make(map[string]int), FamilyCounts: make(map[string]int),
		MediaByCategory: make(map[string]map[string]int), PoseLatencyMS: map[string]float64{"p50_ms": 0, "p95_ms": 0, "max_ms": 0},
		PoseInitializationLatencyMS: map[string]float64{"p50_ms": 0, "p95_ms": 0, "max_ms": 0},
		PostureByCategory:           make(map[string]map[string]int), FallStateByCategory: make(map[string]map[string]int), SemanticQualification: "not_qualified",
		SemanticDebtByCategory: make(map[string]int), NonFallFalsePositives: map[string]int{"Blank": 0, "Lie": 0, "Likefall": 0, "Stand": 0},
		FallRecallObserved:      map[string]float64{"observed_candidates": 0, "labelled_fall_clips": float64(lenCasesByCategory(manifest.Cases, "Fall")), "recall": 0},
		ActionLifecycleByStatus: make(map[string]int), IdempotenceChecks: make(map[string]int), Cases: make([]mediaCaseReport, 0, len(manifest.Cases)),
		MaxFramesPerClip: 16, MaxPoseROIsPerClip: 16,
	}
	model := diagnosePoseModel(repoRoot, modelPath)
	report.ModelStatus, report.ModelReason = le2iModelStatus(modelPath, model), model.Reason
	report.PoseBackendStatus = report.ModelStatus
	if report.ModelStatus == "available" {
		report.VisionBackend = "real_rknn_pose_test"
		report.PoseModelSHA256 = model.ModelSHA256
		report.RKNNRuntimeVersion = model.RuntimeVersion
		report.RKNNDriverVersion = model.DriverVersion
		report.ModelToolkitVersion = model.ToolkitVersion
		report.OutputShapes = append([]string(nil), model.OutputShapes...)
		report.PoseInitializationMS = model.InitializationMS
	}
	allHashesVerified := mediaRoot != ""
	var latencyValues, initializationValues, inferenceLatencyValues []float64
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
		report.RawVisionForwarded = report.RawVisionForwarded || item.RawVisionForwarded
		report.AudioRendered = report.AudioRendered || item.AudioRendered
		report.PhysicalActionExecuted = report.PhysicalActionExecuted || item.PhysicalActionExecuted
		report.NetworkAccess = report.NetworkAccess || item.NetworkAccess
		if item.LatencyMS > 0 {
			latencyValues = append(latencyValues, item.LatencyMS)
		}
		if item.PoseInitializationMS > 0 {
			initializationValues = append(initializationValues, item.PoseInitializationMS)
		}
		inferenceLatencyValues = append(inferenceLatencyValues, item.InferenceLatencySamplesMS...)
		report.HumanGateRejectedFrames += item.HumanGateRejectedFrames
		report.PoseRequestedFrames += item.PoseRequestCount
		report.ValidPoseResults += item.ValidPoseResults
		if item.Posture != "" {
			incrementNested(report.PostureByCategory, item.Category, item.Posture)
		}
		if item.FallState != "" {
			incrementNested(report.FallStateByCategory, item.Category, item.FallState)
		}
		if item.SemanticStatus == "observed_mismatch" {
			report.SemanticDebtByCategory[item.Category]++
			report.SemanticMismatchCases = append(report.SemanticMismatchCases, semanticMismatchReport{
				CaseID: item.ID, Category: item.Category, Reason: item.SemanticMismatchReason, Posture: item.Posture, FallState: item.FallState,
			})
		}
		if item.Category == "Fall" && item.FallState == "candidate" {
			report.FallRecallObserved["observed_candidates"]++
		}
		if item.Category != "Fall" && item.FallState == "candidate" {
			report.NonFallFalsePositives[item.Category]++
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
		report.RejectedActionResults += item.RejectedActionResults
	}
	report.ScenarioCount = len(report.Cases)
	finalizeMediaPipelineAccounting(&report)
	report.FailedCount = report.ScenarioCount - report.PassedCount
	report.Passed = report.ScenarioCount > 0 && report.FailedCount == 0 && report.MediaHashVerificationPassed
	report.MediaHashVerificationPassed = allHashesVerified && report.ScenarioCount == 48
	if len(latencyValues) > 0 {
		p50, p95, maximum := percentileFloat(latencyValues, 50), percentileFloat(latencyValues, 95), maxFloat(latencyValues)
		report.PoseLatencyMS["p50_ms"] = p50
		report.PoseLatencyMS["p95_ms"] = p95
		report.PoseLatencyMS["max_ms"] = maximum
		report.PoseLatencyMS["per_clip_mean_p50_ms"] = p50
		report.PoseLatencyMS["per_clip_mean_p95_ms"] = p95
		report.PoseLatencyMS["per_clip_mean_max_ms"] = maximum
	}
	if len(inferenceLatencyValues) > 0 {
		report.PoseLatencyMS["inference_p50_ms"] = percentileFloat(inferenceLatencyValues, 50)
		report.PoseLatencyMS["inference_p95_ms"] = percentileFloat(inferenceLatencyValues, 95)
		report.PoseLatencyMS["inference_max_ms"] = maxFloat(inferenceLatencyValues)
	}
	if len(initializationValues) > 0 {
		report.PoseInitializationLatencyMS["p50_ms"] = percentileFloat(initializationValues, 50)
		report.PoseInitializationLatencyMS["p95_ms"] = percentileFloat(initializationValues, 95)
		report.PoseInitializationLatencyMS["max_ms"] = maxFloat(initializationValues)
	}
	if total := report.FallRecallObserved["labelled_fall_clips"]; total > 0 {
		report.FallRecallObserved["recall"] = report.FallRecallObserved["observed_candidates"] / total
	}
	// Hash verification is an integrity gate; calculate Passed after its final value.
	report.Passed = report.ScenarioCount == 48 && report.FailedCount == 0 && report.MediaHashVerificationPassed
	return report
}

func lenCasesByCategory(cases []le2iCase, category string) int {
	count := 0
	for _, item := range cases {
		if item.Category == category {
			count++
		}
	}
	return count
}

func finalizeMediaPipelineAccounting(report *mediaSuiteReport) {
	report.PipelineCompletedCount = 0
	report.PipelineIncompleteCount = 0
	report.PipelineTerminalStatusCounts = make(map[string]int)
	for index := range report.Cases {
		item := &report.Cases[index]
		complete, reason, missing := assessJourney(item.Journey)
		if complete {
			item.PipelineComplete = nil
			item.PipelineIncompleteReason = ""
			item.MissingStages = nil
			item.LastObservedStage = nil
			report.PipelineCompletedCount++
		} else {
			falseValue := false
			item.PipelineComplete = &falseValue
			item.PipelineIncompleteReason = reason
			item.MissingStages = &missing
			item.LastObservedStage = lastObservedStage(item.Journey)
			if len(item.Journey) > 0 {
				report.PipelineIncompleteCount++
			}
		}
		if len(item.Journey) > 0 {
			report.PipelineTerminalStatusCounts[item.Journey[len(item.Journey)-1].Status]++
		}
	}
	journeyCount := 0
	for _, item := range report.Cases {
		if len(item.Journey) > 0 {
			journeyCount++
		}
	}
	report.PipelineAccountingValid = journeyCount == report.ScenarioCount && report.PipelineCompletedCount+report.PipelineIncompleteCount == report.ScenarioCount
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
		item.Status, item.Reason = mediaStatusMediaMissing, "SYNORA_VISION_MEDIA_ROOT is not configured"
		return completeUnavailableMediaCase(repoRoot, entry, item)
	}
	path, err := safeMediaPath(mediaRoot, entry.ClipRelativePath)
	if err != nil {
		item.Status, item.Reason = mediaStatusIntegrity, err.Error()
		return completeUnavailableMediaCase(repoRoot, entry, item)
	}
	if _, err := os.Stat(path); err != nil {
		item.Status, item.Reason = mediaStatusMediaMissing, "Le2i clip is missing"
		return completeUnavailableMediaCase(repoRoot, entry, item)
	}
	actualHash, err := sha256File(path)
	if err != nil || !strings.EqualFold(actualHash, entry.ClipSHA256) {
		item.Status, item.Reason = mediaStatusIntegrity, "Le2i clip SHA-256 does not match the manifest"
		return completeUnavailableMediaCase(repoRoot, entry, item)
	}
	if err := validateLe2iClip(path, entry); err != nil {
		item.Status, item.Reason = mediaStatusIntegrity, err.Error()
		return completeUnavailableMediaCase(repoRoot, entry, item)
	}
	queuedPath, ingressStatus, lifecycleEvents, cleanupIngress, ingressErr := ingressLe2iClip(path, entry.ID)
	item.IngressHTTPStatus, item.IngressLifecycleEvents = ingressStatus, lifecycleEvents
	if ingressErr != nil {
		item.Status, item.Reason, item.PoseStatus = mediaStatusFailed, ingressErr.Error(), "unavailable"
		return completeUnavailableMediaCase(repoRoot, entry, item)
	}
	defer cleanupIngress()
	if model.Status != "available" {
		blockStatus := mediaStatusModelMissing
		poseStatus := "blocked_model_missing"
		if modelPath := modelPathFromEnvironment(); modelPath != "" {
			if _, statErr := os.Stat(modelPath); statErr == nil {
				blockStatus, poseStatus = mediaStatusModelBlocked, "unavailable"
			}
		}
		item.Status, item.PoseStatus, item.Reason = blockStatus, poseStatus, model.Reason
		return completeUnavailableMediaCase(repoRoot, entry, item)
	}
	result, err := runPoseMediaCase(repoRoot, queuedPath, modelPathFromEnvironment())
	if err != nil {
		item.Status, item.Reason, item.PoseStatus = mediaStatusFailed, redactMediaPath(err.Error(), path, modelPathFromEnvironment()), "unavailable"
		return completeUnavailableMediaCase(repoRoot, entry, item)
	}
	item.PoseStatus, item.Posture, item.ImmobilitySeconds = result.PoseStatus, result.Posture, result.ImmobilitySeconds
	item.FallState, item.RapidMotionState = result.FallState, result.RapidMotionState
	item.PhysicalInteractionCandidate, item.Confidence, item.LatencyMS = result.PhysicalInteractionCandidate, result.Confidence, result.LatencyMS
	item.HumanDetectorStatus, item.HumanConfirmedFrameCount = result.HumanDetectorStatus, result.HumanConfirmedFrameCount
	item.PoseRequestCount, item.PoseRequestReason = result.PoseRequestCount, result.PoseRequestReason
	item.ValidPoseResults = result.PoseFrameCount
	item.PoseInitializationMS, item.HumanGateInitializationMS = result.PoseInitializationMS, result.HumanGateInitializationMS
	item.InferenceLatencyP50MS, item.InferenceLatencyP95MS, item.InferenceLatencyMaxMS = result.InferenceLatencyP50MS, result.InferenceLatencyP95MS, result.InferenceLatencyMaxMS
	item.MaxFrames, item.MaxPoseROIs = result.MaxFrames, result.MaxPoseROIs
	item.HumanGateRejectedFrames = result.HumanGateRejectedFrames
	item.InferenceLatencySamplesMS = append([]float64(nil), result.InferenceLatencySamplesMS...)
	coreReport := runLe2iAggregateThroughCore(repoRoot, entry, result)
	item.CorePassed = coreReport.Passed
	item.VisionEvidenceV1Status = coreReport.VisionEvidenceV1Status
	item.StoreRevision = coreReport.StoreRevision
	item.APISnapshotObserved = observedAPISnapshot(coreReport.BusTrace)
	item.SnapshotVersion, item.SnapshotDimension = coreReport.SnapshotVersion, coreReport.SnapshotDimension
	item.MLPHeads, item.MLPObservations = coreReport.MLPHeads, coreReport.MLPObservations
	item.SafetyGateStatuses, item.SafetyGateReasons = coreReport.SafetyGateStatuses, coreReport.SafetyGateReasons
	item.RawVisionForwarded, item.AudioRendered, item.PhysicalActionExecuted, item.NetworkAccess = coreReport.RawVisionForwarded, coreReport.AudioRendered, coreReport.PhysicalAction, coreReport.NetworkAccess
	item.Journey, item.ActionLifecycleStatus, item.IdempotenceChecks, item.RejectedActionResults = coreReport.Journey, coreReport.ActionLifecycleStatus, coreReport.IdempotenceChecks, coreReport.RejectedActionResults
	item.Status = mediaStatusPassed
	if !item.CorePassed || item.StoreRevision <= 1 || !item.APISnapshotObserved || item.RawVisionForwarded || item.AudioRendered || item.PhysicalActionExecuted || item.NetworkAccess || item.FallState == "confirmed" {
		item.Status, item.Reason = mediaStatusFailed, "central safety or redaction gate failed"
	}
	if le2iSemanticMatch(entry.Category, result) {
		item.SemanticStatus = "observed_match"
	} else {
		item.SemanticStatus = "observed_mismatch"
		item.SemanticMismatchReason = "manifest expectation not observed"
	}
	if reason := le2iSafetySemanticMismatch(entry.Category, result); reason != "" {
		item.SemanticStatus, item.SemanticMismatchReason = "observed_mismatch", reason
		if result.FallState == "confirmed" {
			item.Status, item.Reason = mediaStatusFailed, reason
		}
	}
	return item
}

func completeUnavailableMediaCase(repoRoot string, entry le2iCase, item mediaCaseReport) mediaCaseReport {
	poseStatus := item.PoseStatus
	if poseStatus != "not_requested" && poseStatus != "low_quality" {
		poseStatus = "unavailable"
	}
	item.PoseStatus = poseStatus
	item.Posture = "unavailable"
	item.FallState = "none"
	item.SemanticStatus = "observed_mismatch"
	item.SemanticMismatchReason = item.Reason
	result := poseAggregateResult{
		PoseStatus: poseStatus, Posture: "unavailable", FallState: "none", RapidMotionState: "unknown",
		HumanDetectorStatus: "unavailable", PoseRequestReason: item.Reason,
	}
	coreReport := runLe2iAggregateThroughCore(repoRoot, entry, result)
	item.CorePassed = coreReport.Passed
	item.VisionEvidenceV1Status = coreReport.VisionEvidenceV1Status
	item.StoreRevision = coreReport.StoreRevision
	item.APISnapshotObserved = observedAPISnapshot(coreReport.BusTrace)
	item.SnapshotVersion, item.SnapshotDimension = coreReport.SnapshotVersion, coreReport.SnapshotDimension
	item.MLPHeads, item.MLPObservations = coreReport.MLPHeads, coreReport.MLPObservations
	item.SafetyGateStatuses, item.SafetyGateReasons = coreReport.SafetyGateStatuses, coreReport.SafetyGateReasons
	item.RawVisionForwarded, item.AudioRendered = coreReport.RawVisionForwarded, coreReport.AudioRendered
	item.PhysicalActionExecuted, item.NetworkAccess = coreReport.PhysicalAction, coreReport.NetworkAccess
	item.Journey, item.ActionLifecycleStatus = coreReport.Journey, coreReport.ActionLifecycleStatus
	item.IdempotenceChecks, item.RejectedActionResults = coreReport.IdempotenceChecks, coreReport.RejectedActionResults
	return item
}

func observedAPISnapshot(trace []traceRecord) bool {
	for _, record := range trace {
		if record.Type == "core.snapshot.v3" && record.Target == "api" {
			return true
		}
	}
	return false
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
		if result.FallState == "candidate" {
			return "Likefall media should prioritize no fall candidate"
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
		return result.Posture == "ambiguous" && result.FallState == "none"
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
	value, err := le2iAggregateEvidenceFixture(value, result)
	if err != nil {
		return caseReport{ID: entry.ID, Suite: value.Suite, Bundle: "v3", Passed: false, Error: "media Evidence V1 conversion failed"}
	}
	return runFixture(repoRoot, value)
}

func le2iAggregateEvidenceFixture(value fixture, result poseAggregateResult) (fixture, error) {
	var envelope map[string]any
	if err := json.Unmarshal(value.Messages[0].Payload, &envelope); err != nil {
		return fixture{}, err
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
	vision["real_detection"] = result.PoseStatus == "available"
	vision["replay_simulation"] = false
	envelope["provenance"] = "vision-media-harness/le2i-v1"
	value.Messages[0].Payload, _ = json.Marshal(envelope)
	// Keep this only as a harness fixture until runFixture's normal Discovery
	// ingress adapter converts it to Evidence V1 immediately before transport.
	return value, nil
}

func diagnosePoseModel(repoRoot, modelPath string) poseModelDiagnostic {
	if modelPath == "" {
		return poseModelDiagnostic{Status: "unavailable", Reason: "SYNORA_POSE_RKNN_MODEL is not configured"}
	}
	info, err := os.Stat(modelPath)
	if err != nil {
		return poseModelDiagnostic{Status: "unavailable", Reason: "pose model file is unavailable"}
	}
	if !info.Mode().IsRegular() || !strings.HasSuffix(strings.ToLower(modelPath), ".rknn") {
		return poseModelDiagnostic{Status: "unavailable", Reason: "pose model must be a regular .rknn file"}
	}
	modelHashBefore, err := sha256File(modelPath)
	if err != nil {
		return poseModelDiagnostic{Status: "unavailable", Reason: "pose model SHA-256 could not be read"}
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		return poseModelDiagnostic{Status: "unavailable", Reason: "YOLOv8-pose RKNN runtime could not load the model: " + err.Error()}
	}
	var result poseModelDiagnostic
	if err := unmarshalAggregateJSON(out, &result); err != nil || result.Status != "available" {
		if result.Reason == "" {
			result.Reason = "YOLOv8-pose RKNN diagnostic returned no available status"
		}
		return poseModelDiagnostic{Status: "unavailable", Reason: redactMediaPath(result.Reason, modelPath)}
	}
	modelHashAfter, err := sha256File(modelPath)
	if err != nil || modelHashBefore != result.ModelSHA256 || modelHashBefore != modelHashAfter {
		return poseModelDiagnostic{Status: "unavailable", Reason: "pose model SHA-256 integrity check failed"}
	}
	if match := regexp.MustCompile(`toolkit version:\s*([0-9.]+)`).FindSubmatch(out); len(match) == 2 {
		result.ToolkitVersion = string(match[1])
	} else {
		result.ToolkitVersion = "not_reported_by_model_runtime"
	}
	return result
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

func redactMediaPath(value string, paths ...string) string {
	for _, path := range paths {
		if path != "" {
			value = strings.ReplaceAll(value, path, "[redacted]")
		}
	}
	return value
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
	return runPoseMediaCaseWithTimeout(repoRoot, path, modelPath, 2*time.Minute)
}

func runPoseMediaCaseWithTimeout(repoRoot, path, modelPath string, timeout time.Duration) (poseAggregateResult, error) {
	python := os.Getenv("PYTHON")
	if python == "" {
		python = "python3"
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "tools/central_yolov8_pose.py", "--video", path, "--model", modelPath, "--max-frames", "16", "--max-rois", "16")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(repoRoot, "services/vision-worker"))
	body, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return poseAggregateResult{}, errors.New("YOLOv8-pose media execution timed out")
		}
		return poseAggregateResult{}, fmt.Errorf("YOLOv8-pose media execution failed: %s", trimCommandError(err, body))
	}
	var result poseAggregateResult
	if err := unmarshalAggregateJSON(body, &result); err != nil {
		return poseAggregateResult{}, errors.New("YOLOv8-pose media output was not an aggregate contract")
	}
	if result.PoseStatus != "available" && result.PoseStatus != "not_requested" && result.PoseStatus != "low_quality" && result.PoseStatus != "unavailable" {
		return poseAggregateResult{}, errors.New("YOLOv8-pose media output has an invalid pose status")
	}
	if result.FallState == "confirmed" {
		return poseAggregateResult{}, errors.New("YOLOv8-pose backend returned forbidden confirmed fall state")
	}
	if result.FrameCount > 16 || result.PoseRequestCount > 16 || result.PoseFrameCount > result.PoseRequestCount || len(result.InferenceLatencySamplesMS) > 16 || result.HumanConfirmedFrameCount > result.FrameCount {
		return poseAggregateResult{}, errors.New("YOLOv8-pose exceeded the per-clip frame or ROI budget")
	}
	if result.HumanConfirmedFrameCount == 0 && (result.PoseRequestCount != 0 || result.PoseFrameCount != 0 || len(result.InferenceLatencySamplesMS) != 0) {
		return poseAggregateResult{}, errors.New("YOLOv8-pose human gate was bypassed")
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
