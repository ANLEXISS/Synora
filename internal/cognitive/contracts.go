// Package cognitive contains the model-independent boundary for future local
// cognitive runtimes. It is deliberately advisory-only: this package exposes
// no command or actuator contract.
package cognitive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = "cognitive/v1"

const (
	CapabilityEventReasoning = "event_reasoning"
	CapabilityStructured     = "structured_context"
	CapabilityVisionSummary  = "vision_summary"
	CapabilityTopologyState  = "topology_state"
	CapabilityPlanner        = "planner"

	ModalityEvent      = "event"
	ModalityStructured = "structured"
	ModalityVision     = "vision"
	ModalityTopology   = "topology"
)

var (
	ErrInvalidInput      = errors.New("invalid cognitive input")
	ErrNoAdapter         = errors.New("no cognitive adapter matches task")
	ErrDuplicateAdapter  = errors.New("cognitive adapter already registered")
	ErrInvalidAdapter    = errors.New("invalid cognitive adapter")
	ErrNonAdvisoryOutput = errors.New("cognitive output must be advisory-only")
	ErrExecutableOutput  = errors.New("cognitive output contains executable actions")
)

// Task describes the capability and modality contract requested by a caller.
// Payload remains opaque so adding a future model cannot change this boundary.
type Task struct {
	ID                    string          `json:"id"`
	Kind                  string          `json:"kind"`
	RequestedCapabilities []string        `json:"requested_capabilities,omitempty"`
	RequestedModalities   []string        `json:"requested_modalities,omitempty"`
	Priority              int             `json:"priority,omitempty"`
	Payload               json.RawMessage `json:"payload,omitempty"`
}

func (t Task) Validate() error {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Kind) == "" {
		return fmt.Errorf("%w: task id and kind are required", ErrInvalidInput)
	}
	if t.Priority < 0 {
		return fmt.Errorf("%w: task priority cannot be negative", ErrInvalidInput)
	}
	return nil
}

// CognitiveInput is the vectorized, read-only input presented to a backend.
// The model sees encoded state and an allow-listed catalog, not free text.
type CognitiveInput struct {
	SchemaVersion string               `json:"schema_version"`
	RequestID     string               `json:"request_id"`
	CreatedAt     time.Time            `json:"created_at"`
	Task          Task                 `json:"task"`
	EncodedState  EncodedState         `json:"encoded_state"`
	ActionCatalog ActionCatalog        `json:"action_catalog"`
	ActionLedger  ActionLedgerSnapshot `json:"action_ledger,omitempty"`
}

// BuildInput is the explicit state-to-model boundary. It is the only place
// where a StateFrame becomes a vectorized CognitiveInput.
func BuildInput(ctx context.Context, requestID string, task Task, frame StateFrame, catalog ActionCatalog, encoder StateEncoder) (CognitiveInput, error) {
	if encoder == nil {
		return CognitiveInput{}, fmt.Errorf("%w: state encoder is required", ErrInvalidInput)
	}
	catalog = catalog.Normalized()
	frame.AvailableActionIDs = make([]string, 0, len(catalog.Actions))
	frame.AvailableCapabilities = make([]string, 0, len(catalog.Actions))
	for _, action := range catalog.Actions {
		if action.Enabled {
			frame.AvailableCapabilities = append(frame.AvailableCapabilities, action.Capability)
			frame.AvailableActionIDs = append(frame.AvailableActionIDs, actionSlot(action))
		}
	}
	encoded, err := encoder.Encode(ctx, frame)
	if err != nil {
		return CognitiveInput{}, err
	}
	input := CognitiveInput{SchemaVersion: SchemaVersion, RequestID: strings.TrimSpace(requestID), CreatedAt: time.Now().UTC(), Task: task, EncodedState: encoded, ActionCatalog: catalog, ActionLedger: ActionLedgerSnapshot{SchemaVersion: ActionLedgerSchemaVersion}}
	if err := input.Validate(); err != nil {
		return CognitiveInput{}, err
	}
	return input, nil
}

func (i CognitiveInput) Validate() error {
	if i.SchemaVersion != "" && i.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: unsupported schema version %q", ErrInvalidInput, i.SchemaVersion)
	}
	if strings.TrimSpace(i.RequestID) == "" {
		return fmt.Errorf("%w: request id is required", ErrInvalidInput)
	}
	if err := i.Task.Validate(); err != nil {
		return err
	}
	if err := i.EncodedState.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if err := i.ActionCatalog.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return nil
}

// CognitiveOutput intentionally contains advisory observations only. In
// particular, it has no command, dispatch, actuator, or execution field.
type CognitiveOutput struct {
	SchemaVersion         string    `json:"schema_version"`
	RequestID             string    `json:"request_id"`
	TaskID                string    `json:"task_id"`
	AdapterID             string    `json:"adapter_id"`
	BackendID             string    `json:"backend_id"`
	AdvisoryOnly          bool      `json:"advisory_only"`
	Classification        string    `json:"classification,omitempty"`
	InferredState         string    `json:"inferred_state,omitempty"`
	RequestedCapabilities []string  `json:"requested_capabilities,omitempty"`
	Confidence            float64   `json:"confidence,omitempty"`
	GeneratedAt           time.Time `json:"generated_at"`

	// This field is only present to make rejection explicit if an untrusted
	// decoder is ever wired to this contract. Backends must leave it empty.
	ExecutableActions   []json.RawMessage `json:"executable_actions,omitempty"`
	DangerLogits        DangerLogits      `json:"danger_logits"`
	DangerProbabilities []float32         `json:"danger_probabilities,omitempty"`
	DangerLabel         string            `json:"danger_label,omitempty"`
	IncidentTags        []ScoredLabel     `json:"incident_tags,omitempty"`
	IncidentPhase       string            `json:"incident_phase,omitempty"`
	TaskScores          []ScoredLabel     `json:"task_scores,omitempty"`
	ActionLogits        []ActionLogit     `json:"action_logits,omitempty"`
	SelectedActionIDs   []string          `json:"selected_action_ids,omitempty"`
	ProposedActionIDs   []string          `json:"proposed_action_ids,omitempty"`
	FilteredActions     []FilteredAction  `json:"filtered_actions,omitempty"`
}

func (o CognitiveOutput) Validate() error {
	if !o.AdvisoryOnly {
		return ErrNonAdvisoryOutput
	}
	if len(o.ExecutableActions) != 0 {
		return ErrExecutableOutput
	}
	if o.Confidence < 0 || o.Confidence > 1 {
		return fmt.Errorf("cognitive confidence must be between 0 and 1")
	}
	if strings.TrimSpace(o.RequestID) == "" || strings.TrimSpace(o.TaskID) == "" {
		return fmt.Errorf("cognitive output request_id and task_id are required")
	}
	return nil
}

// AdapterDescriptor is metadata, not an implementation. Vision and planner
// descriptors may exist as disabled placeholders without implementing them.
type AdapterDescriptor struct {
	ID           string         `json:"id"`
	Capabilities []string       `json:"capabilities"`
	Modalities   []string       `json:"modalities"`
	Version      string         `json:"version"`
	Limits       map[string]int `json:"limits,omitempty"`
	Enabled      bool           `json:"enabled"`
	BackendID    string         `json:"backend_id,omitempty"`
}

func (d AdapterDescriptor) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Version) == "" {
		return ErrInvalidAdapter
	}
	if len(d.Capabilities) == 0 {
		return ErrInvalidAdapter
	}
	return nil
}

// AdapterRegistry owns the stable mapping between descriptors and backends.
type AdapterRegistry interface {
	Register(AdapterDescriptor, Backend) error
	Resolve(Task) (AdapterDescriptor, Backend, error)
	List() []AdapterDescriptor
}

// Backend is the only extension point a future local model must implement.
type Backend interface {
	ID() string
	Run(context.Context, CognitiveInput, AdapterDescriptor) (CognitiveOutput, error)
}

// ModelBackend names the future model implementation explicitly while
// retaining the same typed, non-text Backend contract.
type ModelBackend interface {
	Backend
}

// Scheduler performs deterministic capability matching. It never executes
// actions; it only selects a read-only backend and validates its output.
type Scheduler struct {
	registry AdapterRegistry
}

func NewScheduler(registry AdapterRegistry) *Scheduler {
	return &Scheduler{registry: registry}
}

func (s *Scheduler) Select(task Task) (AdapterDescriptor, Backend, error) {
	if s == nil || s.registry == nil {
		return AdapterDescriptor{}, nil, ErrNoAdapter
	}
	return s.registry.Resolve(task)
}

func (s *Scheduler) Run(ctx context.Context, input CognitiveInput) (CognitiveOutput, error) {
	if err := input.Validate(); err != nil {
		return CognitiveOutput{}, err
	}
	descriptor, backend, err := s.Select(input.Task)
	if err != nil {
		return CognitiveOutput{}, err
	}
	output, err := backend.Run(ctx, input, descriptor)
	if err != nil {
		return CognitiveOutput{}, err
	}
	if output.SchemaVersion == "" {
		output.SchemaVersion = SchemaVersion
	}
	if output.RequestID == "" {
		output.RequestID = input.RequestID
	}
	if output.TaskID == "" {
		output.TaskID = input.Task.ID
	}
	if output.AdapterID == "" {
		output.AdapterID = descriptor.ID
	}
	if output.BackendID == "" {
		output.BackendID = backend.ID()
	}
	if !output.AdvisoryOnly {
		return CognitiveOutput{}, ErrNonAdvisoryOutput
	}
	if err := output.Validate(); err != nil {
		return CognitiveOutput{}, err
	}
	if err := output.ValidateActionSelection(input.ActionCatalog); err != nil {
		return CognitiveOutput{}, err
	}
	return output, nil
}

type registryEntry struct {
	descriptor AdapterDescriptor
	backend    Backend
}

// InMemoryRegistry is deterministic and suitable for production wiring and
// hermetic tests. It is intentionally small until a model backend exists.
type InMemoryRegistry struct {
	entries map[string]registryEntry
}

func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{entries: make(map[string]registryEntry)}
}

func (r *InMemoryRegistry) Register(descriptor AdapterDescriptor, backend Backend) error {
	if r == nil || backend == nil || descriptor.Validate() != nil || strings.TrimSpace(descriptor.BackendID) == "" {
		return ErrInvalidAdapter
	}
	if r.entries == nil {
		r.entries = make(map[string]registryEntry)
	}
	if _, exists := r.entries[descriptor.ID]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateAdapter, descriptor.ID)
	}
	r.entries[descriptor.ID] = registryEntry{descriptor: cloneDescriptor(descriptor), backend: backend}
	return nil
}

func (r *InMemoryRegistry) Resolve(task Task) (AdapterDescriptor, Backend, error) {
	if r == nil {
		return AdapterDescriptor{}, nil, ErrNoAdapter
	}
	wantedCapabilities := set(task.RequestedCapabilities)
	wantedModalities := set(task.RequestedModalities)
	type candidate struct {
		entry registryEntry
		extra int
	}
	items := make([]candidate, 0, len(r.entries))
	for _, entry := range r.entries {
		if !entry.descriptor.Enabled || entry.backend == nil || !containsAll(entry.descriptor.Capabilities, wantedCapabilities) || !containsAll(entry.descriptor.Modalities, wantedModalities) {
			continue
		}
		extra := len(set(entry.descriptor.Capabilities)) - len(wantedCapabilities)
		extra += len(set(entry.descriptor.Modalities)) - len(wantedModalities)
		items = append(items, candidate{entry: entry, extra: extra})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].extra != items[j].extra {
			return items[i].extra < items[j].extra
		}
		return items[i].entry.descriptor.ID < items[j].entry.descriptor.ID
	})
	if len(items) == 0 {
		return AdapterDescriptor{}, nil, ErrNoAdapter
	}
	return cloneDescriptor(items[0].entry.descriptor), items[0].entry.backend, nil
}

func (r *InMemoryRegistry) List() []AdapterDescriptor {
	if r == nil {
		return []AdapterDescriptor{}
	}
	items := make([]AdapterDescriptor, 0, len(r.entries))
	for _, entry := range r.entries {
		items = append(items, cloneDescriptor(entry.descriptor))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func set(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func containsAll(values []string, wanted map[string]struct{}) bool {
	available := set(values)
	for value := range wanted {
		if _, ok := available[value]; !ok {
			return false
		}
	}
	return true
}

func cloneDescriptor(value AdapterDescriptor) AdapterDescriptor {
	value.Capabilities = append([]string(nil), value.Capabilities...)
	value.Modalities = append([]string(nil), value.Modalities...)
	if value.Limits != nil {
		value.Limits = map[string]int{}
		for key, limit := range value.Limits {
			value.Limits[key] = limit
		}
	}
	return value
}
