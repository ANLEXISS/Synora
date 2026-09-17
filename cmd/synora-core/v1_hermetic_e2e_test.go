package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"synora/internal/actions"
	"synora/internal/automation"
	"synora/internal/cognitive"
	"synora/internal/discovery/ingress"
	"synora/internal/discovery/vision"
	"synora/internal/mediamtx"
	"synora/internal/state"
	"synora/pkg/contract"
)

type hermeticMediaMTX struct {
	mu       sync.Mutex
	paths    map[string]bool
	failList bool
}

func newHermeticMediaMTX() *hermeticMediaMTX {
	return &hermeticMediaMTX{paths: map[string]bool{"stale-camera": true}}
}

func (m *hermeticMediaMTX) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failList && r.URL.Path == "/v3/paths/list" {
		http.Error(w, "temporary fake MediaMTX failure", http.StatusServiceUnavailable)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v3/paths/list":
		paths := make([]string, 0, len(m.paths))
		for path := range m.paths {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		items := make([]map[string]string, 0, len(paths))
		for _, path := range paths {
			items = append(items, map[string]string{"name": path})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v3/config/paths/add/"):
		path, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/v3/config/paths/add/"))
		if err != nil || path == "" {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		m.paths[path] = true
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v3/config/paths/delete/"):
		path, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/v3/config/paths/delete/"))
		if err != nil || path == "" {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		delete(m.paths, path)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

type hermeticActionExecutor struct {
	mu       sync.Mutex
	requests []contract.ActionRequest
}

func (e *hermeticActionExecutor) Execute(_ context.Context, request contract.ActionRequest) (actions.ExecutionResult, error) {
	e.mu.Lock()
	e.requests = append(e.requests, request)
	e.mu.Unlock()
	return actions.ExecutionResult{Status: actions.StatusSuccess, Details: map[string]any{"adapter": "hermetic-fake"}}, nil
}

type hermeticV1Harness struct {
	app          *coreApp
	bus          *memoryCoreBus
	queue        *integrationClipQueue
	clipRoot     string
	statePath    string
	media        *mediamtx.Client
	actions      *actions.Service
	actionExec   *hermeticActionExecutor
	messageIndex int
	stop         chan struct{}
	stopOnce     sync.Once
}

func newHermeticV1Harness(t *testing.T) *hermeticV1Harness {
	t.Helper()
	app, bus := newTestCoreApp(t)
	clipRoot := t.TempDir()
	t.Setenv("SYNORA_CLIP_DIR", clipRoot)
	statePath := t.TempDir() + "/state.json"
	app.state.SetPersistence(state.NewFilePersistence(statePath))
	app.automation.Now = func() time.Time { return time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC) }
	if err := app.automation.Add(automation.Rule{
		ID:        "hermetic-unknown-notification",
		Enabled:   true,
		EventType: contract.EventVisionUnknown,
		Actions: []automation.AutomationAction{{
			ID:      "hermetic-push",
			Type:    "push",
			Target:  "owner",
			Enabled: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	fakeMedia := newHermeticMediaMTX()
	mediaServer := httptest.NewServer(http.HandlerFunc(fakeMedia.handler))
	t.Cleanup(mediaServer.Close)
	media, err := mediamtx.NewClient(mediaServer.URL, mediaServer.Client())
	if err != nil {
		t.Fatal(err)
	}

	executor := &hermeticActionExecutor{}
	actionService := &actions.Service{
		Executor:             executor,
		Bus:                  bus,
		Deduper:              actions.NewDeduper(),
		ExecutionMode:        actions.ExecutionDryRun,
		EnforceExecutionMode: true,
		Now:                  func() time.Time { return time.Date(2026, 8, 29, 12, 1, 0, 0, time.UTC) },
		NewID:                func(string) string { return "hermetic-action-result" },
	}

	harness := &hermeticV1Harness{
		app:        app,
		bus:        bus,
		queue:      &integrationClipQueue{},
		clipRoot:   clipRoot,
		statePath:  statePath,
		media:      media,
		actions:    actionService,
		actionExec: executor,
		stop:       make(chan struct{}),
	}
	app.processStop = harness.stop
	app.startBackgroundLoops()
	t.Cleanup(func() {
		harness.stopCore()
	})
	return harness
}

func (h *hermeticV1Harness) stopCore() {
	if h == nil {
		return
	}
	h.stopOnce.Do(func() { close(h.stop) })
	h.app.lifecycleWG.Wait()
}

func (h *hermeticV1Harness) deliverCoreMessages(t *testing.T) {
	t.Helper()
	for {
		h.bus.mu.Lock()
		if h.messageIndex >= len(h.bus.messages) {
			h.bus.mu.Unlock()
			return
		}
		message := h.bus.messages[h.messageIndex]
		h.messageIndex++
		h.bus.mu.Unlock()
		if message.Target != "core" {
			continue
		}
		if _, accepted := h.app.ingest.Ingest(message); !accepted {
			t.Fatalf("core rejected local message type=%s id=%s", message.Type, message.ID)
		}
	}
}

func waitHermetic(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

func (h *hermeticV1Harness) publishDiscoveryOnline(t *testing.T, cameraID string, when time.Time) {
	t.Helper()
	if err := h.bus.Send(contract.Message{
		ID: cameraID + ":online", Type: contract.EventDiscoveryCameraOnline, Kind: contract.KindEvent,
		Source: "discovery", Target: "core", Timestamp: when,
		Payload: mustJSON(t, map[string]any{"device_id": cameraID, "camera_id": cameraID, "node_id": cameraID}),
	}); err != nil {
		t.Fatal(err)
	}
	h.deliverCoreMessages(t)
	waitHermetic(t, cameraID+" online", func() bool {
		value, ok := h.app.state.DeviceState(cameraID)
		return ok && value != nil && value.Online
	})
}

func (h *hermeticV1Harness) upload(t *testing.T, cameraID, clipID string) *vision.ClipJob {
	t.Helper()
	handler := ingress.NewHandler(ingress.Config{
		ClipDir: h.clipRoot, Queue: h.queue, Publisher: h.bus, AllowInsecure: true,
		MaxClipSize: 1024, MaxClipCount: 20, MaxClipBytes: 20 * 1024 * 1024,
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, multipartClipRequest(t, cameraID, clipID, []byte("deterministic-video")))
	if response.Code != http.StatusAccepted {
		t.Fatalf("clip %s status=%d body=%s", clipID, response.Code, response.Body.String())
	}
	h.deliverCoreMessages(t)
	waitHermetic(t, clipID+" ready", func() bool {
		value, ok := h.app.state.Clip(clipID)
		return ok && value != nil && value.Status == contract.ClipStatusReady
	})
	return h.queue.jobs[len(h.queue.jobs)-1]
}

func (h *hermeticV1Harness) runVision(t *testing.T, job *vision.ClipJob, event vision.Event) {
	t.Helper()
	if err := vision.RunClipWorker(visionProcessorFunc(func(*vision.ClipJob) (*vision.WorkerResponse, error) {
		return &vision.WorkerResponse{Events: []vision.Event{event}}, nil
	}), h.bus, job); err != nil {
		t.Fatal(err)
	}
	h.deliverCoreMessages(t)
	waitHermetic(t, job.ID+" processed", func() bool {
		value, ok := h.app.state.Clip(job.ID)
		return ok && value != nil && value.Status == contract.ClipStatusProcessed
	})
}

type hermeticCognitiveTrace struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	TaskID        string `json:"task_id"`
	AdapterID     string `json:"adapter_id"`
	BackendID     string `json:"backend_id"`
	AdvisoryOnly  bool   `json:"advisory_only"`
}

type hermeticV1Trace struct {
	SchemaVersion          string                 `json:"schema_version"`
	TeacherDecisionSource  string                 `json:"teacher_decision_source"`
	VisionEventID          string                 `json:"vision_event_id"`
	VisionEventType        string                 `json:"vision_event_type"`
	ClipID                 string                 `json:"clip_id"`
	EpisodeID              string                 `json:"episode_id"`
	TrackID                string                 `json:"track_id"`
	NodeID                 string                 `json:"node_id"`
	CorrelationID          string                 `json:"correlation_id"`
	ExecutionMode          string                 `json:"execution_mode"`
	PhysicalActionAttempts int                    `json:"physical_action_attempts"`
	Cognitive              hermeticCognitiveTrace `json:"cognitive"`
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestV1HermeticScenarioAcrossBusCoreDiscoveryVisionActionsAndMediaMTX(t *testing.T) {
	h := newHermeticV1Harness(t)
	when := time.Date(2026, 8, 29, 12, 2, 0, 0, time.UTC)

	paths, err := mediamtx.DesiredPaths([]string{"cam_01", "cam_02", "cam_03"})
	if err != nil {
		t.Fatal(err)
	}
	report, err := mediamtx.Reconcile(context.Background(), h.media, paths, when)
	if err != nil || !report.Ready || report.Status != "ready" {
		t.Fatalf("fake MediaMTX did not become ready: report=%#v err=%v", report, err)
	}
	if report.Removed == nil || len(report.Removed) != 1 || report.Removed[0] != "stale-camera" {
		t.Fatalf("MediaMTX stale path was not reconciled: %#v", report)
	}
	if second, err := mediamtx.Reconcile(context.Background(), h.media, paths, when); err != nil || !second.Ready || len(second.Added) != 0 || len(second.Removed) != 0 {
		t.Fatalf("MediaMTX reconciliation was not idempotent: report=%#v err=%v", second, err)
	}

	for _, cameraID := range []string{"cam_01", "cam_02", "cam_03"} {
		h.publishDiscoveryOnline(t, cameraID, when)
	}
	if len(h.app.residents) != 2 || h.app.residents["alexis"] == nil {
		t.Fatalf("resident fixture was not loaded")
	}

	clipV1 := h.upload(t, "cam_01", "clip-v1")
	clipV1.ActivationID = "activation-v1"
	clipV1.SequenceKey = "episode-v1"
	clipV1.TrackID = "track-v1"
	clipV1.EpisodeID = "episode-v1"
	clipV1.NodeID = "entry"
	clipV1.Zone = "entry"
	clipV1.TriggerReason = "hermetic_fixture"
	clipV1.StartedAt = when
	clipV1.EndsAt = when.Add(10 * time.Second)
	clipV1.Pipeline = "clip-v1"
	clipSummary := contract.VisionClipSummary{
		Schema: contract.EventVisionClipSummaryV1, EpisodeID: "episode-v1", ClipID: clipV1.ID, CameraID: clipV1.CameraID,
		Topology:  contract.VisionClipTopology{NodeID: "entry", Zone: "entry"},
		Trigger:   contract.VisionClipTrigger{Reason: "hermetic_fixture", StartedAt: when},
		Track:     contract.VisionClipTrack{ID: "track-v1", SubjectType: "human", FirstSeenAt: when, LastSeenAt: when.Add(2 * time.Second), Confidence: 0.93},
		Identity:  contract.VisionClipIdentity{Status: "not_available", Confidence: 0},
		Plate:     contract.VisionClipPlate{Status: "not_available", Confidence: 0},
		Sensitive: contract.VisionSensitiveObjects{Status: "not_available"},
		Media:     contract.VisionClipMedia{BestROIRefs: []string{"local://clip-v1/roi-0"}},
	}
	if err := clipSummary.Validate(); err != nil {
		t.Fatal(err)
	}
	var clipSummaryPayload map[string]any
	if err := json.Unmarshal(mustJSON(t, clipSummary), &clipSummaryPayload); err != nil {
		t.Fatal(err)
	}
	h.runVision(t, clipV1, vision.Event{Type: contract.EventVisionClipSummaryV1, TrackID: "track-v1", Payload: clipSummaryPayload})

	var clipSummaryEvent *contract.Event
	h.bus.mu.Lock()
	clipMessages := append([]contract.Message(nil), h.bus.messages...)
	h.bus.mu.Unlock()
	for _, message := range clipMessages {
		if message.Target != "core" || message.Type != contract.EventVisionClipSummaryV1 {
			continue
		}
		parsed, err := h.app.ingest.Parser.Parse(message)
		if err != nil {
			t.Fatal(err)
		}
		clipSummaryEvent = parsed
		break
	}
	if clipSummaryEvent == nil || clipSummaryEvent.ClipID != "clip-v1" || clipSummaryEvent.TrackID != "track-v1" || clipSummaryEvent.NodeID != "entry" {
		t.Fatalf("clip-summary/v1 lost its authoritative identity: %#v", clipSummaryEvent)
	}

	known := h.upload(t, "cam_01", "clip-known")
	known.ActivationID, known.SequenceKey, known.TrackID = "activation-known", "sequence-known", "track-known"
	h.runVision(t, known, vision.Event{Type: contract.EventVisionIdentity, TrackID: "track-known", Payload: map[string]any{
		"resident_id": "alexis", "identity": "alexis", "confidence": 0.98,
	}})
	if presence, ok := h.app.state.PresenceState("alexis"); !ok || presence.State != "present" {
		t.Fatalf("known resident did not become present: %#v", presence)
	}

	uncertain := h.upload(t, "cam_02", "clip-uncertain")
	uncertain.ActivationID, uncertain.SequenceKey, uncertain.TrackID = "activation-uncertain", "sequence-uncertain", "track-uncertain"
	h.runVision(t, uncertain, vision.Event{Type: contract.EventVisionUncertain, TrackID: "track-uncertain", Payload: map[string]any{
		"best_match": "Alexis", "confidence": 0.55,
	}})

	unknown := h.upload(t, "cam_01", "clip-unknown")
	unknown.ActivationID, unknown.SequenceKey, unknown.TrackID = "activation-unknown", "sequence-unknown", "track-unknown"
	h.runVision(t, unknown, vision.Event{Type: contract.EventVisionUnknown, TrackID: "track-unknown", Payload: map[string]any{
		"confidence": 0.91,
	}})
	incidents := h.app.state.IncidentsList(10)
	if len(incidents) != 1 || incidents[0].SecurityState != "intrusion" || len(incidents[0].ClipIDs) != 1 || incidents[0].ClipIDs[0] != "clip-unknown" {
		t.Fatalf("unknown did not create exactly one intrusion incident: %#v", incidents)
	}

	var actionRequest contract.Message
	for _, message := range h.bus.messagesOfType(contract.EventActionRequest) {
		if message.Target == "actions" {
			actionRequest = message
			break
		}
	}
	if actionRequest.ID == "" {
		t.Fatal("unknown intrusion did not request an action")
	}
	h.actions.HandleMessage(context.Background(), actionRequest)
	h.deliverCoreMessages(t)
	if len(h.actionExec.requests) != 0 {
		t.Fatalf("dry_run action path attempted physical execution: %#v", h.actionExec.requests)
	}
	var dryRunResult contract.ActionResult
	for _, message := range h.bus.messagesOfType(contract.EventActionResult) {
		if err := json.Unmarshal(message.Payload, &dryRunResult); err == nil && dryRunResult.Status == actions.StatusSimulatedSuccess {
			break
		}
	}
	if dryRunResult.Status != actions.StatusSimulatedSuccess {
		t.Fatalf("dry_run action result was not published: %#v", h.bus.messagesOfType(contract.EventActionResult))
	}
	waitHermetic(t, "action result persisted", func() bool { return len(h.app.state.ActionResultsList()) == 1 })

	var teacherEvent *contract.Event
	h.bus.mu.Lock()
	allMessages := append([]contract.Message(nil), h.bus.messages...)
	h.bus.mu.Unlock()
	for _, message := range allMessages {
		if message.Target != "core" || message.Type != contract.EventVisionUnknown {
			continue
		}
		parsed, err := h.app.ingest.Parser.Parse(message)
		if err != nil {
			t.Fatal(err)
		}
		teacherEvent = parsed
		break
	}
	if teacherEvent == nil {
		t.Fatal("hermetic scenario did not retain the teacher event")
	}
	cognitiveInput, err := cognitive.BuildInput(context.Background(), "hermetic-cognitive-request", cognitive.Task{
		ID: "hermetic-cognitive-task", Kind: "event_reasoning", RequestedCapabilities: []string{cognitive.CapabilityEventReasoning},
	}, cognitive.StateFrame{
		SchemaVersion: cognitive.StateFrameSchemaVersion, Revision: h.app.coreRevision.Load(), CapturedAt: when,
		CurrentEvent: cognitive.StateEventFromContract(teacherEvent), System: cognitive.StateSystem{DangerLevel: "intrusion"},
	}, cognitive.ActionCatalog{SchemaVersion: cognitive.ActionCatalogSchemaVersion, Revision: 1}, cognitive.DeterministicStateEncoder{})
	if err != nil {
		t.Fatal(err)
	}
	cognitiveOutput, err := cognitive.NewScheduler(cognitive.DefaultRegistry()).Run(context.Background(), cognitiveInput)
	if err != nil {
		t.Fatal(err)
	}
	if !cognitiveOutput.AdvisoryOnly || len(cognitiveOutput.ExecutableActions) != 0 {
		t.Fatalf("cognitive shadow output crossed the advisory boundary: %#v", cognitiveOutput)
	}
	trace := hermeticV1Trace{
		SchemaVersion: contract.EventVisionClipSummaryV1, TeacherDecisionSource: "historical_teacher",
		VisionEventID: clipSummaryEvent.ID, VisionEventType: clipSummaryEvent.Type, ClipID: clipSummaryEvent.ClipID,
		EpisodeID: clipV1.EpisodeID, TrackID: clipSummaryEvent.TrackID, NodeID: clipSummaryEvent.NodeID,
		CorrelationID: clipV1.ActivationID, ExecutionMode: string(actions.ExecutionDryRun), PhysicalActionAttempts: len(h.actionExec.requests),
		Cognitive: hermeticCognitiveTrace{SchemaVersion: cognitiveOutput.SchemaVersion, RequestID: cognitiveOutput.RequestID, TaskID: cognitiveOutput.TaskID, AdapterID: cognitiveOutput.AdapterID, BackendID: cognitiveOutput.BackendID, AdvisoryOnly: cognitiveOutput.AdvisoryOnly},
	}
	tracePayload, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	tracePath := t.TempDir() + "/v1-trace.jsonl"
	if err := os.WriteFile(tracePath, append(tracePayload, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if traceBytes, err := os.ReadFile(tracePath); err != nil || len(traceBytes) == 0 || !strings.HasSuffix(string(traceBytes), "\n") {
		t.Fatalf("normalized JSONL trace was not persisted: path=%s err=%v", tracePath, err)
	}

	// At-least-once replay is accepted by the transport but deduplicated by Core.
	beforeReplay := len(h.app.state.IncidentsList(10))
	h.bus.mu.Lock()
	replay := append([]contract.Message(nil), h.bus.messages...)
	h.bus.mu.Unlock()
	for _, message := range replay {
		if message.Target != "core" {
			continue
		}
		// Replay delivery is synchronous here so the test can prove Core's
		// identity gate without filling the bounded input queue with a burst.
		event, parseErr := h.app.ingest.Parser.Parse(message)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		h.app.processEvent(event)
	}
	if len(h.app.state.IncidentsList(10)) != beforeReplay {
		t.Fatalf("replay changed incident count: before=%d after=%d", beforeReplay, len(h.app.state.IncidentsList(10)))
	}

	failed := h.upload(t, "cam_03", "clip-retry")
	transient := errors.New("fake vision timeout")
	if err := vision.RunClipWorkerAttempt(visionProcessorFunc(func(*vision.ClipJob) (*vision.WorkerResponse, error) {
		return nil, transient
	}), h.bus, failed); !errors.Is(err, transient) {
		t.Fatalf("expected retryable fake Vision failure, got %v", err)
	}
	h.deliverCoreMessages(t)
	waitHermetic(t, "failed clip remains retryable", func() bool {
		value, ok := h.app.state.Clip(failed.ID)
		return ok && value != nil && value.Status == contract.ClipStatusProcessing
	})
	h.runVision(t, failed, vision.Event{Type: contract.EventVisionIdentity, TrackID: "retry-track", Payload: map[string]any{
		"resident_id": "alexis", "identity": "alexis", "confidence": 0.97,
	}})

	saturatedQueue := &rejectingClipQueue{err: errors.New("fake queue saturated")}
	saturatedBus := &memoryCoreBus{}
	saturatedHandler := ingress.NewHandler(ingress.Config{ClipDir: h.clipRoot, Queue: saturatedQueue, Publisher: saturatedBus, AllowInsecure: true, MaxClipSize: 1024})
	saturatedResponse := httptest.NewRecorder()
	saturatedHandler.ServeHTTP(saturatedResponse, multipartClipRequest(t, "cam_03", "clip-saturated", []byte("deterministic-video")))
	if saturatedResponse.Code != http.StatusServiceUnavailable || len(saturatedQueue.jobs) != 0 {
		t.Fatalf("saturation was not surfaced safely: status=%d jobs=%d", saturatedResponse.Code, len(saturatedQueue.jobs))
	}
	if len(saturatedBus.messagesOfType(contract.EventClipReady)) != 1 || len(saturatedBus.messagesOfType(contract.EventClipFailed)) != 1 {
		t.Fatalf("saturation must publish ready then failed lifecycle: %#v", saturatedBus.messages)
	}
	for _, message := range saturatedBus.messages {
		event, parseErr := h.app.ingest.Parser.Parse(message)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		h.app.processEvent(event)
	}
	if value, ok := h.app.state.Clip("clip-saturated"); !ok || value.Status != contract.ClipStatusFailed {
		t.Fatalf("saturated clip did not reach failed terminal state: %#v", value)
	}

	if err := h.app.state.SaveNow(); err != nil {
		t.Fatal(err)
	}
	h.stopCore()
	restarted, _ := newTestCoreApp(t)
	restarted.state.SetPersistence(state.NewFilePersistence(h.statePath))
	if _, err := restarted.state.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	if restored, ok := restarted.state.Incident(incidents[0].ID); !ok || restored.Status != contract.IncidentStatusNew || restored.SecurityState != "intrusion" {
		t.Fatalf("restart lost intrusion incident: %#v ok=%t", restored, ok)
	}
	for _, clipID := range []string{"clip-known", "clip-uncertain", "clip-unknown", "clip-retry", "clip-saturated"} {
		if _, ok := restarted.state.Clip(clipID); !ok {
			t.Fatalf("restart lost clip %s", clipID)
		}
	}
	if _, ok, err := restarted.state.AcknowledgeIncident(incidents[0].ID); err != nil || !ok {
		t.Fatalf("incident acknowledgement failed after restart: ok=%t err=%v", ok, err)
	}
	if acknowledged, ok := restarted.state.Incident(incidents[0].ID); !ok || acknowledged.Status != contract.IncidentStatusAcknowledged {
		t.Fatalf("incident was not acknowledged: %#v ok=%t", acknowledged, ok)
	}
}

type rejectingClipQueue struct {
	err  error
	jobs []*vision.ClipJob
}

func (q *rejectingClipQueue) Enqueue(job *vision.ClipJob) error {
	if q.err != nil {
		return q.err
	}
	q.jobs = append(q.jobs, job)
	return nil
}
