package main

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

func TestV1ArchitectureHasNoLegacyDecisionImports(t *testing.T) {
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
				for _, forbidden := range []string{"synora/internal/cge", "synora/internal/engine", "teacher", "shadow"} {
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

func TestV1DiscoveryCoreStoreDiscoveryActionResultLoop(t *testing.T) {
	bus := &e2eBus{}
	store := cognitivecore.NewUniversalStore()
	core := &cognitivecore.Core{Store: store, MLP: testMLP{output: cognitivecore.MLPOutput{DangerLabel: "high", DangerScore: 0.9, Action: cognitivecore.ActionIntent{Action: "notify", Capability: "notify"}}}, Gate: cognitivecore.SafetyGate{DryRun: true}, Now: func() time.Time { return time.Unix(200, 0).UTC() }}
	service := &cognitivecore.Service{Bus: bus, Core: core}
	if err := service.Handle(context.Background(), contract.Message{ID: "loop-1", Type: "sensor.anomaly", Kind: contract.KindEvent, Source: "discovery", Timestamp: time.Unix(200, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if len(bus.sent) != 2 || bus.sent[1].Type != "action.request" {
		t.Fatalf("Core did not emit abstract action request: %#v", bus.sent)
	}
	var request discovery.ActionRequest
	if err := json.Unmarshal(bus.sent[1].Payload, &request); err != nil {
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

type testMLP struct{ output cognitivecore.MLPOutput }

func (m testMLP) Run(context.Context, cognitivecore.EncodedSnapshot, cognitivecore.CognitiveSnapshot) (cognitivecore.MLPOutput, map[string]float64, error) {
	return m.output, map[string]float64{"danger": .1, "incident": .1, "task": .1, "action": .1}, nil
}

func cognitivecoreAction(action string) discovery.ActionRequest {
	return discovery.ActionRequest{SchemaVersion: discovery.BoundarySchemaVersion, RequestID: "e2e-action", Action: action, Capability: action, DryRun: true}
}
func mustMarshal(value any) []byte { body, _ := json.Marshal(value); return body }
