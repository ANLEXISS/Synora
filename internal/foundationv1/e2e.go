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
	ID                       string        `json:"id"`
	Status                   string        `json:"status"`
	FunctionStates           InitialStatus `json:"function_states"`
	Journey                  []string      `json:"journey"`
	ActionStatus             string        `json:"action_status"`
	PeripheralState          string        `json:"peripheral_state"`
	UnauthorizedHTTP         int           `json:"unauthorized_http"`
	AuthorizedHTTP           int           `json:"authorized_http"`
	PersistedRecords         int           `json:"persisted_records"`
	CoreStoreRevision        uint64        `json:"core_store_revision"`
	RestartRecovered         bool          `json:"restart_recovered"`
	ExpectedIngressRejection bool          `json:"expected_ingress_rejection"`
	TerminalStatus           string        `json:"terminal_status"`
	PhysicalActionExecuted   bool          `json:"physical_action_executed"`
	AudioRendered            bool          `json:"audio_rendered"`
	ModelLoaded              bool          `json:"model_loaded"`
	ExecutorInvoked          bool          `json:"dry_run_executor_invoked"`
	Passed                   bool          `json:"passed"`
	Error                    string        `json:"error,omitempty"`
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
	JourneysIncomplete               int            `json:"journeys_incomplete"`
	PhysicalActionExecuted           bool           `json:"physical_action_executed"`
	AudioRendered                    bool           `json:"audio_rendered"`
	RealModelLoaded                  bool           `json:"real_model_loaded"`
}

type e2eScenario struct {
	id             string
	kind           string
	payload        any
	proposal       ActionProposal
	capability     string
	gate           bool
	gateReason     string
	reject         bool
	expectedAction string
}

// RunE2E exercises the software ports against Discovery's current validator,
// the real Core Universal Store, a simulated MLP, a fail-closed gate, a
// non-actuating executor and an authenticated ephemeral state API.
func RunE2E(ctx context.Context, now time.Time) E2ESuite {
	suite := E2ESuite{Status: StatusSimulatedTest, Qualification: "not_qualified", InitialStates: DefaultInitialStatus(), Cases: make([]E2ECase, 0)}
	redactedA := RedactedObservation{ID: "obs-safe-a", Zone: "zone-entry", Category: "person_suspect", Confidence: "medium", ObservedAt: now.UTC(), Redacted: true}
	redactedB := RedactedObservation{ID: "obs-safe-b", Zone: "zone-hall", Category: "person_suspect", Confidence: "low", ObservedAt: now.UTC().Add(-time.Second), Redacted: true}
	ambiguous, _ := Correlate("corr-ambiguous", "episode-e2e", []RedactedObservation{redactedA, redactedB}, "low", "")
	search, _ := NewSearchState(SearchQuery{ID: "search-no-coverage", Zones: []string{"zone-garage"}, Target: "person_suspect", IssuedAt: now}, now.Add(time.Minute), "unknown", nil, 0)
	communication := CommunicationIntent{ID: "comm-safety-blocked", Zones: []string{"zone-entry"}, Priority: "normal", Cooldown: 30 * time.Second, Recipient: "occupant", TextKey: "presence.notice"}
	unknownCapability := CapabilityState{Name: "notify", State: "unknown", Mode: StatusUnavailable}
	knownTopology := Topology{Status: StatusSimulatedTest, Zones: []Zone{{ID: "zone-entry"}, {ID: "zone-hall"}}, Connections: []Transition{{From: "zone-entry", To: "zone-hall"}}}
	_ = knownTopology.Allows("zone-entry", "zone-hall") // the contract carries only declared transitions
	scenarios := []e2eScenario{
		{id: "camera-unavailable", kind: "camera_health", payload: AssessCamera("offline", "absent", "unknown", true, now, true), gate: true, expectedAction: "not_requested"},
		{id: "peripheral-state-unknown", kind: "peripheral", payload: Peripheral{ID: "peripheral-abstract-1", Type: "notification", Zone: "zone-entry", Status: StatusSimulatedTest, State: "unknown", Capabilities: []CapabilityState{unknownCapability}}, gate: true, expectedAction: "not_requested"},
		{id: "action-blocked-safety-gate", kind: "action_proposal", payload: map[string]any{"proposal_state": "proposed", "gate_state": "blocked"}, proposal: ActionProposal{RequestID: "action-blocked-1", Action: "notify", Capability: "notify", Zone: "zone-entry"}, gate: false, gateReason: "safety_gate_denied", expectedAction: "blocked"},
		{id: "action-dry-run-return", kind: "action_proposal", payload: map[string]any{"proposal_state": "proposed", "gate_state": "allowed_dry_run"}, proposal: ActionProposal{RequestID: "action-return-1", Action: "notify", Capability: "notify", Zone: "zone-entry"}, capability: "available", gate: true, expectedAction: "dry_run"},
		{id: "duplicate-action-result", kind: "action_proposal", payload: map[string]any{"proposal_state": "proposed", "idempotency": "same_request_id"}, proposal: ActionProposal{RequestID: "action-duplicate-1", Action: "record", Capability: "record", Zone: "zone-entry"}, capability: "available", gate: true, expectedAction: "duplicate"},
		{id: "communication-safety-suppressed", kind: "communication_intent", payload: communication, proposal: ActionProposal{RequestID: "communication-suppressed", Action: "notify", Capability: "notify", Zone: "zone-entry"}, gate: false, gateReason: "safety_gate_denied", expectedAction: "blocked"},
		{id: "search-without-coverage", kind: "search", payload: search, proposal: ActionProposal{RequestID: "search-no-action", Action: "no_action", Capability: "none", Zone: "zone-garage"}, gate: true, expectedAction: "not_requested"},
		{id: "correlation-ambiguous", kind: "correlation", payload: ambiguous, proposal: ActionProposal{RequestID: "correlation-no-action", Action: "no_action", Capability: "none", Zone: "zone-hall"}, gate: true, expectedAction: "not_requested"},
		{id: "restart-recovery", kind: "topology", payload: knownTopology, proposal: ActionProposal{RequestID: "recovery-no-action", Action: "no_action", Capability: "none", Zone: "zone-entry"}, gate: true, expectedAction: "not_requested"},
		{id: "prohibited-data-rejected", kind: "test_input", payload: map[string]any{"identity": map[string]any{"embedding": "forbidden"}}, proposal: ActionProposal{RequestID: "rejected-no-action", Action: "no_action", Capability: "none", Zone: "zone-entry"}, gate: true, reject: true, expectedAction: "rejected"},
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
		default:
			suite.JourneysIncomplete++
		}
		suite.PhysicalActionExecuted = suite.PhysicalActionExecuted || item.PhysicalActionExecuted
		suite.AudioRendered = suite.AudioRendered || item.AudioRendered
		suite.RealModelLoaded = suite.RealModelLoaded || item.ModelLoaded
	}
	return suite
}

func runE2ECase(ctx context.Context, now time.Time, scenario e2eScenario) E2ECase {
	item := E2ECase{ID: scenario.id, Status: "failed", FunctionStates: DefaultInitialStatus(), ExpectedIngressRejection: scenario.reject,
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
	executor := NewDryRunExecutor()
	pipeline := Pipeline{
		Discovery: DiscoveryBoundaryAdapter{Boundary: &discovery.Boundary{DryRun: true, Now: func() time.Time { return now }}},
		Store:     UniversalStoreAdapter{Store: store}, Core: CognitiveCoreAdapter{Store: store, Now: func() time.Time { return now }}, MLP: SimulatedMLP{Proposal: proposal},
		Gate:     SimulatedGate{Allowed: scenario.gate, Reason: scenario.gateReason},
		Executor: SimulatedDryRun{Executor: executor, Capability: scenario.capability}, Communications: NewCommunicationScheduler(), Now: func() time.Time { return now },
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
		item.ActionStatus = "rejected"
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
	item.ActionStatus, item.PeripheralState = result.Action.Status, result.Action.PeripheralState
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
	}
	if scenario.id == "restart-recovery" {
		reopened, openErr := cognitivecore.OpenUniversalStore(filepath.Join(root, "universal-store"))
		if openErr != nil {
			item.Error = "Universal Store restart failed"
			return item
		}
		records, _, readErr := (UniversalStoreAdapter{Store: reopened}).FoundationSnapshot(ctx)
		item.RestartRecovered = readErr == nil && len(records) >= 2
	}
	publicStatus := StateSnapshot{}
	item.UnauthorizedHTTP, item.AuthorizedHTTP, publicStatus, err = exerciseStateAPI(result.API, now)
	if err != nil {
		item.Error = "authenticated redacted state API contract failed"
		return item
	}
	if item.ActionStatus != scenario.expectedAction && !(scenario.id == "duplicate-action-result" && item.ActionStatus == "duplicate") {
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
	if scenario.id == "restart-recovery" && !item.RestartRecovered {
		item.Error = "Universal Store did not recover records after restart"
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
	item.Status, item.TerminalStatus, item.Passed = "simulated_test", "completed", true
	return item
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
