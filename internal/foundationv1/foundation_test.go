package foundationv1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synora/internal/cognitivecore"
)

func TestFunctionStateVocabularyAndConservativeCameraHealth(t *testing.T) {
	initial := DefaultInitialStatus()
	for name, status := range map[string]FunctionStatus{
		"camera": initial.CameraHealth, "peripheral": initial.Peripherals, "action": initial.Action, "voice": initial.Voice,
		"search": initial.Search, "topology": initial.Topology, "discovery": initial.Discovery, "core": initial.Core,
		"mlp": initial.MLP, "safety_gate": initial.SafetyGate, "store": initial.Store, "executor": initial.Executor, "api": initial.API,
	} {
		if status != StatusNotConfigured {
			t.Errorf("initial %s status=%q", name, status)
		}
	}
	for _, status := range []FunctionStatus{StatusNotConfigured, StatusUnavailable, StatusSimulatedTest, StatusDryRun, StatusAvailable, StatusFailed} {
		if !status.Valid() {
			t.Fatalf("status %q rejected", status)
		}
	}
	if FunctionStatus("qualified").Valid() {
		t.Fatal("qualification was accepted as a function state")
	}
	now := time.Unix(123, 0).UTC()
	for _, tc := range []struct{ name, availability, stream, tamper string }{
		{"missing", "offline", "absent", "unknown"}, {"frozen", "online", "frozen", "none"}, {"tamper", "online", "flowing", "suspected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := AssessCamera(tc.availability, tc.stream, tc.tamper, false, now, false)
			if got.SceneSafety != "unknown" || !got.Degraded || !got.Evidence.Redacted || got.Evidence.Confidence != "coarse" {
				t.Fatalf("camera absence/quality implied safety or leaked evidence: %+v", got)
			}
		})
	}
}

func TestPeripheralRegistryAndActionDryRunIdempotency(t *testing.T) {
	store, err := cognitivecore.OpenUniversalStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	registry := NewPeripheralRegistry(UniversalStoreAdapter{Store: store})
	unknown := Peripheral{ID: "abstract-1", Type: "notification", Zone: "zone-a", Status: StatusUnavailable, State: "unknown", Capabilities: []CapabilityState{{Name: "notify", State: "unknown", Mode: StatusUnavailable}}}
	if err := registry.Upsert(context.Background(), unknown, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	got, err := registry.Snapshot(context.Background())
	if err != nil || len(got) != 1 || got[0].State != "unknown" {
		t.Fatalf("unknown peripheral state lost: %+v", got)
	}
	unknown.State = "known"
	if err := registry.Upsert(context.Background(), unknown, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	got, err = registry.Snapshot(context.Background())
	if err != nil || len(got) != 1 || got[0].State != "known" {
		t.Fatalf("Universal Store was not the latest registry truth: %+v err=%v", got, err)
	}
	executor := NewDryRunExecutor()
	proposal := ActionProposal{RequestID: "r1", Action: "notify", Capability: "notify", Zone: "zone-a"}
	blocked, err := executor.Execute(proposal, false, "available")
	if err != nil || blocked.Status != "blocked" || blocked.PhysicalActionExecuted {
		t.Fatalf("gate did not block action: %+v err=%v", blocked, err)
	}
	result, err := executor.Execute(proposal, true, "available")
	if err != nil || result.Status != "duplicate" || result.Reason != "safety_gate_denied" {
		t.Fatalf("idempotency did not return the recorded gate outcome: %+v err=%v", result, err)
	}
	// A fresh request with unknown feedback can be dry-run, but the resulting
	// peripheral state must remain unknown.
	proposal.RequestID = "r2"
	result, err = executor.Execute(proposal, true, "available")
	if err != nil || result.Status != "dry_run" || result.PeripheralState != "unknown" || result.PhysicalActionExecuted {
		t.Fatalf("unsafe dry-run result: %+v err=%v", result, err)
	}
	duplicate, err := executor.Execute(proposal, true, "available")
	if err != nil || duplicate.Status != "duplicate" {
		t.Fatalf("duplicate was not idempotent: %+v err=%v", duplicate, err)
	}
	proposal.Action = "lock"
	if _, err := executor.Execute(proposal, true, "available"); err == nil {
		t.Fatal("request id reuse with different command was accepted")
	}
}

func TestVoiceSearchTopologyAndCorrelationRemainAbstract(t *testing.T) {
	intent := CommunicationIntent{ID: "comm-1", Zones: []string{"zone-b", "zone-a"}, Priority: "high", Cooldown: time.Minute, Recipient: "occupant", TextKey: "notice.test"}
	communication, err := ProposeCommunication(intent, false)
	if err != nil {
		t.Fatal(err)
	}
	if communication.Status != "suppressed" || communication.TTSStatus != StatusNotConfigured || communication.AudioRendered {
		t.Fatalf("voice is not safely suppressed: %+v", communication)
	}
	scheduler := NewCommunicationScheduler()
	first, err := scheduler.Schedule(intent, true, time.Unix(100, 0).UTC())
	if err != nil || first.Status != "queued_dry_run" {
		t.Fatalf("first abstract request not queued in dry-run: %+v err=%v", first, err)
	}
	second, err := scheduler.Schedule(intent, true, time.Unix(110, 0).UTC())
	if err != nil || second.Status != "suppressed" || second.Reason != "cooldown" {
		t.Fatalf("communication cooldown not applied: %+v err=%v", second, err)
	}
	intent.Recipient = "Jane Doe"
	if _, err := ProposeCommunication(intent, true); err == nil {
		t.Fatal("concrete recipient identity crossed the abstract communication port")
	}
	query := SearchQuery{ID: "search-1", Zones: []string{"zone-a"}, Target: "person_suspect", IssuedAt: time.Now()}
	search, err := NewSearchState(query, time.Now().Add(time.Minute), "unknown", nil, 0)
	if err != nil || search.Coverage != "unknown" || search.Occupancy != "unknown" || search.CrossCameraIdentity || search.PTZUsed {
		t.Fatalf("search assumed coverage or identity: %+v err=%v", search, err)
	}
	if !SearchExpired(search, search.ExpiresAt) {
		t.Fatal("search expiration was not observable")
	}
	observation := &RedactedObservation{ID: "o-search", Zone: "zone-a", Category: "person_suspect", Confidence: "low", ObservedAt: time.Now(), Redacted: true}
	ambiguousSearch, err := NewSearchState(query, query.IssuedAt.Add(time.Minute), "partial", observation, 2)
	if err != nil || ambiguousSearch.Association != "ambiguous" || ambiguousSearch.LastObservation == nil || !ambiguousSearch.LastObservation.Redacted || ambiguousSearch.CrossCameraIdentity || ambiguousSearch.PTZUsed {
		t.Fatalf("search association did not preserve ambiguity/redaction: %+v err=%v", ambiguousSearch, err)
	}
	expired := ExpireSearch(SearchState{Status: StatusSimulatedTest, Occupancy: "occupied", Coverage: "complete", Association: "single_observation", ExpiresAt: time.Now().Add(-time.Second)}, time.Now())
	if expired.Status != StatusUnavailable || expired.Occupancy != "unknown" || expired.Coverage != "unknown" || expired.Association != "none" {
		t.Fatalf("expired search retained stale occupancy or coverage: %+v", expired)
	}
	topo := Topology{Status: StatusSimulatedTest, Zones: []Zone{{ID: "a"}, {ID: "b"}}, Connections: []Transition{{From: "a", To: "b"}}}
	if !topo.Allows("a", "b") || topo.Allows("b", "a") {
		t.Fatal("topology transitions are not directional/allowlisted")
	}
	obs := []RedactedObservation{{ID: "o1", Zone: "a", Category: "person_suspect", Confidence: "low", Redacted: true}, {ID: "o2", Zone: "b", Category: "person_suspect", Confidence: "medium", Redacted: true}}
	corr, err := Correlate("c1", "e1", obs, "low", "")
	if err != nil || corr.Classification != "ambiguous" || corr.Biometric {
		t.Fatalf("correlation promoted an ambiguous hypothesis: %+v err=%v", corr, err)
	}
	single, err := Correlate("c2", "e2", obs[:1], "medium", "evidence-reference")
	if err != nil || single.Classification != "hypothesis" {
		t.Fatalf("a correlation reference was mistaken for a verified fact: %+v err=%v", single, err)
	}
	if _, err := ConfirmObserved(single, false, "evidence-reference"); err == nil {
		t.Fatal("unvalidated evidence promoted a hypothesis to an observation")
	}
	confirmed, err := ConfirmObserved(single, true, "evidence-reference")
	if err != nil || confirmed.Classification != "observed" {
		t.Fatalf("explicit evidence confirmation failed: %+v err=%v", confirmed, err)
	}
}

func TestAuthenticatedPublicProjectionAndForbiddenDataRejection(t *testing.T) {
	snapshot := StateSnapshot{Functions: DefaultInitialStatus(), Camera: AssessCamera("unknown", "unknown", "unknown", true, time.Time{}, true), Topology: Topology{Status: StatusNotConfigured}}
	handler := StateHandler(func() StateSnapshot { return snapshot }, func(r *http.Request) bool { return r.Header.Get("X-Test-Authorized") == "yes" })
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/system/state", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized caller received %d", unauthorized.Code)
	}
	missing := httptest.NewRecorder()
	StateHandler(nil, func(*http.Request) bool { return true }).ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/system/state", nil))
	if missing.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing state returned %d instead of unavailable", missing.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/system/state", nil)
	request.Header.Set("X-Test-Authorized", "yes")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized caller received %d", response.Code)
	}
	body := response.Body.String()
	for _, forbidden := range []string{"embedding", "license_plate", "hardware_id", "local_path", "secret", "raw_media"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("public projection contains forbidden key %q: %s", forbidden, body)
		}
	}
	var decoded StateSnapshot
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePublicState(decoded); err != nil {
		t.Fatal(err)
	}
	unsafe := snapshot
	unsafe.PhysicalActionExecuted = true
	if err := ValidatePublicState(unsafe); err == nil {
		t.Fatal("API projection accepted a physical action claim")
	}
	unsafe = snapshot
	unsafe.Peripherals = []Peripheral{{ID: "../../etc", Type: "switch", Zone: "zone-a", Status: StatusAvailable, State: "known"}}
	if err := ValidatePublicState(unsafe); err == nil {
		t.Fatal("API projection accepted a local path as an abstract peripheral ID")
	}
}

func TestFoundationCentralE2EScenarios(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	suite := RunE2E(context.Background(), now)
	if suite.Status != StatusSimulatedTest || suite.Qualification != "not_qualified" || suite.PassedCount != len(suite.Cases) || suite.FailedCount != 0 {
		t.Fatalf("foundation E2E did not report software-only success: %+v", suite)
	}
	if suite.JourneysTerminalCompleted != 9 || suite.JourneysTerminalRejectedExpected != 1 || suite.JourneysIncomplete != 0 {
		t.Fatalf("terminal journey accounting is incorrect: %+v", suite)
	}
	want := []string{"camera-unavailable", "peripheral-state-unknown", "action-blocked-safety-gate", "action-dry-run-return", "duplicate-action-result", "communication-safety-suppressed", "search-without-coverage", "correlation-ambiguous", "restart-recovery", "prohibited-data-rejected"}
	if len(suite.Cases) != len(want) {
		t.Fatalf("case count=%d want=%d", len(suite.Cases), len(want))
	}
	for index, item := range suite.Cases {
		if item.ID != want[index] || !item.Passed {
			t.Errorf("scenario[%d]=%+v", index, item)
		}
		if len(item.Journey) < 4 {
			t.Errorf("scenario %s did not traverse the software pipeline: %v", item.ID, item.Journey)
		}
		if item.ExpectedIngressRejection && (item.TerminalStatus != "rejected_expected" || item.UnauthorizedHTTP != http.StatusUnauthorized || item.AuthorizedHTTP != http.StatusOK || item.PersistedRecords != 0 || item.ExecutorInvoked) {
			t.Errorf("expected rejection was not structured, redacted and terminal: %+v", item)
		}
		if !item.ExpectedIngressRejection && item.TerminalStatus != "completed" {
			t.Errorf("accepted scenario has wrong terminal status: %+v", item)
		}
		if item.PhysicalActionExecuted || item.AudioRendered || item.ModelLoaded {
			t.Errorf("scenario %s claimed real-world execution: %+v", item.ID, item)
		}
		if item.ID == "action-blocked-safety-gate" || item.ID == "communication-safety-suppressed" {
			if item.ExecutorInvoked {
				t.Errorf("Safety Gate denial still invoked the executor: %+v", item)
			}
		}
		if item.ID == "action-dry-run-return" && !item.ExecutorInvoked {
			t.Errorf("allowed dry-run did not traverse its executor port: %+v", item)
		}
	}
	if suite.PhysicalActionExecuted || suite.AudioRendered || suite.RealModelLoaded {
		t.Fatalf("suite falsely claimed an external execution: %+v", suite)
	}
}

func TestPipelineInputGoldenContractAndValidation(t *testing.T) {
	body, err := os.ReadFile("../../testdata/foundation-v1/pipeline-input-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	var input PipelineInput
	if err := json.Unmarshal(body, &input); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePipelineInput(input); err != nil {
		t.Fatalf("golden pipeline input rejected: %v", err)
	}
	for name, mutate := range map[string]func(*PipelineInput){
		"scenario_id":    func(candidate *PipelineInput) { candidate.ScenarioID = "../local/path" },
		"gate_reason":    func(candidate *PipelineInput) { candidate.GateReason = "allow_physical_action" },
		"function_state": func(candidate *PipelineInput) { candidate.Evidence.Status = FunctionStatus("qualified") },
		"evidence_id":    func(candidate *PipelineInput) { candidate.Evidence.ID = "/tmp/media.mp4" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := input
			mutate(&candidate)
			if ValidatePipelineInput(candidate) == nil {
				t.Fatal("invalid pipeline input was accepted")
			}
		})
	}
}
