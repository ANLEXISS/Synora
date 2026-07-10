package simulation

import (
	"encoding/json"
	"testing"

	"synora/pkg/contract"
)

func TestBuildRunGeneratesID(t *testing.T) {
	run := BuildRun("Unknown at entrance", "unknown_at_entrance", ModeDryRun, GeneratedBySynoraLab, map[string]any{"source": "test"})
	if run.ID == "" || run.Name != "Unknown at entrance" || run.ScenarioID != "unknown_at_entrance" || run.Status != StatusRunning || run.Mode != ModeDryRun || run.CreatedBy != GeneratedBySynoraLab {
		t.Fatalf("unexpected run: %#v", run)
	}
	if run.Metadata["source"] != "test" {
		t.Fatalf("metadata should be copied: %#v", run.Metadata)
	}
}

func TestScenarioResidentEntersHomeSteps(t *testing.T) {
	scenario, ok := ScenarioByID("resident_enters_home")
	if !ok {
		t.Fatal("resident_enters_home scenario missing")
	}
	if len(scenario.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %#v", scenario.Steps)
	}
	if scenario.Steps[0].EventType != contract.EventVisionIdentity || scenario.Steps[1].EventType != contract.EventVisionMotion {
		t.Fatalf("unexpected steps: %#v", scenario.Steps)
	}
}

func TestScenarioUnknownAtEntranceContainsUnknown(t *testing.T) {
	scenario, ok := ScenarioByID("unknown_at_entrance")
	if !ok {
		t.Fatal("unknown_at_entrance scenario missing")
	}
	found := false
	for _, step := range scenario.Steps {
		if step.EventType == contract.EventVisionUnknown {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("scenario should contain vision.unknown: %#v", scenario.Steps)
	}
}

func TestBuildMessageIncludesSimulationMetadata(t *testing.T) {
	run := BuildRun("single", "", ModeDryRun, GeneratedBySynoraLab, nil)
	msg, err := BuildMessage(EventBuildOptions{
		Type:        contract.EventVisionUnknown,
		DeviceID:    "cam_01",
		Run:         &run,
		DryRun:      true,
		GeneratedBy: GeneratedBySynoraLab,
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	if msg.Source != "lab" || msg.SourceType != contract.SourceSimulator || msg.Target != "core" {
		t.Fatalf("unexpected simulation transport fields: %#v", msg)
	}
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["device_id"] != "cam_01" || payload["camera_id"] != "cam_01" {
		t.Fatalf("camera identity should stay in payload: %#v", payload)
	}
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata missing: %#v", payload)
	}
	if metadata["simulated"] != true || metadata["test_run_id"] != run.ID || metadata["dry_run"] != true || metadata["generated_by"] != GeneratedBySynoraLab {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
	if metadata["event_instance_id"] != run.ID {
		t.Fatalf("single event should expose event_instance_id fallback: %#v", metadata)
	}
}

func TestBuildEventsForScenarioPropagatesRunAndScenario(t *testing.T) {
	scenario, ok := ScenarioByID("unknown_at_entrance")
	if !ok {
		t.Fatal("scenario missing")
	}
	run := BuildRun(scenario.Name, scenario.ID, ModeDryRun, GeneratedBySynoraLab, nil)
	messages, err := BuildEventsForScenario(run, scenario, EventBuildOptions{DryRun: true})
	if err != nil {
		t.Fatalf("build events: %v", err)
	}
	if len(messages) != len(scenario.Steps) {
		t.Fatalf("message count mismatch: %d", len(messages))
	}
	for i, msg := range messages {
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		metadata := payload["metadata"].(map[string]any)
		if metadata["simulated"] != true ||
			metadata["test_run_id"] != run.ID ||
			metadata["scenario_id"] != scenario.ID ||
			metadata["scenario_step_id"] != scenario.Steps[i].ID ||
			metadata["event_instance_id"] != run.ID+":"+scenario.Steps[i].ID {
			t.Fatalf("metadata not propagated for step %d: %#v", i, metadata)
		}
	}
}

func TestBuildMessageCanOverrideEventInstanceID(t *testing.T) {
	run := BuildRun("single", "unknown_at_entrance", ModeDryRun, GeneratedBySynoraAPI, nil)
	msg, err := BuildMessage(EventBuildOptions{
		Type:            contract.EventVisionUnknown,
		Run:             &run,
		StepID:          "unknown_first",
		EventInstanceID: "custom-instance",
		GeneratedBy:     GeneratedBySynoraAPI,
		DryRun:          true,
	})
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	if msg.Source != "api" {
		t.Fatalf("synora-api simulations should use api transport source: %#v", msg)
	}
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	metadata := payload["metadata"].(map[string]any)
	if metadata["scenario_step_id"] != "unknown_first" || metadata["event_instance_id"] != "custom-instance" || metadata["generated_by"] != GeneratedBySynoraAPI {
		t.Fatalf("metadata override mismatch: %#v", metadata)
	}
}
