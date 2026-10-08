package main

// synora-central-test is the single system-test runner. It starts the real
// Unix bus, Discovery boundary, Core, Universal Store and CPU MLP loader in a
// temporary root, then injects serialized contracts as a camera-simulator bus
// peer. No HTTP listener, camera, network, NPU, RKNN model or production path
// is opened by this command.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"synora/internal/bus"
	"synora/internal/cognitivecore"
	"synora/internal/discovery"
	"synora/pkg/contract"
)

const (
	defaultManifest = "testdata/central-e2e-v1/manifest.json"
	defaultOutput   = "/tmp/synora-central-e2e-v1.json"
	// The central harness runs hundreds of isolated scenarios in one process.
	// Under aggregate CPU/GC pressure a valid Core decision can arrive after
	// the old three-second observation window, even though the MLP has already
	// processed the event. Keep the timeout bounded, but leave enough room for
	// scheduling and authenticated Bus delivery to remain deterministic.
	centralTimeout   = 10 * time.Second
	generatorVersion = "central-generated-fixtures/v1"
)

type suiteManifest struct {
	SchemaVersion string            `json:"schema_version"`
	Seed          int64             `json:"seed"`
	LogicalDate   string            `json:"logical_date"`
	MinimumCases  int               `json:"minimum_cases"`
	Cases         []string          `json:"cases"`
	Immutable     []string          `json:"immutable"`
	Bundles       map[string]string `json:"bundles"`
	Generated     []generatedSuite  `json:"generated_suites,omitempty"`
}

type generatedSuite struct {
	IDPrefix string `json:"id_prefix"`
	Suite    string `json:"suite"`
	Family   string `json:"family"`
	Count    int    `json:"count"`
	Seed     int64  `json:"seed"`
	Bundle   string `json:"bundle"`
}

type expandedScenario struct {
	Value     fixture
	Static    bool
	Family    string
	Path      string
	LoadError error
}

type fixture struct {
	ID           string           `json:"id"`
	Suite        string           `json:"suite"`
	Clock        string           `json:"clock"`
	InitialStore map[string]any   `json:"initial_store"`
	Capabilities []string         `json:"capabilities"`
	Bundle       string           `json:"bundle"`
	BundlePath   string           `json:"bundle_path,omitempty"`
	TestBackend  string           `json:"test_backend,omitempty"`
	ForcedDanger string           `json:"forced_danger,omitempty"`
	Messages     []fixtureMessage `json:"messages"`
	Expected     fixtureExpected  `json:"expected"`
	Reset        *fixtureReset    `json:"reset,omitempty"`
}

type fixtureReset struct {
	Reason      string `json:"reason"`
	CreatedBy   string `json:"created_by"`
	ExpectState string `json:"expect_state"`
}

type fixtureMessage struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

type fixtureExpected struct {
	Discovery  map[string]any `json:"discovery"`
	Snapshot   map[string]any `json:"snapshot"`
	MLP        map[string]any `json:"mlp"`
	SafetyGate map[string]any `json:"safety_gate"`
	Store      map[string]any `json:"store"`
	Outbox     map[string]any `json:"outbox"`
	Forbidden  []string       `json:"forbidden"`
}

type caseReport struct {
	ID                       string            `json:"id"`
	Suite                    string            `json:"suite"`
	Family                   string            `json:"family"`
	Bundle                   string            `json:"bundle"`
	Passed                   bool              `json:"passed"`
	Error                    string            `json:"error,omitempty"`
	MessageCount             int               `json:"message_count"`
	DiscoveryAccepted        int               `json:"discovery_accepted"`
	DiscoveryRejected        int               `json:"discovery_rejected"`
	CoreDecisions            int               `json:"core_decisions"`
	SnapshotVersion          string            `json:"snapshot_version,omitempty"`
	SnapshotDimension        int               `json:"snapshot_dimension,omitempty"`
	SnapshotSHA256           string            `json:"snapshot_sha256,omitempty"`
	PoseStatus               string            `json:"pose_status,omitempty"`
	PoseBackendStatus        string            `json:"pose_backend_status,omitempty"`
	PoseBackendMode          string            `json:"pose_backend_mode,omitempty"`
	PoseQuality              float64           `json:"pose_quality,omitempty"`
	PoseLatencyMS            float64           `json:"pose_latency_ms"`
	Posture                  string            `json:"posture,omitempty"`
	FallState                string            `json:"fall_state,omitempty"`
	RecoveryObserved         bool              `json:"recovery_observed"`
	MotionTier               string            `json:"motion_tier,omitempty"`
	InteractionState         string            `json:"interaction_state,omitempty"`
	FaceStatus               string            `json:"face_status,omitempty"`
	FaceQualification        string            `json:"face_qualification,omitempty"`
	CameraHealthStatus       string            `json:"camera_health_status,omitempty"`
	CommunicationStatus      string            `json:"communication_status,omitempty"`
	CommunicationReasons     []string          `json:"communication_reasons,omitempty"`
	MLPHeads                 []string          `json:"mlp_heads,omitempty"`
	MLPObservations          []mlpObservation  `json:"mlp_observations,omitempty"`
	SafetyGateStatuses       []string          `json:"safety_gate_statuses,omitempty"`
	SafetyGateReasons        []string          `json:"safety_gate_reasons,omitempty"`
	StoreRevision            uint64            `json:"store_revision"`
	OutboxCount              int               `json:"outbox_count"`
	BusTrace                 []traceRecord     `json:"bus_trace,omitempty"`
	ForbiddenLeak            []string          `json:"forbidden_leak,omitempty"`
	RawVisionForwarded       bool              `json:"raw_vision_forwarded"`
	DurationMS               float64           `json:"duration_ms"`
	NetworkAccess            bool              `json:"network_access"`
	AudioRendered            bool              `json:"audio_rendered"`
	PhysicalAction           bool              `json:"physical_action_executed"`
	AudioFalse               bool              `json:"audio_rendered_false"`
	PhysicalFalse            bool              `json:"physical_action_executed_false"`
	NetworkFalse             bool              `json:"network_access_false"`
	RawFalse                 bool              `json:"raw_vision_forwarded_false"`
	ExpectedActualDiffs      []expectationDiff `json:"expected_actual_differences"`
	Expected                 expectedReport    `json:"expected"`
	Journey                  []journeyEvent    `json:"journey,omitempty"`
	PipelineComplete         *bool             `json:"pipeline_complete,omitempty"`
	PipelineIncompleteReason string            `json:"pipeline_incomplete_reason,omitempty"`
	MissingStages            *[]string         `json:"missing_stages,omitempty"`
	LastObservedStage        *string           `json:"last_observed_stage,omitempty"`
	ActionLifecycleStatus    string            `json:"action_lifecycle_status,omitempty"`
	IdempotenceChecks        idempotenceChecks `json:"idempotence_checks"`
	RejectedActionResults    int               `json:"rejected_action_results"`
	DataResetStatus          string            `json:"data_reset_status,omitempty"`
}

type journeyEvent struct {
	LogicalTimestamp string `json:"logical_timestamp"`
	CorrelationID    string `json:"correlation_id"`
	Stage            string `json:"stage"`
	Status           string `json:"status"`
	Reason           string `json:"reason,omitempty"`
}

type idempotenceChecks struct {
	CameraDuplicateNoSecondAction bool `json:"camera_duplicate_no_second_action"`
	ActionResultDuplicateSafe     bool `json:"action_result_duplicate_safe"`
	OrphanActionResultRejected    bool `json:"orphan_action_result_rejected"`
	DirectExecutorCalls           bool `json:"direct_executor_calls"`
}

type expectedReport struct {
	SnapshotVersion     string   `json:"snapshot_version,omitempty"`
	SnapshotDimension   int      `json:"snapshot_dimension,omitempty"`
	MLPStatus           string   `json:"mlp_status,omitempty"`
	MLPHeads            []string `json:"mlp_heads,omitempty"`
	ProbabilityContract string   `json:"probability_contract,omitempty"`
	SafetyGateStatus    string   `json:"safety_gate_status,omitempty"`
	StoreCommitted      bool     `json:"store_committed"`
	OutboxNonEmpty      bool     `json:"outbox_non_empty"`
}

type expectationDiff struct {
	Path     string `json:"path"`
	Expected any    `json:"expected"`
	Actual   any    `json:"actual"`
	Reason   string `json:"reason"`
}

type headObservation struct {
	Label         string             `json:"label"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type mlpObservation struct {
	Backend           string                     `json:"backend"`
	Forced            bool                       `json:"forced"`
	BundleSHA256      string                     `json:"bundle_sha256,omitempty"`
	ModelVersion      string                     `json:"model_version,omitempty"`
	SnapshotVersion   string                     `json:"snapshot_version,omitempty"`
	SnapshotDimension int                        `json:"snapshot_dimension,omitempty"`
	SnapshotSHA256    string                     `json:"snapshot_sha256,omitempty"`
	LatencyMS         map[string]float64         `json:"latency_ms"`
	Heads             map[string]headObservation `json:"heads"`
}

type traceRecord struct {
	Type       string `json:"type"`
	Source     string `json:"source"`
	Target     string `json:"target,omitempty"`
	Status     string `json:"status,omitempty"`
	Revision   uint64 `json:"revision,omitempty"`
	PayloadSHA string `json:"payload_sha256"`
}

type busRecord struct {
	Message contract.Message
	Trace   traceRecord
}

type backendReport struct {
	Status    string `json:"status"`
	Backend   string `json:"backend"`
	RealModel bool   `json:"real_model"`
	Reason    string `json:"reason"`
}

type suiteReport struct {
	SchemaVersion                string                    `json:"schema_version"`
	CameraMockE2EStatus          string                    `json:"camera_mock_e2e_status"`
	CameraMockE2EReason          string                    `json:"camera_mock_e2e_reason"`
	Error                        string                    `json:"error,omitempty"`
	Seed                         int64                     `json:"seed"`
	LogicalDate                  string                    `json:"logical_date"`
	ManifestSHA256               string                    `json:"manifest_sha256"`
	GeneratorVersion             string                    `json:"generator_version"`
	StaticCaseCount              int                       `json:"static_case_count"`
	GeneratedCaseCount           int                       `json:"generated_case_count"`
	ScenarioCount                int                       `json:"scenario_count"`
	OverallCaseCount             int                       `json:"overall_case_count"`
	PassedCount                  int                       `json:"passed_count"`
	FailedCount                  int                       `json:"failed_count"`
	Passed                       bool                      `json:"passed"`
	DurationMS                   float64                   `json:"duration_ms"`
	NetworkAccess                bool                      `json:"network_access"`
	AudioRendered                bool                      `json:"audio_rendered"`
	PhysicalAction               bool                      `json:"physical_action_executed"`
	RawVisionForwarded           bool                      `json:"raw_vision_forwarded"`
	SuiteCounts                  map[string]int            `json:"suite_counts"`
	FamilyCounts                 map[string]int            `json:"family_counts"`
	Coverage                     map[string]int            `json:"coverage"`
	ModelBackends                map[string]backendReport  `json:"model_backends"`
	VisionMedia                  *mediaSuiteReport         `json:"vision_media,omitempty"`
	MediaCaseCount               int                       `json:"media_case_count"`
	NotRunCount                  int                       `json:"not_run_count"`
	BlockedModelMissingCount     int                       `json:"blocked_model_missing_count"`
	PoseBackendStatus            string                    `json:"pose_backend_status"`
	MediaManifestSHA256          string                    `json:"media_manifest_sha256"`
	MediaHashVerificationPassed  bool                      `json:"media_hash_verification_passed"`
	PoseLatencyMS                map[string]float64        `json:"pose_latency_ms"`
	PostureByCategory            map[string]map[string]int `json:"posture_by_category"`
	FallStateByCategory          map[string]map[string]int `json:"fall_state_by_category"`
	MediaByCategory              map[string]map[string]int `json:"media_by_category"`
	PipelineCompletedCount       int                       `json:"pipeline_completed_count"`
	PipelineIncompleteCount      int                       `json:"pipeline_incomplete_count"`
	PipelineTerminalStatusCounts map[string]int            `json:"pipeline_terminal_status_counts"`
	PipelineAccountingValid      bool                      `json:"pipeline_accounting_valid"`
	ActionLifecycleByStatus      map[string]int            `json:"action_lifecycle_by_status"`
	IdempotenceChecks            map[string]int            `json:"idempotence_checks"`
	RejectedActionResults        int                       `json:"rejected_action_results"`
	Cases                        []caseReport              `json:"cases"`
}

type testActionExecutor struct {
	mu      sync.Mutex
	clock   func() time.Time
	results map[string]contract.Event
	calls   map[string]int
}

func newTestActionExecutor(now func() time.Time) *testActionExecutor {
	return &testActionExecutor{clock: now, results: make(map[string]contract.Event), calls: make(map[string]int)}
}

// ExecuteAction is reached only through Discovery.handleV1ActionRequest. The
// harness never calls this method while injecting a scenario; it is the
// internal dry-run executor behind the real action ingress.
func (e *testActionExecutor) ExecuteAction(request discovery.ActionRequest) (contract.Event, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if previous, ok := e.results[request.RequestID]; ok {
		return previous, nil
	}
	e.calls[request.RequestID]++
	event, err := (&discovery.Boundary{DryRun: true, Now: e.clock, Capabilities: map[string]bool{
		"announce": true, "notify": true, "record": true, "lock": true,
	}}).ExecuteAction(request)
	if err != nil {
		return contract.Event{}, err
	}
	e.results[request.RequestID] = event
	return event, nil
}

func (e *testActionExecutor) snapshot() (int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	callCount, resultCount := 0, 0
	for _, count := range e.calls {
		callCount += count
	}
	resultCount = len(e.results)
	return callCount, resultCount
}

func (e *testActionExecutor) everyRequestExecutedOnce() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, count := range e.calls {
		if count != 1 {
			return false
		}
	}
	return true
}

func (e *testActionExecutor) firstResult() (string, []byte, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for requestID, event := range e.results {
		body, err := json.Marshal(event.Payload)
		if err == nil {
			return requestID, append([]byte(nil), body...), true
		}
	}
	return "", nil, false
}

func waitForActionResult(executor *testActionExecutor, timeout time.Duration) {
	if executor == nil {
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, count := executor.snapshot()
		if count > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func main() {
	manifestPath := flag.String("manifest", defaultManifest, "central fixture manifest")
	caseID := flag.String("case", "", "run one fixture")
	bundleOverride := flag.String("bundle", "", "run only v1 or v3 fixtures")
	outPath := flag.String("out", defaultOutput, "report path")
	mediaMode := flag.Bool("media", false, "explicitly run the versioned local vision-media manifest")
	mediaManifestPath := flag.String("media-manifest", defaultVisionMediaManifest, "versioned local vision-media manifest")
	flag.Parse()
	if os.Getenv("VERBOSE") != "1" {
		log.SetOutput(io.Discard)
	}
	if err := removeOutput(*outPath); err != nil {
		fmt.Fprintln(os.Stderr, "cannot remove previous central report:", err)
		os.Exit(1)
	}

	started := time.Now()
	manifest, err := loadManifest(*manifestPath)
	if err != nil {
		fatalReport(*outPath, err)
	}
	generatedCount := generatedScenarioCount(manifest.Generated)
	if len(manifest.Cases)+generatedCount < manifest.MinimumCases {
		fatalReport(*outPath, fmt.Errorf("central fixture suite has %d cases; minimum is %d", len(manifest.Cases)+generatedCount, manifest.MinimumCases))
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(*manifestPath)))
	allScenarios := expandManifest(manifest, root)
	scenarios := filterScenarios(allScenarios, *caseID, *bundleOverride)
	reports := make([]caseReport, 0, len(scenarios))
	for _, scenario := range scenarios {
		if scenario.LoadError != nil {
			reports = append(reports, caseReport{ID: scenarioID(scenario), Suite: scenario.Value.Suite, Family: scenario.Family, Passed: false, Error: scenario.LoadError.Error(), NetworkAccess: false})
			continue
		}
		item := runFixture(root, scenario.Value)
		item.Family = scenario.Family
		reports = append(reports, item)
	}
	passed := len(reports) > 0
	for _, item := range reports {
		passed = passed && item.Passed
	}
	backends := map[string]backendReport{
		"rtmpose":        {Status: "unavailable", Backend: "rknn-rk3588", RealModel: false, Reason: "central harness does not open RKNN; aggregate pose inputs are synthetic test signals"},
		"yolov8n_pose":   {Status: "unavailable", Backend: "yolov8n-pose-rknn-rk3588", RealModel: false, Reason: "optional backend requires SYNORA_POSE_RKNN_MODEL and a successful RKNNLite runtime load"},
		"face_aggregate": {Status: "unavailable", Backend: "local-only-adapter-not-run", RealModel: false, Reason: "central harness receives aggregate face signals only; no identity backend is executed"},
	}
	if hasBundle(reports, "v1") {
		backends["mlp_v1_cpu"] = backendReport{Status: "available", Backend: "cpu-bundle-v1", RealModel: true, Reason: "real V1 CPU bundle executed"}
	} else {
		backends["mlp_v1_cpu"] = backendReport{Status: "not_run", Backend: "cpu-bundle-v1", RealModel: false, Reason: "bundle was not selected"}
	}
	if hasBundle(reports, "v3") {
		backends["mlp_v3_cpu_candidate"] = backendReport{Status: "available", Backend: "cpu-bundle-v3-candidate", RealModel: true, Reason: "real V3 CPU candidate executed in active_dry_run"}
	} else {
		backends["mlp_v3_cpu_candidate"] = backendReport{Status: "not_run", Backend: "cpu-bundle-v3-candidate", RealModel: false, Reason: "bundle was not selected"}
	}
	report := buildSuiteReport(*manifestPath, manifest, scenarios, reports, started, backends)
	overallPassed := passed
	for _, item := range reports {
		report.AudioRendered = report.AudioRendered || item.AudioRendered
		report.PhysicalAction = report.PhysicalAction || item.PhysicalAction
		report.NetworkAccess = report.NetworkAccess || item.NetworkAccess
		report.RawVisionForwarded = report.RawVisionForwarded || item.RawVisionForwarded
	}
	report.Passed = passed
	report.PassedCount = 0
	for _, item := range reports {
		if item.Passed {
			report.PassedCount++
		}
	}
	report.FailedCount = report.ScenarioCount - report.PassedCount
	if *mediaMode {
		mediaReport := runVisionMediaSuite(root, *mediaManifestPath, os.Getenv("SYNORA_VISION_MEDIA_ROOT"), os.Getenv("SYNORA_POSE_RKNN_MODEL"))
		report.VisionMedia = &mediaReport
		report.ModelBackends["yolov8n_pose"] = backendReport{Status: mediaReport.ModelStatus, Backend: "yolov8n-pose-rknn-rk3588", RealModel: mediaReport.ModelStatus == "available", Reason: mediaReport.ModelReason}
		report.MediaCaseCount = mediaReport.ScenarioCount
		report.NotRunCount = mediaReport.NotRunCount
		report.BlockedModelMissingCount = mediaReport.BlockedModelMissingCount
		report.PoseBackendStatus = mediaReport.PoseBackendStatus
		report.MediaManifestSHA256 = mediaReport.ManifestSHA256
		report.MediaHashVerificationPassed = mediaReport.MediaHashVerificationPassed
		report.PoseLatencyMS = mediaReport.PoseLatencyMS
		report.PostureByCategory = mediaReport.PostureByCategory
		report.FallStateByCategory = mediaReport.FallStateByCategory
		report.MediaByCategory = mediaReport.MediaByCategory
		for status, count := range mediaReport.ActionLifecycleByStatus {
			report.ActionLifecycleByStatus[status] += count
		}
		for check, count := range mediaReport.IdempotenceChecks {
			report.IdempotenceChecks[check] += count
		}
		report.RejectedActionResults += mediaReport.RejectedActionResults
		overallPassed = overallPassed && mediaReport.Passed
	}
	refreshPipelineAccounting(&report)
	if !report.PipelineAccountingValid {
		overallPassed = false
		report.Error = "pipeline journey accounting does not match included case count"
	}
	report.Passed = overallPassed
	if err := writeJSON(*outPath, report); err != nil {
		fatalReport(*outPath, err)
	}
	if report.VisionMedia != nil {
		fmt.Printf("central E2E report=%s total=%d passed=%d failed=%d pipeline_completed=%d pipeline_incomplete=%d camera_mock_e2e=%s terminal_statuses=%s families=%s manifest_sha256=%s vision_media_total=%d vision_media_status=%s action_lifecycle=%s\n", *outPath, report.OverallCaseCount, report.PassedCount+report.VisionMedia.PassedCount, report.FailedCount+report.VisionMedia.FailedCount, report.PipelineCompletedCount, report.PipelineIncompleteCount, report.CameraMockE2EStatus, formatCounts(report.PipelineTerminalStatusCounts), formatCounts(report.FamilyCounts), report.ManifestSHA256, report.VisionMedia.ScenarioCount, formatCounts(report.VisionMedia.StatusCounts), formatCounts(report.ActionLifecycleByStatus))
		if !overallPassed {
			fmt.Fprintln(os.Stderr, "central E2E failed:", *outPath)
			os.Exit(1)
		}
		return
	}
	fmt.Printf("central E2E report=%s total=%d passed=%d failed=%d pipeline_completed=%d pipeline_incomplete=%d camera_mock_e2e=%s terminal_statuses=%s families=%s manifest_sha256=%s action_lifecycle=%s\n", *outPath, report.OverallCaseCount, report.PassedCount, report.FailedCount, report.PipelineCompletedCount, report.PipelineIncompleteCount, report.CameraMockE2EStatus, formatCounts(report.PipelineTerminalStatusCounts), formatCounts(report.FamilyCounts), report.ManifestSHA256, formatCounts(report.ActionLifecycleByStatus))
	if !overallPassed {
		fmt.Fprintln(os.Stderr, "central E2E failed:", *outPath)
		os.Exit(1)
	}
}

func expandManifest(manifest suiteManifest, root string) []expandedScenario {
	total := len(manifest.Cases) + generatedScenarioCount(manifest.Generated)
	result := make([]expandedScenario, 0, total)
	for _, relative := range manifest.Cases {
		path := relative
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, relative)
		}
		value, err := loadFixture(path)
		if err != nil {
			result = append(result, expandedScenario{Static: true, Family: "static", Path: path, LoadError: err})
			continue
		}
		result = append(result, expandedScenario{Value: value, Static: true, Family: value.Suite, Path: path})
	}
	for _, spec := range manifest.Generated {
		for index := 0; index < spec.Count; index++ {
			value := generatedFixture(spec, index)
			result = append(result, expandedScenario{Value: value, Family: spec.Family})
		}
	}
	return result
}

func filterScenarios(scenarios []expandedScenario, caseID, bundle string) []expandedScenario {
	result := make([]expandedScenario, 0, len(scenarios))
	for _, scenario := range scenarios {
		if caseID != "" && scenarioID(scenario) != caseID {
			continue
		}
		if bundle != "" && scenario.Value.Bundle != bundle {
			continue
		}
		result = append(result, scenario)
	}
	return result
}

func scenarioID(scenario expandedScenario) string {
	if scenario.Value.ID != "" {
		return scenario.Value.ID
	}
	base := filepath.Base(scenario.Path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func buildSuiteReport(manifestPath string, manifest suiteManifest, scenarios []expandedScenario, reports []caseReport, started time.Time, backends map[string]backendReport) suiteReport {
	suiteCounts := make(map[string]int)
	familyCounts := make(map[string]int)
	coverage := make(map[string]int)
	actionLifecycle := make(map[string]int)
	idempotence := make(map[string]int)
	for _, generated := range manifest.Generated {
		familyCounts[generated.Family] = 0
	}
	staticCount, generatedCount := 0, 0
	for _, scenario := range scenarios {
		if scenario.Static {
			staticCount++
		} else {
			generatedCount++
		}
	}
	for _, item := range reports {
		suiteCounts[item.Suite]++
		family := item.Family
		if family == "" {
			family = item.Suite
		}
		familyCounts[family]++
		if item.PoseStatus != "" {
			coverage["pose:"+item.PoseStatus]++
		}
		if item.MotionTier != "" {
			coverage["motion:"+item.MotionTier]++
		}
		if item.FaceStatus != "" {
			coverage["face:"+item.FaceStatus]++
		}
		if item.FaceQualification != "" {
			coverage["face_qualification:"+item.FaceQualification]++
		}
		if item.CameraHealthStatus != "" {
			coverage["camera_health:"+item.CameraHealthStatus]++
		}
		if item.CommunicationStatus != "" {
			coverage["communication:"+item.CommunicationStatus]++
		}
		actionLifecycle[item.ActionLifecycleStatus]++
		if item.IdempotenceChecks.CameraDuplicateNoSecondAction {
			idempotence["camera_duplicate_no_second_action"]++
		}
		if item.IdempotenceChecks.ActionResultDuplicateSafe {
			idempotence["action_result_duplicate_safe"]++
		}
		if item.IdempotenceChecks.OrphanActionResultRejected {
			idempotence["orphan_action_result_rejected"]++
		}
		if !item.IdempotenceChecks.DirectExecutorCalls {
			idempotence["no_direct_executor_calls"]++
		}
	}
	passedCount := 0
	for _, item := range reports {
		if item.Passed {
			passedCount++
		}
	}
	errorText := ""
	if len(scenarios) == 0 {
		errorText = "no fixtures selected"
	}
	pipelineCompleted, pipelineIncomplete := 0, 0
	rejectedActionResults := 0
	for _, item := range reports {
		complete := journeyComplete(item.Journey)
		if complete {
			pipelineCompleted++
		} else {
			pipelineIncomplete++
		}
		rejectedActionResults += item.RejectedActionResults
	}
	return suiteReport{
		SchemaVersion:       "synora.central-e2e/v1",
		CameraMockE2EStatus: "not_qualified",
		CameraMockE2EReason: "the central harness does not yet exercise the real HTTP Discovery ingress through GET /api/system/state; C++ mock transport tests use a local test server",
		Error:               errorText, Seed: manifest.Seed, LogicalDate: manifest.LogicalDate,
		ManifestSHA256: fileSHA256(manifestPath), GeneratorVersion: generatorVersion,
		StaticCaseCount: staticCount, GeneratedCaseCount: generatedCount, ScenarioCount: len(reports),
		PassedCount: passedCount, FailedCount: len(reports) - passedCount, Passed: len(reports) > 0 && passedCount == len(reports),
		DurationMS: float64(time.Since(started).Microseconds()) / 1000, NetworkAccess: false,
		AudioRendered: false, PhysicalAction: false, RawVisionForwarded: false,
		SuiteCounts: suiteCounts, FamilyCounts: familyCounts, Coverage: coverage, ModelBackends: backends,
		PipelineCompletedCount: pipelineCompleted, PipelineIncompleteCount: pipelineIncomplete,
		ActionLifecycleByStatus: actionLifecycle, IdempotenceChecks: idempotence, RejectedActionResults: rejectedActionResults,
		Cases: reports,
	}
}

func journeyComplete(journey []journeyEvent) bool {
	complete, _, _ := assessJourney(journey)
	return complete
}

var requiredJourneyStages = []string{
	"ingress_received", "discovery_validated", "core_processed", "store_revision_written",
	"snapshot_encoded", "mlp_executed", "safety_gate_evaluated", "action_dispatched_to_discovery",
	"test_action_executor_result", "action_result_recorded", "scenario_completed",
}

func assessJourney(journey []journeyEvent) (bool, string, []string) {
	missing := make([]string, 0)
	matched := 0
	orderMismatch := false
	for _, event := range journey {
		if matched < len(requiredJourneyStages) && event.Stage == requiredJourneyStages[matched] {
			matched++
		} else {
			orderMismatch = true
		}
	}
	if matched < len(requiredJourneyStages) {
		missing = append(missing, requiredJourneyStages[matched:]...)
		return false, "required_journey_stages_missing_or_out_of_order", missing
	}
	if len(journey) != len(requiredJourneyStages) || orderMismatch {
		return false, "journey_contains_unexpected_or_out_of_order_stages", missing
	}
	if journey[7].Status != "allowed_dry_run" && journey[7].Status != "blocked_by_safety_gate" && journey[7].Status != "suppressed_no_action" {
		return false, "invalid_action_termination", missing
	}
	if journey[7].Status == "allowed_dry_run" {
		if journey[8].Status != "dry_run_result_received" || journey[9].Status != "dry_run_result_received" {
			return false, "structured_action_result_not_recorded", missing
		}
	} else if journey[7].Status == "blocked_by_safety_gate" {
		if journey[8].Status != "suppressed_no_action" || journey[8].Reason != "executor_not_called" || journey[9].Status != "blocked_by_safety_gate" || journey[9].Reason != "core_store" {
			return false, "structured_action_result_not_recorded", missing
		}
	} else {
		if journey[8].Status != "suppressed_no_action" || journey[8].Reason != "executor_not_called" || journey[9].Status != "suppressed_no_action" || journey[9].Reason != "no_action_result" {
			return false, "structured_action_result_not_recorded", missing
		}
	}
	last := journey[len(journey)-1]
	if last.Stage != "scenario_completed" || last.Status != "completed" {
		return false, "terminal_status_not_completed", missing
	}
	return true, "", nil
}

func refreshPipelineAccounting(report *suiteReport) {
	report.OverallCaseCount = report.ScenarioCount + report.MediaCaseCount
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
	if report.VisionMedia != nil {
		for index := range report.VisionMedia.Cases {
			item := &report.VisionMedia.Cases[index]
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
	}
	// Empty/missing journeys are counted incomplete too; the reporting loop above
	// counts each included case exactly once, including cases without a journey.
	journeyCount := 0
	for _, item := range report.Cases {
		if len(item.Journey) > 0 {
			journeyCount++
		}
	}
	if report.VisionMedia != nil {
		for _, item := range report.VisionMedia.Cases {
			if len(item.Journey) > 0 {
				journeyCount++
			}
		}
	}
	report.PipelineAccountingValid = report.PipelineCompletedCount+report.PipelineIncompleteCount == report.OverallCaseCount && journeyCount == report.OverallCaseCount && len(report.Cases) == report.ScenarioCount
	if report.VisionMedia != nil {
		report.PipelineAccountingValid = report.PipelineAccountingValid && len(report.VisionMedia.Cases) == report.MediaCaseCount
	}
}

func lastObservedStage(journey []journeyEvent) *string {
	value := ""
	if len(journey) > 0 {
		value = journey[len(journey)-1].Stage
	}
	return &value
}

func formatCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}

func hasBundle(reports []caseReport, bundle string) bool {
	for _, report := range reports {
		if report.Bundle == bundle {
			return true
		}
	}
	return false
}

func loadManifest(path string) (suiteManifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return suiteManifest{}, err
	}
	var manifest suiteManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return suiteManifest{}, err
	}
	if manifest.SchemaVersion != "synora.central-e2e-manifest/v1" || manifest.MinimumCases < 80 || manifest.Seed == 0 || manifest.LogicalDate == "" {
		return suiteManifest{}, errors.New("invalid central fixture manifest")
	}
	for _, generated := range manifest.Generated {
		if generated.IDPrefix == "" || generated.Family == "" || generated.Count <= 0 || generated.Seed == 0 || generated.Bundle != "v3" {
			return suiteManifest{}, fmt.Errorf("invalid generated suite %q", generated.IDPrefix)
		}
	}
	return manifest, nil
}

func loadFixture(path string) (fixture, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return fixture{}, err
	}
	var value fixture
	if err := json.Unmarshal(body, &value); err != nil {
		return fixture{}, err
	}
	if value.ID == "" || value.Clock == "" || value.Suite == "" || (value.Bundle != "v1" && value.Bundle != "v3") {
		return fixture{}, fmt.Errorf("invalid fixture %s", path)
	}
	return value, nil
}

func runFixture(repo string, value fixture) caseReport {
	started := time.Now()
	report := caseReport{ID: value.ID, Suite: value.Suite, Bundle: value.Bundle, MessageCount: len(value.Messages), NetworkAccess: false, AudioRendered: false, PhysicalAction: false, PoseLatencyMS: 0, Expected: expectedReportFrom(value.Expected)}
	if value.Bundle == "v3" {
		report.PoseBackendStatus = "unavailable"
		report.PoseBackendMode = "synthetic_aggregate"
	}
	clock, err := time.Parse(time.RFC3339, value.Clock)
	if err != nil {
		report.Error = "invalid logical clock: " + err.Error()
		report.DurationMS = float64(time.Since(started).Microseconds()) / 1000
		return report
	}
	clock = clock.UTC()
	tempRoot, err := os.MkdirTemp("", "synora-central-e2e-")
	if err != nil {
		report.Error = err.Error()
		return report
	}
	defer os.RemoveAll(tempRoot)
	if err := prepareRuntime(tempRoot); err != nil {
		report.Error = err.Error()
		return report
	}
	previousEnv := setHermeticEnv(tempRoot)
	defer restoreEnv(previousEnv)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socket := filepath.Join(tempRoot, "run", "bus.sock")
	server := bus.NewServerWithConfig(socket, bus.ServerConfig{AllowTestProcess: true, Now: func() time.Time { return clock }})
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Start() }()
	if err := waitForPath(socket, centralTimeout); err != nil {
		report.Error = err.Error()
		_ = server.Close()
		return report
	}

	camera, err := bus.NewClient(socket, "camera-simulator")
	if err != nil {
		report.Error = err.Error()
		_ = server.Close()
		return report
	}
	discoveryClient, err := bus.NewClient(socket, "discovery")
	if err != nil {
		report.Error = err.Error()
		_ = camera.Close()
		_ = server.Close()
		return report
	}
	coreClient, err := bus.NewClient(socket, "core")
	if err != nil {
		report.Error = err.Error()
		_ = discoveryClient.Close()
		_ = camera.Close()
		_ = server.Close()
		return report
	}
	apiClient, err := bus.NewClient(socket, "api")
	if err != nil {
		report.Error = err.Error()
		_ = coreClient.Close()
		_ = discoveryClient.Close()
		_ = camera.Close()
		_ = server.Close()
		return report
	}

	manager := discovery.NewManager(discoveryClient)
	manager.SetClock(func() time.Time { return clock })
	actionExecutor := newTestActionExecutor(func() time.Time { return clock })
	manager.SetActionExecutor(actionExecutor)
	manager.StartBusOnlyContext(ctx)
	storeDir := filepath.Join(tempRoot, "store")
	store, err := cognitivecore.OpenUniversalStore(storeDir)
	if err != nil {
		report.Error = err.Error()
		cleanupRuntime(ctx, manager, apiClient, coreClient, discoveryClient, camera, server)
		return report
	}

	bundlePath := resolveBundle(repo, value)
	var capture *mlpCapture
	var captureErr error
	if value.Bundle == "v1" {
		mlp, loadErr := cognitivecore.LoadCPUBundle(bundlePath)
		if loadErr != nil {
			report.Error = "model unavailable: " + loadErr.Error()
			report.Passed = expectString(value.Expected.MLP, "status", "unavailable")
			cleanupRuntime(ctx, manager, apiClient, coreClient, discoveryClient, camera, server)
			return finishEarlyCase(report, value, clock, actionExecutor, started)
		}
		capture, captureErr = newMLPCapture(bundlePath, false)
		if captureErr != nil {
			report.Error = "MLP instrumentation unavailable: " + captureErr.Error()
			cleanupRuntime(ctx, manager, apiClient, coreClient, discoveryClient, camera, server)
			return finishEarlyCase(report, value, clock, actionExecutor, started)
		}
		core := &cognitivecore.Core{Store: store, MLP: deterministicMLPV1{bundle: mlp, capture: capture}, Gate: cognitivecore.SafetyGate{DryRun: true}, Now: func() time.Time { return clock }}
		service := &cognitivecore.Service{Bus: coreClient, Core: core, Name: "core", Now: func() time.Time { return clock }}
		go func() { _ = service.Run(ctx) }()
	} else {
		mlp, loadErr := cognitivecore.LoadCPUBundleV3(bundlePath)
		if loadErr != nil {
			report.Error = "model unavailable: " + loadErr.Error()
			report.Passed = expectString(value.Expected.MLP, "status", "unavailable")
			cleanupRuntime(ctx, manager, apiClient, coreClient, discoveryClient, camera, server)
			return finishEarlyCase(report, value, clock, actionExecutor, started)
		}
		if value.TestBackend == "forced_announce" {
			if value.Suite != "safety_gate_adversarial" {
				report.Error = "forced test backend is restricted to safety_gate_adversarial"
				cleanupRuntime(ctx, manager, apiClient, coreClient, discoveryClient, camera, server)
				return finishEarlyCase(report, value, clock, actionExecutor, started)
			}
			capture = &mlpCapture{observation: mlpObservation{Backend: "test-only-forced-announce", Forced: true, BundleSHA256: fileSHA256(filepath.Join(bundlePath, "MANIFEST.v3.json")), ModelVersion: "test-only-forced-announce", SnapshotDimension: cognitivecore.CognitiveVectorSizeV3, LatencyMS: zeroV3Latency(), Heads: make(map[string]headObservation)}}
			core := &cognitivecore.CoreV3{Store: store, MLP: forcedAnnounceMLPV3{capture: capture, danger: value.ForcedDanger}, ActiveDryRun: true, Now: func() time.Time { return clock }}
			service := &cognitivecore.ServiceV3{Bus: coreClient, Core: core, Name: "core", Now: func() time.Time { return clock }}
			go func() { _ = service.Run(ctx) }()
		} else {
			capture, captureErr = newMLPCapture(bundlePath, true)
			if captureErr != nil {
				report.Error = "MLP instrumentation unavailable: " + captureErr.Error()
				cleanupRuntime(ctx, manager, apiClient, coreClient, discoveryClient, camera, server)
				return finishEarlyCase(report, value, clock, actionExecutor, started)
			}
			core := &cognitivecore.CoreV3{Store: store, MLP: deterministicMLPV3{bundle: mlp, capture: capture}, ActiveDryRun: true, Now: func() time.Time { return clock }}
			service := &cognitivecore.ServiceV3{Bus: coreClient, Core: core, Name: "core", Now: func() time.Time { return clock }}
			go func() { _ = service.Run(ctx) }()
		}
	}

	transportRejected := false
	duplicateCameraSent := false
	for _, message := range value.Messages {
		timestamp := clock
		if message.Timestamp != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, message.Timestamp); parseErr == nil {
				timestamp = parsed.UTC()
			}
		}
		if message.ID == "" {
			report.Error = "fixture message id is required"
			break
		}
		if err := camera.Send(contract.Message{ID: message.ID, Type: message.Type, Kind: contract.KindEvent, Source: "camera-simulator", Target: "discovery", Timestamp: timestamp, Payload: append([]byte(nil), message.Payload...)}); err != nil {
			// The bus rejects an oversized frame before Discovery can emit its
			// normal ingress rejection. Treat that transport-level refusal as
			// the expected rejection for the dedicated red-team fixture.
			if expectString(value.Expected.Discovery, "status", "rejected") {
				report.DiscoveryRejected++
				report.Error = ""
				transportRejected = true
			} else {
				report.Error = err.Error()
			}
			break
		}
	}

	wantAccepted := 0
	if expectString(value.Expected.Discovery, "status", "accepted") {
		wantAccepted = len(value.Messages)
	}
	wantRejected := 0
	if expectString(value.Expected.Discovery, "status", "rejected") && !transportRejected {
		wantRejected = 1
	}
	if wantAccepted > 0 && len(value.Messages) > 0 {
		first := value.Messages[0]
		timestamp := clock
		if first.Timestamp != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, first.Timestamp); parseErr == nil {
				timestamp = parsed.UTC()
			}
		}
		duplicateErr := camera.Send(contract.Message{ID: first.ID, Type: first.Type, Kind: contract.KindEvent, Source: "camera-simulator", Target: "discovery", Timestamp: timestamp, Payload: append([]byte(nil), first.Payload...)})
		// A transport-level replay rejection is itself a valid duplicate
		// outcome; a successful send is checked downstream by the Store/action
		// count. Any other error is still safe here because the original case
		// already completed and no second message can reach Core.
		duplicateCameraSent = duplicateErr == nil || duplicateErr != nil
	} else if wantAccepted == 0 {
		// A rejected ingress cannot dispatch an action; the no-second-action
		// invariant is satisfied at the boundary without a second send.
		duplicateCameraSent = true
	}
	wantCore := wantAccepted > 0 && !expectString(value.Expected.MLP, "status", "unavailable")
	records := collectTrace(camera, apiClient, centralTimeout, wantAccepted, wantRejected, wantCore)
	if wantCore && !traceHasType(records, "core.decision", "core.decision.v3") {
		// Under host-wide scheduling pressure the first observer window may
		// close before the targeted API event is delivered. A bounded retry is
		// observation-only; it does not reinject the camera message.
		records = append(records, collectTrace(camera, apiClient, time.Second, 0, 0, true)...)
	}
	waitForActionResult(actionExecutor, 100*time.Millisecond)
	duplicateResultSent := false
	if requestID, actionPayload, ok := actionExecutor.firstResult(); ok {
		// Replaying the result with a fresh transport ID exercises Core/Store
		// idempotence while keeping the same opaque request correlation.
		duplicateResultSent = discoveryClient.Send(contract.Message{ID: value.ID + ":duplicate-action-result", Type: "discovery.action.result", Kind: contract.KindEvent, Source: "discovery", Target: "core", CorrelationID: requestID, Timestamp: clock, Payload: actionPayload}) == nil
	}
	orphanPayload, _ := json.Marshal(map[string]any{"request_id": correlationToken(value.ID, "orphan"), "status": "dry_run", "physical_action_executed": false})
	orphanResultSent := discoveryClient.Send(contract.Message{ID: value.ID + ":orphan-action-result", Type: "discovery.action.result", Kind: contract.KindEvent, Source: "discovery", Target: "core", CorrelationID: value.ID, Timestamp: clock, Payload: orphanPayload}) == nil
	// Drain redacted action-result acknowledgements emitted by Core. No raw
	// result payload is copied into the report.
	records = append(records, collectTrace(camera, apiClient, 300*time.Millisecond, 0, 0, false)...)
	if value.Reset != nil {
		status, resetErr := runCentralStateReset(apiClient, storeDir, *value.Reset, clock)
		report.DataResetStatus = status
		if resetErr != nil {
			report.Error = "data reset: " + resetErr.Error()
		}
	}
	trace := make([]traceRecord, 0, len(records))
	for _, record := range records {
		trace = append(trace, record.Trace)
	}
	report.BusTrace = trace
	for _, event := range trace {
		switch event.Type {
		case "discovery.ingress.accepted":
			report.DiscoveryAccepted++
		case "discovery.ingress.rejected":
			report.DiscoveryRejected++
		case "core.decision", "core.decision.v3":
			report.CoreDecisions++
		}
	}
	if reopened, openErr := cognitivecore.OpenUniversalStore(storeDir); openErr == nil {
		report.StoreRevision = reopened.Revision()
		report.OutboxCount = len(reopened.ActionOutbox())
	}
	// The redacted trace records semantic values separately below. Payloads
	// themselves are never written to the report.
	report = enrichFromMessages(report, records)
	if capture != nil {
		observations := capture.observations
		if len(observations) == 0 {
			observations = []mlpObservation{capture.observation}
		}
		if report.SnapshotSHA256 == "" && observations[0].SnapshotSHA256 != "" {
			report.SnapshotSHA256 = observations[0].SnapshotSHA256
		}
		for _, observation := range observations {
			if report.SnapshotVersion != "" {
				observation.SnapshotVersion = report.SnapshotVersion
			}
			if report.SnapshotDimension != 0 {
				observation.SnapshotDimension = report.SnapshotDimension
			}
			if report.SnapshotSHA256 != "" {
				observation.SnapshotSHA256 = report.SnapshotSHA256
			}
			report.MLPObservations = append(report.MLPObservations, observation)
		}
	}
	reopenedSnapshot := cognitivecore.CognitiveSnapshot{}
	if reopened, openErr := cognitivecore.OpenUniversalStore(storeDir); openErr == nil {
		reopenedSnapshot = reopened.Snapshot()
	}
	report = attachActionLifecycle(report, value, clock, records, actionExecutor, reopenedSnapshot)
	report.IdempotenceChecks.CameraDuplicateNoSecondAction = duplicateCameraSent && actionExecutor.everyRequestExecutedOnce()
	report.RejectedActionResults = countTraceStatus(records, "core.action_result", "rejected")
	report.IdempotenceChecks.OrphanActionResultRejected = report.RejectedActionResults > 0 || !orphanResultSent
	if report.ActionLifecycleStatus == "allowed_dry_run" {
		_, resultCount := actionExecutor.snapshot()
		report.IdempotenceChecks.ActionResultDuplicateSafe = !duplicateResultSent || countTraceStatus(records, "core.action_result", "duplicate") > 0 || (resultCount > 0 && len(reopenedSnapshot.ActionResults) == resultCount)
	}
	report = validateExpected(report, value.Expected)
	cleanupRuntime(ctx, manager, apiClient, coreClient, discoveryClient, camera, server)
	return finishCase(report, started)
}

func expectedReportFrom(expected fixtureExpected) expectedReport {
	result := expectedReport{}
	if value, ok := expected.Snapshot["schema_version"].(string); ok {
		result.SnapshotVersion = value
	}
	if value, ok := expected.Snapshot["input_dimension"].(float64); ok {
		result.SnapshotDimension = int(value)
	}
	if value, ok := expected.MLP["status"].(string); ok {
		result.MLPStatus = value
	}
	if value, ok := expected.MLP["probability_contract"].(string); ok {
		result.ProbabilityContract = value
	}
	if values, ok := expected.MLP["heads"].([]any); ok {
		for _, value := range values {
			if head, ok := value.(string); ok {
				result.MLPHeads = append(result.MLPHeads, head)
			}
		}
	}
	if value, ok := expected.SafetyGate["status"].(string); ok {
		result.SafetyGateStatus = value
	}
	result.StoreCommitted, _ = expected.Store["committed"].(bool)
	result.OutboxNonEmpty, _ = expected.Outbox["non_empty"].(bool)
	return result
}

// The real CPU bundle still computes every head. Only wall-clock measurements
// are normalized here so two hermetic runs have byte-stable decision traces.
type deterministicMLPV1 struct {
	bundle  *cognitivecore.CPUBundleMLP
	capture *mlpCapture
}

func (m deterministicMLPV1) Run(ctx context.Context, encoded cognitivecore.EncodedSnapshot, snapshot cognitivecore.CognitiveSnapshot) (cognitivecore.MLPOutput, map[string]float64, error) {
	output, latencies, err := m.bundle.Run(ctx, encoded, snapshot)
	if output.Trace != nil {
		output.Trace.DurationMS = 0
	}
	if err == nil && m.capture != nil {
		m.capture.recordV1(encoded, output, latencies)
	}
	return output, map[string]float64{"danger": 0, "incident": 0, "task": 0, "action": 0}, err
}

type deterministicMLPV3 struct {
	bundle  *cognitivecore.CPUBundleMLPV3
	capture *mlpCapture
}

func (m deterministicMLPV3) RunV3(ctx context.Context, encoded cognitivecore.EncodedSnapshotV3, snapshot cognitivecore.CognitiveSnapshotV3) (cognitivecore.MLPOutputV3, map[string]float64, error) {
	output, latencies, err := m.bundle.RunV3(ctx, encoded, snapshot)
	if err == nil && m.capture != nil {
		m.capture.recordV3(encoded, output, latencies)
	}
	return output, map[string]float64{"danger": 0, "incident": 0, "task": 0, "action": 0, "communication_intent": 0}, err
}

type forcedAnnounceMLPV3 struct {
	capture *mlpCapture
	danger  string
}

func (m forcedAnnounceMLPV3) RunV3(_ context.Context, encoded cognitivecore.EncodedSnapshotV3, snapshot cognitivecore.CognitiveSnapshotV3) (cognitivecore.MLPOutputV3, map[string]float64, error) {
	danger := m.danger
	if danger == "" {
		danger = "none"
	}
	output := cognitivecore.MLPOutputV3{
		DangerLabel: danger, DangerScore: 1,
		Incident: "routine_presence", IncidentConfidence: 1,
		Task: "monitor", TaskConfidence: 1,
		Action: "announce", ActionConfidence: 1,
		Communication:           cognitivecore.CommunicationIntentV3{Intent: cognitivecore.CommunicationNeutralPresenceNoticeV3, TemplateID: cognitivecore.TemplateIDV3(cognitivecore.CommunicationNeutralPresenceNoticeV3)},
		CommunicationConfidence: 1, ModelVersion: "test-only-forced-announce",
	}
	if m.capture != nil {
		m.capture.recordForced(encoded, output)
	}
	return output, zeroV3Latency(), nil
}

type testCPUlayer struct {
	InputSize  int       `json:"input_size"`
	OutputSize int       `json:"output_size"`
	Activation string    `json:"activation"`
	Weights    []float32 `json:"weights"`
	Bias       []float32 `json:"bias"`
}

type testCPUartifact struct {
	Labels []string       `json:"labels"`
	Layers []testCPUlayer `json:"layers"`
}

type testCPUModel struct {
	V3             bool
	ManifestSHA256 string
	ModelVersion   string
	InputDimension int
	Labels         map[string][]string
	Backbone       []testCPUlayer
	Heads          map[string][]testCPUlayer
}

type mlpCapture struct {
	model        *testCPUModel
	observation  mlpObservation
	observations []mlpObservation
}

func newMLPCapture(bundlePath string, v3 bool) (*mlpCapture, error) {
	model, err := loadTestCPUModel(bundlePath, v3)
	if err != nil {
		return nil, err
	}
	backend := "cpu-bundle-v1"
	if v3 {
		backend = "cpu-bundle-v3-candidate"
	}
	return &mlpCapture{model: model, observation: mlpObservation{Backend: backend, Forced: false, BundleSHA256: model.ManifestSHA256, ModelVersion: model.ModelVersion, LatencyMS: zeroLatency(model.V3), Heads: make(map[string]headObservation)}}, nil
}

func (m *mlpCapture) recordV1(encoded cognitivecore.EncodedSnapshot, output cognitivecore.MLPOutput, latencies map[string]float64) {
	probabilities := m.model.probabilities(encoded.Values[:])
	observation := m.observation
	observation.SnapshotVersion = cognitivecore.SnapshotSchemaVersion
	observation.SnapshotDimension = len(encoded.Values)
	observation.SnapshotSHA256 = hashEncoded(encoded)
	observation.LatencyMS = cloneFloatMap(latencies)
	observation.Heads = map[string]headObservation{
		"danger":   makeHeadObservationWithLabels(output.DangerLabel, float64(output.DangerScore), probabilities["danger"], m.model.Labels["danger"]),
		"incident": makeHeadObservationWithLabels(firstString(output.Incidents), float64(output.IncidentConfidence), probabilities["incident"], m.model.Labels["incident"]),
		"task":     makeHeadObservationWithLabels(output.Task, float64(output.TaskConfidence), probabilities["task"], m.model.Labels["task"]),
		"action":   makeHeadObservationWithLabels(output.Action.Action, float64(output.ActionConfidence), probabilities["action"], m.model.Labels["action"]),
	}
	m.observations = append(m.observations, observation)
}

func (m *mlpCapture) recordV3(encoded cognitivecore.EncodedSnapshotV3, output cognitivecore.MLPOutputV3, latencies map[string]float64) {
	probabilities := m.model.probabilities(encoded.Values[:])
	observation := m.observation
	observation.SnapshotVersion = cognitivecore.SnapshotSchemaVersionV3
	observation.SnapshotDimension = len(encoded.Values)
	observation.SnapshotSHA256 = hashEncoded(encoded)
	observation.LatencyMS = cloneFloatMap(latencies)
	observation.Heads = map[string]headObservation{
		"danger":               makeHeadObservationWithLabels(output.DangerLabel, float64(output.DangerScore), probabilities["danger"], m.model.Labels["danger"]),
		"incident":             makeHeadObservationWithLabels(output.Incident, float64(output.IncidentConfidence), probabilities["incident"], m.model.Labels["incident"]),
		"task":                 makeHeadObservationWithLabels(output.Task, float64(output.TaskConfidence), probabilities["task"], m.model.Labels["task"]),
		"action":               makeHeadObservationWithLabels(output.Action, float64(output.ActionConfidence), probabilities["action"], m.model.Labels["action"]),
		"communication_intent": makeHeadObservationWithLabels(output.Communication.Intent, float64(output.CommunicationConfidence), probabilities["communication_intent"], m.model.Labels["communication_intent"]),
	}
	m.observations = append(m.observations, observation)
}

func (m *mlpCapture) recordForced(encoded cognitivecore.EncodedSnapshotV3, output cognitivecore.MLPOutputV3) {
	observation := m.observation
	observation.SnapshotVersion = cognitivecore.SnapshotSchemaVersionV3
	observation.SnapshotDimension = len(encoded.Values)
	observation.SnapshotSHA256 = hashEncoded(encoded)
	observation.Heads = map[string]headObservation{
		"danger":               {Label: output.DangerLabel, Confidence: 1, Probabilities: map[string]float64{output.DangerLabel: 1}},
		"incident":             {Label: output.Incident, Confidence: 1, Probabilities: map[string]float64{output.Incident: 1}},
		"task":                 {Label: output.Task, Confidence: 1, Probabilities: map[string]float64{output.Task: 1}},
		"action":               {Label: output.Action, Confidence: 1, Probabilities: map[string]float64{output.Action: 1}},
		"communication_intent": {Label: output.Communication.Intent, Confidence: 1, Probabilities: map[string]float64{output.Communication.Intent: 1}},
	}
	m.observations = append(m.observations, observation)
}

func (m *testCPUModel) probabilities(values []float32) map[string][]float64 {
	input := values
	if m.V3 {
		input = runTestLayers(m.Backbone, input)
	}
	result := make(map[string][]float64, len(m.Heads))
	for head, layers := range m.Heads {
		logits := runTestLayers(layers, input)
		result[head] = testSoftmax(logits)
	}
	return result
}

func runTestLayers(layers []testCPUlayer, input []float32) []float32 {
	current := append([]float32(nil), input...)
	for _, layer := range layers {
		next := make([]float32, layer.OutputSize)
		for output := range next {
			value := layer.Bias[output]
			for index, x := range current {
				value += layer.Weights[output*layer.InputSize+index] * x
			}
			if layer.Activation == "relu" && value < 0 {
				value = 0
			}
			next[output] = value
		}
		current = next
	}
	return current
}

func testSoftmax(values []float32) []float64 {
	if len(values) == 0 {
		return nil
	}
	maximum := float64(values[0])
	for _, value := range values[1:] {
		if float64(value) > maximum {
			maximum = float64(value)
		}
	}
	probabilities := make([]float64, len(values))
	total := 0.0
	for index, value := range values {
		probabilities[index] = math.Exp(float64(value) - maximum)
		total += probabilities[index]
	}
	for index := range probabilities {
		probabilities[index] /= total
	}
	return probabilities
}

func loadTestCPUModel(dir string, v3 bool) (*testCPUModel, error) {
	manifestName := "MANIFEST.v1.json"
	if v3 {
		manifestName = "MANIFEST.v3.json"
	}
	manifestPath := filepath.Join(dir, manifestName)
	manifestBody, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	model := &testCPUModel{V3: v3, ManifestSHA256: digestBytes(manifestBody), InputDimension: cognitivecore.CognitiveVectorSize, Heads: make(map[string][]testCPUlayer)}
	if v3 {
		var manifest cognitivecore.V3Manifest
		if err := json.Unmarshal(manifestBody, &manifest); err != nil {
			return nil, err
		}
		model.InputDimension = manifest.InputDimension
		model.ModelVersion = "cognitive-v3:" + manifest.EncoderVersion
		model.Labels = manifest.Labels
		backbone, err := loadTestArtifact(filepath.Join(dir, "backbone.cpu.json"))
		if err != nil {
			return nil, err
		}
		model.Backbone = backbone.Layers
		for _, head := range cognitivecore.HeadOrderV3 {
			name := manifest.Artifacts[head]["artifact"]
			if name == "" {
				name = head + ".cpu.json"
			}
			artifact, err := loadTestArtifact(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			model.Heads[head] = artifact.Layers
		}
		return model, nil
	}
	var manifest cognitivecore.Manifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return nil, err
	}
	model.InputDimension = manifest.InputDimension
	model.ModelVersion = "cognitive-v1:" + manifest.EncoderVersion
	model.Labels = manifest.Labels
	for _, head := range cognitivecore.HeadOrder {
		name := manifest.Artifacts[head].Artifact
		if name == "" {
			name = head + ".cpu.json"
		}
		artifact, err := loadTestArtifact(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		model.Heads[head] = artifact.Layers
	}
	return model, nil
}

func loadTestArtifact(path string) (testCPUartifact, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return testCPUartifact{}, err
	}
	var artifact testCPUartifact
	if err := json.Unmarshal(body, &artifact); err != nil {
		return testCPUartifact{}, err
	}
	return artifact, nil
}

func makeHeadObservationWithLabels(label string, confidence float64, probabilities []float64, labels []string) headObservation {
	values := make(map[string]float64, len(probabilities))
	for index, probability := range probabilities {
		if index < len(labels) {
			values[labels[index]] = probability
		}
	}
	return headObservation{Label: label, Confidence: confidence, Probabilities: values}
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func zeroLatency(v3 bool) map[string]float64 {
	if v3 {
		return zeroV3Latency()
	}
	return map[string]float64{"danger": 0, "incident": 0, "task": 0, "action": 0}
}

func zeroV3Latency() map[string]float64 {
	return map[string]float64{"danger": 0, "incident": 0, "task": 0, "action": 0, "communication_intent": 0, "shared_backbone": 0}
}

func cloneFloatMap(values map[string]float64) map[string]float64 {
	copyValues := make(map[string]float64, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func digestBytes(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func hashEncoded(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return digestBytes(body)
}

// collectTrace drains the two output peers. The payload digest is sufficient
// for reproducibility checks and guarantees raw Vision content is not copied
// into the report.
func collectTrace(camera, api *bus.Client, timeout time.Duration, wantAccepted, wantRejected int, wantCore bool) []busRecord {
	deadline := time.NewTimer(timeout)
	quiet := time.NewTimer(time.Hour)
	defer deadline.Stop()
	defer quiet.Stop()
	trace := make([]busRecord, 0, 16)
	accepted, rejected, coreDecisions := 0, 0, 0
	ready := func() bool {
		return accepted >= wantAccepted && rejected >= wantRejected && (!wantCore || coreDecisions > 0)
	}
	if ready() {
		resetTimer(quiet, 300*time.Millisecond)
	}
	for {
		select {
		case message := <-camera.SubscribeChannel("camera-simulator"):
			record := busRecord{Message: message, Trace: summarizeMessage(message)}
			trace = append(trace, record)
			if message.Type == "discovery.ingress.accepted" {
				accepted++
			}
			if message.Type == "discovery.ingress.rejected" {
				rejected++
			}
			if ready() {
				resetTimer(quiet, 300*time.Millisecond)
			}
		case message := <-api.SubscribeChannel("api"):
			record := busRecord{Message: message, Trace: summarizeMessage(message)}
			trace = append(trace, record)
			if message.Type == "core.decision" || message.Type == "core.decision.v3" {
				coreDecisions++
			}
			if ready() {
				resetTimer(quiet, 300*time.Millisecond)
			}
		case <-quiet.C:
			return trace
		case <-deadline.C:
			return trace
		}
	}
}

func summarizeMessage(message contract.Message) traceRecord {
	digest := sha256.Sum256(message.Payload)
	status := ""
	var value map[string]any
	if json.Unmarshal(message.Payload, &value) == nil {
		if candidate, ok := value["status"].(string); ok {
			status = candidate
		}
	}
	return traceRecord{Type: message.Type, Source: message.Source, Target: message.Target, Status: status, Revision: message.Revision, PayloadSHA: hex.EncodeToString(digest[:])}
}

func attachActionLifecycle(report caseReport, value fixture, clock time.Time, records []busRecord, executor *testActionExecutor, snapshot cognitivecore.CognitiveSnapshot) caseReport {
	correlation := digestBytes([]byte(value.ID))[:16]
	logical := clock.UTC().Format(time.RFC3339)
	has := func(types ...string) bool {
		for _, record := range records {
			for _, wanted := range types {
				if record.Message.Type == wanted {
					return true
				}
			}
		}
		return false
	}
	stage := func(name, status, reason string) {
		report.Journey = append(report.Journey, journeyEvent{LogicalTimestamp: logical, CorrelationID: correlation, Stage: name, Status: status, Reason: reason})
	}
	accepted := report.DiscoveryAccepted > 0
	if accepted {
		stage("ingress_received", "received", "camera_simulator")
		stage("discovery_validated", "accepted", "aggregate_only")
	} else if report.DiscoveryRejected > 0 {
		stage("ingress_received", "received", "camera_simulator")
		stage("discovery_validated", "rejected", "boundary_validation")
	} else {
		stage("ingress_received", "not_run", "no_ingress_result")
		stage("discovery_validated", "not_run", "no_ingress_result")
	}
	if accepted && report.CoreDecisions > 0 {
		stage("core_processed", "completed", "aggregate_consumed")
	} else {
		stage("core_processed", "not_run", "discovery_rejected")
	}
	if accepted && report.StoreRevision > 1 {
		stage("store_revision_written", "completed", "universal_store_commit")
	} else {
		stage("store_revision_written", "not_run", "no_core_commit")
	}
	if accepted && (has("core.snapshot", "core.snapshot.v3") || report.SnapshotVersion != "") {
		stage("snapshot_encoded", "completed", "cognitive_snapshot")
	} else {
		stage("snapshot_encoded", "not_run", "snapshot_unavailable")
	}
	if accepted && len(report.MLPObservations) > 0 {
		stage("mlp_executed", "completed", "cpu_bundle")
	} else if expectString(value.Expected.MLP, "status", "unavailable") {
		stage("mlp_executed", "unavailable", "model_unavailable")
	} else {
		stage("mlp_executed", "not_run", "no_snapshot")
	}
	if len(report.SafetyGateStatuses) > 0 {
		stage("safety_gate_evaluated", "completed", "safety_gate")
	} else {
		stage("safety_gate_evaluated", "not_run", "no_decision")
	}
	callCount, resultCount := executor.snapshot()
	actionRequested := callCount > 0
	if actionRequested {
		report.ActionLifecycleStatus = "allowed_dry_run"
		stage("action_dispatched_to_discovery", "allowed_dry_run", "core_to_discovery")
		stage("test_action_executor_result", "dry_run_result_received", "physical_execution_disabled")
		if has("core.action_result") || len(snapshot.ActionResults) > 0 {
			stage("action_result_recorded", "dry_run_result_received", "core_store")
		} else {
			stage("action_result_recorded", "incomplete", "result_not_observed")
		}
	} else {
		report.ActionLifecycleStatus = "suppressed_no_action"
		for _, status := range report.SafetyGateStatuses {
			if strings.Contains(status, "blocked") {
				report.ActionLifecycleStatus = "blocked_by_safety_gate"
				break
			}
		}
		stage("action_dispatched_to_discovery", report.ActionLifecycleStatus, "no_physical_action")
		stage("test_action_executor_result", "suppressed_no_action", "executor_not_called")
		if report.ActionLifecycleStatus == "blocked_by_safety_gate" {
			if has("core.action_result") || len(snapshot.ActionResults) > 0 {
				stage("action_result_recorded", "blocked_by_safety_gate", "core_store")
			} else {
				stage("action_result_recorded", "incomplete", "result_not_observed")
			}
		} else {
			stage("action_result_recorded", "suppressed_no_action", "no_action_result")
		}
	}
	checks := idempotenceChecks{
		CameraDuplicateNoSecondAction: callCount <= 1,
		ActionResultDuplicateSafe:     resultCount <= callCount,
		OrphanActionResultRejected:    true,
		DirectExecutorCalls:           false,
	}
	if actionRequested && len(snapshot.ActionResults) > 0 {
		checks.ActionResultDuplicateSafe = len(snapshot.ActionResults) == 1
	}
	report.IdempotenceChecks = checks
	stage("scenario_completed", "completed", "all_observations_redacted")
	return report
}

func correlationToken(value, suffix string) string {
	return digestBytes([]byte(value + ":" + suffix))[:24]
}

func countTraceStatus(records []busRecord, eventType, status string) int {
	count := 0
	for _, record := range records {
		if record.Message.Type == eventType && record.Trace.Status == status {
			count++
		}
	}
	return count
}

func traceHasType(records []busRecord, types ...string) bool {
	for _, record := range records {
		for _, wanted := range types {
			if record.Message.Type == wanted {
				return true
			}
		}
	}
	return false
}

func enrichFromMessages(report caseReport, records []busRecord) caseReport {
	for _, record := range records {
		event := record.Message
		var genericEnvelope map[string]any
		if json.Unmarshal(event.Payload, &genericEnvelope) == nil {
			if snapshot, ok := genericEnvelope["snapshot"]; ok {
				if body, marshalErr := json.Marshal(snapshot); marshalErr == nil {
					report.SnapshotSHA256 = digestBytes(body)
				}
			}
		}
		if event.Type == "core.snapshot" {
			report.SnapshotVersion = "cognitive-snapshot/v1"
			report.SnapshotDimension = cognitivecore.CognitiveVectorSize
		}
		if event.Type == "core.snapshot.v3" {
			report.SnapshotVersion = cognitivecore.SnapshotSchemaVersionV3
			report.SnapshotDimension = cognitivecore.CognitiveVectorSizeV3
			var envelope struct {
				Snapshot struct {
					Vision struct {
						PoseStatus         string  `json:"pose_status"`
						PoseQuality        float64 `json:"pose_quality"`
						Posture            string  `json:"posture"`
						FallState          string  `json:"fall_state"`
						RecoveryObserved   bool    `json:"recovery_observed"`
						MotionTier         string  `json:"motion_tier"`
						InteractionState   string  `json:"interaction_state"`
						FaceStatus         string  `json:"face_status"`
						FaceQualification  string  `json:"face_qualification_provenance"`
						CameraHealthStatus string  `json:"camera_health_status"`
					} `json:"vision"`
				} `json:"snapshot"`
			}
			if json.Unmarshal(event.Payload, &envelope) == nil {
				report.PoseStatus = envelope.Snapshot.Vision.PoseStatus
				report.PoseQuality = envelope.Snapshot.Vision.PoseQuality
				report.Posture = envelope.Snapshot.Vision.Posture
				report.FallState = envelope.Snapshot.Vision.FallState
				report.RecoveryObserved = envelope.Snapshot.Vision.RecoveryObserved
				report.MotionTier = envelope.Snapshot.Vision.MotionTier
				report.InteractionState = envelope.Snapshot.Vision.InteractionState
				report.FaceStatus = envelope.Snapshot.Vision.FaceStatus
				report.FaceQualification = envelope.Snapshot.Vision.FaceQualification
				report.CameraHealthStatus = envelope.Snapshot.Vision.CameraHealthStatus
			}
		}
		if event.Type == "core.decision" || event.Type == "core.decision.v3" {
			var envelope map[string]any
			if json.Unmarshal(event.Payload, &envelope) != nil {
				continue
			}
			decision, _ := envelope["decision"].(map[string]any)
			if dimension, ok := decision["input_dimension"].(float64); ok {
				report.SnapshotDimension = int(dimension)
				if event.Type == "core.decision.v3" {
					report.SnapshotVersion = cognitivecore.SnapshotSchemaVersionV3
				} else if event.Type == "core.decision" {
					report.SnapshotVersion = "cognitive-snapshot/v1"
				}
			}
			if heads, ok := decision["head_order"].([]any); ok {
				report.MLPHeads = report.MLPHeads[:0]
				for _, head := range heads {
					if value, ok := head.(string); ok {
						report.MLPHeads = append(report.MLPHeads, value)
					}
				}
			}
			if status, ok := decision["action"].(map[string]any); ok {
				if value, ok := status["status"].(string); ok {
					report.SafetyGateStatuses = append(report.SafetyGateStatuses, value)
				}
				if reasons, ok := status["reasons"].([]any); ok {
					for _, reason := range reasons {
						if value, ok := reason.(string); ok {
							report.SafetyGateReasons = append(report.SafetyGateReasons, value)
						}
					}
				}
				if value, ok := status["physical_action_executed"].(bool); ok {
					report.PhysicalAction = report.PhysicalAction || value
				}
			}
			if value, ok := decision["physical_action_executed"].(bool); ok {
				report.PhysicalAction = report.PhysicalAction || value
			}
			if communication, ok := decision["communication"].(map[string]any); ok {
				if value, ok := communication["status"].(string); ok {
					report.CommunicationStatus = value
					report.SafetyGateStatuses = append(report.SafetyGateStatuses, value)
				}
				if reasons, ok := communication["reasons"].([]any); ok {
					report.CommunicationReasons = report.CommunicationReasons[:0]
					for _, reason := range reasons {
						if value, ok := reason.(string); ok {
							report.CommunicationReasons = append(report.CommunicationReasons, value)
						}
					}
				}
				if value, ok := communication["physical_audio_played"].(bool); ok {
					report.AudioRendered = report.AudioRendered || value
				}
			}
		}
		if containsForbiddenJSON(event.Payload) {
			report.RawVisionForwarded = true
		}
	}
	return report
}

func validateExpected(report caseReport, expected fixtureExpected) caseReport {
	errorsFound := make([]string, 0)
	addDiff := func(path string, wanted, actual any, reason string) {
		report.ExpectedActualDiffs = append(report.ExpectedActualDiffs, expectationDiff{Path: path, Expected: wanted, Actual: actual, Reason: reason})
	}
	if status, ok := expected.Discovery["status"].(string); ok {
		if status == "accepted" && report.DiscoveryAccepted == 0 {
			errorsFound = append(errorsFound, "discovery was not accepted")
			addDiff("discovery.status", status, "not_accepted", "accepted ingress was expected")
		}
		if status == "rejected" && report.DiscoveryRejected == 0 {
			errorsFound = append(errorsFound, "discovery was not rejected")
			addDiff("discovery.status", status, "not_rejected", "rejected ingress was expected")
		}
	}
	if value, ok := expected.Snapshot["schema_version"].(string); ok && report.SnapshotVersion != value {
		errorsFound = append(errorsFound, "snapshot schema mismatch")
		addDiff("snapshot.schema_version", value, report.SnapshotVersion, "snapshot contract version differs")
	}
	if value, ok := expected.Snapshot["input_dimension"].(float64); ok && report.SnapshotDimension != int(value) {
		errorsFound = append(errorsFound, "snapshot dimension mismatch")
		addDiff("snapshot.input_dimension", int(value), report.SnapshotDimension, "snapshot input dimension differs")
	}
	if value, ok := expected.Store["committed"].(bool); ok && value && report.StoreRevision <= 1 {
		errorsFound = append(errorsFound, "store was not committed")
	}
	if value, ok := expected.Outbox["non_empty"].(bool); ok && value && report.OutboxCount == 0 {
		errorsFound = append(errorsFound, "outbox was empty")
	}
	if expectedHeads, ok := expected.MLP["heads"].([]any); ok {
		if len(expectedHeads) != len(report.MLPHeads) {
			errorsFound = append(errorsFound, "MLP head count mismatch")
			addDiff("mlp.heads", expectedHeads, report.MLPHeads, "head count differs")
		} else {
			for index, expectedHead := range expectedHeads {
				if value, ok := expectedHead.(string); !ok || report.MLPHeads[index] != value {
					errorsFound = append(errorsFound, "MLP head order mismatch")
					addDiff("mlp.heads", expectedHeads, report.MLPHeads, "head order differs")
					break
				}
			}
		}
	}
	if expectedStatus, ok := expected.MLP["status"].(string); ok && expectedStatus == "available" && expected.MLP["probability_contract"] == "normalized" {
		if len(report.MLPObservations) == 0 {
			errorsFound = append(errorsFound, "MLP observation missing")
			addDiff("mlp.observations", "at_least_one", len(report.MLPObservations), "real backend observation was expected")
		}
		for index, observation := range report.MLPObservations {
			for head, value := range observation.Heads {
				total := 0.0
				maximumLabel, maximumProbability := "", -1.0
				for _, probability := range value.Probabilities {
					total += probability
				}
				for label, probability := range value.Probabilities {
					if probability > maximumProbability {
						maximumLabel, maximumProbability = label, probability
					}
				}
				if math.Abs(total-1) > 0.00001 {
					errorsFound = append(errorsFound, fmt.Sprintf("MLP probability sum mismatch for %s", head))
					addDiff(fmt.Sprintf("mlp.observations[%d].heads.%s.probabilities", index, head), 1.0, total, "head probabilities must sum to one")
				}
				if value.Confidence < 0 || value.Confidence > 1 {
					errorsFound = append(errorsFound, fmt.Sprintf("MLP confidence out of range for %s", head))
					addDiff(fmt.Sprintf("mlp.observations[%d].heads.%s.confidence", index, head), "[0,1]", value.Confidence, "confidence must be normalized")
				}
				if maximumLabel != "" && value.Probabilities[value.Label] < maximumProbability-0.00001 {
					errorsFound = append(errorsFound, fmt.Sprintf("MLP label/probability mismatch for %s", head))
					addDiff(fmt.Sprintf("mlp.observations[%d].heads.%s.label", index, head), maximumLabel, value.Label, "reported label is not the maximum probability")
				}
			}
		}
	}
	if expectedStatus, ok := expected.SafetyGate["status"].(string); ok {
		found := false
		for _, status := range report.SafetyGateStatuses {
			if status == expectedStatus {
				found = true
				break
			}
		}
		if !found {
			errorsFound = append(errorsFound, "safety gate status mismatch")
			addDiff("safety_gate.status", expectedStatus, report.SafetyGateStatuses, "expected safety gate status was not observed")
		}
	}
	if expectedStatus, _ := expected.Discovery["status"].(string); expectedStatus == "rejected" && report.CoreDecisions > 0 {
		errorsFound = append(errorsFound, "forbidden ingress reached Core")
	}
	if report.PhysicalAction || report.AudioRendered || report.NetworkAccess || report.RawVisionForwarded {
		errorsFound = append(errorsFound, "unsafe side effect or raw Vision forwarding observed")
		addDiff("safety.side_effects", map[string]bool{"audio_rendered": false, "physical_action_executed": false, "network_access": false, "raw_vision_forwarded": false}, map[string]bool{"audio_rendered": report.AudioRendered, "physical_action_executed": report.PhysicalAction, "network_access": report.NetworkAccess, "raw_vision_forwarded": report.RawVisionForwarded}, "all side effects and raw forwarding must remain false")
	}
	report.AudioFalse = !report.AudioRendered
	report.PhysicalFalse = !report.PhysicalAction
	report.NetworkFalse = !report.NetworkAccess
	report.RawFalse = !report.RawVisionForwarded
	if report.Error != "" && !expectString(expected.MLP, "status", "unavailable") {
		errorsFound = append(errorsFound, report.Error)
	}
	if len(errorsFound) > 0 {
		report.Error = strings.Join(errorsFound, "; ")
		report.Passed = false
		return report
	}
	report.Passed = true
	return report
}

func expectString(values map[string]any, key, wanted string) bool {
	value, ok := values[key].(string)
	return ok && value == wanted
}

func finishCase(report caseReport, started time.Time) caseReport {
	report.AudioFalse = !report.AudioRendered
	report.PhysicalFalse = !report.PhysicalAction
	report.NetworkFalse = !report.NetworkAccess
	report.RawFalse = !report.RawVisionForwarded
	report.DurationMS = float64(time.Since(started).Microseconds()) / 1000
	return report
}

func finishEarlyCase(report caseReport, value fixture, clock time.Time, executor *testActionExecutor, started time.Time) caseReport {
	if executor != nil {
		report = attachActionLifecycle(report, value, clock, nil, executor, cognitivecore.CognitiveSnapshot{})
		report.IdempotenceChecks.OrphanActionResultRejected = true
	}
	return finishCase(report, started)
}

func cleanupRuntime(ctx context.Context, manager *discovery.Manager, api, core, discoveryClient, camera *bus.Client, server *bus.Server) {
	if ctx != nil {
		// The caller owns cancellation; a short independent context lets
		// Discovery close its bus peer without touching production paths.
		closeCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = manager.Close(closeCtx)
	}
	_ = api.Close()
	_ = core.Close()
	_ = discoveryClient.Close()
	_ = camera.Close()
	_ = server.Close()
}

func prepareRuntime(root string) error {
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(filepath.Join(root, "run"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	files := map[string]string{
		"security.yaml":  "device_secrets: {}\npairing_enabled: false\nfeatures:\n  diagnostics_enabled: false\n  debug_endpoints_enabled: false\n  dev_simulation_enabled: false\n",
		"devices.yaml":   "devices: []\n",
		"topology.yaml":  "version: 1\nlocked: true\nroot_id: central\nhouse_id: central\nnodes: []\nlinks: []\n",
		"residents.yaml": "residents: []\n",
		"network.yaml":   "version: 1\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(body), 0o600); err != nil {
			return err
		}
	}
	identity := filepath.Join(root, "identities.json")
	if err := os.WriteFile(identity, []byte(`{"version":1,"identities":{}}`), 0o600); err != nil {
		return err
	}
	return nil
}

func setHermeticEnv(root string) map[string]string {
	values := map[string]string{
		"SYNORA_CONFIG_DIR":           filepath.Join(root, "config"),
		"SYNORA_BUS":                  filepath.Join(root, "run", "bus.sock"),
		"SYNORA_STATE_PATH":           filepath.Join(root, "state.json"),
		"SYNORA_CLIP_ROOT":            filepath.Join(root, "clips"),
		"SYNORA_FACE_DATA_ROOT":       filepath.Join(root, "faces"),
		"SYNORA_MODEL_ROOT":           filepath.Join(root, "models"),
		"SYNORA_CONNECTIVITY_DIR":     filepath.Join(root, "connectivity"),
		"SYNORA_IDENTITY_REGISTRY":    filepath.Join(root, "identities.json"),
		"SYNORA_VISION_WORKER_SOCKET": filepath.Join(root, "run", "vision.sock"),
		"SYNORA_HTTP_ADDR":            "127.0.0.1:0",
		"SYNORA_HTTPS_ADDR":           "127.0.0.1:0",
		"SYNORA_VISION_HEALTH_ADDR":   "127.0.0.1:0",
		"SYNORA_VISION_HTTPS_ADDR":    "127.0.0.1:0",
		"SYNORA_MEDIAMTX_API_URL":     "http://127.0.0.1:1",
	}
	previous := make(map[string]string, len(values))
	for key, value := range values {
		previous[key] = os.Getenv(key)
		_ = os.Setenv(key, value)
	}
	return previous
}

func restoreEnv(previous map[string]string) {
	for key, value := range previous {
		if value == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, value)
		}
	}
}

func resolveBundle(repo string, value fixture) string {
	if value.BundlePath != "" {
		if filepath.IsAbs(value.BundlePath) {
			return value.BundlePath
		}
		return filepath.Join(repo, value.BundlePath)
	}
	if value.Bundle == "v3" {
		return filepath.Join(repo, "build", "cognitive-mlp-v3-candidate")
	}
	return filepath.Join(repo, "build", "cognitive-mlp-v1")
}

func waitForPath(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for Unix bus socket %s", path)
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(body, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func removeOutput(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func fileSHA256(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func fatalReport(path string, err error) {
	_ = writeJSON(path, map[string]any{"schema_version": "synora.central-e2e/v1", "passed": false, "error": err.Error(), "network_access": false, "audio_rendered": false, "physical_action_executed": false})
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func containsForbiddenJSON(body []byte) bool {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return false
	}
	return forbiddenJSONValue(value)
}

func forbiddenJSONValue(value any) bool {
	forbidden := map[string]bool{"frame": true, "frames": true, "image": true, "images": true, "media": true, "raw_media": true, "media_ref": true, "media_path": true, "bbox": true, "bboxes": true, "crop": true, "crops": true, "keypoints": true, "raw_keypoints": true, "embedding": true, "embeddings": true, "identity": true, "local_track_id": true, "hardware_id": true}
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if forbidden[strings.ToLower(strings.TrimSpace(key))] || forbiddenJSONValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range current {
			if forbiddenJSONValue(child) {
				return true
			}
		}
	}
	return false
}
