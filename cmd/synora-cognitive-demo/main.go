package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"synora/internal/cognitive"
)

type traceRecord struct {
	SchemaVersion string                     `json:"schema_version"`
	Frame         cognitive.StateFrame       `json:"frame"`
	Vector        cognitive.EncodedState     `json:"vector"`
	Danger        map[string]any             `json:"danger"`
	Incident      []cognitive.ScoredLabel    `json:"incident"`
	Tasks         []cognitive.ScoredLabel    `json:"tasks"`
	Proposed      []string                   `json:"actions_proposed"`
	Filtered      []cognitive.FilteredAction `json:"actions_filtered"`
	DryRun        bool                       `json:"dry_run"`
}

func main() {
	modelDir := flag.String("models", os.Getenv("SYNORA_COGNITIVE_MODEL_DIR"), "exported CPU model directory")
	tracePath := flag.String("trace", "cognitive-dry-run.jsonl", "append-only trace path")
	flag.Parse()
	if strings.TrimSpace(*modelDir) == "" {
		fail("--models or SYNORA_COGNITIVE_MODEL_DIR is required")
	}
	if value := strings.TrimSpace(os.Getenv(cognitive.CognitiveDryRunEnv)); value != "1" {
		fail("strict dry-run requires SYNORA_COGNITIVE_DRY_RUN=1")
	}
	backend, err := cognitive.NewCPUMLPBackend(*modelDir)
	if err != nil {
		fail(err.Error())
	}
	now := time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC)
	frame := cognitive.StateFrame{
		SchemaVersion: cognitive.StateFrameSchemaVersion,
		Revision:      42,
		CapturedAt:    now,
		CurrentEvent:  &cognitive.StateEvent{ID: "fixture-motion-001", Type: "motion.person.detected", NodeID: "perimeter", Priority: 5, Confidence: 0.98, Timestamp: now.Add(-12 * time.Second)},
		System:        cognitive.StateSystem{Armed: true, LastState: "armed_away"},
		Devices:       []cognitive.StateDevice{{ID: "camera-perimeter", Type: "camera", NodeID: "perimeter", Online: true, Enabled: true}, {ID: "light-entry", Type: "light", NodeID: "event_zone", Online: true, Enabled: true}},
		Topology:      []cognitive.StateNode{{ID: "perimeter", Type: "perimeter"}},
		Residents:     []cognitive.StateResident{{ID: "resident-1", Enabled: true, Trusted: true, State: "absent"}},
		RecentEvents:  []cognitive.StateEvent{{ID: "fixture-door-001", Type: "access.door.opened", NodeID: "perimeter", Priority: 3, Timestamp: now.Add(-30 * time.Second)}},
		SecurityMode:  "armed_away",
		Temporal:      cognitive.EncoderV4TemporalContext{EvaluationTrigger: "event", SecondsSinceLastEvent: 12, EventCount: 2},
	}
	catalog := cognitive.ActionCatalog{SchemaVersion: cognitive.ActionCatalogSchemaVersion, Revision: 42, Actions: []cognitive.ActionDescriptor{
		{ID: "observation.record@event_zone", Slot: "observation.record@event_zone", Capability: "observation.record", RiskClass: "low", Enabled: true},
		{ID: "camera.record@event_zone", Slot: "camera.record@event_zone", Capability: "camera.record", RiskClass: "low", Enabled: true},
		{ID: "light.activate@event_zone", Slot: "light.activate@event_zone", Capability: "light.activate", RiskClass: "medium", Enabled: true},
		{ID: "notify.security@global", Slot: "notify.security@global", Capability: "notify.security", RiskClass: "high", Enabled: true, RequiresTeacherApproval: true},
		{ID: "alarm.activate@global", Slot: "alarm.activate@global", Capability: "alarm.activate", RiskClass: "critical", Enabled: false, RequiresTeacherApproval: true},
	}}
	input, err := cognitive.BuildInput(context.Background(), "demo-request-001", cognitive.Task{ID: "demo-task-001", Kind: "sensor_event", RequestedCapabilities: []string{cognitive.CapabilityEventReasoning}, RequestedModalities: []string{cognitive.ModalityEvent, cognitive.ModalityStructured}}, frame, catalog, cognitive.V4StateEncoder{})
	if err != nil {
		fail(err.Error())
	}
	input.ActionLedger = cognitive.ActionLedgerSnapshot{SchemaVersion: cognitive.ActionLedgerSchemaVersion, IncidentOpen: true, RecentActions: []cognitive.ActionLedgerEntry{{Slot: "camera.record@event_zone", Status: "executed"}}}
	output, err := cognitive.NewScheduler(cognitive.RegistryWithBackend(backend)).Run(context.Background(), input)
	if err != nil {
		fail(err.Error())
	}
	record := traceRecord{SchemaVersion: "synora.cognitive-dry-run-trace/v1", Frame: frame, Vector: input.EncodedState, Danger: map[string]any{"label": output.DangerLabel, "probabilities": output.DangerProbabilities}, Incident: output.IncidentTags, Tasks: output.TaskScores, Proposed: output.ProposedActionIDs, Filtered: output.FilteredActions, DryRun: true}
	file, err := os.OpenFile(*tracePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		fail(err.Error())
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(record); err != nil {
		fail(err.Error())
	}
	body, _ := json.MarshalIndent(record, "", "  ")
	fmt.Println(string(body))
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "synora-cognitive-demo:", message)
	os.Exit(1)
}
