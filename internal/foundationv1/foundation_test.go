package foundationv1

import (
	"context"
	"encoding/json"
	"errors"
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
	claimedAvailable := Peripheral{ID: "abstract-no-feedback", Type: "notification", Zone: "zone-a", Status: StatusAvailable, State: "known", ObservedState: "unknown", Health: "unknown", Provenance: "user_provided"}
	if err := registry.Upsert(context.Background(), claimedAvailable, time.Now().UTC()); err == nil {
		t.Fatal("peripheral without real feedback was marked available")
	}
	executor := NewDryRunExecutor()
	proposal := ActionProposal{RequestID: "r1", Action: "notify", Capability: "notify", Zone: "zone-a"}
	blocked, err := executor.Execute(proposal, false, "available")
	if err != nil || blocked.Status != "blocked" || blocked.PhysicalActionExecuted {
		t.Fatalf("gate did not block action: %+v err=%v", blocked, err)
	}
	result, err := executor.Execute(proposal, true, "available")
	if err != nil || result.Status != ActionBlocked || result.Reason != "safety_gate_denied" || !result.IdempotentReplay {
		t.Fatalf("idempotency did not return the recorded gate outcome: %+v err=%v", result, err)
	}
	// A fresh request with unknown feedback can be dry-run, but the resulting
	// peripheral state must remain unknown.
	proposal.RequestID = "r2"
	result, err = executor.Execute(proposal, true, "available")
	if err != nil || result.Status != ActionAllowedDryRun || result.PeripheralState != "unknown" || result.PhysicalActionExecuted {
		t.Fatalf("unsafe dry-run result: %+v err=%v", result, err)
	}
	duplicate, err := executor.Execute(proposal, true, "available")
	if err != nil || duplicate.Status != ActionAllowedDryRun || !duplicate.IdempotentReplay {
		t.Fatalf("duplicate was not idempotent: %+v err=%v", duplicate, err)
	}
	proposal.Action = "lock"
	if _, err := executor.Execute(proposal, true, "available"); err == nil {
		t.Fatal("request id reuse with different command was accepted")
	}
}

func TestVoiceSearchTopologyAndCorrelationRemainAbstract(t *testing.T) {
	intent := CommunicationIntent{ID: "comm-1", Zones: []string{"zone-b", "zone-a"}, Priority: "high", Cooldown: time.Minute, Recipient: "occupant", TextKey: "notice.test", PermissionGranted: true}
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
	intent.ID = "comm-2"
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
	if search.Result != "coverage_unknown" || search.Provenance != "inferred" || search.Confidence != "unknown" {
		t.Fatalf("search without coverage was overstated: %+v", search)
	}
	observation := &RedactedObservation{ID: "o-search", Zone: "zone-a", Category: "person_suspect", Confidence: "low", ObservedAt: time.Now(), Redacted: true}
	ambiguousSearch, err := NewSearchState(query, query.IssuedAt.Add(time.Minute), "partial", observation, 2)
	if err != nil || ambiguousSearch.Association != "ambiguous" || ambiguousSearch.LastObservation == nil || !ambiguousSearch.LastObservation.Redacted || ambiguousSearch.CrossCameraIdentity || ambiguousSearch.PTZUsed {
		t.Fatalf("search association did not preserve ambiguity/redaction: %+v err=%v", ambiguousSearch, err)
	}
	if ambiguousSearch.Result != "ambiguous" {
		t.Fatalf("ambiguous search result collapsed: %+v", ambiguousSearch)
	}
	expired := ExpireSearch(SearchState{Status: StatusSimulatedTest, Occupancy: "occupied", Coverage: "complete", Association: "single_observation", ExpiresAt: time.Now().Add(-time.Second)}, time.Now())
	if expired.Status != StatusUnavailable || expired.Occupancy != "unknown" || expired.Coverage != "unknown" || expired.Association != "none" {
		t.Fatalf("expired search retained stale occupancy or coverage: %+v", expired)
	}
	if expired.Result != "expired" {
		t.Fatalf("expired search did not preserve an explicit terminal result: %+v", expired)
	}
	topo := Topology{Status: StatusSimulatedTest, Integrity: "valid", RootZone: "a", Provenance: "user_provided", ExpiresAt: time.Now().Add(time.Minute), Zones: []Zone{
		{ID: "a", Type: "public", Coverage: "unknown", Uncertainty: "unknown", Provenance: "user_provided"},
		{ID: "b", Type: "protected", Coverage: "unknown", Uncertainty: "unknown", Provenance: "user_provided"},
	}, Connections: []Transition{{From: "a", To: "b"}}}
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
	if confirmed.Provenance != "observed" || confirmed.Status != "active" || confirmed.ExpiresAt.IsZero() {
		t.Fatalf("correlation provenance/expiry was not preserved: %+v", confirmed)
	}
	if got := ExpireCorrelation(confirmed, confirmed.ExpiresAt); got.Status != "expired" || got.Provenance != "unknown" {
		t.Fatalf("expired correlation retained authority: %+v", got)
	}
}

func TestActionPolicyAndArbitrationFailClosed(t *testing.T) {
	proposal := ActionProposal{RequestID: "a1", Action: "lock", Capability: "lock", Zone: "zone-a", Priority: "high"}
	now := time.Unix(1_800_000_000, 0).UTC()
	peripheral := Peripheral{ID: "lock-abstract", Type: "lock", Zone: "zone-a", Status: StatusSimulatedTest, State: "unknown", Permissions: []string{"action.lock"}, Capabilities: []CapabilityState{{Name: "lock", State: "available", Mode: StatusSimulatedTest}}}
	allowed, _, state := (ActionPolicy{Peripherals: []Peripheral{peripheral}}).Authorize(proposal, now)
	if !allowed || state != "unknown" {
		t.Fatalf("simulated dry-run capability was not safely admitted: allowed=%t state=%q", allowed, state)
	}
	for name, policy := range map[string]ActionPolicy{
		"absent_peripheral":  {},
		"missing_permission": {Peripherals: []Peripheral{{ID: "p", Type: "lock", Zone: "zone-a", Status: StatusSimulatedTest, State: "unknown", Capabilities: peripheral.Capabilities}}},
		"missing_capability": {Peripherals: []Peripheral{{ID: "p", Type: "lock", Zone: "zone-a", Status: StatusSimulatedTest, State: "unknown", Permissions: peripheral.Permissions}}},
	} {
		t.Run(name, func(t *testing.T) {
			ok, reason, _ := policy.Authorize(proposal, now)
			if ok || reason == "" {
				t.Fatalf("unsafe action passed policy: ok=%t reason=%q", ok, reason)
			}
		})
	}
	bad := proposal
	bad.Capability = "open"
	if ok, reason, _ := (ActionPolicy{Peripherals: []Peripheral{peripheral}}).Authorize(bad, now); ok || reason != "action_capability_mismatch" {
		t.Fatalf("action/capability mismatch was not rejected: %t %q", ok, reason)
	}
	batch := []ActionProposal{
		{RequestID: "lock-1", Action: "lock", Capability: "lock", Zone: "zone-a", Priority: "normal"},
		{RequestID: "unlock-1", Action: "unlock", Capability: "lock", Zone: "zone-a", Priority: "normal"},
	}
	decisions := NewActionArbitrator().ResolveBatch(batch, now)
	if len(decisions) != 2 || decisions[0].Outcome != "suppressed" || decisions[1].Outcome != "suppressed" || decisions[0].Reason != "contradictory_action" {
		t.Fatalf("contradictory actions were not deterministically suppressed: %+v", decisions)
	}
	batch[0].Priority, batch[1].Priority = "low", "urgent"
	decisions = NewActionArbitrator().ResolveBatch(batch, now)
	if decisions[0].Outcome != "suppressed" || decisions[1].Outcome != "eligible" {
		t.Fatalf("priority arbitration was not deterministic: %+v", decisions)
	}
	batch = []ActionProposal{{RequestID: "expire-1", Action: "lock", Capability: "lock", Zone: "zone-a", ExpiresAt: now}}
	if got := NewActionArbitrator().ResolveBatch(batch, now)[0]; got.Outcome != "suppressed" || got.Reason != "proposal_expired" {
		t.Fatalf("expired action was not suppressed: %+v", got)
	}
	cooldownProposal := ActionProposal{RequestID: "cooldown-1", Action: "lock", Capability: "lock", Zone: "zone-a", CooldownNS: int64(time.Minute)}
	arbitrator := NewActionArbitrator()
	if got := arbitrator.ResolveBatch([]ActionProposal{cooldownProposal}, now)[0]; got.Outcome != "eligible" {
		t.Fatalf("first action unexpectedly suppressed: %+v", got)
	}
	cooldownProposal.RequestID = "cooldown-2"
	if got := arbitrator.ResolveBatch([]ActionProposal{cooldownProposal}, now.Add(time.Second))[0]; got.Outcome != "suppressed" || got.Reason != "action_cooldown" {
		t.Fatalf("action cooldown was not enforced: %+v", got)
	}
}

func TestCommunicationDeduplicationAndRestartCooldown(t *testing.T) {
	intent := CommunicationIntent{ID: "comm-replay-a", Zones: []string{"zone-a"}, Priority: "normal", Cooldown: time.Minute, Recipient: "occupant", TextKey: "presence.notice", PermissionGranted: true}
	now := time.Unix(1_800_000_000, 0).UTC()
	scheduler := NewCommunicationScheduler()
	first, err := scheduler.Schedule(intent, true, now)
	if err != nil || first.Status != "queued_dry_run" || first.ScheduledAt.IsZero() {
		t.Fatalf("communication was not recorded as a dry-run request: %+v %v", first, err)
	}
	replayed, err := scheduler.Schedule(intent, true, now.Add(time.Second))
	if err != nil || !replayed.IdempotentReplay || replayed.Status != first.Status {
		t.Fatalf("same intent id was not idempotent: %+v %v", replayed, err)
	}
	other := intent
	other.ID = "comm-replay-b"
	blocked, err := scheduler.Schedule(other, true, now.Add(2*time.Second))
	if err != nil || blocked.Status != "suppressed" || blocked.Reason != "cooldown" {
		t.Fatalf("communication cooldown was not enforced: %+v %v", blocked, err)
	}
	restored := NewCommunicationScheduler()
	if err := restored.RestoreFromRecords([]Record{{Kind: "communication", Payload: first}}); err != nil {
		t.Fatal(err)
	}
	other.ID = "comm-replay-c"
	blocked, err = restored.Schedule(other, true, now.Add(3*time.Second))
	if err != nil || blocked.Status != "suppressed" || blocked.Reason != "cooldown" {
		t.Fatalf("cooldown did not survive scheduler restart: %+v %v", blocked, err)
	}
	batchScheduler := NewCommunicationScheduler()
	batch := []CommunicationIntent{
		{ID: "batch-low", Zones: []string{"zone-a"}, Priority: "low", Cooldown: time.Minute, Recipient: "operator", TextKey: "door.notice", PermissionGranted: true},
		{ID: "batch-high", Zones: []string{"zone-a"}, Priority: "urgent", Cooldown: time.Minute, Recipient: "operator", TextKey: "door.notice", PermissionGranted: true},
	}
	results, err := batchScheduler.ScheduleBatch(batch, true, now)
	if err != nil || results[0].Status != "suppressed" || results[1].Status != "queued_dry_run" {
		t.Fatalf("communication priority did not arbitrate cooldown deterministically: %+v %v", results, err)
	}
	denied := intent
	denied.ID, denied.PermissionGranted = "comm-no-consent", false
	deniedRequest, err := NewCommunicationScheduler().Schedule(denied, true, now)
	if err != nil || deniedRequest.Status != "suppressed" || deniedRequest.Reason != "permission_not_granted" || deniedRequest.AudioRendered {
		t.Fatalf("communication without permission was not suppressed: %+v %v", deniedRequest, err)
	}
}

func TestTopologyStrictConnectivityCoverageAndExpiry(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	zone := func(id string) Zone {
		return Zone{ID: id, Type: "protected", Coverage: "unknown", Uncertainty: "unknown", Provenance: "user_provided"}
	}
	valid := Topology{Status: StatusSimulatedTest, Integrity: "valid", RootZone: "a", Provenance: "user_provided", ExpiresAt: now.Add(time.Minute), Zones: []Zone{zone("a"), zone("b")}, Connections: []Transition{{From: "a", To: "b"}}}
	if err := valid.Validate(); err != nil || !valid.AllowsAt("a", "b", now) || valid.AllowsAt("b", "a", now) {
		t.Fatalf("valid directed topology failed: %v", err)
	}
	disconnected := valid
	disconnected.Zones = append(disconnected.Zones, zone("c"))
	if err := disconnected.Validate(); err == nil {
		t.Fatal("disconnected topology was accepted as valid")
	} else {
		var structured *TopologyValidationError
		if !errors.As(err, &structured) || structured.Code != "topology_disconnected_zone" {
			t.Fatalf("topology error was not structured: %T %v", err, err)
		}
	}
	badTransition := valid
	badTransition.Connections = []Transition{{From: "a", To: "missing"}}
	if err := badTransition.Validate(); err == nil {
		t.Fatal("transition to unknown zone was accepted")
	}
	expired := valid.EffectiveAt(now.Add(2 * time.Minute))
	if expired.Status != StatusUnavailable || expired.Integrity != "incomplete" || expired.Zones[0].Coverage != "unknown" || expired.AllowsAt("a", "b", now.Add(2*time.Minute)) {
		t.Fatalf("expired topology retained authority: %+v", expired)
	}
}

func TestSearchAndCorrelationProjectionPreservesUncertaintyAndExpiry(t *testing.T) {
	now := time.Now().UTC()
	search, err := NewSearchState(SearchQuery{ID: "search-projection", Zones: []string{"zone-a"}, Target: "person_suspect", IssuedAt: now}, now.Add(time.Minute), "unknown", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	obs := []RedactedObservation{{ID: "obs-a", Zone: "zone-a", Category: "person_suspect", Confidence: "low", ObservedAt: now, Redacted: true}, {ID: "obs-b", Zone: "zone-b", Category: "person_suspect", Confidence: "low", ObservedAt: now, Redacted: true}}
	correlation, err := CorrelateAt("corr-projection", "episode-projection", obs, "low", "", now)
	if err != nil {
		t.Fatal(err)
	}
	state := StateSnapshot{Functions: DefaultInitialStatus(), Camera: AssessCamera("unknown", "unknown", "unknown", true, now, true), Search: &search, Topology: Topology{Status: StatusNotConfigured}, Correlations: []Correlation{correlation}}
	if err := ValidatePublicStateAt(state, now); err != nil {
		t.Fatalf("unknown search/correlation projection was rejected: %v; search=%+v correlation=%+v", err, search, correlation)
	}
	expiredSearch := ExpireSearch(search, search.ExpiresAt)
	expiredCorrelation := ExpireCorrelation(correlation, correlation.ExpiresAt)
	state.Search, state.Correlations = &expiredSearch, []Correlation{expiredCorrelation}
	if err := ValidatePublicStateAt(state, correlation.ExpiresAt); err != nil {
		t.Fatalf("expired search/correlation did not retain explicit uncertainty: %v; search=%+v correlation=%+v", err, expiredSearch, expiredCorrelation)
	}
}

func TestRedactedProjectionRejectsExpandedSensitiveKeyFamilies(t *testing.T) {
	for _, forbiddenKey := range []string{"url", "bbox", "crop", "keypoints", "track_id", "resident_id", "phone", "address"} {
		record := Record{ID: "forbidden-key-test", Kind: "diagnostic", Status: StatusSimulatedTest, Payload: map[string]any{forbiddenKey: "redacted-test-value"}}
		if err := ValidateRecord(record); !errors.Is(err, ErrForbiddenProjection) {
			t.Errorf("forbidden key %q accepted: %v", forbiddenKey, err)
		}
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
	for _, forbidden := range []string{"embedding", "license_plate", "hardware_id", "local_path", "secret", "raw_media", "bbox", "keypoints", "url", "track_id"} {
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
	// The authenticated state API validates expiration against wall-clock time;
	// use the same clock for the E2E fixture so its short-lived records are fresh.
	now := time.Now().UTC()
	suite := RunE2E(context.Background(), now)
	if suite.Status != StatusSimulatedTest || suite.Qualification != "not_qualified" || suite.PassedCount != len(suite.Cases) || suite.FailedCount != 0 {
		t.Fatalf("foundation E2E did not report software-only success: %+v", suite)
	}
	if suite.JourneysTerminalCompleted != 2 || suite.JourneysTerminalRejectedExpected != 1 || suite.JourneysTerminalBlocked != 3 || suite.JourneysTerminalSuppressed != 20 || suite.JourneysTerminalUnknownResult != 2 || suite.JourneysTerminalFailedExpected != 1 || suite.JourneysIncomplete != 0 {
		t.Fatalf("terminal journey accounting is incorrect: %+v", suite)
	}
	want := []string{
		"camera-unavailable", "peripheral-state-unknown", "action-blocked-safety-gate", "action-dry-run-return", "duplicate-action-result", "communication-safety-suppressed", "search-without-coverage", "correlation-ambiguous", "restart-recovery", "prohibited-data-rejected",
		"topology-invalid-disconnected", "zone-coverage-unknown", "transition-impossible", "peripheral-absent", "capability-absent", "permission-refused", "action-contradictory", "action-cooldown", "action-priority", "action-timeout", "executor-return-missing", "executor-result-rejected", "communication-cooldown", "communication-blocked", "communication-permission-denied", "search-ambiguous", "search-expired", "store-corrupt-quarantine", "api-unauthorized",
	}
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
		if item.PhysicalActionExecuted || item.AudioRendered || item.ModelLoaded {
			t.Errorf("scenario %s claimed real-world execution: %+v", item.ID, item)
		}
		if item.CorrelationID != "foundation-"+item.ID || item.UnauthorizedHTTP != http.StatusUnauthorized || item.AuthorizedHTTP != http.StatusOK {
			t.Errorf("scenario %s lacks stable correlation or authenticated API checks", item.ID)
		}
		if !item.ExpectedIngressRejection && item.TerminalStatus == "" {
			t.Errorf("accepted scenario lacks a structured terminal outcome: %+v", item)
		}
		if item.ID == "action-blocked-safety-gate" || item.ID == "communication-safety-suppressed" || item.ID == "communication-blocked" {
			if item.ExecutorInvoked {
				t.Errorf("Safety Gate denial still invoked the executor: %+v", item)
			}
		}
		if item.ID == "action-dry-run-return" && (!item.ExecutorInvoked || item.ActionStatus != ActionAllowedDryRun) {
			t.Errorf("allowed dry-run did not traverse its executor port: %+v", item)
		}
		if item.ID == "executor-result-rejected" && item.TerminalStatus != "failed_expected" {
			t.Errorf("expected executor rejection was not a terminal expected failure: %+v", item)
		}
		if item.ID == "duplicate-action-result" && !item.IdempotencyConflictRejected {
			t.Errorf("action idempotency replay/conflict was not enforced: %+v", item)
		}
		if item.ID == "restart-recovery" && (!item.RestartRecovered || !item.RestartReplay) {
			t.Errorf("action/store replay was not recovered after restart: %+v", item)
		}
	}
	if suite.JourneysTerminalCompleted+suite.JourneysTerminalRejectedExpected+suite.JourneysTerminalBlocked+suite.JourneysTerminalSuppressed+suite.JourneysTerminalUnknownResult+suite.JourneysTerminalFailedExpected != len(suite.Cases) {
		t.Fatalf("terminal outcome accounting does not equal actual journeys: %+v", suite)
	}
	if suite.Metrics["peripheral_available"] != 0 || suite.Metrics["software_pipeline_completed"] != len(suite.Cases)-suite.JourneysTerminalRejectedExpected {
		t.Fatalf("health metrics claimed unavailable hardware or missed completed software journeys: %+v", suite.Metrics)
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
