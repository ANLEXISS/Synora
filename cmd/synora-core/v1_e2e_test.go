package main

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"synora/internal/cognitivecore"
	"synora/internal/discovery"
	"synora/pkg/contract"
)

type e2eBus struct{ sent []contract.Message }

func (b *e2eBus) Send(message contract.Message) error             { b.sent = append(b.sent, message); return nil }
func (b *e2eBus) SubscribeChannel(string) <-chan contract.Message { return make(chan contract.Message) }

func TestV1CoreEndToEndScenarios(t *testing.T) {
	store := cognitivecore.NewUniversalStore()
	core := &cognitivecore.Core{Store: store, MLP: cognitivecore.UnavailableMLP{}, Gate: cognitivecore.SafetyGate{DryRun: true}, Now: func() time.Time { return time.Unix(100, 0).UTC() }}
	boundary := &discovery.Boundary{DryRun: true, Capabilities: map[string]bool{"lock": true}}
	scenarios := []struct {
		name, typ string
		payload   map[string]any
	}{
		{"ordinary_sensor", "sensor.normal", nil},
		{"low_anomaly", "sensor.anomaly", map[string]any{"confidence": 0.2}},
		{"perimeter_intrusion", contract.EventVisionSegmentReadyV1, map[string]any{"topology": contract.VisionTopologyPrivatePerimeter, "human_present": true, "track_count": 1, "priority": contract.VisionPriorityP2}},
		{"threshold_human", contract.EventVisionSegmentReadyV1, map[string]any{"topology": contract.VisionTopologyRestrictedThreshold, "human_present": true, "track_count": 1, "priority": contract.VisionPriorityP1}},
		{"protected_interior_human", contract.EventVisionSegmentReadyV1, map[string]any{"topology": contract.VisionTopologyProtectedInterior, "human_present": true, "track_count": 1, "track_confirmed": true, "priority": contract.VisionPriorityP1}},
		{"vision_segment_2", contract.EventVisionSegmentReadyV1, map[string]any{"topology": contract.VisionTopologyProtectedInterior, "human_present": true, "track_count": 1, "segment_count": 2, "episode_phase": "confirmed", "priority": contract.VisionPriorityP1}},
		{"calm_decay", "vision.episode.end", map[string]any{"topology": contract.VisionTopologyProtectedInterior, "calm_seconds": 10, "is_final": true, "episode_phase": "final"}},
		{"action_allowed_dry_run", "web.command", map[string]any{"command": "review"}},
		{"blocked_safety", "device.unavailable", map[string]any{"status": "unavailable"}},
		{"duplicate_message", "sensor.normal", nil},
		{"degraded_healthcheck", contract.EventDiscoveryRuntimeStatus, map[string]any{"degraded": true}},
	}
	for i, scenario := range scenarios {
		eventID := "scenario-" + scenario.name
		if scenario.name == "duplicate_message" {
			eventID = "scenario-ordinary_sensor"
		}
		result, err := core.Process(context.Background(), contract.Event{ID: eventID, Type: scenario.typ, Source: "discovery", Timestamp: time.Unix(int64(100+i), 0).UTC(), Payload: scenario.payload})
		if err != nil {
			t.Fatalf("%s: %v", scenario.name, err)
		}
		if scenario.name == "duplicate_message" && !result.Result.Duplicate {
			t.Fatalf("duplicate was processed")
		}
		if result.Commit.Decision.Mode != "active_dry_run" && result.Commit.Decision.Mode != "active" {
			t.Fatalf("%s: unsafe decision mode %#v", scenario.name, result.Commit.Decision)
		}
	}
	if len(store.Journal()) != len(scenarios)-1 {
		t.Fatalf("unexpected journal length=%d", len(store.Journal()))
	}
	if err := store.ValidateBounds(); err != nil {
		t.Fatal(err)
	}
	resultEvent, err := boundary.ExecuteAction(cognitivecoreAction("lock"))
	if err != nil {
		t.Fatal(err)
	}
	if resultEvent.Payload["result"] == nil {
		t.Fatalf("missing new action result event: %#v", resultEvent)
	}
	if strings.Contains(string(mustMarshal(resultEvent)), `"physical_action_executed":true`) {
		t.Fatal("physical action executed")
	}
}

func TestV1ArchitectureHasNoRetiredDecisionImports(t *testing.T) {
	for _, root := range []string{".", "../../internal/cognitivecore"} {
		fset := token.NewFileSet()
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imported := range file.Imports {
				value := strings.Trim(imported.Path.Value, `"`)
				for _, forbidden := range []string{strings.Join([]string{"synora/internal/c", "ge"}, ""), strings.Join([]string{"synora/internal/eng", "ine"}, ""), strings.Join([]string{"teac", "her"}, ""), strings.Join([]string{"sha", "dow"}, "")} {
					if value == forbidden || strings.HasPrefix(value, forbidden+"/") {
						t.Errorf("%s imports forbidden runtime domain %q", path, value)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestV1ActiveRuntimeHasNoLegacyDecisionRuntime(t *testing.T) {
	for _, root := range []string{"../../internal/cognitivecore", "../../internal/discovery", "."} {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(contents)
			for _, forbidden := range []string{strings.Join([]string{"advisory_", "shadow"}, ""), strings.Join([]string{"state-encoder/", "v4"}, ""), strings.Join([]string{"V4", "StateEncoder"}, ""), strings.Join([]string{"Encoder", "V4"}, ""), strings.Join([]string{"teac", "her"}, ""), strings.Join([]string{"shadow", "_mode"}, "")} {
				if strings.Contains(text, forbidden) {
					t.Errorf("%s contains forbidden active-runtime marker %q", path, forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestV1FinalArchitectureHasNoRetiredRuntimeArtifacts(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	forbidden := []string{
		"C" + "GE",
		"internal/" + "engine",
		"state-encoder/" + "v4",
		"V" + "4StateEncoder",
		"teac" + "her",
		"sha" + "dow",
		"advi" + "sory",
		"cmd/synora-" + "api",
		"core" + "client",
		"rpc " + "legacy",
	}
	allowedExtensions := map[string]bool{".go": true, ".py": true, ".sh": true, ".yaml": true, ".yml": true, ".json": true, ".env": true, ".tsx": true, ".ts": true}
	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "build" || base == "node_modules" || base == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"docs"+string(filepath.Separator)) || strings.HasSuffix(path, "_test.go") || !allowedExtensions[filepath.Ext(path)] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		for _, marker := range forbidden {
			if strings.Contains(text, marker) {
				matches = append(matches, path+" contains "+marker)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 0 {
		t.Fatalf("retired architecture markers remain: %s", strings.Join(matches, "; "))
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "synora-api")); !os.IsNotExist(err) {
		t.Fatalf("separate API server path exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "cge")); !os.IsNotExist(err) {
		t.Fatalf("retired cognitive package path exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "engine")); !os.IsNotExist(err) {
		t.Fatalf("retired decision engine path exists: %v", err)
	}
}

func TestV1ActiveSurfaceContainsOnlyCanonicalRuntime(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	markers := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bcge\b`),
		regexp.MustCompile(`(?i)\bengine\b`),
		regexp.MustCompile(`(?i)\bv4\b`),
		regexp.MustCompile(`(?i)\bteacher\b`),
		regexp.MustCompile(`(?i)\bshadow\b`),
		regexp.MustCompile(`(?i)\badvisory\b`),
		regexp.MustCompile(`(?i)synora-api`),
		regexp.MustCompile(`(?i)\bwebapp\b`),
		regexp.MustCompile(`(?i)\binspector\b`),
		regexp.MustCompile(`(?i)\blab\b`),
		regexp.MustCompile(`(?i)\bsimulator\b`),
		regexp.MustCompile(`(?i)\blegacy\b`),
		regexp.MustCompile(`(?i)\bcompat`),
	}
	roots := []string{"cmd", "internal", "pkg", "services", "configs", "deployments"}
	var matches []string
	for _, relative := range roots {
		path := filepath.Join(root, relative)
		err := filepath.Walk(path, func(filePath string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				if info.Name() == "__pycache__" || info.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			base := filepath.Base(filePath)
			if strings.HasSuffix(base, "_test.go") || strings.HasPrefix(base, "test_") || strings.Contains(filePath, string(filepath.Separator)+"tests"+string(filepath.Separator)) {
				return nil
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			for _, marker := range markers {
				if marker.Match(data) {
					matches = append(matches, filePath+" contains "+marker.String())
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range markers {
		if marker.Match(makefile) {
			matches = append(matches, "Makefile contains "+marker.String())
		}
	}
	if len(matches) > 0 {
		t.Fatalf("forbidden active runtime surface: %s", strings.Join(matches, "; "))
	}
}

func TestV1DiscoveryCoreStoreDiscoveryActionResultLoop(t *testing.T) {
	bus := &e2eBus{}
	store := cognitivecore.NewUniversalStore()
	core := &cognitivecore.Core{Store: store, MLP: testMLP{output: cognitivecore.MLPOutput{DangerLabel: "high", DangerScore: 0.9, Action: cognitivecore.ActionIntent{Action: "notify", Capability: "notify"}}}, Gate: cognitivecore.SafetyGate{DryRun: true}, Now: func() time.Time { return time.Unix(200, 0).UTC() }}
	service := &cognitivecore.Service{Bus: bus, Core: core}
	if err := service.Handle(context.Background(), contract.Message{ID: "loop-1", Type: "sensor.anomaly", Kind: contract.KindEvent, Source: "discovery", Timestamp: time.Unix(200, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if len(bus.sent) != 3 || bus.sent[2].Type != "action.request" {
		t.Fatalf("Core did not emit abstract action request: %#v", bus.sent)
	}
	var request discovery.ActionRequest
	if err := json.Unmarshal(bus.sent[2].Payload, &request); err != nil {
		t.Fatal(err)
	}
	resultEvent, err := (&discovery.Boundary{DryRun: true, Capabilities: map[string]bool{"notify": true}}).ExecuteAction(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Process(context.Background(), resultEvent); err != nil {
		t.Fatal(err)
	}
	if len(store.Journal()) != 2 || store.Journal()[1].Event.Type != discovery.ActionResultEvent {
		t.Fatalf("action result did not become a new Core fact: %#v", store.Journal())
	}
	if strings.Contains(string(mustMarshal(resultEvent)), `"physical_action_executed":true`) {
		t.Fatal("physical action executed")
	}
}

func TestV1LoadedBundleRunsAllHeadsInActiveDryRun(t *testing.T) {
	bundle := os.Getenv("SYNORA_COGNITIVE_BUNDLE")
	if bundle == "" {
		t.Skip("set SYNORA_COGNITIVE_BUNDLE to execute the promoted bundle E2E")
	}
	mlp, err := cognitivecore.LoadCPUBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	store := cognitivecore.NewUniversalStore()
	core := &cognitivecore.Core{Store: store, MLP: mlp, Gate: cognitivecore.SafetyGate{DryRun: true}, Now: func() time.Time { return time.Unix(300, 0).UTC() }}
	result, err := core.Process(context.Background(), contract.Event{ID: "loaded-bundle", Type: contract.EventVisionSegmentReadyV1, Source: "discovery", Timestamp: time.Unix(300, 0).UTC(), Payload: map[string]any{"topology": contract.VisionTopologyProtectedInterior, "human_present": true, "track_count": 1, "track_confirmed": true, "priority": contract.VisionPriorityP1, "real_detection": true, "observation_count": 1, "confidence": .9}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Commit.Decision.Status != "available" || result.Commit.Decision.Mode != "active_dry_run" || result.Commit.Decision.Action.PhysicalActionExecuted {
		t.Fatalf("loaded bundle did not stay in active dry-run: %#v", result.Commit.Decision)
	}
	for _, head := range cognitivecore.HeadOrder {
		if _, ok := result.Commit.Decision.HeadLatencyMS[head]; !ok {
			t.Fatalf("missing latency for head %s: %#v", head, result.Commit.Decision.HeadLatencyMS)
		}
	}
}

func TestV1StoreSaturationRemainsBounded(t *testing.T) {
	store := cognitivecore.NewUniversalStore()
	core := &cognitivecore.Core{Store: store, MLP: testMLP{output: cognitivecore.MLPOutput{Action: cognitivecore.ActionIntent{Action: "notify"}}}, Gate: cognitivecore.SafetyGate{DryRun: true}}
	for index := 0; index < cognitivecore.MaxJournalEntries+32; index++ {
		if _, err := core.Process(context.Background(), contract.Event{ID: "saturation-" + strconv.Itoa(index), Type: "sensor.motion", Source: "discovery", Timestamp: time.Unix(int64(index+1), 0).UTC(), Payload: map[string]any{"movement": true}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ValidateBounds(); err != nil {
		t.Fatal(err)
	}
}

type testMLP struct{ output cognitivecore.MLPOutput }

func (m testMLP) Run(context.Context, cognitivecore.EncodedSnapshot, cognitivecore.CognitiveSnapshot) (cognitivecore.MLPOutput, map[string]float64, error) {
	return m.output, map[string]float64{"danger": .1, "incident": .1, "task": .1, "action": .1}, nil
}

func cognitivecoreAction(action string) discovery.ActionRequest {
	return discovery.ActionRequest{SchemaVersion: discovery.BoundarySchemaVersion, RequestID: "e2e-action", Action: action, Capability: action, DryRun: true}
}
func mustMarshal(value any) []byte { body, _ := json.Marshal(value); return body }
