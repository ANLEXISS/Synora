package main

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
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
		msg.Source != "cam_01" ||
		msg.SourceType != contract.SourceDevice ||
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
