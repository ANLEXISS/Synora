package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"synora/internal/security"
	"synora/internal/simulation"
	"synora/pkg/contract"
)

type dynamicStateCore struct {
	mu    sync.RWMutex
	state *contract.PublicSnapshot
}

func (c *dynamicStateCore) State() (*contract.PublicSnapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state, nil
}

func (c *dynamicStateCore) setState(state *contract.PublicSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = state
}

type recordingSender struct {
	mu       sync.Mutex
	messages []contract.Message
	err      error
}

type fakeCGEProvider struct {
	summary   map[string]any
	sequences []map[string]any
	detail    map[string]any
	received  map[string]any
}

func (f *fakeCGEProvider) CGESummary() (map[string]any, error) {
	return f.summary, nil
}

func (f *fakeCGEProvider) CGESequences(params map[string]any) ([]map[string]any, error) {
	f.received = params
	limit, _ := params["limit"].(int)
	if limit <= 0 || limit > len(f.sequences) {
		limit = len(f.sequences)
	}
	return f.sequences[:limit], nil
}

func (f *fakeCGEProvider) CGETransitions(params map[string]any) ([]map[string]any, error) {
	f.received = params
	return []map[string]any{}, nil
}

func (f *fakeCGEProvider) CGELearnedBehaviors(params map[string]any) ([]map[string]any, error) {
	f.received = params
	return []map[string]any{}, nil
}

func (f *fakeCGEProvider) CGESequence(id string) (map[string]any, error) {
	return f.detail, nil
}

func (f *fakeCGEProvider) CGELearnedBehavior(id string) (map[string]any, error) {
	return f.detail, nil
}

func (s *recordingSender) Send(msg contract.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.messages = append(s.messages, msg)
	return nil
}

func (s *recordingSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func (s *recordingSender) first() (contract.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return contract.Message{}, false
	}
	return s.messages[0], true
}

func TestWebSocketWithoutTokenReturnsUnauthorized(t *testing.T) {
	core := &dynamicStateCore{state: emptyPublicSnapshot()}
	hub := newWebSocketHub(core)
	defer hub.Close()
	cfg := &security.Config{APITokenHash: security.HashSecret("dev-token")}
	server := httptest.NewServer(apiAuthMiddleware(cfg, hub))
	defer server.Close()

	_, resp, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", nil)
	if err == nil {
		t.Fatal("websocket dial without token should fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got resp=%#v err=%v", resp, err)
	}
}

func TestWebSocketWithBearerReceivesInitialSnapshot(t *testing.T) {
	core := &dynamicStateCore{state: emptyPublicSnapshot()}
	core.state.System["last_state"] = "idle"
	core.state.CGE = compactCGEFixture(20, 30, 20)
	hub := newWebSocketHub(core)
	defer hub.Close()
	cfg := &security.Config{APITokenHash: security.HashSecret("dev-token")}
	server := httptest.NewServer(apiAuthMiddleware(cfg, hub))
	defer server.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer dev-token")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", header)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close()

	var envelope wsEnvelope
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatalf("read initial snapshot: %v", err)
	}
	if envelope.Type != "snapshot.initial" {
		t.Fatalf("unexpected websocket envelope: %#v", envelope)
	}
	data := envelope.Data.(map[string]any)
	system := data["system"].(map[string]any)
	if system["last_state"] != "idle" {
		t.Fatalf("unexpected initial snapshot data: %#v", data)
	}
	assertCompactCGEEnvelope(t, data["cge"])
}

func TestWebSocketAcceptsQueryTokenForBrowserTests(t *testing.T) {
	core := &dynamicStateCore{state: emptyPublicSnapshot()}
	hub := newWebSocketHub(core)
	defer hub.Close()
	cfg := &security.Config{APITokenHash: security.HashSecret("dev-token")}
	server := httptest.NewServer(apiAuthMiddleware(cfg, hub))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws?token=dev-token", nil)
	if err != nil {
		t.Fatalf("websocket dial with query token: %v", err)
	}
	defer conn.Close()
}

func TestWebSocketBroadcastsSnapshotUpdateAfterBusSignal(t *testing.T) {
	core := &dynamicStateCore{state: emptyPublicSnapshot()}
	hub := newWebSocketHub(core)
	defer hub.Close()
	server := httptest.NewServer(hub)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close()
	discardInitial(t, conn)

	updated := emptyPublicSnapshot()
	updated.CGE = compactCGEFixture(20, 30, 20)
	updated.Events = []map[string]any{{
		"id":   "evt-1",
		"type": contract.EventVisionUnknown,
		"payload": map[string]any{
			"metadata": map[string]any{"simulated": true, "test_run_id": "sim-1"},
		},
	}}
	core.setState(updated)
	hub.handleBusMessage(contract.Message{Type: "state.snapshot", Kind: contract.KindEvent, Source: "core"})

	var envelope wsEnvelope
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatalf("read update: %v", err)
	}
	if envelope.Type != "snapshot.updated" {
		t.Fatalf("unexpected update type: %#v", envelope)
	}
	data, _ := json.Marshal(envelope.Data)
	if strings.Contains(string(data), "security.yaml") || strings.Contains(string(data), "api_token") {
		t.Fatalf("websocket message leaks private configuration: %s", string(data))
	}
	payload := envelope.Data.(map[string]any)
	assertCompactCGEEnvelope(t, payload["cge"])
}

func TestWebSocketMultipleClientsReceivePublishedMessage(t *testing.T) {
	core := &dynamicStateCore{state: emptyPublicSnapshot()}
	hub := newWebSocketHub(core)
	defer hub.Close()
	server := httptest.NewServer(hub)
	defer server.Close()

	first, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", nil)
	if err != nil {
		t.Fatalf("first websocket dial: %v", err)
	}
	defer first.Close()
	second, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", nil)
	if err != nil {
		t.Fatalf("second websocket dial: %v", err)
	}
	defer second.Close()
	discardInitial(t, first)
	discardInitial(t, second)

	hub.Publish("system.updated", map[string]any{"status": "ok"})
	for name, conn := range map[string]*websocket.Conn{"first": first, "second": second} {
		var envelope wsEnvelope
		if err := conn.ReadJSON(&envelope); err != nil {
			t.Fatalf("%s read publish: %v", name, err)
		}
		if envelope.Type != "system.updated" {
			t.Fatalf("%s unexpected envelope: %#v", name, envelope)
		}
	}
}

func TestWebSocketSlowClientDoesNotBlockHub(t *testing.T) {
	hub := newWebSocketHub(&dynamicStateCore{state: emptyPublicSnapshot()})
	client := &websocketClient{hub: hub, send: make(chan []byte, 1), done: make(chan struct{})}
	client.send <- []byte(`{"type":"old"}`)
	hub.register(client)

	done := make(chan struct{})
	go func() {
		hub.Publish("snapshot.updated", emptyPublicSnapshot())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on slow websocket client")
	}

	hub.mu.RLock()
	_, stillRegistered := hub.clients[client]
	hub.mu.RUnlock()
	if stillRegistered {
		t.Fatal("slow client should be disconnected")
	}
}

func TestSimulationScenariosEndpointIncludesUnknownAtEntrance(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/simulation/scenarios", nil)
	rec := httptest.NewRecorder()

	handleSimulationScenarios().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var scenarios []simulation.Scenario
	if err := json.Unmarshal(rec.Body.Bytes(), &scenarios); err != nil {
		t.Fatalf("unmarshal scenarios: %v", err)
	}
	for _, scenario := range scenarios {
		if scenario.ID == "unknown_at_entrance" && len(scenario.Steps) > 0 {
			return
		}
	}
	t.Fatalf("unknown_at_entrance missing from scenarios: %#v", scenarios)
}

func TestSimulationRunDefaultsAndSendsEventsThroughBus(t *testing.T) {
	sender := &recordingSender{}
	runner := newSimulationRunner(sender, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/simulation/run", strings.NewReader(`{"scenario_id":"fall_detected","device_id":"cam_01","node_id":"zoneA.L0.entree"}`))
	rec := httptest.NewRecorder()

	handleSimulationRun(runner).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response simulationRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.TestRunID == "" || response.Status != "started" || !response.DryRunActions || response.LearningMode != "simulation" {
		t.Fatalf("unexpected simulation response: %#v", response)
	}
	waitFor(t, time.Second, func() bool { return sender.count() == 1 })

	msg, ok := sender.first()
	if !ok {
		t.Fatal("expected simulation message sent through bus")
	}
	if msg.Target != "core" || msg.Source != "api" || msg.SourceType != contract.SourceSimulator {
		t.Fatalf("simulation should go through API -> Bus -> Core path: %#v", msg)
	}
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal simulation payload: %v", err)
	}
	metadata := payload["metadata"].(map[string]any)
	if metadata["simulated"] != true ||
		metadata["test_run_id"] != response.TestRunID ||
		metadata["event_instance_id"] == "" ||
		metadata["generated_by"] != simulation.GeneratedBySynoraAPI ||
		metadata["dry_run"] != true ||
		metadata["learning_mode"] != "simulation" {
		t.Fatalf("unexpected simulation metadata: %#v", metadata)
	}
	status, ok := runner.Status(response.TestRunID)
	if !ok || status.Status != "completed" || status.StepsSent != 1 || len(status.EventInstanceIDs) != 1 {
		t.Fatalf("unexpected run status: ok=%v status=%#v", ok, status)
	}
}

func TestSimulationRunRejectsUnsupportedLearningModeAndRealActions(t *testing.T) {
	falseValue := false
	runner := newSimulationRunner(&recordingSender{}, nil)
	if _, err := runner.Start(simulationRunRequest{ScenarioID: "fall_detected", LearningMode: "real"}); err == nil {
		t.Fatal("real learning mode should be rejected")
	}
	if _, err := runner.Start(simulationRunRequest{ScenarioID: "fall_detected", DryRunActions: &falseValue}); err == nil {
		t.Fatal("real actions should be rejected")
	}
}

func TestSimulationRunFailureIsStored(t *testing.T) {
	sender := &recordingSender{err: errors.New("bus unavailable")}
	runner := newSimulationRunner(sender, nil)
	response, err := runner.Start(simulationRunRequest{ScenarioID: "fall_detected"})
	if err != nil {
		t.Fatalf("start simulation: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		status, ok := runner.Status(response.TestRunID)
		return ok && status.Status == "failed"
	})
	status, _ := runner.Status(response.TestRunID)
	if len(status.Errors) != 1 || !strings.Contains(status.Errors[0], "bus unavailable") {
		t.Fatalf("failure should be stored: %#v", status)
	}
}

func TestCGEAPIHandlersExposeSummaryAndPaginatedSequences(t *testing.T) {
	core := &fakeCGEProvider{
		summary: map[string]any{"sequence_count": 200},
		sequences: []map[string]any{
			{"id": "seq-1", "signature": "vision.unknown > vision.motion", "evidence_count": 1},
			{"id": "seq-2", "signature": "vision.motion > vision.unknown", "evidence_count": 1},
			{"id": "seq-3", "signature": "vision.identity > vision.motion", "evidence_count": 1},
			{"id": "seq-4", "signature": "vision.fall > vision.motion", "evidence_count": 1},
			{"id": "seq-5", "signature": "vision.weapon > vision.motion", "evidence_count": 1},
			{"id": "seq-6", "signature": "vision.tamper > vision.motion", "evidence_count": 1},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/cge/summary", nil)
	rec := httptest.NewRecorder()
	handleCGESummary(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/cge/sequences?limit=5&signature_contains=vision.unknown", nil)
	rec = httptest.NewRecorder()
	handleCGESequences(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sequences status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sequences []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &sequences); err != nil {
		t.Fatalf("unmarshal sequences: %v", err)
	}
	if len(sequences) > 5 {
		t.Fatalf("limit should cap sequences: %#v", sequences)
	}
	if core.received["signature_contains"] != "vision.unknown" {
		t.Fatalf("signature filter should be forwarded: %#v", core.received)
	}
}

func TestCGEAPIHardCapsLimitAndReturnsBoundedDetail(t *testing.T) {
	core := &fakeCGEProvider{
		sequences: []map[string]any{{"id": "seq-1"}},
		detail: map[string]any{
			"id":       "seq-1",
			"evidence": []any{"a", "b"},
			"examples": []any{"ex-1"},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/cge/sequences?limit=500", nil)
	rec := httptest.NewRecorder()
	handleCGESequences(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sequences status=%d body=%s", rec.Code, rec.Body.String())
	}
	if core.received["limit"] != apiCGELimitMax {
		t.Fatalf("API should hard-cap CGE limit: %#v", core.received)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/cge/sequences/seq-1", nil)
	rec = httptest.NewRecorder()
	handleCGEDetail(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", rec.Code, rec.Body.String())
	}
	var detail map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if len(detail["evidence"].([]any)) > 10 || strings.Contains(rec.Body.String(), "security.yaml") || strings.Contains(rec.Body.String(), "api_token") {
		t.Fatalf("detail should be bounded and public: %s", rec.Body.String())
	}
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func discardInitial(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	var envelope wsEnvelope
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatalf("read initial snapshot: %v", err)
	}
	if envelope.Type != "snapshot.initial" {
		t.Fatalf("expected initial snapshot, got %#v", envelope)
	}
}

func emptyPublicSnapshot() *contract.PublicSnapshot {
	return &contract.PublicSnapshot{
		System:        map[string]any{},
		Devices:       []map[string]any{},
		Residents:     []map[string]any{},
		Nodes:         []map[string]any{},
		Events:        []map[string]any{},
		Automations:   []map[string]any{},
		Cameras:       []map[string]any{},
		Tracks:        []map[string]any{},
		Clusters:      []map[string]any{},
		Clips:         []map[string]any{},
		Presence:      []map[string]any{},
		Identities:    []map[string]any{},
		Validations:   []map[string]any{},
		ActionResults: []map[string]any{},
		Metrics:       map[string]any{},
		CGE:           map[string]any{},
	}
}

func compactCGEFixture(sequenceCount int, transitionCount int, behaviorCount int) map[string]any {
	sequences := make([]map[string]any, 0, sequenceCount)
	for i := 0; i < sequenceCount; i++ {
		sequences = append(sequences, map[string]any{
			"id":             "seq",
			"signature":      "vision.unknown > vision.motion",
			"evidence_count": 1,
		})
	}
	transitions := make([]map[string]any, 0, transitionCount)
	for i := 0; i < transitionCount; i++ {
		transitions = append(transitions, map[string]any{"id": "tr", "count": 1})
	}
	behaviors := make([]map[string]any, 0, behaviorCount)
	for i := 0; i < behaviorCount; i++ {
		behaviors = append(behaviors, map[string]any{"id": "beh", "evidence_count": 1})
	}
	return map[string]any{
		"stats":             map[string]any{"sequence_count": sequenceCount},
		"sequences":         sequences,
		"transitions":       transitions,
		"learned_behaviors": behaviors,
	}
}

func assertCompactCGEEnvelope(t *testing.T, value any) {
	t.Helper()
	cge := value.(map[string]any)
	if cge["stats"] == nil {
		t.Fatalf("cge stats missing: %#v", cge)
	}
	if len(cge["sequences"].([]any)) > 20 || len(cge["transitions"].([]any)) > 30 || len(cge["learned_behaviors"].([]any)) > 20 {
		t.Fatalf("websocket cge payload is not compact: %#v", cge)
	}
	first := cge["sequences"].([]any)[0].(map[string]any)
	if _, ok := first["evidence"]; ok {
		t.Fatalf("websocket compact cge should not expose evidence: %#v", first)
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
