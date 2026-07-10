package main

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synora/internal/simulation"
	"synora/pkg/contract"
)

func TestBuildPayloadVisionIdentity(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	payload := buildPayload(EventOptions{
		Type:       contract.EventVisionIdentity,
		DeviceID:   "cam_01",
		CameraID:   "cam_01",
		NodeID:     "maison.rdc.entree",
		Identity:   "alexis",
		Confidence: 0.92,
		Now:        now,
	})

	if payload["device_id"] != "cam_01" ||
		payload["camera_id"] != "cam_01" ||
		payload["node_id"] != "maison.rdc.entree" ||
		payload["identity"] != "alexis" ||
		payload["confidence"] != 0.92 ||
		payload["timestamp"] != now.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected identity payload: %#v", payload)
	}
	metadata := payload["metadata"].(map[string]any)
	if metadata["simulated"] != true {
		t.Fatalf("expected simulated metadata: %#v", metadata)
	}
}

func TestBuildPayloadVisionUnknown(t *testing.T) {
	payload := buildPayload(EventOptions{
		Type:     contract.EventVisionUnknown,
		DeviceID: "cam_01",
	})

	if payload["identity"] != "unknown" || payload["confidence"] != 0.70 {
		t.Fatalf("unexpected unknown payload: %#v", payload)
	}
}

func TestBuildMessageTargetsCore(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	msg, err := buildMessage(EventOptions{
		Type:     contract.EventVisionMotion,
		DeviceID: "cam_01",
		Now:      now,
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	if msg.Type != contract.EventVisionMotion ||
		msg.Kind != contract.KindEvent ||
		msg.Target != "core" ||
		msg.Source != "lab" ||
		msg.SourceType != contract.SourceSimulator ||
		msg.Priority != contract.EventPriority(contract.EventVisionMotion) {
		t.Fatalf("unexpected message: %#v", msg)
	}
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if payload["device_id"] != "cam_01" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestParseConfigVerbose(t *testing.T) {
	cfg, err := parseConfig([]string{"--send", "vision.unknown", "--verbose"})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if !cfg.Verbose {
		t.Fatalf("verbose flag should be enabled: %#v", cfg)
	}
}

func TestParseConfigLearningInspectionFlags(t *testing.T) {
	cfg, err := parseConfig([]string{"--scenario", "unknown_at_entrance", "--repeat", "5", "--inspect-learning", "--expect-sequence", "unknown_at_entrance", "--learning-mode", "simulation"})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.Repeat != 5 || !cfg.InspectLearning || cfg.ExpectSequence != "unknown_at_entrance" || cfg.LearningMode != "simulation" {
		t.Fatalf("unexpected learning config: %#v", cfg)
	}
}

func TestParseConfigDangerFlags(t *testing.T) {
	cfg, err := parseConfig([]string{"--scenario", "unknown_at_entrance", "--show-danger", "--expect-danger-level", "3", "--expect-category", "security", "--expect-system-action", "create_validation"})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if !cfg.ShowDanger || cfg.ExpectDangerLevel != 3 || cfg.ExpectCategory != "security" || cfg.ExpectSystemAction != "create_validation" {
		t.Fatalf("unexpected danger config: %#v", cfg)
	}
}

func TestRenderCGEAndExpectSequence(t *testing.T) {
	snapshot := &contract.PublicSnapshot{
		CGE: map[string]any{
			"sequences": []any{map[string]any{
				"signature":       "vision.unknown > vision.motion > vision.unknown",
				"count":           float64(5),
				"confidence":      float64(0.71),
				"simulated_count": float64(5),
				"real_count":      float64(0),
				"evidence":        []any{"matched vision.unknown > vision.motion > vision.unknown"},
			}},
			"transitions": []any{map[string]any{
				"from_event_type": "vision.unknown",
				"to_event_type":   "vision.motion",
				"count":           float64(5),
				"confidence":      float64(0.71),
				"simulated_count": float64(5),
				"real_count":      float64(0),
			}},
			"learned_behaviors": []any{map[string]any{
				"trigger_sequence_signature": "vision.unknown > vision.motion > vision.unknown",
				"status":                     "observing",
				"count":                      float64(5),
				"confidence":                 float64(0.71),
				"simulated_count":            float64(5),
				"real_count":                 float64(0),
				"requires_validation":        true,
			}},
		},
	}
	text := renderCGE(snapshot)
	for _, want := range []string{"vision.unknown > vision.motion > vision.unknown", "count: 5", "simulated_count: 5", "status: learned_in_simulation", "status: observing"} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered CGE missing %q in:\n%s", want, text)
		}
	}
	if err := expectScenarioSequence(snapshot, "unknown_at_entrance"); err != nil {
		t.Fatalf("expected sequence should be found: %v", err)
	}
}

func TestRenderDangerAndExpectations(t *testing.T) {
	snapshot := &contract.PublicSnapshot{
		CGE: map[string]any{
			"danger_assessments": []any{map[string]any{
				"id":                  "danger-1",
				"level":               float64(3),
				"score":               float64(0.62),
				"category":            "security",
				"explanation":         "An unknown subject was detected near an access-control area.",
				"validation_required": true,
				"recommended_system_actions": []any{map[string]any{
					"type": "create_validation",
				}, map[string]any{
					"type": "store_evidence",
				}},
			}},
		},
	}

	text := renderDanger(snapshot)
	for _, want := range []string{"level: 3", "score: 0.62", "category: security", "create_validation"} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered danger missing %q in:\n%s", want, text)
		}
	}
	cfg := Config{ExpectDangerLevel: 3, ExpectCategory: "security", ExpectSystemAction: "create_validation"}
	if err := expectDanger(snapshot, cfg); err != nil {
		t.Fatalf("expected danger should pass: %v", err)
	}
}

func TestVerboseMessageTextIncludesTransportAndMetadata(t *testing.T) {
	msg, err := buildMessage(EventOptions{
		Type:       contract.EventVisionUnknown,
		Source:     defaultBusClient,
		SourceType: contract.SourceSimulator,
		DeviceID:   "cam_01",
		StepID:     "unknown_first",
		Run:        &simulation.SimulationRun{ID: "run-1", CreatedBy: simulation.GeneratedBySynoraLab},
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	text := verboseMessageText(msg)
	for _, want := range []string{`"source": "lab"`, `"source_type": "simulator"`, `"target": "core"`, `"device_id": "cam_01"`, `"scenario_step_id": "unknown_first"`, `"event_instance_id": "run-1:unknown_first"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("verbose message missing %s in %s", want, text)
		}
	}
}

func TestObserveInSnapshotFindsScenarioStepEvent(t *testing.T) {
	before := contract.PublicSnapshot{Metrics: map[string]any{"event_processed": float64(0)}}
	after := contract.PublicSnapshot{
		Events: []map[string]any{{
			"id":   "evt-1",
			"type": contract.EventVisionUnknown,
			"payload": map[string]any{
				"metadata": map[string]any{
					"test_run_id":       "run-1",
					"scenario_step_id":  "unknown_first",
					"event_instance_id": "run-1:unknown_first",
				},
			},
		}},
		Metrics: map[string]any{"event_processed": float64(1)},
	}
	msg, err := buildMessage(EventOptions{
		Type:       contract.EventVisionUnknown,
		Source:     defaultBusClient,
		SourceType: contract.SourceSimulator,
		DeviceID:   "cam_01",
		StepID:     "unknown_first",
		Run:        &simulation.SimulationRun{ID: "run-1", CreatedBy: simulation.GeneratedBySynoraLab},
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	responses := []*contract.PublicSnapshot{&before, &after}
	client := SnapshotClient{
		URL: "http://synora.test/api/state",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			next := responses[0]
			if len(responses) > 1 {
				responses = responses[1:]
			}
			body, _ := json.Marshal(next)
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     make(http.Header),
			}, nil
		})},
	}
	initial, err := client.Fetch()
	if err != nil {
		t.Fatalf("fetch initial: %v", err)
	}
	obs := observeInSnapshot(client, initial, msg, time.Second)
	if !obs.Observed {
		t.Fatalf("expected observed snapshot, got %#v", obs)
	}
	if obs.Reason != "event_instance_id" {
		t.Fatalf("expected event_instance_id observation, got %#v", obs)
	}
	if observationText(obs) != "observed in snapshot observed_by=event_instance_id" {
		t.Fatalf("unexpected observation text: %s", observationText(obs))
	}
}

func TestSnapshotFetchUnauthorizedMessage(t *testing.T) {
	client := SnapshotClient{
		URL: "http://synora.test/api/state",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Status:     "401 Unauthorized",
				Body:       io.NopCloser(strings.NewReader("unauthorized")),
				Header:     make(http.Header),
			}, nil
		})},
	}
	_, err := client.Fetch()
	if err == nil || !strings.Contains(err.Error(), "API unauthorized: set SYNORA_API_TOKEN or pass --token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestScenariosResidentEntersHome(t *testing.T) {
	scenario := scenarios()["resident_enters_home"]
	if len(scenario.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %#v", scenario.Steps)
	}
	want := []string{contract.EventVisionIdentity, contract.EventVisionMotion, contract.EventVisionIdentity}
	for i, step := range scenario.Steps {
		if step.EventType != want[i] {
			t.Fatalf("step %d event mismatch got %s want %s", i, step.EventType, want[i])
		}
	}
}

func TestScenariosUnknownAtEntrance(t *testing.T) {
	scenario := scenarios()["unknown_at_entrance"]
	if len(scenario.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %#v", scenario.Steps)
	}
	for _, step := range []int{0, 2} {
		if scenario.Steps[step].EventType != contract.EventVisionUnknown {
			t.Fatalf("expected unknown at step %d: %#v", step, scenario.Steps)
		}
	}
	if scenario.Steps[1].EventType != contract.EventVisionMotion {
		t.Fatalf("expected motion in middle: %#v", scenario.Steps)
	}
}

func TestLabScenariosUseInternalSimulationRegistry(t *testing.T) {
	labScenario := scenarios()["unknown_at_entrance"]
	registryScenario, ok := simulation.ScenarioByID("unknown_at_entrance")
	if !ok {
		t.Fatal("internal simulation scenario missing")
	}
	if labScenario.ID != registryScenario.ID || labScenario.Name != registryScenario.Name || len(labScenario.Steps) != len(registryScenario.Steps) {
		t.Fatalf("lab scenario should come from internal/simulation: lab=%#v registry=%#v", labScenario, registryScenario)
	}
}

func TestParseConfigMinimal(t *testing.T) {
	cfg, err := parseConfig([]string{"--send", "vision.unknown", "--device", "cam_01", "--node", "maison.rdc.entree", "--api", "http://127.0.0.1:8080"})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.SendType != "vision.unknown" ||
		cfg.DeviceID != "cam_01" ||
		cfg.NodeID != "maison.rdc.entree" ||
		cfg.APIURL != "http://127.0.0.1:8080/api/state" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestParseConfigIdentityShortcut(t *testing.T) {
	cfg, err := parseConfig([]string{"--identity", "alexis", "--device", "cam_01"})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.SendType != contract.EventVisionIdentity {
		t.Fatalf("identity shortcut should send vision.identity: %#v", cfg)
	}
}

func TestListScenariosText(t *testing.T) {
	text := listScenariosText()
	if !strings.Contains(text, "unknown_at_entrance") || !strings.Contains(text, "fall_detected") {
		t.Fatalf("list scenarios output missing expected scenarios:\n%s", text)
	}
}

func TestSynoraLabDoesNotImportRuntimeDomains(t *testing.T) {
	forbiddenImports := []string{
		"synora/internal/actions",
		"synora/internal/engine",
		"synora/internal/engine/cognitive",
		"synora/internal/engine/graph",
		"synora/internal/engine/situation",
	}
	assertNoForbiddenImports(t, ".", forbiddenImports)
}

func assertNoForbiddenImports(t *testing.T, root string, forbiddenImports []string) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			for _, forbidden := range forbiddenImports {
				if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
					t.Errorf("%s imports forbidden domain %q", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
