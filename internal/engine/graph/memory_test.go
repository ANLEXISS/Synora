package graph

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"synora/internal/engine/contracts"
)

func TestSequenceKeyUsedForLastNodeLookup(t *testing.T) {
	memory := NewGraphMemory()
	event := &contracts.Event{
		ID:           "evt_1",
		Type:         "vision.id.seen",
		SubjectType:  contracts.SubjectResident,
		SubjectID:    "alexis",
		TargetType:   contracts.SubjectDevice,
		TargetID:     "cam_01",
		TopologyNode: "zoneA.L0.entree",
		Timestamp:    time.Now().UTC(),
	}

	memory.LearnEvent(event)

	if _, ok := memory.GetLastNode(SequenceKey(event)); !ok {
		t.Fatal("last node should be addressable with the same sequence key used by LearnEvent")
	}
}

func TestLearnedSequenceCountsRepeatedSimulationScenario(t *testing.T) {
	memory := NewGraphMemory()
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	for run := 0; run < 5; run++ {
		runID := "run-" + string(rune('a'+run))
		events := []string{"vision.unknown", "vision.motion", "vision.unknown"}
		steps := []string{"unknown_first", "entry_motion", "unknown_confirmed"}
		for i, eventType := range events {
			memory.LearnEvent(simulatedCGEEvent(eventType, runID, steps[i], base.Add(time.Duration(run*3+i)*500*time.Millisecond)))
		}
	}

	inspection := memory.Inspection()
	sequences := inspection["sequences"].([]contracts.LearnedSequence)
	sequence := findLearnedSequence(sequences, "vision.unknown > vision.motion > vision.unknown")
	if sequence == nil {
		t.Fatalf("learned sequence missing: %#v", sequences)
	}
	if sequence.Count != 5 || sequence.SimulatedCount != 5 || sequence.RealCount != 0 || sequence.Confidence <= 0 {
		t.Fatalf("unexpected learned sequence: %#v", sequence)
	}

	transitions := inspection["transitions"].([]contracts.LearnedTransition)
	if transition := findLearnedTransition(transitions, "vision.unknown", "vision.motion"); transition == nil || transition.Count != 5 || transition.SimulatedCount != 5 {
		t.Fatalf("unknown -> motion transition not learned: %#v", transitions)
	}
	if transition := findLearnedTransition(transitions, "vision.motion", "vision.unknown"); transition == nil || transition.Count != 5 || transition.SimulatedCount != 5 {
		t.Fatalf("motion -> unknown transition not learned: %#v", transitions)
	}

	behaviors := inspection["learned_behaviors"].([]contracts.LearnedBehavior)
	if len(behaviors) == 0 || behaviors[0].Status != "observing" || behaviors[0].RealCount != 0 || !behaviors[0].RequiresValidation {
		t.Fatalf("simulation-only behavior should remain observing: %#v", behaviors)
	}
}

func TestActionEvidenceAttachesToObservedBehavior(t *testing.T) {
	memory := NewGraphMemory()
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	for run := 0; run < 3; run++ {
		runID := "run-" + string(rune('a'+run))
		events := []string{"vision.unknown", "vision.motion", "vision.unknown"}
		steps := []string{"unknown_first", "entry_motion", "unknown_confirmed"}
		for i, eventType := range events {
			memory.LearnEvent(simulatedCGEEvent(eventType, runID, steps[i], base.Add(time.Duration(run*3+i)*500*time.Millisecond)))
		}
		if run == 2 {
			memory.ObserveActionEvidence(runID, "action_result action=light-entry status=simulated_success", true, base.Add(10*time.Second))
		}
	}

	behaviors := memory.Inspection()["learned_behaviors"].([]contracts.LearnedBehavior)
	if len(behaviors) == 0 {
		t.Fatal("expected learned behavior")
	}
	found := false
	for _, evidence := range behaviors[0].Evidence {
		if evidence == "action_result action=light-entry status=simulated_success" {
			found = true
		}
	}
	if !found || behaviors[0].Status != "observing" || behaviors[0].RealCount != 0 {
		t.Fatalf("action evidence should attach without approval: %#v", behaviors[0])
	}
}

func TestSimulationLearningDoesNotPopulateProductionGraph(t *testing.T) {
	memory := NewGraphMemory()
	memory.LearnEvent(simulatedCGEEvent("vision.unknown", "run-1", "unknown_first", time.Now().UTC()))
	if len(memory.GetGraph().Roots) != 0 {
		t.Fatalf("simulated learning should not populate production graph: %#v", memory.GetGraph().Roots)
	}

	memory.LearnEvent(realCGEEvent("vision.unknown", time.Now().UTC()))
	if len(memory.GetGraph().Roots) != 1 {
		t.Fatalf("real learning should populate production graph: %#v", memory.GetGraph().Roots)
	}
}

func TestLearningModeDisabledSkipsInspection(t *testing.T) {
	memory := NewGraphMemory()
	event := simulatedCGEEvent("vision.unknown", "run-1", "unknown_first", time.Now().UTC())
	event.Metadata["metadata"].(map[string]any)["learning_mode"] = "disabled"

	memory.LearnEvent(event)
	inspection := memory.Inspection()
	if len(inspection["sequences"].([]contracts.LearnedSequence)) != 0 || len(memory.GetGraph().Roots) != 0 {
		t.Fatalf("disabled learning should not update inspection or graph: %#v", inspection)
	}
}

func TestControlledValidationLearningIsOptIn(t *testing.T) {
	memory := NewGraphMemory()
	withoutLearning := realCGEEvent("vision.unknown", time.Now().UTC())
	withoutLearning.Metadata["metadata"] = map[string]any{
		"validation":  true,
		"source_type": "validation",
		"test_mode":   "controlled_real_test",
		"learn":       false,
	}
	memory.LearnEvent(withoutLearning)
	if len(memory.GetGraph().Roots) != 0 || len(memory.Inspection()["sequences"].([]contracts.LearnedSequence)) != 0 {
		t.Fatal("learn=false validation must not populate CGE memory")
	}

	withLearning := realCGEEvent("vision.unknown", time.Now().UTC().Add(time.Second))
	withLearning.Metadata["metadata"] = map[string]any{
		"validation":  true,
		"source_type": "validation",
		"test_mode":   "controlled_real_test",
		"learn":       true,
	}
	memory.LearnEvent(withLearning)
	second := realCGEEvent("vision.motion", time.Now().UTC().Add(2*time.Second))
	second.Metadata["metadata"] = map[string]any{
		"validation":  true,
		"source_type": "validation",
		"test_mode":   "controlled_real_test",
		"learn":       true,
	}
	memory.LearnEvent(second)
	if len(memory.GetGraph().Roots) != 1 || len(memory.Inspection()["sequences"].([]contracts.LearnedSequence)) == 0 {
		t.Fatalf("learn=true validation should populate CGE memory: roots=%d inspection=%#v", len(memory.GetGraph().Roots), memory.Inspection())
	}
}

func TestGraphMemoryLimitsLearnedInspection(t *testing.T) {
	memory := NewGraphMemory()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	total := CGEMaxSequences + 4000
	for run := 0; run < total; run++ {
		runID := fmt.Sprintf("run-%04d", run)
		deviceID := fmt.Sprintf("cam_%04d", run)
		memory.LearnEvent(simulatedCGEEventForDevice("vision.unknown", runID, "unknown_first", deviceID, base.Add(time.Duration(run)*time.Second)))
		memory.LearnEvent(simulatedCGEEventForDevice("vision.motion", runID, "entry_motion", deviceID, base.Add(time.Duration(run)*time.Second+500*time.Millisecond)))
	}

	inspection := memory.Inspection()
	sequences := inspection["sequences"].([]contracts.LearnedSequence)
	if len(sequences) > CGEMaxSequences {
		t.Fatalf("sequence memory limit exceeded: got=%d max=%d", len(sequences), CGEMaxSequences)
	}
	transitions := inspection["transitions"].([]contracts.LearnedTransition)
	if len(transitions) > CGEMaxTransitions {
		t.Fatalf("transition memory limit exceeded: got=%d max=%d", len(transitions), CGEMaxTransitions)
	}
	for _, sequence := range sequences {
		if len(sequence.Evidence) > CGEMaxEvidencePerSequence {
			t.Fatalf("sequence evidence limit exceeded: %#v", sequence)
		}
		if len(sequence.Examples) > CGEMaxExamplesPerSequence {
			t.Fatalf("sequence examples limit exceeded: %#v", sequence)
		}
	}
}

func TestCompactInspectionLimitsPublicCGEAndOmitsEvidence(t *testing.T) {
	memory := NewGraphMemory()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	for run := 0; run < 40; run++ {
		runID := fmt.Sprintf("run-%04d", run)
		deviceID := fmt.Sprintf("cam_%04d", run)
		for repeat := 0; repeat < 3; repeat++ {
			memory.LearnEvent(simulatedCGEEventForDevice("vision.unknown", runID, fmt.Sprintf("unknown_%d", repeat), deviceID, base.Add(time.Duration(run*10+repeat*3)*time.Second)))
			memory.LearnEvent(simulatedCGEEventForDevice("vision.motion", runID, fmt.Sprintf("motion_%d", repeat), deviceID, base.Add(time.Duration(run*10+repeat*3)*time.Second+500*time.Millisecond)))
			memory.LearnEvent(simulatedCGEEventForDevice("vision.unknown", runID, fmt.Sprintf("confirm_%d", repeat), deviceID, base.Add(time.Duration(run*10+repeat*3)*time.Second+time.Second)))
		}
	}

	compact := memory.CompactInspection()
	assertCompactCollectionLimit(t, compact["sequences"], defaultCGEPublicSequencesLimit)
	assertCompactCollectionLimit(t, compact["transitions"], defaultCGEPublicTransitionsLimit)
	assertCompactCollectionLimit(t, compact["learned_behaviors"], defaultCGEPublicBehaviorsLimit)
	if compact["stats"] == nil {
		t.Fatalf("compact cge should expose stats: %#v", compact)
	}
	sequences := reflect.ValueOf(compact["sequences"])
	if sequences.Len() == 0 {
		t.Fatalf("expected compact sequences: %#v", compact)
	}
	sequence := sequences.Index(0).Interface()
	data, err := jsonMarshalMap(sequence)
	if err != nil {
		t.Fatalf("marshal compact sequence: %v", err)
	}
	if _, ok := data["evidence"]; ok {
		t.Fatalf("compact sequence should not expose evidence: %#v", data)
	}
	if _, ok := data["examples"]; ok {
		t.Fatalf("compact sequence should not expose examples: %#v", data)
	}
	if _, ok := data["evidence_count"]; !ok {
		t.Fatalf("compact sequence should expose evidence_count: %#v", data)
	}
}

func simulatedCGEEvent(eventType string, runID string, stepID string, at time.Time) *contracts.Event {
	return simulatedCGEEventForDevice(eventType, runID, stepID, "cam_01", at)
}

func simulatedCGEEventForDevice(eventType string, runID string, stepID string, deviceID string, at time.Time) *contracts.Event {
	return &contracts.Event{
		ID:           runID + "-" + stepID,
		Type:         eventType,
		SubjectType:  contracts.SubjectDevice,
		SubjectID:    deviceID,
		TargetType:   contracts.SubjectDevice,
		TargetID:     deviceID,
		TopologyNode: "zoneA.L0.entree",
		Timestamp:    at,
		Metadata: map[string]any{
			"raw_type":    eventType,
			"device_id":   deviceID,
			"identity":    identityForEvent(eventType),
			"source_type": "simulator",
			"metadata": map[string]any{
				"simulated":         true,
				"test_run_id":       runID,
				"scenario_id":       "unknown_at_entrance",
				"scenario_step_id":  stepID,
				"event_instance_id": runID + ":" + stepID,
				"learning_mode":     "simulation",
			},
		},
	}
}

func assertCompactCollectionLimit(t *testing.T, value any, limit int) {
	t.Helper()
	v := reflect.ValueOf(value)
	if !v.IsValid() || v.Kind() != reflect.Slice {
		t.Fatalf("expected slice, got %#v", value)
	}
	if v.Len() > limit {
		t.Fatalf("compact collection exceeded limit: got=%d limit=%d", v.Len(), limit)
	}
}

func jsonMarshalMap(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func realCGEEvent(eventType string, at time.Time) *contracts.Event {
	return &contracts.Event{
		ID:           "real-1",
		Type:         eventType,
		SubjectType:  contracts.SubjectDevice,
		SubjectID:    "cam_01",
		TargetType:   contracts.SubjectDevice,
		TargetID:     "cam_01",
		TopologyNode: "zoneA.L0.entree",
		Timestamp:    at,
		Metadata: map[string]any{
			"raw_type":  eventType,
			"device_id": "cam_01",
			"identity":  identityForEvent(eventType),
		},
	}
}

func identityForEvent(eventType string) string {
	if eventType == "vision.unknown" {
		return "unknown"
	}
	return ""
}

func findLearnedSequence(sequences []contracts.LearnedSequence, signature string) *contracts.LearnedSequence {
	for i := range sequences {
		if sequences[i].Signature == signature {
			return &sequences[i]
		}
	}
	return nil
}

func findLearnedTransition(transitions []contracts.LearnedTransition, from string, to string) *contracts.LearnedTransition {
	for i := range transitions {
		if transitions[i].FromEventType == from && transitions[i].ToEventType == to {
			return &transitions[i]
		}
	}
	return nil
}
