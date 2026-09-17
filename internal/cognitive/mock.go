package cognitive

import (
	"context"
	"time"
)

// MockBackend is the only executable backend in this foundation. It performs
// no inference and emits a stable, non-actionable advisory envelope.
type MockBackend struct{}

var _ ModelBackend = MockBackend{}

func (MockBackend) ID() string { return "mock" }

func (MockBackend) Run(ctx context.Context, input CognitiveInput, descriptor AdapterDescriptor) (CognitiveOutput, error) {
	if err := ctx.Err(); err != nil {
		return CognitiveOutput{}, err
	}
	return CognitiveOutput{
		SchemaVersion:         SchemaVersion,
		RequestID:             input.RequestID,
		TaskID:                input.Task.ID,
		AdapterID:             descriptor.ID,
		BackendID:             "mock",
		AdvisoryOnly:          true,
		Classification:        "mock_advisory",
		InferredState:         "unknown",
		RequestedCapabilities: append([]string(nil), input.Task.RequestedCapabilities...),
		Confidence:            0,
		GeneratedAt:           time.Now().UTC(),
	}, nil
}

// DefaultRegistry exposes the planned adapter topology without implementing
// vision or planner behavior. Disabled descriptors are documentation for the
// future registry, not runnable adapters.
func DefaultRegistry() *InMemoryRegistry {
	registry := NewInMemoryRegistry()
	mock := MockBackend{}
	active := []AdapterDescriptor{
		{
			ID: "event_reasoning", Capabilities: []string{CapabilityEventReasoning},
			Modalities: []string{ModalityEvent}, Version: "0.1.0", Enabled: true, BackendID: mock.ID(),
		},
		{
			ID: "structured_context", Capabilities: []string{CapabilityStructured},
			Modalities: []string{ModalityStructured}, Version: "0.1.0", Enabled: true, BackendID: mock.ID(),
		},
		{
			ID: "topology_state", Capabilities: []string{CapabilityTopologyState},
			Modalities: []string{ModalityTopology, ModalityStructured}, Version: "0.1.0", Enabled: true, BackendID: mock.ID(),
		},
	}
	for _, descriptor := range active {
		_ = registry.Register(descriptor, mock)
	}
	// These entries keep future capability names explicit while preventing
	// accidental activation before a real implementation is reviewed.
	for _, descriptor := range []AdapterDescriptor{
		{ID: "vision_summary", Capabilities: []string{CapabilityVisionSummary}, Modalities: []string{ModalityVision}, Version: "0.1.0", Enabled: false, BackendID: ""},
		{ID: "planner", Capabilities: []string{CapabilityPlanner}, Modalities: []string{ModalityStructured}, Version: "0.1.0", Enabled: false, BackendID: ""},
	} {
		registry.entries[descriptor.ID] = registryEntry{descriptor: descriptor}
	}
	return registry
}

// RegistryWithBackend wires one reviewed backend into the same stable adapter
// topology. The scheduler and CognitiveInput/CognitiveOutput contracts remain
// unchanged regardless of whether this is the mock or CPU MLP backend.
func RegistryWithBackend(backend Backend) *InMemoryRegistry {
	if backend == nil {
		return DefaultRegistry()
	}
	registry := NewInMemoryRegistry()
	for _, descriptor := range []AdapterDescriptor{
		{ID: "event_reasoning", Capabilities: []string{CapabilityEventReasoning}, Modalities: []string{ModalityEvent, ModalityStructured, ModalityTopology}, Version: "1.0.0", Enabled: true, BackendID: backend.ID()},
		{ID: "structured_context", Capabilities: []string{CapabilityStructured}, Modalities: []string{ModalityStructured, ModalityTopology}, Version: "1.0.0", Enabled: true, BackendID: backend.ID()},
		{ID: "topology_state", Capabilities: []string{CapabilityTopologyState}, Modalities: []string{ModalityTopology, ModalityStructured}, Version: "1.0.0", Enabled: true, BackendID: backend.ID()},
	} {
		_ = registry.Register(descriptor, backend)
	}
	registry.entries["vision_summary"] = registryEntry{descriptor: AdapterDescriptor{ID: "vision_summary", Capabilities: []string{CapabilityVisionSummary}, Modalities: []string{ModalityVision}, Version: "1.0.0", Enabled: false}}
	registry.entries["planner"] = registryEntry{descriptor: AdapterDescriptor{ID: "planner", Capabilities: []string{CapabilityPlanner}, Modalities: []string{ModalityStructured}, Version: "1.0.0", Enabled: false}}
	return registry
}
