package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
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
		!strings.Contains(report.CameraMockE2EReason, "real HTTP Discovery ingress") ||
		!strings.Contains(report.CameraMockE2EReason, "GET /api/system/state") {
		t.Fatalf("camera mock software tests must not qualify the real central HTTP E2E: status=%q reason=%q", report.CameraMockE2EStatus, report.CameraMockE2EReason)
	}
	if report.ScenarioCount != len(all) || report.ScenarioCount != report.StaticCaseCount+report.GeneratedCaseCount {
		t.Fatalf("report count mismatch: %+v", report)
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
