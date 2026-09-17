package cognitive

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestContractsRoundTripAndAdvisoryValidation(t *testing.T) {
	encoded, catalog := validInputParts(t)
	input := CognitiveInput{
		SchemaVersion: SchemaVersion,
		RequestID:     "req-1",
		Task:          Task{ID: "task-1", Kind: "observe", RequestedCapabilities: []string{CapabilityEventReasoning}},
		EncodedState:  encoded,
		ActionCatalog: catalog,
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CognitiveInput
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}

	output := CognitiveOutput{RequestID: "req-1", TaskID: "task-1", AdvisoryOnly: true, Confidence: 0.25}
	if err := output.Validate(); err != nil {
		t.Fatal(err)
	}
	output.AdvisoryOnly = false
	if !errors.Is(output.Validate(), ErrNonAdvisoryOutput) {
		t.Fatalf("expected advisory validation error, got %v", output.Validate())
	}
}

func validInputParts(t *testing.T) (EncodedState, ActionCatalog) {
	t.Helper()
	encoded, err := (DeterministicStateEncoder{}).Encode(context.Background(), StateFrame{SchemaVersion: StateFrameSchemaVersion, CapturedAt: time.Unix(1, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return encoded, ActionCatalog{SchemaVersion: ActionCatalogSchemaVersion, Actions: []ActionDescriptor{{ID: "observe", Capability: "observe", RiskClass: "none", Enabled: true}}}
}

func TestSchedulerAndMockBackend(t *testing.T) {
	scheduler := NewScheduler(DefaultRegistry())
	encoded, catalog := validInputParts(t)
	output, err := scheduler.Run(context.Background(), CognitiveInput{
		SchemaVersion: SchemaVersion,
		RequestID:     "req-2",
		Task:          Task{ID: "task-2", Kind: "context", RequestedCapabilities: []string{CapabilityStructured}, RequestedModalities: []string{ModalityStructured}},
		EncodedState:  encoded,
		ActionCatalog: catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.AdapterID != "structured_context" || output.BackendID != "mock" || !output.AdvisoryOnly {
		t.Fatalf("unexpected output: %+v", output)
	}
}

func TestBuildInputUsesVersionedEncoderAndCatalog(t *testing.T) {
	input, err := BuildInput(context.Background(), "request-3", Task{ID: "task-3", Kind: "observe"}, StateFrame{SchemaVersion: StateFrameSchemaVersion, CapturedAt: time.Unix(2, 0).UTC()}, ActionCatalog{SchemaVersion: ActionCatalogSchemaVersion}, DeterministicStateEncoder{})
	if err != nil {
		t.Fatal(err)
	}
	if input.EncodedState.EncoderID != "deterministic" || input.ActionCatalog.SchemaVersion != ActionCatalogSchemaVersion {
		t.Fatalf("unexpected state-to-input boundary: %+v", input)
	}
}

func TestRegistrySelectionIsDeterministic(t *testing.T) {
	registry := NewInMemoryRegistry()
	backend := MockBackend{}
	for _, id := range []string{"zeta", "alpha"} {
		if err := registry.Register(AdapterDescriptor{ID: id, Capabilities: []string{CapabilityEventReasoning}, Modalities: []string{ModalityEvent}, Version: "1", Enabled: true, BackendID: backend.ID()}, backend); err != nil {
			t.Fatal(err)
		}
	}
	descriptor, _, err := registry.Resolve(Task{ID: "task", Kind: "observe", RequestedCapabilities: []string{CapabilityEventReasoning}})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.ID != "alpha" {
		t.Fatalf("expected lexical tie-breaker alpha, got %s", descriptor.ID)
	}
}
