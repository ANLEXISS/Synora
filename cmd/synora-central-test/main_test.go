package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"synora/internal/cognitivecore"
	"synora/pkg/contract"
)

func TestCentralFixtureManifestHasRequiredMinimum(t *testing.T) {
	manifest, err := loadManifest("../../testdata/central-e2e-v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	available := len(manifest.Cases) + generatedScenarioCount(manifest.Generated)
	if manifest.MinimumCases < 80 || available < manifest.MinimumCases {
		t.Fatalf("fixture minimum is not met: %d/%d", available, manifest.MinimumCases)
	}
	if manifest.SchemaVersion != "synora.central-e2e-manifest/v1" || manifest.Seed == 0 || manifest.LogicalDate == "" {
		t.Fatalf("invalid manifest metadata: %+v", manifest)
	}
}

func TestFixtureVisionEvidenceAdapterProducesStrictV1(t *testing.T) {
	body, err := os.ReadFile("../../testdata/central-e2e-v1/cases/v3-pose-not-requested.json")
	if err != nil {
		t.Fatal(err)
	}
	var value fixture
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	if len(value.Messages) == 0 {
		t.Fatal("fixture has no message")
	}
	if containsForbiddenJSON(value.Messages[0].Payload) {
		t.Fatalf("fixture template unexpectedly contains forbidden data: %s", value.Messages[0].Payload)
	}
	encoded, err := attachFixtureVisionEvidenceV1(value.Messages[0].Payload, value.Messages[0].ID, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contract.DecodeVisionEvidenceV1(encoded); err != nil {
		t.Fatalf("adapter emitted invalid evidence: %v", err)
	}
	if containsForbiddenJSON(encoded) {
		t.Fatal("validated aggregate contract misclassified as raw Vision data")
	}
	var simulated map[string]any
	_ = json.Unmarshal(value.Messages[0].Payload, &simulated)
	simulated["provenance"] = "simulated_test_worker"
	simulated["simulated_camera"] = true
	simulated["vision_status"] = "unavailable"
	simulated["vision_evidence_source"] = "simulated_test_worker"
	simulated["inference_executed"] = false
	if snapshot, ok := simulated["snapshot"].(map[string]any); ok {
		snapshot["simulated_camera"] = true
		snapshot["vision_status"] = "unavailable"
		snapshot["vision_evidence_source"] = "simulated_test_worker"
		snapshot["inference_executed"] = false
		if vision, ok := snapshot["vision"].(map[string]any); ok {
			vision["pose_status"] = "unavailable"
			vision["pose_quality"] = 0
			vision["posture"] = "unknown"
			vision["fall_state"] = "unknown"
			vision["real_detection"] = false
			vision["replay_simulation"] = true
		}
		if base, ok := snapshot["base_v2"].(map[string]any); ok {
			if vision, ok := base["vision"].(map[string]any); ok {
				vision["pose_status"] = "unavailable"
				vision["pose_quality"] = 0
				vision["posture"] = "unknown"
				vision["fall_state"] = "unknown"
			}
		}
	}
	simulatedBody, _ := json.Marshal(simulated)
	if containsForbiddenJSON(simulatedBody) {
		t.Fatalf("mock worker marker projection misclassified as raw: %s", simulatedBody)
	}
	workerEncoded, err := attachFixtureVisionEvidenceV1(value.Messages[0].Payload, value.Messages[0].ID, time.Now().UTC(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contract.DecodeVisionEvidenceV1(workerEncoded); err != nil {
		t.Fatalf("simulated adapter emitted invalid evidence: %v", err)
	}
	if containsForbiddenJSON(workerEncoded) {
		t.Fatal("simulated aggregate contract misclassified as raw Vision data")
	}
}

func TestLe2iEvidenceUsesLogicalIngressTimestamp(t *testing.T) {
	value := generatedFixture(generatedSuite{IDPrefix: "le2i", Suite: "le2i_media", Family: "le2i_media", Count: 1, Seed: 997, Bundle: "v3"}, 0)
	logicalTime, err := time.Parse(time.RFC3339, value.Clock)
	if err != nil {
		t.Fatal(err)
	}
	value, err = le2iAggregateEvidenceFixture(value, poseAggregateResult{
		PoseStatus: "available", Posture: "upright", FallState: "none", RapidMotionState: "none",
		Confidence: 0.8, PoseFrameCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.Messages[0].Type != contract.EventVisionEnrichmentV3 {
		t.Fatalf("Le2i fixture should reach the Discovery ingress adapter as an external fixture, got %q", value.Messages[0].Type)
	}
	encoded, err := attachFixtureVisionEvidenceV1(value.Messages[0].Payload, value.Messages[0].ID, logicalTime, false)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := contract.DecodeVisionEvidenceV1(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.WindowEnd.Equal(logicalTime) || evidence.WindowStart.After(evidence.WindowEnd) || evidence.WindowSeconds != 1 {
		t.Fatalf("Evidence V1 timestamps do not match ingress logical time: start=%s end=%s ingress=%s", evidence.WindowStart, evidence.WindowEnd, logicalTime)
	}
}

func TestLe2iAggregateEvidenceTraversesCore(t *testing.T) {
	report := runLe2iAggregateThroughCore("../..", le2iCase{ID: "le2i-evidence-regression"}, poseAggregateResult{
		PoseStatus: "available", Posture: "upright", FallState: "none", RapidMotionState: "none",
		Confidence: 0.8, PoseFrameCount: 3,
	})
	if !report.Passed || report.DiscoveryAccepted < 1 || report.DiscoveryRejected != 0 || report.CoreDecisions != 1 || report.StoreRevision <= 1 || report.VisionEvidenceV1Status != "validated_and_stored" {
		t.Fatalf("Le2i Evidence V1 did not traverse Discovery/Core/Store: passed=%t error=%q accepted=%d rejected=%d core=%d store_revision=%d evidence=%s", report.Passed, report.Error, report.DiscoveryAccepted, report.DiscoveryRejected, report.CoreDecisions, report.StoreRevision, report.VisionEvidenceV1Status)
	}
}

func TestAllStaticLegacyVisionInputsBecomeEvidenceV1(t *testing.T) {
	manifest, err := loadManifest("../../testdata/central-e2e-v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range expandManifest(manifest, filepath.Clean(filepath.Join("..", ".."))) {
		for _, message := range scenario.Value.Messages {
			if message.Type != contract.EventVisionEnrichmentV3 && message.Type != contract.EventValidationTestInference {
				continue
			}
			encoded, conversionErr := attachFixtureVisionEvidenceV1(message.Payload, message.ID, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), false)
			if conversionErr != nil {
				t.Errorf("fixture %s: %v", scenario.Value.ID, conversionErr)
				continue
			}
			if _, err := contract.DecodeVisionEvidenceV1(encoded); err != nil {
				t.Errorf("fixture %s did not convert to Evidence V1: %v", scenario.Value.ID, err)
			}
		}
	}
}

func TestFixtureNonVisionSeedExcludesLegacyVisionObject(t *testing.T) {
	value, err := loadFixture("../../testdata/central-e2e-v1/cases/gate-announce-safe.json")
	if err != nil || len(value.Messages) == 0 {
		t.Fatalf("load fixture: %v", err)
	}
	seed := fixtureNonVisionV3State(value)
	if seed == nil || seed.BaseV2.Communication.TTSStatus != cognitivecore.TTSAvailable || !seed.BaseV2.Communication.AnnounceAvailable {
		t.Fatalf("non-Vision Core context was not preserved: %+v", seed)
	}
	if seed.VisionEvidence != nil || seed.Vision.PoseStatus != "" || seed.BaseV2.Vision.PoseStatus != "" {
		t.Fatalf("legacy Vision object leaked into the Core seed: %+v", seed)
	}
}

func TestCentralGeneratedSuitesAreFixedAndSeparated(t *testing.T) {
	manifest, err := loadManifest("../../testdata/central-e2e-v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, suite := range manifest.Generated {
		if suite.Bundle != "v3" || suite.Seed == 0 || suite.Count <= 0 {
			t.Fatalf("unexpected generated suite: %+v", suite)
		}
		if suite.Family == "" || suite.IDPrefix == "" || suite.Suite == "" {
			t.Fatalf("generated suite is not declarative: %+v", suite)
		}
	}
	first := generatedFixture(manifest.Generated[0], 0)
	second := generatedFixture(manifest.Generated[0], 0)
	left, _ := json.Marshal(first)
	right, _ := json.Marshal(second)
	if string(left) != string(right) {
		t.Fatal("generated fixture expansion is not deterministic")
	}
	if containsForbiddenJSON(first.Messages[0].Payload) {
		t.Fatal("generated fixture contains a forbidden raw Vision field")
	}
}

func TestCentralExpansionIsTheCLIExecutionSet(t *testing.T) {
	manifestPath := "../../testdata/central-e2e-v1/manifest.json"
	manifest, err := loadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join("..", ".."))
	all := expandManifest(manifest, root)
	wantTotal := len(manifest.Cases) + generatedScenarioCount(manifest.Generated)
	if len(all) != wantTotal {
		t.Fatalf("expanded scenarios=%d, manifest total=%d", len(all), wantTotal)
	}
	reports := make([]caseReport, 0, len(all))
	generatedByFamily := map[string]int{}
	for _, scenario := range all {
		reports = append(reports, caseReport{Suite: scenario.Value.Suite, Family: scenario.Family, Passed: true})
		if !scenario.Static {
			generatedByFamily[scenario.Family]++
		}
	}
	report := buildSuiteReport(manifestPath, manifest, all, reports, time.Now(), map[string]backendReport{})
	if report.CameraMockE2EStatus != "not_qualified" ||
		!strings.Contains(report.CameraMockE2EReason, "simulated") ||
		!strings.Contains(report.CameraMockE2EReason, "not Vision inference qualification") {
		t.Fatalf("camera mock software tests must not qualify the real central HTTP E2E: status=%q reason=%q", report.CameraMockE2EStatus, report.CameraMockE2EReason)
	}
	if report.ScenarioCount != len(all) || report.ScenarioCount != report.StaticCaseCount+report.GeneratedCaseCount {
		t.Fatalf("report count mismatch: %+v", report)
	}
	if report.VisionMigration.LegacyToCoreAttempts != 0 {
		t.Fatalf("legacy Vision contracts reached Core: %+v", report.VisionMigration)
	}
	for _, suite := range manifest.Generated {
		if generatedByFamily[suite.Family] != suite.Count || report.FamilyCounts[suite.Family] != suite.Count {
			t.Fatalf("declared family %q was not fully executed: expanded=%d report=%d declared=%d", suite.Family, generatedByFamily[suite.Family], report.FamilyCounts[suite.Family], suite.Count)
		}
	}
	caseFiltered := filterScenarios(all, "pose-movement-001", "")
	if len(caseFiltered) != 1 || caseFiltered[0].Family != "pose_movement" {
		t.Fatalf("CASE filter was not applied after expansion: %+v", caseFiltered)
	}
	v1Filtered := filterScenarios(all, "", "v1")
	for _, scenario := range v1Filtered {
		if !scenario.Static || scenario.Value.Bundle != "v1" {
			t.Fatalf("BUNDLE=v1 admitted a generated or non-v1 case: %+v", scenario)
		}
	}
	v3Filtered := filterScenarios(all, "", "v3")
	generatedV3 := 0
	for _, scenario := range v3Filtered {
		if !scenario.Static {
			generatedV3++
		}
	}
	if generatedV3 != generatedScenarioCount(manifest.Generated) {
		t.Fatalf("BUNDLE=v3 dropped generated scenarios: %d/%d", generatedV3, generatedScenarioCount(manifest.Generated))
	}
}

func TestCameraMockE2EIsTransportOnlyAndPreservesProvenance(t *testing.T) {
	report := runCameraMockE2E(filepath.Clean(filepath.Join("..", "..")))
	if report.CaseCount != 6 || report.PassedCount != 6 || report.FailedCount != 0 {
		t.Fatalf("mock E2E scenarios did not all pass: %+v", report)
	}
	if report.Qualification != "not_qualified" || report.VisionStatus != "unavailable" || report.PoseStatus != "unavailable" ||
		report.ModelLoads != 0 || report.InferenceExecutions != 0 || report.RawVisionForwarded || report.ExternalNetworkAccess ||
		report.AudioRendered || report.PhysicalActionExecuted {
		t.Fatalf("mock E2E crossed the simulated transport boundary: %+v", report)
	}
	if report.StateHTTPBefore != http.StatusServiceUnavailable || report.StateHTTPDuring != http.StatusOK || report.StateHTTPAfter != http.StatusOK ||
		!containsTrue(report.StateDuring, "simulated_camera") || !containsTrue(report.StateAfter, "simulated_camera") ||
		report.StateDuring["status"] != "pending_vision" || report.StateAfter["vision_status"] != "unavailable" ||
		report.StateAfter["vision_evidence_source"] != "simulated_test_worker" {
		t.Fatalf("system state did not report before/pending/final provenance: %+v", report)
	}
	accounting := suiteReport{CameraMockE2E: &report, MockCameraCaseCount: report.CaseCount}
	refreshPipelineAccounting(&accounting)
	if !accounting.PipelineAccountingValid || accounting.OverallCaseCount != report.CaseCount ||
		accounting.PipelineCompletedCount != report.CaseCount || accounting.PipelineIncompleteCount != 0 {
		t.Fatalf("mock journeys were not fully included in central accounting: %+v", accounting)
	}
	for _, item := range report.Cases {
		if !journeyComplete(item.Journey) || !item.MockCamera || !item.StoreSimulatedCamera || item.InferenceExecuted || item.ModelLoaded || item.PhysicalActionExecuted || item.AudioRendered || item.ExternalNetworkAccess {
			complete, reason, missing := assessJourney(item.Journey)
			t.Fatalf("mock case is incomplete or unsafe (journey_complete=%t reason=%s missing=%v marker=%t store_marker=%t inference=%t model=%t physical=%t audio=%t network=%t): %+v", complete, reason, missing, item.MockCamera, item.StoreSimulatedCamera, item.InferenceExecuted, item.ModelLoaded, item.PhysicalActionExecuted, item.AudioRendered, item.ExternalNetworkAccess, item)
		}
		for _, stage := range item.Journey {
			if !strings.Contains(stage.Reason, "simulated_camera=true") {
				t.Fatalf("simulation provenance missing at journey stage %s: %+v", stage.Stage, stage)
			}
		}
		if item.ActionStatus == "blocked_by_safety_gate" && item.ExecutorCalls != 0 {
			t.Fatalf("blocked Safety Gate case contacted executor: %+v", item)
		}
	}
}

func TestPipelineAccountingUsesEveryStaticAndLe2iJourney(t *testing.T) {
	manifestPath := "../../testdata/central-e2e-v1/manifest.json"
	manifest, err := loadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	static := expandManifest(manifest, filepath.Clean(filepath.Join("..", "..")))
	staticReports := make([]caseReport, len(static))
	for index, scenario := range static {
		staticReports[index] = caseReport{ID: scenario.Value.ID, Journey: completedJourney()}
	}
	mediaManifest, err := loadLe2iManifest("../../testdata/central-e2e-v1/media/le2i-v1-regression.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(mediaManifest.Cases) != 48 {
		t.Fatalf("Le2i regression manifest cases=%d want=48", len(mediaManifest.Cases))
	}
	mediaCases := make([]mediaCaseReport, len(mediaManifest.Cases))
	for index, scenario := range mediaManifest.Cases {
		mediaCases[index] = mediaCaseReport{ID: scenario.ID, Journey: completedJourney()}
	}
	report := suiteReport{
		ScenarioCount: len(staticReports), MediaCaseCount: len(mediaCases), Cases: staticReports,
		VisionMedia: &mediaSuiteReport{ScenarioCount: len(mediaCases), Cases: mediaCases},
	}
	refreshPipelineAccounting(&report)
	want := len(staticReports) + len(mediaCases)
	if report.OverallCaseCount != want || report.PipelineCompletedCount != want || report.PipelineIncompleteCount != 0 || !report.PipelineAccountingValid {
		t.Fatalf("pipeline counters do not match actual journeys: overall=%d completed=%d incomplete=%d valid=%t want=%d", report.OverallCaseCount, report.PipelineCompletedCount, report.PipelineIncompleteCount, report.PipelineAccountingValid, want)
	}
	if report.PipelineTerminalStatusCounts["completed"] != want {
		t.Fatalf("terminal status counts=%v want completed=%d", report.PipelineTerminalStatusCounts, want)
	}
	if report.OverallCaseCount != report.ScenarioCount+report.MediaCaseCount {
		t.Fatalf("overall count does not include media journeys: %+v", report)
	}

	// A selected count that disagrees with the actual case arrays must fail.
	report.MediaCaseCount++
	refreshPipelineAccounting(&report)
	if report.PipelineAccountingValid {
		t.Fatal("pipeline accounting accepted a mismatch between report totals and actual journeys")
	}
}

func TestMediaPipelineAccountingUsesActualJourneyCount(t *testing.T) {
	cases := make([]mediaCaseReport, 48)
	for index := range cases {
		cases[index] = mediaCaseReport{ID: "media-" + strconv.Itoa(index), Journey: completedJourney()}
	}
	report := mediaSuiteReport{Cases: cases}
	report.ScenarioCount = len(report.Cases)
	finalizeMediaPipelineAccounting(&report)
	if !report.PipelineAccountingValid || report.PipelineCompletedCount != 48 || report.PipelineIncompleteCount != 0 {
		t.Fatalf("media accounting does not reflect all journeys: valid=%t completed=%d incomplete=%d", report.PipelineAccountingValid, report.PipelineCompletedCount, report.PipelineIncompleteCount)
	}
}

func TestPipelineIncompleteDiagnosticsOnlyAppearForIncompleteJourney(t *testing.T) {
	complete := caseReport{Journey: completedJourney()}
	incomplete := caseReport{Journey: completedJourney()[:10]}
	report := suiteReport{ScenarioCount: 2, Cases: []caseReport{complete, incomplete}}
	refreshPipelineAccounting(&report)
	if report.PipelineCompletedCount != 1 || report.PipelineIncompleteCount != 1 || !report.PipelineAccountingValid {
		t.Fatalf("unexpected pipeline counts: %+v", report)
	}
	if report.Cases[0].PipelineComplete != nil || report.Cases[0].PipelineIncompleteReason != "" || report.Cases[0].MissingStages != nil || report.Cases[0].LastObservedStage != nil {
		t.Fatalf("complete case contains incomplete diagnostics: %+v", report.Cases[0])
	}
	bad := report.Cases[1]
	if bad.PipelineComplete == nil || *bad.PipelineComplete || bad.PipelineIncompleteReason == "" || bad.MissingStages == nil || len(*bad.MissingStages) != 1 || (*bad.MissingStages)[0] != "scenario_completed" || bad.LastObservedStage == nil || *bad.LastObservedStage != "action_result_recorded" {
		t.Fatalf("incomplete case diagnostics are missing or incorrect: %+v", bad)
	}
}

func TestPipelineAccountingFailsWhenAReportHasNoJourney(t *testing.T) {
	report := suiteReport{
		ScenarioCount: 2,
		Cases:         []caseReport{{Journey: completedJourney()}, {ID: "missing-journey"}},
	}
	refreshPipelineAccounting(&report)
	if report.PipelineCompletedCount != 1 || report.PipelineIncompleteCount != 0 || report.OverallCaseCount != 2 || report.PipelineAccountingValid {
		t.Fatalf("missing journey was silently counted as an executed pipeline: %+v", report)
	}
	missing := report.Cases[1]
	if missing.PipelineComplete == nil || *missing.PipelineComplete || missing.PipelineIncompleteReason == "" || missing.MissingStages == nil || missing.LastObservedStage == nil {
		t.Fatalf("missing journey has no incomplete diagnostics: %+v", missing)
	}
}

func TestSafetyGateBlockedJourneyCompletesWithoutExecutorCall(t *testing.T) {
	value := generatedFixture(generatedSuite{IDPrefix: "blocked", Suite: "safety", Family: "safety", Count: 1, Seed: 1, Bundle: "v3"}, 0)
	executor := newTestActionExecutor(func() time.Time { return time.Unix(100, 0).UTC() })
	report := caseReport{ID: value.ID, SafetyGateStatuses: []string{"blocked_by_safety_gate"}}
	report = attachActionLifecycle(report, value, time.Unix(100, 0).UTC(), []busRecord{{Message: contract.Message{Type: "core.action_result"}}}, executor, cognitivecore.CognitiveSnapshot{})
	calls, results := executor.snapshot()
	if calls != 0 || results != 0 {
		t.Fatalf("Safety Gate block called action executor: calls=%d results=%d", calls, results)
	}
	if report.ActionLifecycleStatus != "blocked_by_safety_gate" || !journeyComplete(report.Journey) {
		t.Fatalf("Safety Gate blocked journey should be complete: status=%s journey=%+v", report.ActionLifecycleStatus, report.Journey)
	}
}

func completedJourney() []journeyEvent {
	stages := []string{"ingress_received", "discovery_validated", "core_processed", "store_revision_written", "snapshot_encoded", "mlp_executed", "safety_gate_evaluated", "action_dispatched_to_discovery", "test_action_executor_result", "action_result_recorded", "scenario_completed"}
	statuses := []string{"received", "accepted", "completed", "completed", "completed", "completed", "completed", "suppressed_no_action", "suppressed_no_action", "suppressed_no_action", "completed"}
	journey := make([]journeyEvent, len(stages))
	for index, stage := range stages {
		journey[index] = journeyEvent{Stage: stage, Status: statuses[index]}
	}
	journey[8].Reason = "executor_not_called"
	journey[9].Reason = "no_action_result"
	return journey
}

func TestCentralReportWriteIsAtomicAndLeavesNoTemporaryFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "report.json")
	if err := writeJSON(path, map[string]any{"scenario_count": 1}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"scenario_count": 1`) {
		t.Fatalf("unexpected report body: %s", body)
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".report.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("atomic report temporary file was left behind: %v", matches)
	}
}

func TestVisionMediaManifestIsDeclarativeAndMissingMediaIsExplicit(t *testing.T) {
	manifestPath := "../../testdata/central-e2e-v1/vision-media-v1/manifest.json"
	manifest, err := loadVisionMediaManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 80 {
		t.Fatalf("unexpected vision media inventory size: %d", len(manifest.Entries))
	}
	wantFamilies := map[string]int{"fall": 10, "lie_down": 10, "sit_down": 10, "face_known": 10, "face_unknown": 10, "near_fall_or_bend": 10, "no_person_or_occluded": 10, "multi_person": 5, "delivery_failure": 5}
	gotFamilies := make(map[string]int)
	for _, entry := range manifest.Entries {
		gotFamilies[entry.Family]++
		if filepath.Dir(entry.RelativePath) != entry.Family {
			t.Fatalf("media entry is outside its family directory: %+v", entry)
		}
	}
	if len(gotFamilies) != len(wantFamilies) {
		t.Fatalf("unexpected media families: %+v", gotFamilies)
	}
	for family, count := range wantFamilies {
		if gotFamilies[family] != count {
			t.Fatalf("media family %s count=%d want=%d", family, gotFamilies[family], count)
		}
	}
	root := t.TempDir()
	report := runVisionMediaSuite(filepath.Clean(filepath.Join("..", "..")), manifestPath, root, "")
	if report.ScenarioCount != len(manifest.Entries) || report.StatusCounts[mediaStatusMediaMissing] != len(manifest.Entries) {
		t.Fatalf("missing media was not explicit: %+v", report)
	}
	if report.Passed || report.FailedCount != len(manifest.Entries) {
		t.Fatalf("missing media was reported as success: %+v", report)
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if containsForbiddenJSON(body) {
		t.Fatal("vision media report exposed a raw media or Vision field")
	}
}

func TestVisionMediaPathCannotEscapeRoot(t *testing.T) {
	if _, err := safeMediaPath(t.TempDir(), "../outside.mp4"); err == nil {
		t.Fatal("media path traversal was accepted")
	}
}

func TestLe2iManifestStrictContractAndMissingModelState(t *testing.T) {
	manifest, err := loadLe2iManifest("../../testdata/central-e2e-v1/media/le2i-v1-regression.json")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.CaseCount != 48 || len(manifest.Cases) != 48 || manifest.Backend != "yolov8n_pose_rknn/v1" {
		t.Fatalf("unexpected Le2i manifest: %+v", manifest)
	}
	if le2iModelStatus("", poseModelDiagnostic{Status: "unavailable"}) != mediaStatusModelMissing {
		t.Fatal("missing explicit model path was not classified as blocked_model_missing")
	}
	if le2iModelStatus("/tmp/does-not-exist-yolov8n-pose.rknn", poseModelDiagnostic{Status: "unavailable"}) != mediaStatusModelMissing {
		t.Fatal("missing model file was not classified as blocked_model_missing")
	}
}

func TestLe2iMissingMediaAndBadHashReturnStructuredCoreJourneys(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	entry := le2iCase{ID: "le2i-redacted-case", Category: "Fall", ClipRelativePath: "clip.mp4",
		ClipSHA256: strings.Repeat("0", 64), ClipCodec: "h264", ClipWidth: 320, ClipHeight: 240,
		ClipFPS: "25/1", ClipFrameCount: 16}
	mediaRoot := t.TempDir()
	missing := evaluateLe2iCase(repoRoot, entry, mediaRoot, poseModelDiagnostic{Status: "available"})
	if missing.Status != mediaStatusMediaMissing || !journeyComplete(missing.Journey) || missing.PoseStatus != "unavailable" {
		t.Fatalf("missing media did not return a structured fail-closed Core journey: %+v", missing)
	}
	clipPath := filepath.Join(mediaRoot, entry.ClipRelativePath)
	if err := os.WriteFile(clipPath, []byte("not-the-declared-clip"), 0600); err != nil {
		t.Fatal(err)
	}
	badHash := evaluateLe2iCase(repoRoot, entry, mediaRoot, poseModelDiagnostic{Status: "available"})
	if badHash.Status != mediaStatusIntegrity || !journeyComplete(badHash.Journey) || badHash.PoseRequestCount != 0 {
		t.Fatalf("invalid media hash did not return a structured, non-inference result: %+v", badHash)
	}
	encoded, err := json.Marshal([]mediaCaseReport{missing, badHash})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), mediaRoot) || containsForbiddenJSON(encoded) {
		t.Fatal("media failure report leaked a path or raw Vision field")
	}
}

func TestLe2iMediaUsesPrivateDiscoveryHTTPQueueBeforeWorker(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(source, []byte("bounded-test-video-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	storedPath, status, lifecycleEvents, cleanup, err := ingressLe2iClip(source, "le2i-test-clip")
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil || status != http.StatusAccepted || lifecycleEvents != 0 {
		t.Fatalf("Discovery ingress did not accept privately or leaked clip lifecycle: status=%d events=%d err=%v", status, lifecycleEvents, err)
	}
	if storedPath == "" || !strings.HasSuffix(storedPath, "le2i-test-clip.mp4") {
		t.Fatalf("worker queue did not receive the temporary stored media path: %q", storedPath)
	}
}

func TestLe2iNonFallCategoriesDoNotCountCandidateAsSemanticMatch(t *testing.T) {
	for _, category := range []string{"Blank", "Lie", "Likefall", "Stand"} {
		if le2iSemanticMatch(category, poseAggregateResult{PoseStatus: "available", Posture: "ambiguous", FallState: "candidate"}) {
			t.Errorf("%s candidate was counted as a semantic match", category)
		}
	}
}

func TestLe2iPoseWorkerTimeoutIsExplicit(t *testing.T) {
	root := t.TempDir()
	python := filepath.Join(root, "slow-python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\nsleep 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PYTHON", python)
	_, err := runPoseMediaCaseWithTimeout(filepath.Clean(filepath.Join("..", "..")), "redacted.mp4", "redacted.rknn", 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("pose worker timeout was not explicit: %v", err)
	}
}

func TestCentralSummaryRedactsPayload(t *testing.T) {
	payload := []byte(`{"media_ref":"must-not-appear","keypoints":[[1,2,0.9]],"status":"accepted"}`)
	digest := sha256.Sum256(payload)
	record := summarizeMessage(contract.Message{Type: "test", Source: "camera", Target: "core", Payload: payload})
	if record.Status != "accepted" {
		t.Fatalf("status was not retained: %+v", record)
	}
	if record.PayloadSHA != hex.EncodeToString(digest[:]) {
		t.Fatalf("payload digest mismatch: %+v", record)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if containsForbiddenJSON(encoded) {
		t.Fatal("redacted trace unexpectedly contains a forbidden Vision field")
	}
}

func TestCentralForbiddenPayloadDetection(t *testing.T) {
	for _, payload := range []string{
		`{"frame":"raw"}`,
		`{"bbox":[1,2,3,4]}`,
		`{"raw_keypoints":[[1,2,0.8]]}`,
		`{"embedding":[0.1,0.2]}`,
		`{"local_track_id":"track-1"}`,
	} {
		if !containsForbiddenJSON([]byte(payload)) {
			t.Errorf("forbidden payload was not detected: %s", payload)
		}
	}
}

func TestCentralJourneyContainsFullActionLifecycleForNoAction(t *testing.T) {
	value := generatedFixture(generatedSuite{IDPrefix: "journey", Suite: "pose_movement", Family: "pose_movement", Count: 1, Seed: 20260111, Bundle: "v3"}, 0)
	executor := newTestActionExecutor(func() time.Time { return time.Unix(100, 0).UTC() })
	report := caseReport{ID: value.ID, DiscoveryAccepted: 1, CoreDecisions: 1, StoreRevision: 2, SnapshotVersion: "cognitive-snapshot/v3", SnapshotDimension: 86, MLPObservations: []mlpObservation{{Backend: "test"}}, SafetyGateStatuses: []string{"not_requested"}}
	report = attachActionLifecycle(report, value, time.Unix(100, 0).UTC(), []busRecord{{Message: contract.Message{Type: "core.snapshot.v3"}}}, executor, cognitivecore.CognitiveSnapshot{})
	want := []string{"ingress_received", "discovery_validated", "core_processed", "store_revision_written", "snapshot_encoded", "mlp_executed", "safety_gate_evaluated", "action_dispatched_to_discovery", "test_action_executor_result", "action_result_recorded", "scenario_completed"}
	if len(report.Journey) != len(want) {
		t.Fatalf("journey stages=%d want=%d: %+v", len(report.Journey), len(want), report.Journey)
	}
	for index, stage := range want {
		if report.Journey[index].Stage != stage {
			t.Fatalf("journey[%d]=%s want=%s", index, report.Journey[index].Stage, stage)
		}
	}
	if report.ActionLifecycleStatus != "suppressed_no_action" || !report.IdempotenceChecks.OrphanActionResultRejected {
		t.Fatalf("unexpected no-action lifecycle: %+v", report)
	}
}

func TestMLPObservationUsesNamedProbabilitiesAndNoRawVision(t *testing.T) {
	model, err := loadTestCPUModel("../../build/cognitive-mlp-v3-candidate", true)
	if err != nil {
		t.Fatal(err)
	}
	encoded := make([]float32, cognitiveVectorSizeForTestV3)
	probabilities := model.probabilities(encoded)
	for head, values := range probabilities {
		total := 0.0
		for _, value := range values {
			total += value
		}
		if total < 0.99999 || total > 1.00001 {
			t.Fatalf("probability sum for %s is %v", head, total)
		}
	}
	observation, err := json.Marshal(mlpObservation{Backend: "cpu-bundle-v3-candidate", Heads: map[string]headObservation{"danger": makeHeadObservationWithLabels("none", 1, []float64{1, 0, 0, 0, 0}, model.Labels["danger"])}})
	if err != nil {
		t.Fatal(err)
	}
	if containsForbiddenJSON(observation) {
		t.Fatal("MLP observation contains a raw Vision field")
	}
}

const cognitiveVectorSizeForTestV3 = 86
