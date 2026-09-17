package cognitive

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

func TestCPUMLPParityFixtures(t *testing.T) {
	modelDir := os.Getenv("SYNORA_COGNITIVE_MODEL_DIR")
	fixturePath := os.Getenv("SYNORA_COGNITIVE_FIXTURES")
	if modelDir == "" || fixturePath == "" {
		t.Skip("set SYNORA_COGNITIVE_MODEL_DIR and SYNORA_COGNITIVE_FIXTURES for exported model parity")
	}
	backend, err := NewCPUMLPBackend(modelDir)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var count int
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var fixture struct {
			State        []float32       `json:"state"`
			Availability []float32       `json:"availability"`
			Ledger       []float32       `json:"ledger"`
			Expected     json.RawMessage `json:"expected"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatal(err)
		}
		var expected struct {
			Danger   int               `json:"danger"`
			Incident []json.RawMessage `json:"incident"`
			Task     []float64         `json:"task"`
			Action   []float64         `json:"action"`
		}
		if err := json.Unmarshal(fixture.Expected, &expected); err != nil {
			t.Fatal(err)
		}
		catalog := ActionCatalog{SchemaVersion: ActionCatalogSchemaVersion}
		for i, available := range fixture.Availability {
			if available > 0.5 {
				catalog.Actions = append(catalog.Actions, ActionDescriptor{ID: backend.Action.Labels[i], Slot: backend.Action.Labels[i], Capability: backend.Action.Labels[i], RiskClass: "teacher_gated", Enabled: true})
			}
		}
		ledger := ActionLedgerSnapshot{SchemaVersion: ActionLedgerSchemaVersion, IncidentOpen: len(fixture.Ledger) > 0 && fixture.Ledger[0] > 0.5}
		for i, slot := range backend.Action.Labels {
			if fixture.Ledger[1+i] > 0.5 {
				ledger.RecentActions = append(ledger.RecentActions, ActionLedgerEntry{Slot: slot, Status: "executed"})
			}
			if fixture.Ledger[1+len(backend.Action.Labels)+i] > 0.5 {
				ledger.RecentActions = append(ledger.RecentActions, ActionLedgerEntry{Slot: slot, Status: "failed"})
			}
		}
		input := CognitiveInput{SchemaVersion: SchemaVersion, RequestID: "parity", Task: Task{ID: "parity", Kind: "fixture"}, EncodedState: EncodedState{SchemaVersion: StateEncoderSchemaVersion, EncoderID: EncoderV4ID, EncoderVersion: EncoderV4Version, DType: "float32", Shape: []int{len(fixture.State)}, FeatureNames: make([]string, len(fixture.State)), Values: fixture.State, FrameChecksum: "0000000000000000000000000000000000000000000000000000000000000000"}, ActionCatalog: catalog, ActionLedger: ledger}
		output, err := backend.Run(context.Background(), input, AdapterDescriptor{ID: "event_reasoning"})
		if err != nil {
			t.Fatal(err)
		}
		if output.DangerLabel != backend.Danger.Labels[expected.Danger] {
			t.Fatalf("fixture %d danger=%s want=%s", count, output.DangerLabel, backend.Danger.Labels[expected.Danger])
		}
		incidentSelected := make([]interface{}, len(output.IncidentTags))
		for i, item := range output.IncidentTags {
			incidentSelected[i] = boolToFloat(item.Selected)
		}
		var expectedIncident []interface{}
		var expectedPhase float64
		if len(expected.Incident) == 2 {
			_ = json.Unmarshal(expected.Incident[0], &expectedIncident)
			_ = json.Unmarshal(expected.Incident[1], &expectedPhase)
		}
		if len(expected.Incident) != 2 || !reflect.DeepEqual(incidentSelected, expectedIncident) || output.IncidentPhase != backend.Incident.Labels[len(output.IncidentTags)+int(expectedPhase)] {
			t.Fatalf("fixture %d incident mismatch", count)
		}
		taskSelected := make([]float64, len(output.TaskScores))
		for i, item := range output.TaskScores {
			if item.Selected {
				taskSelected[i] = 1
			}
		}
		if !reflect.DeepEqual(taskSelected, expected.Task) {
			t.Fatalf("fixture %d task mismatch got=%v want=%v", count, taskSelected, expected.Task)
		}
		wantActions := []string{}
		for i, value := range expected.Action {
			completed := 1+i < len(fixture.Ledger) && fixture.Ledger[1+i] > 0.5
			if value > 0.5 && !completed {
				wantActions = append(wantActions, backend.Action.Labels[i])
			}
		}
		sort.Strings(wantActions)
		gotActions := append([]string{}, output.ProposedActionIDs...)
		sort.Strings(gotActions)
		if !reflect.DeepEqual(gotActions, wantActions) {
			t.Fatalf("fixture %d action mismatch got=%v want=%v", count, gotActions, wantActions)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count < 100 {
		t.Fatalf("only %d parity fixtures", count)
	}
}

func boolToFloat(value bool) interface{} {
	if value {
		return float64(1)
	}
	return float64(0)
}
