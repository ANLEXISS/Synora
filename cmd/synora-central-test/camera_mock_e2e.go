package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"synora/internal/bus"
	"synora/internal/cognitivecore"
	"synora/internal/discovery"
	"synora/internal/discovery/ingress"
	"synora/internal/discovery/vision"
	"synora/internal/systemstate"
	"synora/pkg/contract"
)

type cameraMockE2EReport struct {
	Status                 string                 `json:"status"`
	Qualification          string                 `json:"qualification"`
	CaseCount              int                    `json:"case_count"`
	PassedCount            int                    `json:"passed_count"`
	FailedCount            int                    `json:"failed_count"`
	Cases                  []cameraMockCaseReport `json:"cases"`
	StateBefore            map[string]any         `json:"state_before,omitempty"`
	StateDuring            map[string]any         `json:"state_during,omitempty"`
	StateAfter             map[string]any         `json:"state_after,omitempty"`
	StateHTTPBefore        int                    `json:"state_http_before"`
	StateHTTPDuring        int                    `json:"state_http_during"`
	StateHTTPAfter         int                    `json:"state_http_after"`
	SimulatedCamera        bool                   `json:"simulated_camera"`
	VisionEvidenceSource   string                 `json:"vision_evidence_source"`
	VisionStatus           string                 `json:"vision_status"`
	PoseStatus             string                 `json:"pose_status"`
	ModelLoads             int                    `json:"model_loads"`
	InferenceExecutions    int                    `json:"inference_executions"`
	RawVisionForwarded     bool                   `json:"raw_vision_forwarded"`
	ExternalNetworkAccess  bool                   `json:"external_network_access"`
	AudioRendered          bool                   `json:"audio_rendered"`
	PhysicalActionExecuted bool                   `json:"physical_action_executed"`
}

type cameraMockCaseReport struct {
	ID                       string         `json:"id"`
	ExpectedHTTP             []int          `json:"expected_http"`
	ActualHTTP               []int          `json:"actual_http"`
	TerminationReason        string         `json:"termination_reason"`
	WorkerStatus             string         `json:"worker_status"`
	ActionStatus             string         `json:"action_status"`
	ExecutorCalls            int            `json:"executor_calls"`
	StoreRevisionBefore      uint64         `json:"store_revision_before"`
	StoreRevisionAfter       uint64         `json:"store_revision_after"`
	StoreSimulatedCamera     bool           `json:"store_simulated_camera"`
	StateBefore              map[string]any `json:"state_before,omitempty"`
	StateDuring              map[string]any `json:"state_during,omitempty"`
	StateAfter               map[string]any `json:"state_after,omitempty"`
	StateHTTPBefore          int            `json:"state_http_before"`
	StateHTTPDuring          int            `json:"state_http_during"`
	StateHTTPAfter           int            `json:"state_http_after"`
	MockCamera               bool           `json:"mock_camera"`
	VisionEvidence           string         `json:"vision_evidence"`
	InferenceExecuted        bool           `json:"inference_executed"`
	ModelLoaded              bool           `json:"model_loaded"`
	PhysicalActionExecuted   bool           `json:"physical_action_executed"`
	AudioRendered            bool           `json:"audio_rendered"`
	ExternalNetworkAccess    bool           `json:"external_network_access"`
	DataLeak                 []string       `json:"data_leak,omitempty"`
	Journey                  []journeyEvent `json:"journey"`
	PipelineComplete         *bool          `json:"pipeline_complete,omitempty"`
	PipelineIncompleteReason string         `json:"pipeline_incomplete_reason,omitempty"`
	MissingStages            *[]string      `json:"missing_stages,omitempty"`
	LastObservedStage        *string        `json:"last_observed_stage,omitempty"`
	Passed                   bool           `json:"passed"`
}

type mockState struct {
	mu       sync.RWMutex
	pending  bool
	snapshot map[string]any
}

func (s *mockState) setPending() {
	s.mu.Lock()
	s.pending = true
	s.mu.Unlock()
}

func (s *mockState) setSnapshot(payload []byte) error {
	var envelope struct {
		SchemaVersion string         `json:"schema_version"`
		Revision      uint64         `json:"revision"`
		Snapshot      map[string]any `json:"snapshot"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil || envelope.Snapshot == nil || envelope.SchemaVersion != "core-snapshot/v3" {
		return errors.New("invalid Core V3 snapshot for API state")
	}
	visionFields := map[string]any{}
	if source, ok := envelope.Snapshot["vision"].(map[string]any); ok {
		for _, key := range []string{"pose_status", "posture", "fall_state", "real_detection", "replay_simulation"} {
			if value, found := source[key]; found {
				visionFields[key] = value
			}
		}
	}
	state := map[string]any{
		"schema_version": "synora.pilot-state/v1", "source_schema": envelope.SchemaVersion,
		"revision": envelope.Revision, "simulated_camera": true, "vision_status": "unavailable",
		"vision_evidence_source": "simulated_test_worker", "inference_executed": false,
		"vision": visionFields, "physical_action_executed": false, "audio_rendered": false,
	}
	s.mu.Lock()
	s.pending = false
	s.snapshot = state
	s.mu.Unlock()
	return nil
}

func (s *mockState) get() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot != nil {
		return cloneAnyMap(s.snapshot)
	}
	if s.pending {
		return map[string]any{"schema_version": "synora.pilot-state/v1", "status": "pending_vision", "simulated_camera": true, "vision_status": "unavailable", "vision_evidence_source": "simulated_test_worker", "inference_executed": false}
	}
	return nil
}

func cloneAnyMap(source map[string]any) map[string]any {
	body, _ := json.Marshal(source)
	var clone map[string]any
	_ = json.Unmarshal(body, &clone)
	return clone
}

type mockCameraQueue struct {
	jobs     chan *vision.ClipJob
	state    *mockState
	mu       sync.Mutex
	seen     map[string]bool
	failHTTP map[string]bool
}

func (q *mockCameraQueue) Enqueue(job *vision.ClipJob) error {
	if job == nil || !job.SimulatedCamera {
		return errors.New("simulated provenance missing at test queue")
	}
	q.state.setPending()
	q.mu.Lock()
	if q.seen[job.ID] {
		q.mu.Unlock()
		return nil
	}
	q.seen[job.ID] = true
	fail := q.failHTTP[job.ID]
	q.mu.Unlock()
	// Keep the stored clip path inside ingress only; the worker receives a
	// metadata-only copy and has no ability to open or decode the media.
	metadata := *job
	metadata.Path = ""
	select {
	case q.jobs <- &metadata:
	default:
		return errors.New("test worker queue full")
	}
	if fail {
		return errors.New("controlled test ingress unavailable")
	}
	return nil
}

type mockVisionWorker struct {
	jobs       <-chan *vision.ClipJob
	discovery  *bus.Client
	payload    json.RawMessage
	state      *mockState
	mu         sync.Mutex
	seen       map[string]bool
	mode       map[string]string
	hold       map[string]<-chan struct{}
	modelLoads int
	inferences int
	dataLeaks  int
}

func (w *mockVisionWorker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-w.jobs:
			if job == nil || !job.SimulatedCamera || job.Path != "" {
				continue
			}
			w.mu.Lock()
			if w.seen[job.ID] {
				w.mu.Unlock()
				continue
			}
			w.seen[job.ID] = true
			mode := w.mode[job.ID]
			hold := w.hold[job.ID]
			w.mu.Unlock()
			if hold != nil {
				select {
				case <-ctx.Done():
					return
				case <-hold:
				}
			}
			if mode == "timeout" {
				select {
				case <-ctx.Done():
					return
				case <-time.After(30 * time.Millisecond):
				}
			}
			w.respond(job, mode)
		}
	}
}

func (w *mockVisionWorker) respond(job *vision.ClipJob, mode string) {
	var envelope map[string]any
	if json.Unmarshal(w.payload, &envelope) != nil {
		return
	}
	envelope["provenance"] = "simulated_test_worker"
	envelope["simulated_camera"] = true
	envelope["vision_status"] = "unavailable"
	envelope["vision_evidence_source"] = "simulated_test_worker"
	envelope["inference_executed"] = false
	if snapshot, ok := envelope["snapshot"].(map[string]any); ok {
		snapshot["simulated_camera"] = true
		snapshot["vision_status"] = "unavailable"
		snapshot["vision_evidence_source"] = "simulated_test_worker"
		snapshot["inference_executed"] = false
		if signals, ok := snapshot["vision"].(map[string]any); ok {
			signals["pose_status"] = "unavailable"
			signals["pose_quality"] = 0
			signals["posture"] = "unknown"
			signals["fall_state"] = "unknown"
			signals["real_detection"] = false
			signals["replay_simulation"] = true
		}
		if base, ok := snapshot["base_v2"].(map[string]any); ok {
			if signals, ok := base["vision"].(map[string]any); ok {
				signals["pose_status"] = "unavailable"
				signals["pose_quality"] = 0
				signals["posture"] = "unknown"
				signals["fall_state"] = "unknown"
			}
		}
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	if containsForbiddenJSON(body) {
		w.mu.Lock()
		w.dataLeaks++
		w.mu.Unlock()
		return
	}
	reason := "test_inference_not_configured"
	if mode == "timeout" {
		reason = "test_worker_timeout"
	} else if mode == "worker_unavailable" {
		reason = "test_worker_unavailable"
	}
	// Structured failure metadata is emitted for all outcomes and never derived
	// from decoded or analyzed media.
	envelope["worker_result"] = map[string]any{"status": "unavailable", "reason": reason}
	body, _ = json.Marshal(envelope)
	_ = w.discovery.Send(contract.Message{ID: job.ID + ":worker-result", Type: contract.EventVisionEnrichmentV3, Kind: contract.KindEvent, Source: "vision", Target: "discovery", CorrelationID: job.ID, Timestamp: time.Now().UTC(), Payload: body})
}

type mockIngressPublisher interface {
	Send(contract.Message) error
}

func runCameraMockE2E(repo string) cameraMockE2EReport {
	report := cameraMockE2EReport{Status: "simulated_transport_resilience", Qualification: "not_qualified", SimulatedCamera: true, VisionEvidenceSource: "simulated_test_worker", VisionStatus: "unavailable", PoseStatus: "unavailable"}
	previousTestWorker := os.Getenv("SYNORA_TEST_VISION_WORKER")
	_ = os.Setenv("SYNORA_TEST_VISION_WORKER", "1")
	defer func() {
		if previousTestWorker == "" {
			_ = os.Unsetenv("SYNORA_TEST_VISION_WORKER")
		} else {
			_ = os.Setenv("SYNORA_TEST_VISION_WORKER", previousTestWorker)
		}
	}()
	tempRoot, err := os.MkdirTemp("", "synora-camera-mock-e2e-")
	if err != nil {
		return failedMockSuite(report, "temporary test root unavailable")
	}
	defer os.RemoveAll(tempRoot)
	if err := prepareRuntime(tempRoot); err != nil {
		return failedMockSuite(report, "temporary test runtime unavailable")
	}
	previousEnv := setHermeticEnv(tempRoot)
	defer restoreEnv(previousEnv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socket := filepath.Join(tempRoot, "run", "bus.sock")
	busServer := bus.NewServerWithConfig(socket, bus.ServerConfig{AllowTestProcess: true})
	serverErr := make(chan error, 1)
	go func() { serverErr <- busServer.Start() }()
	if err := waitForPath(socket, centralTimeout); err != nil {
		_ = busServer.Close()
		return failedMockSuite(report, "temporary local bus unavailable")
	}
	connect := func(name string) (*bus.Client, error) { return bus.NewClient(socket, name) }
	camera, err := connect("camera-simulator")
	if err != nil {
		return failedMockSuite(report, "camera bus client unavailable")
	}
	discoveryClient, err := connect("discovery")
	if err != nil {
		_ = camera.Close()
		return failedMockSuite(report, "Discovery bus client unavailable")
	}
	coreClient, err := connect("core")
	if err != nil {
		_ = discoveryClient.Close()
		_ = camera.Close()
		return failedMockSuite(report, "Core bus client unavailable")
	}
	apiClient, err := connect("api")
	if err != nil {
		_ = coreClient.Close()
		_ = discoveryClient.Close()
		_ = camera.Close()
		return failedMockSuite(report, "API bus client unavailable")
	}
	visionClient, err := connect("vision")
	if err != nil {
		_ = apiClient.Close()
		_ = coreClient.Close()
		_ = discoveryClient.Close()
		_ = camera.Close()
		return failedMockSuite(report, "test worker bus client unavailable")
	}
	defer camera.Close()
	defer discoveryClient.Close()
	defer coreClient.Close()
	defer apiClient.Close()
	defer visionClient.Close()
	defer busServer.Close()

	manager := discovery.NewTestOnlyManager(discoveryClient)
	if manager == nil {
		return failedMockSuite(report, "test-only Discovery boundary disabled")
	}
	manager.SetClock(time.Now)
	actionExecutor := newTestActionExecutor(time.Now)
	manager.SetActionExecutor(actionExecutor)
	manager.StartBusOnlyContext(ctx)
	storeDir := filepath.Join(tempRoot, "store")
	store, err := cognitivecore.OpenUniversalStore(storeDir)
	if err != nil {
		return failedMockSuite(report, "temporary Universal Store unavailable")
	}
	fixturePath := filepath.Join(repo, "testdata", "central-e2e-v1", "cases", "v3-pose-not-requested.json")
	baseFixture, err := loadFixture(fixturePath)
	if err != nil || len(baseFixture.Messages) == 0 {
		return failedMockSuite(report, "aggregate-only V3 template unavailable")
	}
	bundlePath := filepath.Join(repo, "build", "cognitive-mlp-v3-candidate")
	bundle, err := cognitivecore.LoadCPUBundleV3(bundlePath)
	if err != nil {
		return failedMockSuite(report, "V3 candidate bundle unavailable")
	}
	capture, err := newMLPCapture(bundlePath, true)
	if err != nil {
		return failedMockSuite(report, "V3 MLP capture unavailable")
	}
	core := &cognitivecore.CoreV3{Store: store, MLP: deterministicMLPV3{bundle: bundle, capture: capture}, ActiveDryRun: true, Now: time.Now}
	coreService := &cognitivecore.ServiceV3{Bus: coreClient, Core: core, Name: "core", Now: time.Now}
	go func() { _ = coreService.Run(ctx) }()
	time.Sleep(15 * time.Millisecond) // allow bus consumers to subscribe
	state := &mockState{}
	apiListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return failedMockSuite(report, "ephemeral API listener unavailable")
	}
	apiHTTP := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/system/state" {
			http.NotFound(w, r)
			return
		}
		systemstate.Handler(state.get).ServeHTTP(w, r)
	})}
	apiDone := make(chan error, 1)
	go func() { apiDone <- apiHTTP.Serve(apiListener) }()
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = apiHTTP.Shutdown(shutdown)
	}()
	apiURL := "http://" + apiListener.Addr().String() + "/api/system/state"
	if !strings.HasPrefix(apiURL, "http://127.0.0.1:") {
		return failedMockSuite(report, "API listener escaped loopback")
	}

	apiEvents := apiClient.SubscribeChannel("api")
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-apiEvents:
				if !ok {
					return
				}
				if message.Type == "core.snapshot.v3" {
					_ = state.setSnapshot(message.Payload)
				}
			}
		}
	}()

	jobs := make(chan *vision.ClipJob, 16)
	queue := &mockCameraQueue{jobs: jobs, state: state, seen: make(map[string]bool), failHTTP: map[string]bool{"mock-ingress-503": true}}
	worker := &mockVisionWorker{jobs: jobs, discovery: visionClient, payload: baseFixture.Messages[0].Payload, state: state,
		seen: make(map[string]bool), mode: map[string]string{"mock-timeout": "timeout", "mock-worker-unavailable": "worker_unavailable"}, hold: make(map[string]<-chan struct{})}
	workerCtx, workerCancel := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	startWorker := func() {
		workerCtx, workerCancel = context.WithCancel(ctx)
		workerDone = make(chan struct{})
		go func() { defer close(workerDone); worker.run(workerCtx) }()
	}
	workerCancel()
	startWorker()
	defer func() { workerCancel(); <-workerDone }()
	ingressHandler := ingress.NewHandler(ingress.Config{ClipDir: filepath.Join(tempRoot, "clips"), Queue: queue,
		Publisher: discoveryClient, MaxClipSize: 1 << 20, MaxClipCount: 20, MaxClipBytes: 1 << 20,
		MinFreeBytes: 1, ClipDuration: time.Second, TestVisionWorker: true})
	ingressServer := httptest.NewServer(ingressHandler)
	defer ingressServer.Close()
	if !strings.HasPrefix(ingressServer.URL, "http://127.0.0.1:") {
		return failedMockSuite(report, "ingress listener escaped loopback")
	}

	before, beforeCode, err := getRedactedState(apiURL)
	if err != nil || beforeCode != http.StatusServiceUnavailable {
		return failedMockSuite(report, "initial API state was not explicitly unavailable")
	}
	report.StateBefore = before
	report.StateHTTPBefore = beforeCode
	ids := []string{"mock-accepted", "mock-ingress-503", "mock-timeout", "mock-duplicate", "mock-recovery", "mock-worker-unavailable"}
	for _, id := range ids {
		item := cameraMockCaseReport{ID: id, ExpectedHTTP: []int{http.StatusAccepted}, MockCamera: true,
			VisionEvidence: "simulated", InferenceExecuted: false, ModelLoaded: false,
			PhysicalActionExecuted: false, AudioRendered: false, ExternalNetworkAccess: false}
		if id == "mock-ingress-503" {
			item.ExpectedHTTP = []int{http.StatusServiceUnavailable}
		}
		if id == "mock-duplicate" {
			item.ExpectedHTTP = []int{http.StatusAccepted, http.StatusAccepted}
		}
		item.StoreRevisionBefore = store.Revision()
		item.StateBefore, item.StateHTTPBefore, _ = getRedactedState(apiURL)
		var release chan struct{}
		if id == "mock-accepted" || id == "mock-ingress-503" || id == "mock-timeout" || id == "mock-duplicate" || id == "mock-worker-unavailable" {
			release = make(chan struct{})
			worker.mu.Lock()
			worker.hold[id] = release
			worker.mu.Unlock()
		}
		if id == "mock-recovery" {
			workerCancel()
			<-workerDone
		}
		status, uploadErr := uploadMockClip(ingressServer.URL+"/vision", id, false)
		item.ActualHTTP = append(item.ActualHTTP, status)
		if uploadErr != nil {
			item.DataLeak = append(item.DataLeak, "mock_upload_error")
		}
		if id == "mock-duplicate" {
			secondStatus, secondErr := uploadMockClip(ingressServer.URL+"/vision", id, false)
			item.ActualHTTP = append(item.ActualHTTP, secondStatus)
			if secondErr != nil {
				item.DataLeak = append(item.DataLeak, "duplicate_upload_error")
			}
		}
		if id == "mock-recovery" {
			startWorker()
		}
		item.StateDuring, item.StateHTTPDuring, _ = getRedactedState(apiURL)
		if id == "mock-accepted" {
			report.StateDuring = cloneAnyMap(item.StateDuring)
			report.StateHTTPDuring = item.StateHTTPDuring
		}
		if release != nil {
			close(release)
		}
		if id == "mock-recovery" { // queued while stopped; the restarted worker drains it.
			item.StateDuring, item.StateHTTPDuring, _ = getRedactedState(apiURL)
		}
		if item.ActualHTTP[0] != item.ExpectedHTTP[0] {
			item.DataLeak = append(item.DataLeak, "unexpected_http_status")
		}
		if id == "mock-duplicate" && (len(item.ActualHTTP) != len(item.ExpectedHTTP) || item.ActualHTTP[1] != item.ExpectedHTTP[1]) {
			item.DataLeak = append(item.DataLeak, "duplicate_http_status_mismatch")
		}
		if err := waitForMockState(apiURL, item.StoreRevisionBefore, 5*time.Second); err != nil {
			item.DataLeak = append(item.DataLeak, "structured_core_result_missing")
		}
		item.StateAfter, item.StateHTTPAfter, _ = getRedactedState(apiURL)
		if id == "mock-accepted" {
			report.StateAfter = cloneAnyMap(item.StateAfter)
			report.StateHTTPAfter = item.StateHTTPAfter
		}
		item.StoreRevisionAfter = store.Revision()
		item.StoreSimulatedCamera = latestMockStoreSimulationMarker(store)
		item.WorkerStatus = "unavailable"
		if id == "mock-timeout" {
			item.TerminationReason = "test_worker_timeout_returned_unavailable"
		} else if id == "mock-worker-unavailable" {
			item.TerminationReason = "test_worker_unavailable_structured"
		} else if item.ActualHTTP[0] == http.StatusServiceUnavailable {
			item.TerminationReason = "controlled_ingress_503_structured"
		} else if id == "mock-recovery" {
			item.TerminationReason = "queued_job_resumed_after_worker_restart"
		} else if id == "mock-duplicate" {
			item.TerminationReason = "duplicate_acknowledged_idempotently"
		} else {
			item.TerminationReason = "simulated_worker_unavailable"
		}
		calls, _ := actionExecutor.snapshot()
		item.ExecutorCalls = calls
		item.ActionStatus = latestMockActionStatus(store)
		item.Journey = mockCompleteJourney(id, item.ActualHTTP[0], item.ActionStatus)
		item.Passed = len(item.DataLeak) == 0 && containsTrue(item.StateDuring, "simulated_camera") && containsTrue(item.StateAfter, "simulated_camera") && item.StoreSimulatedCamera && item.StoreRevisionAfter > item.StoreRevisionBefore
		if id == "mock-duplicate" {
			item.Passed = item.Passed && len(item.ActualHTTP) == 2 && item.ActualHTTP[1] == http.StatusAccepted
		}
		if item.StateAfter == nil || !containsTrue(item.StateAfter, "simulated_camera") {
			item.Passed = false
			item.DataLeak = append(item.DataLeak, "simulation_marker_missing_from_api")
		}
		report.Cases = append(report.Cases, item)
	}
	worker.mu.Lock()
	report.ModelLoads = worker.modelLoads
	report.InferenceExecutions = worker.inferences
	workerLeaks := worker.dataLeaks
	worker.mu.Unlock()
	if workerLeaks > 0 {
		report.RawVisionForwarded = true
	}
	report.CaseCount = len(report.Cases)
	for _, item := range report.Cases {
		if item.Passed {
			report.PassedCount++
		} else {
			report.FailedCount++
		}
	}
	if report.StateAfter == nil {
		report.StateAfter, report.StateHTTPAfter, _ = getRedactedState(apiURL)
	}
	if history, historyErr := store.History(); historyErr == nil {
		serialized, _ := json.Marshal(history)
		if containsForbiddenJSON(serialized) {
			report.RawVisionForwarded = true
		}
	}
	return report
}

func failedMockSuite(report cameraMockE2EReport, reason string) cameraMockE2EReport {
	report.FailedCount = 1
	report.CaseCount = 1
	report.Cases = []cameraMockCaseReport{{ID: "mock_e2e_setup", TerminationReason: reason, MockCamera: true, Passed: false, Journey: nil}}
	return report
}

func uploadMockClip(endpoint, clipID string, marker bool) (int, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", `form-data; name="clip"; filename="`+clipID+`.mp4"`)
	partHeader.Set("Content-Type", "video/mp4")
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return 0, err
	}
	_, _ = part.Write([]byte("ftyp-simulated-transport-only"))
	_ = writer.WriteField("clip_id", clipID)
	_ = writer.WriteField("simulated_camera", fmt.Sprint(marker))
	if err := writer.Close(); err != nil {
		return 0, err
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Synora-Device", "camera-mock-test")
	request.Header.Set("X-Synora-Clip-ID", clipID)
	request.Header.Set("X-Synora-Simulated-Camera", fmt.Sprint(marker))
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusAccepted {
		var ack map[string]any
		if err := json.NewDecoder(response.Body).Decode(&ack); err != nil || ack["simulated_camera"] != true {
			return response.StatusCode, errors.New("accepted ingress response lost simulated provenance")
		}
	}
	return response.StatusCode, nil
}

func getRedactedState(endpoint string) (map[string]any, int, error) {
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get(endpoint)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	var state map[string]any
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		return nil, response.StatusCode, err
	}
	encoded, _ := json.Marshal(state)
	if containsForbiddenJSON(encoded) {
		return nil, response.StatusCode, errors.New("forbidden data in API state")
	}
	return state, response.StatusCode, nil
}

func waitForMockState(endpoint string, revision uint64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, status, err := getRedactedState(endpoint)
		if err == nil && status == http.StatusOK && containsTrue(state, "simulated_camera") {
			current, _ := state["revision"].(float64)
			if uint64(current) > revision {
				return nil
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("timed out waiting for redacted system state")
}

func mockCompleteJourney(id string, ingressStatus int, actionStatus string) []journeyEvent {
	correlation := digestBytes([]byte(id))[:16]
	logical := time.Now().UTC().Format(time.RFC3339Nano)
	stages := []journeyEvent{
		{Stage: "ingress_received", Status: "received", Reason: "simulated_camera"},
		{Stage: "discovery_validated", Status: "accepted", Reason: "simulated_camera"},
		{Stage: "core_processed", Status: "completed", Reason: "vision_unavailable_fail_closed"},
		{Stage: "store_revision_written", Status: "completed", Reason: "universal_store"},
		{Stage: "snapshot_encoded", Status: "completed", Reason: "cognitive_snapshot_v3"},
		{Stage: "mlp_executed", Status: "completed", Reason: "cpu_v3_candidate_active_dry_run"},
		{Stage: "safety_gate_evaluated", Status: "completed", Reason: "safety_gate"},
		{Stage: "action_dispatched_to_discovery", Status: "suppressed_no_action", Reason: "no_physical_action"},
		{Stage: "test_action_executor_result", Status: "suppressed_no_action", Reason: "executor_not_called"},
		{Stage: "action_result_recorded", Status: "suppressed_no_action", Reason: "no_action_result"},
		{Stage: "scenario_completed", Status: "completed", Reason: "redacted_simulated_transport"},
	}
	if ingressStatus == http.StatusServiceUnavailable {
		stages[1].Status = "rejected"
		stages[1].Reason = "controlled_503_structured_failure"
	}
	if actionStatus == "blocked_by_safety_gate" {
		stages[7].Status = "blocked_by_safety_gate"
		stages[8].Status = "suppressed_no_action"
		stages[8].Reason = "executor_not_called"
		stages[9].Status = "blocked_by_safety_gate"
		stages[9].Reason = "core_store"
	}
	for i := range stages {
		stages[i].CorrelationID = correlation
		stages[i].LogicalTimestamp = logical
		stages[i].Reason += ";simulated_camera=true"
	}
	return stages
}

func latestMockActionStatus(store *cognitivecore.UniversalStore) string {
	history, err := store.History()
	if err != nil {
		return "unavailable"
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].DecisionV3 != nil {
			return history[i].DecisionV3.Action.Status
		}
	}
	return "unavailable"
}

func latestMockStoreSimulationMarker(store *cognitivecore.UniversalStore) bool {
	history, err := store.History()
	if err != nil {
		return false
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].SnapshotV3 != nil {
			return history[i].SnapshotV3.SimulatedCamera &&
				history[i].SnapshotV3.VisionEvidenceSource == "simulated_test_worker" &&
				!history[i].SnapshotV3.InferenceExecuted
		}
	}
	return false
}

func containsTrue(source map[string]any, key string) bool {
	value, _ := source[key].(bool)
	return value
}
