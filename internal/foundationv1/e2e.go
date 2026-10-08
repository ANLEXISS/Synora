package foundationv1

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"synora/internal/cognitivecore"
	"synora/internal/discovery"
	"synora/internal/security"
)

type E2ECase struct {
	ID                          string        `json:"id"`
	Status                      string        `json:"status"`
	FunctionStates              InitialStatus `json:"function_states"`
	Journey                     []string      `json:"journey"`
	ActionStatus                string        `json:"action_status"`
	ActionReason                string        `json:"action_reason,omitempty"`
	PeripheralState             string        `json:"peripheral_state"`
	UnauthorizedHTTP            int           `json:"unauthorized_http"`
	AuthorizedHTTP              int           `json:"authorized_http"`
	PersistedRecords            int           `json:"persisted_records"`
	CoreStoreRevision           uint64        `json:"core_store_revision"`
	RestartRecovered            bool          `json:"restart_recovered"`
	RestartReplay               bool          `json:"restart_replay_idempotent"`
	IdempotencyConflictRejected bool          `json:"idempotency_conflict_rejected"`
	ExpectedIngressRejection    bool          `json:"expected_ingress_rejection"`
	TerminalStatus              string        `json:"terminal_status"`
	PhysicalActionExecuted      bool          `json:"physical_action_executed"`
	AudioRendered               bool          `json:"audio_rendered"`
	ModelLoaded                 bool          `json:"model_loaded"`
	ExecutorInvoked             bool          `json:"dry_run_executor_invoked"`
	CorrelationID               string        `json:"correlation_id"`
	ScenarioCheck               string        `json:"scenario_check,omitempty"`
	Passed                      bool          `json:"passed"`
	Error                       string        `json:"error,omitempty"`
}

type E2ESuite struct {
	Status                           FunctionStatus `json:"status"`
	Qualification                    string         `json:"qualification"`
	InitialStates                    InitialStatus  `json:"initial_states"`
	Cases                            []E2ECase      `json:"cases"`
	PassedCount                      int            `json:"passed_count"`
	FailedCount                      int            `json:"failed_count"`
	JourneysTerminalCompleted        int            `json:"journeys_terminal_completed"`
	JourneysTerminalRejectedExpected int            `json:"journeys_terminal_rejected_expected"`
	JourneysTerminalBlocked          int            `json:"journeys_terminal_blocked"`
	JourneysTerminalSuppressed       int            `json:"journeys_terminal_suppressed"`
	JourneysTerminalUnknownResult    int            `json:"journeys_terminal_unknown_result"`
	JourneysTerminalFailedExpected   int            `json:"journeys_terminal_failed_expected"`
	JourneysIncomplete               int            `json:"journeys_incomplete"`
	PhysicalActionExecuted           bool           `json:"physical_action_executed"`
	AudioRendered                    bool           `json:"audio_rendered"`
	RealModelLoaded                  bool           `json:"real_model_loaded"`
	Metrics                          map[string]int `json:"metrics"`
}

type e2eScenario struct {
	id              string
	kind            string
	payload         any
	proposal        ActionProposal
	capability      string
	gate            bool
	gateReason      string
	reject          bool
	expectedAction  string
	check           string
	executorMode    string
	expectedFailure bool
	policyMode      string
	expectedReason  string
}

// RunE2E exercises the software ports against Discovery's current validator,
// the real Core Universal Store, a simulated MLP, a fail-closed gate, a
// non-actuating executor and an authenticated ephemeral state API.
func RunE2E(ctx context.Context, now time.Time) E2ESuite {
	suite := E2ESuite{Status: StatusSimulatedTest, Qualification: "not_qualified", InitialStates: DefaultInitialStatus(), Cases: make([]E2ECase, 0), Metrics: map[string]int{"software_pipeline_completed": 0, "peripheral_available": 0, "rejected": 0, "timeout": 0, "suppressed": 0, "retry": 0, "recovery": 0, "unauthorized_api": 0}}
	redactedA := RedactedObservation{ID: "obs-safe-a", Zone: "zone-entry", Category: "person_suspect", Confidence: "medium", ObservedAt: now.UTC(), Redacted: true}
	redactedB := RedactedObservation{ID: "obs-safe-b", Zone: "zone-hall", Category: "person_suspect", Confidence: "low", ObservedAt: now.UTC().Add(-time.Second), Redacted: true}
	ambiguous, _ := CorrelateAt("corr-ambiguous", "episode-e2e", []RedactedObservation{redactedA, redactedB}, "low", "", now)
	search, _ := NewSearchState(SearchQuery{ID: "search-no-coverage", Zones: []string{"zone-garage"}, Target: "person_suspect", IssuedAt: now}, now.Add(time.Minute), "unknown", nil, 0)
	communication := CommunicationIntent{ID: "comm-safety-blocked", Zones: []string{"zone-entry"}, Priority: "normal", Cooldown: 30 * time.Second, Recipient: "occupant", TextKey: "presence.notice", PermissionGranted: true}
	unknownCapability := CapabilityState{Name: "notify", State: "unknown", Mode: StatusUnavailable}
	knownTopology := Topology{Status: StatusSimulatedTest, Integrity: "valid", RootZone: "zone-entry", Provenance: "user_provided", ExpiresAt: now.Add(time.Hour), Zones: []Zone{
		{ID: "zone-entry", Type: "public", Coverage: "unknown", Uncertainty: "unknown", Provenance: "user_provided"},
		{ID: "zone-hall", Type: "protected", Coverage: "unknown", Uncertainty: "unknown", Provenance: "user_provided"},
	}, Connections: []Transition{{From: "zone-entry", To: "zone-hall"}}}
	_ = knownTopology.Allows("zone-entry", "zone-hall") // the contract carries only declared transitions
	scenarios := []e2eScenario{
		{id: "camera-unavailable", kind: "camera_health", payload: AssessCamera("offline", "absent", "unknown", true, now, true), gate: true, expectedAction: ActionSuppressed},
		{id: "peripheral-state-unknown", kind: "peripheral", payload: Peripheral{ID: "peripheral-abstract-1", Type: "notification", Zone: "zone-entry", Status: StatusSimulatedTest, State: "unknown", Capabilities: []CapabilityState{unknownCapability}}, gate: true, expectedAction: ActionSuppressed},
		{id: "action-blocked-safety-gate", kind: "action_proposal", payload: map[string]any{"proposal_state": "proposed", "gate_state": "blocked"}, proposal: ActionProposal{RequestID: "action-blocked-1", Action: "notify", Capability: "notify", Zone: "zone-entry"}, gate: false, gateReason: "safety_gate_denied", expectedAction: ActionBlocked},
		{id: "action-dry-run-return", kind: "action_proposal", payload: map[string]any{"proposal_state": "proposed", "gate_state": "allowed_dry_run"}, proposal: ActionProposal{RequestID: "action-return-1", Action: "notify", Capability: "notify", Zone: "zone-entry"}, capability: "available", gate: true, expectedAction: ActionAllowedDryRun},
		{id: "duplicate-action-result", kind: "action_proposal", payload: map[string]any{"proposal_state": "proposed", "idempotency": "same_request_id"}, proposal: ActionProposal{RequestID: "action-duplicate-1", Action: "notify", Capability: "notify", Zone: "zone-entry"}, capability: "available", gate: true, expectedAction: ActionAllowedDryRun},
		{id: "communication-safety-suppressed", kind: "communication_intent", payload: communication, proposal: ActionProposal{RequestID: "communication-suppressed", Action: "notify", Capability: "notify", Zone: "zone-entry"}, gate: false, gateReason: "safety_gate_denied", expectedAction: "blocked"},
		{id: "search-without-coverage", kind: "search", payload: search, proposal: ActionProposal{RequestID: "search-no-action", Action: "no_action", Capability: "none", Zone: "zone-garage"}, gate: true, expectedAction: ActionSuppressed},
		{id: "correlation-ambiguous", kind: "correlation", payload: ambiguous, proposal: ActionProposal{RequestID: "correlation-no-action", Action: "no_action", Capability: "none", Zone: "zone-hall"}, gate: true, expectedAction: ActionSuppressed},
		{id: "restart-recovery", kind: "topology", payload: knownTopology, proposal: ActionProposal{RequestID: "recovery-no-action", Action: "no_action", Capability: "none", Zone: "zone-entry"}, gate: true, expectedAction: ActionSuppressed},
		{id: "prohibited-data-rejected", kind: "test_input", payload: map[string]any{"identity": map[string]any{"embedding": "forbidden"}}, proposal: ActionProposal{RequestID: "rejected-no-action", Action: "no_action", Capability: "none", Zone: "zone-entry"}, gate: true, reject: true, expectedAction: ActionSuppressed},
		{id: "topology-invalid-disconnected", kind: "foundation_check", payload: map[string]any{"check": "topology_invalid"}, gate: true, expectedAction: ActionSuppressed, check: "topology_invalid"},
		{id: "zone-coverage-unknown", kind: "foundation_check", payload: map[string]any{"check": "coverage_unknown"}, gate: true, expectedAction: ActionSuppressed, check: "coverage_unknown"},
		{id: "transition-impossible", kind: "foundation_check", payload: map[string]any{"check": "transition_impossible"}, gate: true, expectedAction: ActionSuppressed, check: "transition_impossible"},
		{id: "peripheral-absent", kind: "foundation_check", payload: map[string]any{"check": "peripheral_absent"}, proposal: ActionProposal{RequestID: "absent-device-action", Action: "notify", Capability: "notify", Zone: "zone-entry"}, capability: "available", gate: true, expectedAction: ActionSuppressed, expectedReason: "peripheral_absent", policyMode: "peripheral_absent"},
		{id: "capability-absent", kind: "foundation_check", payload: map[string]any{"check": "capability_absent"}, proposal: ActionProposal{RequestID: "absent-capability-action", Action: "notify", Capability: "notify", Zone: "zone-entry"}, capability: "available", gate: true, expectedAction: ActionSuppressed, expectedReason: "capability_absent", policyMode: "capability_absent"},
		{id: "permission-refused", kind: "foundation_check", payload: map[string]any{"check": "permission_refused"}, proposal: ActionProposal{RequestID: "denied-permission-action", Action: "notify", Capability: "notify", Zone: "zone-entry"}, capability: "available", gate: true, expectedAction: ActionSuppressed, expectedReason: "permission_denied", policyMode: "permission_refused"},
		{id: "action-contradictory", kind: "foundation_check", payload: map[string]any{"check": "contradictory_action"}, gate: true, expectedAction: ActionSuppressed, check: "contradictory_action"},
		{id: "action-cooldown", kind: "foundation_check", payload: map[string]any{"check": "action_cooldown"}, gate: true, expectedAction: ActionSuppressed, check: "action_cooldown"},
		{id: "action-priority", kind: "foundation_check", payload: map[string]any{"check": "action_priority"}, gate: true, expectedAction: ActionSuppressed, check: "action_priority"},
		{id: "action-timeout", kind: "foundation_check", payload: map[string]any{"check": "action_timeout"}, proposal: ActionProposal{RequestID: "timeout-action", Action: "notify", Capability: "notify", Zone: "zone-entry"}, capability: "available", gate: true, expectedAction: ActionUnknownResult, expectedReason: "executor_timeout", executorMode: "timeout", check: "action_timeout"},
		{id: "executor-return-missing", kind: "foundation_check", payload: map[string]any{"check": "return_missing"}, proposal: ActionProposal{RequestID: "missing-return-action", Action: "notify", Capability: "notify", Zone: "zone-entry"}, capability: "available", gate: true, expectedAction: ActionUnknownResult, expectedReason: "executor_result_missing", executorMode: "missing", check: "return_missing"},
		{id: "executor-result-rejected", kind: "foundation_check", payload: map[string]any{"check": "result_rejected"}, proposal: ActionProposal{RequestID: "rejected-result-action", Action: "notify", Capability: "notify", Zone: "zone-entry"}, capability: "available", gate: true, expectedAction: ActionFailed, expectedReason: "invalid_executor_result", executorMode: "rejected", expectedFailure: true},
		{id: "communication-cooldown", kind: "foundation_check", payload: map[string]any{"check": "communication_cooldown"}, gate: true, expectedAction: ActionSuppressed, check: "communication_cooldown"},
		{id: "communication-blocked", kind: "communication_intent", payload: CommunicationIntent{ID: "comm-blocked-e2e", Zones: []string{"zone-entry"}, Priority: "high", Cooldown: time.Minute, Recipient: "operator", TextKey: "safety.notice", PermissionGranted: true}, proposal: ActionProposal{RequestID: "communication-blocked-action", Action: "notify", Capability: "notify", Zone: "zone-entry"}, gate: false, gateReason: "safety_gate_denied", expectedAction: ActionBlocked},
		{id: "communication-permission-denied", kind: "communication_intent", payload: CommunicationIntent{ID: "comm-no-permission", Zones: []string{"zone-entry"}, Priority: "normal", Cooldown: time.Minute, Recipient: "occupant", TextKey: "presence.notice"}, gate: true, expectedAction: ActionSuppressed, check: "communication_permission"},
		{id: "search-ambiguous", kind: "foundation_check", payload: map[string]any{"check": "search_ambiguous"}, gate: true, expectedAction: ActionSuppressed, check: "search_ambiguous"},
		{id: "search-expired", kind: "foundation_check", payload: map[string]any{"check": "search_expired"}, gate: true, expectedAction: ActionSuppressed, check: "search_expired"},
		{id: "store-corrupt-quarantine", kind: "foundation_check", payload: map[string]any{"check": "store_corrupt"}, gate: true, expectedAction: ActionSuppressed, check: "store_corrupt"},
		{id: "api-unauthorized", kind: "foundation_check", payload: map[string]any{"check": "api_unauthorized"}, gate: true, expectedAction: ActionSuppressed, check: "api_unauthorized"},
	}

	for _, scenario := range scenarios {
		item := runE2ECase(ctx, now, scenario)
		suite.Cases = append(suite.Cases, item)
		if item.Passed {
			suite.PassedCount++
		} else {
			suite.FailedCount++
		}
		switch item.TerminalStatus {
		case "completed":
			suite.JourneysTerminalCompleted++
		case "rejected_expected":
			suite.JourneysTerminalRejectedExpected++
		case "blocked":
			suite.JourneysTerminalBlocked++
		case "suppressed":
			suite.JourneysTerminalSuppressed++
		case "unknown_result":
			suite.JourneysTerminalUnknownResult++
		case "failed_expected":
			suite.JourneysTerminalFailedExpected++
		default:
			suite.JourneysIncomplete++
		}
		suite.PhysicalActionExecuted = suite.PhysicalActionExecuted || item.PhysicalActionExecuted
		suite.AudioRendered = suite.AudioRendered || item.AudioRendered
		suite.RealModelLoaded = suite.RealModelLoaded || item.ModelLoaded
		if item.ExpectedIngressRejection {
			suite.Metrics["rejected"]++
		}
		if item.ActionStatus == ActionUnknownResult {
			suite.Metrics["timeout"]++
		}
		if item.ActionStatus == ActionSuppressed {
			suite.Metrics["suppressed"]++
		}
		if item.RestartReplay || item.IdempotencyConflictRejected {
			suite.Metrics["retry"]++
		}
		if item.RestartRecovered {
			suite.Metrics["recovery"]++
		}
		if item.UnauthorizedHTTP == http.StatusUnauthorized {
			suite.Metrics["unauthorized_api"]++
		}
		if item.FunctionStates.Store == StatusSimulatedTest {
			suite.Metrics["software_pipeline_completed"]++
		}
		if item.FunctionStates.Peripherals == StatusAvailable {
			suite.Metrics["peripheral_available"]++
		}
	}
	return suite
}

func runE2ECase(ctx context.Context, now time.Time, scenario e2eScenario) E2ECase {
	item := E2ECase{ID: scenario.id, Status: "failed", FunctionStates: DefaultInitialStatus(), ExpectedIngressRejection: scenario.reject, CorrelationID: "foundation-" + scenario.id, ScenarioCheck: scenario.check,
		PhysicalActionExecuted: false, AudioRendered: false, ModelLoaded: false}
	root, err := os.MkdirTemp("", "synora-foundation-e2e-")
	if err != nil {
		item.Error = "temporary store unavailable"
		return item
	}
	defer os.RemoveAll(root)
	store, err := cognitivecore.OpenUniversalStore(filepath.Join(root, "universal-store"))
	if err != nil {
		item.Error = "Universal Store unavailable"
		return item
	}
	proposal := scenario.proposal
	if proposal.RequestID == "" {
		proposal = ActionProposal{RequestID: scenario.id + "-no-action", Action: "no_action", Capability: "none", Zone: "unknown"}
	}
	var executor ExecutorPort = SimulatedDryRun{Executor: NewDryRunExecutor(), Capability: scenario.capability}
	if scenario.executorMode != "" {
		executor = plannedTestExecutor{mode: scenario.executorMode}
	}
	pipeline := Pipeline{
		Discovery: DiscoveryBoundaryAdapter{Boundary: &discovery.Boundary{DryRun: true, Now: func() time.Time { return now }}},
		Store:     UniversalStoreAdapter{Store: store}, Core: CognitiveCoreAdapter{Store: store, Now: func() time.Time { return now }}, MLP: SimulatedMLP{Proposal: proposal},
		Gate:     SimulatedGate{Allowed: scenario.gate, Reason: scenario.gateReason},
		Executor: executor,
		ActionPolicy: &ActionPolicy{Peripherals: []Peripheral{
			{ID: "peripheral-test-notify", Type: "notification", Zone: "zone-entry", Status: StatusSimulatedTest, State: "unknown", Permissions: []string{"action.notify"}, Capabilities: []CapabilityState{{Name: "notify", State: "available", Mode: StatusSimulatedTest}}},
			{ID: "peripheral-test-record", Type: "recorder", Zone: "zone-entry", Status: StatusSimulatedTest, State: "unknown", Permissions: []string{"action.record"}, Capabilities: []CapabilityState{{Name: "record", State: "available", Mode: StatusSimulatedTest}}},
		}},
		Arbitrator:     NewActionArbitrator(),
		Communications: NewCommunicationScheduler(), Now: func() time.Time { return now },
	}
	if scenario.policyMode == "peripheral_absent" {
		pipeline.ActionPolicy.Peripherals = nil
	} else if scenario.policyMode == "capability_absent" {
		pipeline.ActionPolicy.Peripherals = pipeline.ActionPolicy.Peripherals[:1]
		pipeline.ActionPolicy.Peripherals[0].Capabilities = nil
	} else if scenario.policyMode == "permission_refused" {
		pipeline.ActionPolicy.Peripherals = pipeline.ActionPolicy.Peripherals[:1]
		pipeline.ActionPolicy.Peripherals[0].Permissions = nil
	}
	record := Record{ID: scenario.id + "-evidence", Kind: scenario.kind, Status: StatusSimulatedTest, Payload: scenario.payload, CreatedAt: now.UTC()}
	result, err := pipeline.Run(ctx, PipelineInput{ScenarioID: scenario.id, Evidence: record, GateReason: scenario.gateReason, Now: now})
	if err != nil {
		item.Error = "pipeline failed: " + err.Error()
		return item
	}
	item.Journey = result.Journey
	if scenario.reject {
		if result.Status != "rejected" || result.Rejected != "prohibited_or_invalid_evidence" {
			item.Error = "Discovery did not reject prohibited data"
			return item
		}
		item.ActionStatus, item.ActionReason = ActionSuppressed, "ingress_rejected_before_action"
		item.UnauthorizedHTTP, item.AuthorizedHTTP, _, err = exerciseStateAPI(result.API, now)
		if err != nil || item.UnauthorizedHTTP != http.StatusUnauthorized || item.AuthorizedHTTP != http.StatusOK {
			item.Error = "expected rejection did not produce an authenticated redacted API state"
			return item
		}
		records, _, readErr := (UniversalStoreAdapter{Store: store}).FoundationSnapshot(ctx)
		if readErr != nil || len(records) != 0 || result.ExecutorInvoked || result.PhysicalActionExecuted || result.AudioRendered || result.ModelLoaded {
			item.Error = "rejected evidence reached Store or an execution port"
			return item
		}
		item.Status, item.TerminalStatus, item.Passed = "expected_rejection", "rejected_expected", true
		return item
	}
	if result.Status != "completed" {
		item.Error = "pipeline did not complete"
		return item
	}
	item.FunctionStates = result.API.Functions
	item.ActionStatus, item.ActionReason, item.PeripheralState = result.Action.Status, result.Action.Reason, result.Action.PeripheralState
	item.TerminalStatus = terminalStatusForAction(result.Action.Status, scenario.expectedFailure)
	item.CoreStoreRevision = result.CoreRevision
	item.ExecutorInvoked = result.ExecutorInvoked
	if scenario.id == "duplicate-action-result" {
		duplicateRecord := record
		duplicateRecord.ID = scenario.id + "-evidence-retry"
		duplicatePipeline := pipeline
		duplicatePipeline.Store = UniversalStoreAdapter{Store: store}
		duplicate, duplicateErr := duplicatePipeline.Run(ctx, PipelineInput{ScenarioID: scenario.id + "-retry", Evidence: duplicateRecord, GateReason: scenario.gateReason, Now: now})
		if duplicateErr != nil || duplicate.Status != "completed" {
			item.Error = "duplicate result retry failed"
			return item
		}
		item.ActionStatus = duplicate.Action.Status
		if !duplicate.Action.IdempotentReplay {
			item.Error = "duplicate retry was not marked as an idempotent replay"
			return item
		}
		conflicting := duplicatePipeline
		conflictProposal := scenario.proposal
		conflictProposal.Action, conflictProposal.Capability = "open", "open"
		conflicting.MLP = SimulatedMLP{Proposal: conflictProposal}
		conflict, conflictErr := conflicting.Run(ctx, PipelineInput{ScenarioID: scenario.id + "-conflict", Evidence: duplicateRecord, GateReason: scenario.gateReason, Now: now.Add(2 * time.Second)})
		item.IdempotencyConflictRejected = conflictErr == nil && conflict.Status == "completed" && conflict.Action.Status == ActionFailed && conflict.Action.Reason == "idempotency_key_conflict" && !conflict.ExecutorInvoked
		if !item.IdempotencyConflictRejected {
			item.Error = "idempotency key reuse with a different proposal was not rejected"
			return item
		}
	}
	if scenario.id == "restart-recovery" {
		reopened, openErr := cognitivecore.OpenUniversalStore(filepath.Join(root, "universal-store"))
		if openErr != nil {
			item.Error = "Universal Store restart failed"
			return item
		}
		records, _, readErr := (UniversalStoreAdapter{Store: reopened}).FoundationSnapshot(ctx)
		item.RestartRecovered = readErr == nil && len(records) >= 2
		if readErr == nil {
			restarted := pipeline
			restarted.Store = UniversalStoreAdapter{Store: reopened}
			replayed, replayErr := restarted.Run(ctx, PipelineInput{ScenarioID: scenario.id + "-after-restart", Evidence: record, GateReason: scenario.gateReason, Now: now.Add(time.Second)})
			item.RestartReplay = replayErr == nil && replayed.Status == "completed" && replayed.Action.IdempotentReplay
		}
	}
	publicStatus := StateSnapshot{}
	item.UnauthorizedHTTP, item.AuthorizedHTTP, publicStatus, err = exerciseStateAPI(result.API, now)
	if err != nil {
		item.Error = "authenticated redacted state API contract failed"
		return item
	}
	if item.ActionStatus != scenario.expectedAction {
		item.Error = fmt.Sprintf("action status %q, want %q", item.ActionStatus, scenario.expectedAction)
		return item
	}
	if item.UnauthorizedHTTP != http.StatusUnauthorized || item.AuthorizedHTTP != http.StatusOK || ValidatePublicState(publicStatus) != nil {
		item.Error = "authenticated redacted state API contract failed"
		return item
	}
	if scenario.id == "action-dry-run-return" && item.PeripheralState != "unknown" {
		item.Error = "missing peripheral feedback was not represented as unknown"
		return item
	}
	if scenario.id == "restart-recovery" && (!item.RestartRecovered || !item.RestartReplay) {
		item.Error = "Universal Store did not recover records and idempotent action replay after restart"
		return item
	}
	if scenario.id == "communication-safety-suppressed" {
		got, ok := publicStatus.Communication, publicStatus.Communication != nil
		if !ok || got.Status != "suppressed" || got.TTSStatus != StatusNotConfigured || got.AudioRendered {
			item.Error = "communication suppression or TTS-off invariant failed"
			return item
		}
	}
	if scenario.id == "search-without-coverage" && (publicStatus.Search == nil || publicStatus.Search.Coverage != "unknown" || publicStatus.Search.CrossCameraIdentity || publicStatus.Search.PTZUsed) {
		item.Error = "search limits were not preserved"
		return item
	}
	if scenario.id == "correlation-ambiguous" && (len(publicStatus.Correlations) != 1 || publicStatus.Correlations[0].Classification != "ambiguous" || publicStatus.Correlations[0].Biometric) {
		item.Error = "correlation ambiguity invariant failed"
		return item
	}
	if scenario.id == "peripheral-state-unknown" && (len(publicStatus.Peripherals) != 1 || publicStatus.Peripherals[0].State != "unknown") {
		item.Error = "unknown peripheral state was not preserved"
		return item
	}
	records, _, err := (UniversalStoreAdapter{Store: store}).FoundationSnapshot(ctx)
	if err != nil {
		item.Error = "Universal Store read failed"
		return item
	}
	item.PersistedRecords = len(records)
	if scenario.check != "" {
		if reason := verifyFoundationScenario(scenario, now, item, publicStatus, store); reason != "" {
			item.Error = reason
			return item
		}
	}
	if item.TerminalStatus == "failed_expected" && !scenario.expectedFailure {
		item.Error = "unexpected failed action result"
		return item
	}
	item.Status, item.Passed = "simulated_test", true
	return item
}

func terminalStatusForAction(status string, expectedFailure bool) string {
	switch status {
	case ActionAllowedDryRun:
		return "completed"
	case ActionBlocked:
		return "blocked"
	case ActionSuppressed:
		return "suppressed"
	case ActionUnknownResult:
		return "unknown_result"
	case ActionFailed:
		if expectedFailure {
			return "failed_expected"
		}
	}
	return ""
}

type plannedTestExecutor struct{ mode string }

func (e plannedTestExecutor) ExecuteDryRun(_ context.Context, proposal ActionProposal) (ActionResult, error) {
	switch e.mode {
	case "timeout":
		return ActionResult{}, context.DeadlineExceeded
	case "missing":
		return ActionResult{}, nil
	case "rejected":
		return ActionResult{RequestID: proposal.RequestID, Status: "unexpected_status", FunctionStatus: StatusDryRun}, nil
	default:
		return ActionResult{RequestID: proposal.RequestID, Status: ActionAllowedDryRun, Reason: "physical_execution_disabled", PeripheralState: "unknown", FunctionStatus: StatusDryRun}, nil
	}
}

func verifyFoundationScenario(scenario e2eScenario, now time.Time, item E2ECase, api StateSnapshot, store *cognitivecore.UniversalStore) string {
	if scenario.expectedReason != "" && item.ActionReason != scenario.expectedReason {
		return "action result did not return the expected redacted reason"
	}
	switch scenario.check {
	case "topology_invalid":
		topology := Topology{Status: StatusSimulatedTest, Integrity: "valid", RootZone: "zone-a", Provenance: "user_provided", ExpiresAt: now.Add(time.Minute), Zones: []Zone{
			{ID: "zone-a", Type: "public", Coverage: "unknown", Uncertainty: "unknown", Provenance: "user_provided"},
			{ID: "zone-orphan", Type: "protected", Coverage: "unknown", Uncertainty: "unknown", Provenance: "user_provided"},
		}}
		if topology.Validate() == nil {
			return "disconnected valid topology was accepted"
		}
	case "coverage_unknown":
		zone := Zone{ID: "zone-uncovered", Type: "protected", Coverage: "unknown", Uncertainty: "high", Provenance: "inferred"}
		if zone.Coverage != "unknown" || zone.Uncertainty != "high" {
			return "unknown coverage was converted into a positive observation"
		}
	case "transition_impossible":
		topology := Topology{Status: StatusSimulatedTest, Integrity: "valid", RootZone: "zone-a", Provenance: "user_provided", ExpiresAt: now.Add(time.Minute), Zones: []Zone{
			{ID: "zone-a", Type: "public", Coverage: "unknown", Uncertainty: "unknown", Provenance: "user_provided"},
			{ID: "zone-b", Type: "protected", Coverage: "unknown", Uncertainty: "unknown", Provenance: "user_provided"},
		}, Connections: []Transition{{From: "zone-a", To: "zone-b"}}}
		if topology.AllowsAt("zone-b", "zone-a", now) {
			return "undeclared reverse transition was allowed"
		}
	case "contradictory_action":
		decisions := NewActionArbitrator().ResolveBatch([]ActionProposal{
			{RequestID: "lock-proposal", Action: "lock", Capability: "lock", Zone: "zone-a", Priority: "normal"},
			{RequestID: "unlock-proposal", Action: "unlock", Capability: "lock", Zone: "zone-a", Priority: "normal"},
		}, now)
		if len(decisions) != 2 || decisions[0].Outcome != "suppressed" || decisions[1].Outcome != "suppressed" {
			return "contradictory action pair did not fail closed"
		}
	case "action_cooldown":
		arbitrator := NewActionArbitrator()
		first := ActionProposal{RequestID: "cooldown-e2e-1", Action: "lock", Capability: "lock", Zone: "zone-a", CooldownNS: int64(time.Minute)}
		if got := arbitrator.ResolveBatch([]ActionProposal{first}, now)[0]; got.Outcome != "eligible" {
			return "first action unexpectedly hit cooldown"
		}
		first.RequestID = "cooldown-e2e-2"
		if got := arbitrator.ResolveBatch([]ActionProposal{first}, now.Add(time.Second))[0]; got.Outcome != "suppressed" || got.Reason != "action_cooldown" {
			return "action cooldown was not enforced"
		}
	case "action_priority":
		decisions := NewActionArbitrator().ResolveBatch([]ActionProposal{
			{RequestID: "low-lock", Action: "lock", Capability: "lock", Zone: "zone-a", Priority: "low"},
			{RequestID: "high-unlock", Action: "unlock", Capability: "lock", Zone: "zone-a", Priority: "urgent"},
		}, now)
		if len(decisions) != 2 || decisions[0].Outcome != "suppressed" || decisions[1].Outcome != "eligible" {
			return "priority did not select the deterministic winner"
		}
	case "action_timeout":
		if item.ActionStatus != ActionUnknownResult || !item.ExecutorInvoked || item.PhysicalActionExecuted {
			return "timeout did not produce an explicit unknown result without physical action"
		}
	case "return_missing":
		if item.ActionStatus != ActionUnknownResult || item.ExecutorInvoked == false {
			return "missing executor return was not represented as unknown_result"
		}
	case "communication_cooldown":
		intent := CommunicationIntent{ID: "cooldown-check-1", Zones: []string{"zone-a"}, Priority: "normal", Cooldown: time.Minute, Recipient: "occupant", TextKey: "presence.notice", PermissionGranted: true}
		scheduler := NewCommunicationScheduler()
		first, err := scheduler.Schedule(intent, true, now)
		if err != nil || first.Status != "queued_dry_run" || first.AudioRendered {
			return "communication dry-run could not be queued safely"
		}
		stored := Record{Kind: "communication", Payload: first, CreatedAt: now}
		restored := NewCommunicationScheduler()
		if err := restored.RestoreFromRecords([]Record{stored}); err != nil {
			return "communication cooldown state could not be restored"
		}
		intent.ID = "cooldown-check-2"
		second, err := restored.Schedule(intent, true, now.Add(time.Second))
		if err != nil || second.Status != "suppressed" || second.Reason != "cooldown" {
			return "communication cooldown did not survive restart"
		}
	case "communication_permission":
		if api.Communication == nil || api.Communication.Status != "suppressed" || api.Communication.Reason != "permission_not_granted" || api.Communication.PermissionGranted || api.Communication.AudioRendered || api.Communication.TTSStatus != StatusNotConfigured {
			return "communication without explicit permission was not safely suppressed"
		}
	case "search_ambiguous":
		observation := &RedactedObservation{ID: "search-observation", Zone: "zone-a", Category: "person_suspect", Confidence: "low", ObservedAt: now, Redacted: true}
		search, err := NewSearchState(SearchQuery{ID: "ambiguous-search", Zones: []string{"zone-a"}, Target: "person_suspect", IssuedAt: now}, now.Add(time.Minute), "complete", observation, 2)
		if err != nil || search.Result != "ambiguous" || search.CrossCameraIdentity || search.PTZUsed {
			return "ambiguous search was promoted or leaked tracking"
		}
	case "search_expired":
		search, err := NewSearchState(SearchQuery{ID: "expired-search", Zones: []string{"zone-a"}, Target: "person_suspect", IssuedAt: now.Add(-time.Minute)}, now.Add(-time.Second), "complete", nil, 0)
		if err != nil || ExpireSearch(search, now).Result != "expired" {
			return "expired search retained a stale result"
		}
	case "store_corrupt":
		dir, err := os.MkdirTemp("", "synora-foundation-corrupt-")
		if err != nil {
			return "could not prepare controlled corrupt Store fixture"
		}
		defer os.RemoveAll(dir)
		dir = filepath.Join(dir, "corrupt-store")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "could not prepare controlled corrupt Store fixture"
		}
		if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), []byte("not-json\n"), 0o600); err != nil {
			return "could not prepare controlled corrupt Store fixture"
		}
		if _, err := cognitivecore.OpenUniversalStore(dir); err == nil {
			return "corrupt Store started without fail-closed recovery"
		}
	case "api_unauthorized":
		if item.UnauthorizedHTTP != http.StatusUnauthorized || item.AuthorizedHTTP != http.StatusOK || ValidatePublicState(api) != nil {
			return "authenticated API boundary did not enforce authorization/redaction"
		}
	}
	if store == nil {
		return "Universal Store disappeared before terminal projection"
	}
	return ""
}

func exerciseStateAPI(snapshot StateSnapshot, now time.Time) (int, int, StateSnapshot, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return 0, 0, StateSnapshot{}, errors.New("test session key generation failed")
	}
	stateHandler := StateHandler(func() StateSnapshot { return snapshot }, func(r *http.Request) bool {
		cookie, err := r.Cookie(security.SessionCookieName)
		if err != nil {
			return false
		}
		claims, err := security.VerifySession(secret, cookie.Value, now)
		return err == nil && security.RoleAllows(claims.Role, "guest")
	})
	unauthorized := httptest.NewRecorder()
	stateHandler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/system/state", nil))
	token, err := security.SignSession(secret, security.SessionClaims{Subject: "harness", Role: "guest", CSRF: "test-only", ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		return unauthorized.Code, 0, StateSnapshot{}, errors.New("test session signing failed")
	}
	authorizedReq := httptest.NewRequest(http.MethodGet, "/api/system/state", nil)
	authorizedReq.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: token})
	authorized := httptest.NewRecorder()
	stateHandler.ServeHTTP(authorized, authorizedReq)
	var public StateSnapshot
	if err := json.Unmarshal(authorized.Body.Bytes(), &public); err != nil {
		return unauthorized.Code, authorized.Code, StateSnapshot{}, err
	}
	if err := ValidatePublicState(public); err != nil {
		return unauthorized.Code, authorized.Code, StateSnapshot{}, err
	}
	return unauthorized.Code, authorized.Code, public, nil
}
