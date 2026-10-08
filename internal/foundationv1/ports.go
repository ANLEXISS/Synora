package foundationv1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"synora/internal/cognitivecore"
	"synora/internal/discovery"
	"synora/pkg/contract"
)

// DiscoveryBoundaryAdapter reuses the current V1 Discovery payload guard.
type DiscoveryBoundaryAdapter struct{ Boundary *discovery.Boundary }

func (a DiscoveryBoundaryAdapter) Accept(_ context.Context, record Record) (Record, error) {
	if err := ValidateRecord(record); err != nil {
		return Record{}, err
	}
	body, err := json.Marshal(record)
	if err != nil {
		return Record{}, err
	}
	if err := discovery.ValidatePayload(body); err != nil {
		return Record{}, err
	}
	if a.Boundary != nil {
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			return Record{}, err
		}
		if _, err := a.Boundary.NormalizeEvent(contract.Event{ID: record.ID, Type: "foundation." + record.Kind, Source: "discovery", Timestamp: record.CreatedAt, Payload: payload}); err != nil {
			return Record{}, err
		}
	}
	return record, nil
}

// UniversalStoreAdapter stores each foundation record as a normal immutable
// Core commit. Universal Store remains the persistence and idempotency authority.
type UniversalStoreAdapter struct{ Store *cognitivecore.UniversalStore }

func (a UniversalStoreAdapter) PutFoundation(ctx context.Context, record Record) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if a.Store == nil {
		return 0, errors.New("Universal Store is not configured")
	}
	if err := ValidateRecord(record); err != nil {
		return 0, err
	}
	body, err := json.Marshal(record)
	if err != nil {
		return 0, err
	}
	now := record.CreatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	event := contract.Event{ID: "foundation-" + record.ID, Type: "foundation." + record.Kind, Source: "discovery", Timestamp: now, Payload: map[string]any{"record": json.RawMessage(body)}}
	snapshot := a.Store.Snapshot()
	decision := cognitivecore.Decision{SchemaVersion: cognitivecore.DecisionSchemaVersion, Status: "recorded", Mode: "active_dry_run", Source: "foundation-v1", Action: cognitivecore.ActionAssessment{Status: "not_requested", PhysicalActionExecuted: false}, GeneratedAt: now}
	result, err := a.Store.Commit(cognitivecore.Commit{Event: event, Snapshot: snapshot, Decision: decision, CommittedAt: now})
	if err != nil {
		return 0, err
	}
	return result.Revision, nil
}

func (a UniversalStoreAdapter) FoundationSnapshot(ctx context.Context) ([]Record, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if a.Store == nil {
		return nil, 0, errors.New("Universal Store is not configured")
	}
	commits, err := a.Store.History()
	if err != nil {
		return nil, 0, err
	}
	result := make([]Record, 0)
	for _, commit := range commits {
		if !strings.HasPrefix(commit.Event.Type, "foundation.") {
			continue
		}
		wrapped, ok := commit.Event.Payload["record"]
		if !ok {
			continue
		}
		body, err := json.Marshal(wrapped)
		if err != nil {
			return nil, 0, err
		}
		var record Record
		if err := json.Unmarshal(body, &record); err != nil {
			return nil, 0, fmt.Errorf("decode Universal Store foundation record: %w", err)
		}
		result = append(result, record)
	}
	return result, a.Store.Revision(), nil
}

// CognitiveCoreAdapter invokes the existing Core and its SafetyGate with a
// simulated MLP adapter. It does not load or claim a trained model.
type CognitiveCoreAdapter struct {
	Store *cognitivecore.UniversalStore
	Now   func() time.Time
}

func (a CognitiveCoreAdapter) Process(ctx context.Context, record Record, mlp MLPPort, gate GatePort) (CoreOutput, error) {
	if a.Store == nil || mlp == nil || gate == nil {
		return CoreOutput{}, errors.New("Core, MLP and Safety Gate ports are required")
	}
	proposal, err := mlp.Propose(ctx, record)
	if err != nil {
		return CoreOutput{}, err
	}
	allowed, reason, err := gate.Allow(ctx, proposal)
	if err != nil {
		return CoreOutput{}, err
	}
	now := time.Now().UTC()
	if a.Now != nil {
		now = a.Now().UTC()
	}
	capabilities := map[string]bool{}
	if proposal.Capability != "" {
		capabilities[proposal.Capability] = allowed
	}
	core := cognitivecore.Core{Store: a.Store, Encoder: cognitivecore.SnapshotEncoder{}, MLP: simulatedMLPBackend{proposal: proposal}, Gate: cognitivecore.SafetyGate{DryRun: true, Capabilities: capabilities}, Now: func() time.Time { return now }}
	body, err := json.Marshal(record)
	if err != nil {
		return CoreOutput{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return CoreOutput{}, err
	}
	result, err := core.Process(ctx, contract.Event{ID: "foundation-core-" + record.ID, Type: "foundation.evidence." + record.Kind, Source: "discovery", Timestamp: now, Payload: payload})
	if err != nil {
		return CoreOutput{}, err
	}
	decision := result.Commit.Decision.Action.Status
	return CoreOutput{Proposal: proposal, Decision: reason, Allowed: decision == "allowed_dry_run" || decision == "not_requested", StoreRevision: result.Result.Revision, ModelStatus: StatusSimulatedTest}, nil
}

type simulatedMLPBackend struct{ proposal ActionProposal }

func (m simulatedMLPBackend) Run(_ context.Context, _ cognitivecore.EncodedSnapshot, _ cognitivecore.CognitiveSnapshot) (cognitivecore.MLPOutput, map[string]float64, error) {
	return cognitivecore.MLPOutput{DangerLabel: "unknown", Action: cognitivecore.ActionIntent{Action: m.proposal.Action, Capability: m.proposal.Capability, Topology: m.proposal.Zone}}, map[string]float64{}, nil
}

type PipelineInput struct {
	ScenarioID string    `json:"scenario_id"`
	Evidence   Record    `json:"evidence"`
	GateReason string    `json:"gate_reason,omitempty"`
	Now        time.Time `json:"now"`
}

// ValidatePipelineInput freezes the software harness input contract. Test
// adapters such as the MLP proposal, capability state and Safety Gate outcome
// remain explicit ports, not extra evidence fields.
func ValidatePipelineInput(input PipelineInput) error {
	if !validOpaqueID(input.ScenarioID) {
		return errors.New("pipeline input requires an abstract scenario id")
	}
	if !validOpaqueID(input.Evidence.ID) || !validOpaqueID(input.Evidence.Kind) || !input.Evidence.Status.Valid() {
		return errors.New("pipeline input evidence envelope is invalid")
	}
	if input.GateReason != "" && input.GateReason != "safety_gate_denied" {
		return errors.New("pipeline input contains an unsupported Safety Gate reason")
	}
	return nil
}

type PipelineResult struct {
	ScenarioID             string        `json:"scenario_id"`
	Status                 string        `json:"status"`
	Journey                []string      `json:"journey"`
	Revision               uint64        `json:"revision"`
	CoreRevision           uint64        `json:"core_store_revision"`
	Action                 ActionResult  `json:"action"`
	API                    StateSnapshot `json:"api_state"`
	Rejected               string        `json:"rejected,omitempty"`
	PhysicalActionExecuted bool          `json:"physical_action_executed"`
	AudioRendered          bool          `json:"audio_rendered"`
	ModelLoaded            bool          `json:"model_loaded"`
	ExecutorInvoked        bool          `json:"dry_run_executor_invoked"`
}

type Pipeline struct {
	Discovery      DiscoveryPort
	Store          StorePort
	Core           CorePort
	MLP            MLPPort
	Gate           GatePort
	Executor       ExecutorPort
	Communications *CommunicationScheduler
	Now            func() time.Time
}

// Run executes the abstract V1 software boundary. MLP, Safety Gate and
// executor are ports, so deployment requires explicit implementations.
func (p Pipeline) Run(ctx context.Context, input PipelineInput) (PipelineResult, error) {
	if p.Discovery == nil || p.Store == nil || p.Core == nil || p.MLP == nil || p.Gate == nil || p.Executor == nil {
		return PipelineResult{}, errors.New("all foundation pipeline ports must be explicitly configured")
	}
	if err := ValidatePipelineInput(input); err != nil {
		return PipelineResult{}, err
	}
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if p.Now != nil {
		now = p.Now().UTC()
	}
	journey := []string{"discovery_received"}
	accepted, err := p.Discovery.Accept(ctx, input.Evidence)
	if err != nil {
		snapshot := StateSnapshot{SchemaVersion: "synora.foundation-state/v1", Functions: DefaultInitialStatus(),
			Camera:   AssessCamera("unknown", "unknown", "unknown", true, time.Time{}, true),
			Topology: Topology{Status: StatusNotConfigured}, PhysicalActionExecuted: false, AudioRendered: false}
		if validateErr := ValidatePublicState(snapshot); validateErr != nil {
			return PipelineResult{}, validateErr
		}
		journey = append(journey, "discovery_rejected", "rejection_result_structured", "api_projected")
		return PipelineResult{ScenarioID: input.ScenarioID, Status: "rejected", Journey: journey, Rejected: "prohibited_or_invalid_evidence", API: snapshot,
			PhysicalActionExecuted: false, AudioRendered: false, ModelLoaded: false, ExecutorInvoked: false}, nil
	}
	journey = append(journey, "discovery_validated", "evidence_redacted")
	revision, err := p.Store.PutFoundation(ctx, accepted)
	if err != nil {
		return PipelineResult{}, err
	}
	if accepted.Kind == "peripheral" {
		peripheral, ok := asPeripheral(accepted.Payload)
		if !ok {
			return PipelineResult{}, errors.New("invalid abstract peripheral record")
		}
		if err := NewPeripheralRegistry(p.Store).Upsert(ctx, peripheral, now); err != nil {
			return PipelineResult{}, err
		}
	}
	journey = append(journey, "evidence_store_written")
	coreOutput, err := p.Core.Process(ctx, accepted, p.MLP, p.Gate)
	if err != nil {
		return PipelineResult{}, err
	}
	journey = append(journey, "core_processed", "mlp_proposed", "safety_gate_evaluated")
	proposal, allowed := coreOutput.Proposal, coreOutput.Allowed
	reason := coreOutput.Decision
	if !allowed && input.GateReason != "" {
		reason = input.GateReason
	}
	if accepted.Kind == "communication_intent" {
		if p.Communications == nil {
			return PipelineResult{}, errors.New("communication cooldown scheduler is not configured")
		}
		intent, ok := asCommunicationIntent(accepted.Payload)
		if !ok {
			return PipelineResult{}, errors.New("invalid abstract communication intent")
		}
		request, err := p.Communications.Schedule(intent, allowed, now)
		if err != nil {
			return PipelineResult{}, err
		}
		communicationRecord := Record{ID: input.ScenarioID + "-communication", Kind: "communication", Status: StatusSimulatedTest, Payload: request, CreatedAt: now}
		if _, err := p.Store.PutFoundation(ctx, communicationRecord); err != nil {
			return PipelineResult{}, err
		}
	}
	var result ActionResult
	executorInvoked := false
	priorResult, duplicate, err := findActionResult(ctx, p.Store, proposal.RequestID)
	if err != nil {
		return PipelineResult{}, err
	}
	if duplicate {
		result = priorResult
		result.Status = "duplicate"
	} else if !allowed {
		result = ActionResult{RequestID: proposal.RequestID, Status: "blocked", Reason: reason, PeripheralState: "unknown", PhysicalActionExecuted: false, FunctionStatus: StatusDryRun}
	} else {
		executorInvoked = true
		result, err = p.Executor.ExecuteDryRun(ctx, proposal)
		if err != nil {
			return PipelineResult{}, err
		}
	}
	journey = append(journey, "dry_run_executor", "result_store_written")
	resultRecord := Record{ID: "action-result-" + proposal.RequestID + "-" + input.ScenarioID, Kind: "action_result", Status: result.FunctionStatus, Payload: result, CreatedAt: now}
	revision, err = p.Store.PutFoundation(ctx, resultRecord)
	if err != nil {
		return PipelineResult{}, err
	}
	records, revision, err := p.Store.FoundationSnapshot(ctx)
	if err != nil {
		return PipelineResult{}, err
	}
	journey = append(journey, "api_projected")
	snapshot := project(records, revision, result, now)
	if err := ValidatePublicState(snapshot); err != nil {
		return PipelineResult{}, err
	}
	return PipelineResult{ScenarioID: input.ScenarioID, Status: "completed", Journey: journey, Revision: revision, CoreRevision: coreOutput.StoreRevision, Action: result, API: snapshot, PhysicalActionExecuted: false, AudioRendered: false, ModelLoaded: false, ExecutorInvoked: executorInvoked}, nil
}

func findActionResult(ctx context.Context, store StorePort, requestID string) (ActionResult, bool, error) {
	if requestID == "" {
		return ActionResult{}, false, errors.New("action request id is required")
	}
	records, _, err := store.FoundationSnapshot(ctx)
	if err != nil {
		return ActionResult{}, false, err
	}
	for index := len(records) - 1; index >= 0; index-- {
		if records[index].Kind != "action_result" {
			continue
		}
		var result ActionResult
		if !decodePayload(records[index].Payload, &result) {
			continue
		}
		if result.RequestID == requestID {
			return result, true, nil
		}
	}
	return ActionResult{}, false, nil
}

func project(records []Record, revision uint64, action ActionResult, now time.Time) StateSnapshot {
	state := StateSnapshot{SchemaVersion: "synora.foundation-state/v1", Revision: revision, Functions: DefaultInitialStatus(),
		Camera:   AssessCamera("unknown", "unknown", "unknown", true, time.Time{}, true),
		Topology: Topology{Status: StatusNotConfigured}, LastAction: &action, PhysicalActionExecuted: false, AudioRendered: false}
	state.Functions.Discovery = StatusSimulatedTest
	state.Functions.Core = StatusSimulatedTest
	state.Functions.MLP = StatusSimulatedTest
	state.Functions.SafetyGate = StatusDryRun
	state.Functions.Store = StatusSimulatedTest
	state.Functions.Executor = StatusDryRun
	peripherals := make(map[string]Peripheral)
	for _, record := range records {
		switch record.Kind {
		case "camera_health":
			if camera, ok := asCameraHealth(record.Payload); ok {
				state.Camera = camera
				state.Functions.CameraHealth = camera.Status
			}
		case "peripheral":
			if peripheral, ok := asPeripheral(record.Payload); ok {
				peripherals[peripheral.ID] = peripheral
				state.Functions.Peripherals = record.Status
			}
		case "communication":
			if communication, ok := asCommunication(record.Payload); ok {
				state.Communication = &communication
				state.Functions.Voice = communication.TTSStatus
			}
		case "search":
			if search, ok := asSearch(record.Payload); ok {
				search = ExpireSearch(search, now)
				state.Search = &search
				state.Functions.Search = search.Status
			}
		case "topology":
			if topology, ok := asTopology(record.Payload); ok {
				state.Topology = topology
				state.Functions.Topology = topology.Status
			}
		case "correlation":
			if correlation, ok := asCorrelation(record.Payload); ok {
				state.Correlations = append(state.Correlations, correlation)
				state.Functions.Topology = record.Status
			}
		}
	}
	for _, peripheral := range peripherals {
		state.Peripherals = append(state.Peripherals, peripheral)
	}
	sort.Slice(state.Peripherals, func(i, j int) bool { return state.Peripherals[i].ID < state.Peripherals[j].ID })
	if action.FunctionStatus != "" {
		state.Functions.Action = action.FunctionStatus
	}
	state.Functions.API = StatusDryRun
	return state
}

func decodePayload(value any, out any) bool {
	body, err := json.Marshal(value)
	return err == nil && json.Unmarshal(body, out) == nil
}
func asCameraHealth(v any) (CameraHealth, bool) { var x CameraHealth; return x, decodePayload(v, &x) }
func asPeripheral(v any) (Peripheral, bool)     { var x Peripheral; return x, decodePayload(v, &x) }
func asCommunication(v any) (CommunicationRequest, bool) {
	var x CommunicationRequest
	return x, decodePayload(v, &x)
}
func asCommunicationIntent(v any) (CommunicationIntent, bool) {
	var x CommunicationIntent
	return x, decodePayload(v, &x)
}
func asSearch(v any) (SearchState, bool)      { var x SearchState; return x, decodePayload(v, &x) }
func asTopology(v any) (Topology, bool)       { var x Topology; return x, decodePayload(v, &x) }
func asCorrelation(v any) (Correlation, bool) { var x Correlation; return x, decodePayload(v, &x) }

// Explicit simulated ports are used by the central software harness only.
type SimulatedMLP struct{ Proposal ActionProposal }

func (m SimulatedMLP) Propose(_ context.Context, _ Record) (ActionProposal, error) {
	return m.Proposal, nil
}

type SimulatedGate struct {
	Allowed bool
	Reason  string
}

func (g SimulatedGate) Allow(context.Context, ActionProposal) (bool, string, error) {
	return g.Allowed, g.Reason, nil
}

type SimulatedDryRun struct {
	Executor   *DryRunExecutor
	Capability string
}

func (e SimulatedDryRun) ExecuteDryRun(_ context.Context, proposal ActionProposal) (ActionResult, error) {
	return e.Executor.Execute(proposal, true, e.Capability)
}
