package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"synora/internal/actions"
	"synora/internal/automation"
	"synora/internal/bus"
	"synora/internal/cognitive"
	"synora/internal/discovery/ingress"
	"synora/internal/discovery/vision"
	"synora/pkg/contract"
)

type replayExpectation struct {
	MinimumHumanDetections int    `json:"minimum_human_detections"`
	MinimumTracks          int    `json:"minimum_tracks"`
	BackendStatus          string `json:"backend_status"`
	PhysicalActionExecuted bool   `json:"physical_action_executed"`
}

func TestV1RealClipReplay(t *testing.T) {
	clipPath := strings.TrimSpace(os.Getenv("SYNORA_REPLAY_CLIP"))
	if clipPath == "" {
		t.Skip("SYNORA_REPLAY_CLIP is not set")
	}
	clipPath, _ = filepath.Abs(clipPath)
	stat, err := os.Stat(clipPath)
	if err != nil || !stat.Mode().IsRegular() {
		t.Fatalf("replay clip is not a regular file: %s err=%v", clipPath, err)
	}
	if filepath.Ext(clipPath) == "" {
		t.Fatal("replay clip must have a media extension")
	}
	outDir := strings.TrimSpace(os.Getenv("SYNORA_REPLAY_OUT"))
	if outDir == "" {
		outDir = t.TempDir()
	}
	if err := os.MkdirAll(outDir, 0700); err != nil {
		t.Fatal(err)
	}

	var expectation replayExpectation
	if path := strings.TrimSpace(os.Getenv("SYNORA_REPLAY_EXPECT")); path != "" {
		var err error
		expectation, err = readReplayExpectation(path)
		if err != nil {
			t.Fatalf("invalid replay expectation: %v", err)
		}
	}

	clipRoot := t.TempDir()
	t.Setenv("SYNORA_CLIP_DIR", clipRoot)
	when := time.Now().UTC().Truncate(time.Millisecond)
	cameraID := firstReplayValue("SYNORA_REPLAY_CAMERA_ID", "cam_entry_01")
	nodeID := firstReplayValue("SYNORA_REPLAY_NODE_ID", "entry")
	zone := firstReplayValue("SYNORA_REPLAY_ZONE", "interior_entry")
	trigger := firstReplayValue("SYNORA_REPLAY_TRIGGER", "motion")
	queue := &integrationClipQueue{}

	replayBus := newReplayBusRuntime(t)
	defer replayBus.close()
	handler := ingress.NewHandler(ingress.Config{
		ClipDir: clipRoot, Queue: queue, Publisher: replayBus.discovery,
		AllowInsecure: true, MaxClipSize: stat.Size() + (1 << 20), MaxClipCount: 4,
		MaxClipBytes: stat.Size() + (1 << 20), ClipDuration: 10 * time.Second,
	})
	request := replayMultipartRequest(t, clipPath, cameraID, filepath.Base(clipPath))
	request.Header.Set("X-Synora-Pipeline", "clip-v1")
	request.Header.Set("X-Synora-Node-ID", nodeID)
	request.Header.Set("X-Synora-Zone", zone)
	request.Header.Set("X-Synora-Trigger-Reason", trigger)
	request.Header.Set("X-Synora-Started-At", when.Format(time.RFC3339Nano))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || len(queue.jobs) != 1 {
		t.Fatalf("replay ingress failed status=%d body=%s jobs=%d", response.Code, response.Body.String(), len(queue.jobs))
	}
	job := queue.jobs[0]
	waitHermetic(t, "replay clip ready", func() bool {
		value, ok := replayBus.app.state.Clip(job.ID)
		return ok && value != nil && value.Status == contract.ClipStatusReady
	})

	workerSocket := filepath.Join(t.TempDir(), "vision-worker.sock")
	worker, logs := startReplayWorker(t, workerSocket)
	defer func() {
		if worker.Process != nil {
			_ = worker.Process.Kill()
		}
		_ = worker.Wait()
	}()
	conn, err := dialEventually(workerSocket, 5*time.Second)
	if err != nil {
		t.Fatalf("worker did not start: %v logs=%s", err, logs.String())
	}
	defer conn.Close()
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)
	if err := encoder.Encode(map[string]any{"request_id": "replay-hello", "operation": "protocol.hello", "protocol_version": vision.VisionProtocolVersion}); err != nil {
		t.Fatal(err)
	}
	var hello map[string]any
	if err := decoder.Decode(&hello); err != nil || hello["protocol_version"] != vision.VisionProtocolVersion {
		t.Fatalf("worker handshake failed response=%#v err=%v logs=%s", hello, err, logs.String())
	}
	if err := encoder.Encode(map[string]any{
		"request_id": "replay-clip", "operation": vision.VisionClipProcess, "pipeline": "clip-v1",
		"clip_path": job.Path, "clip_id": job.ID, "camera_id": job.CameraID,
		"episode_id": job.EpisodeID, "node_id": job.NodeID, "zone": job.Zone,
		"topology_class": job.TopologyClass,
		"trigger_reason": job.TriggerReason, "started_at": job.StartedAt, "ends_at": job.EndsAt,
	}); err != nil {
		t.Fatal(err)
	}
	var workerResponse vision.WorkerResponse
	if err := decoder.Decode(&workerResponse); err != nil {
		t.Fatalf("worker replay response failed: %v logs=%s", err, logs.String())
	}
	if workerResponse.Error != "" {
		t.Fatalf("real replay worker error: %s logs=%s", workerResponse.Error, logs.String())
	}
	if len(workerResponse.Events) == 0 {
		t.Fatalf("real replay worker returned no summary logs=%s", logs.String())
	}
	observations := []map[string]any{}
	lastObservationSequence := 0
	workerSummarySeen := false
	for _, event := range workerResponse.Events {
		if event.Type == contract.EventVisionClipObservationV1 {
			if workerSummarySeen {
				t.Fatal("worker emitted progressive observation after final summary")
			}
			payload := event.Payload
			assertNoRawVisionData(t, payload)
			observationBytes, _ := json.Marshal(payload)
			observation, observationErr := contract.DecodeVisionClipObservation(observationBytes)
			if observationErr != nil {
				t.Fatalf("invalid progressive observation: %v", observationErr)
			}
			if observation.Sequence <= lastObservationSequence {
				t.Fatalf("progressive observation sequence is not increasing: %d after %d", observation.Sequence, lastObservationSequence)
			}
			lastObservationSequence = observation.Sequence
			observations = append(observations, payload)
		}
		if event.Type == contract.EventVisionClipSummaryV1 {
			workerSummarySeen = true
		}
	}
	if err := vision.RunClipWorker(visionProcessorFunc(func(*vision.ClipJob) (*vision.WorkerResponse, error) {
		return &workerResponse, nil
	}), replayBus.discovery, job); err != nil {
		t.Fatal(err)
	}
	waitHermetic(t, "replay clip processed", func() bool {
		value, ok := replayBus.app.state.Clip(job.ID)
		return ok && value != nil && value.Status == contract.ClipStatusProcessed
	})

	var summaryEvent *contract.Event
	trackIDs := map[string]struct{}{}
	summarySeen := false
	for _, event := range replayBus.app.eventStore.List() {
		if event == nil {
			continue
		}
		if event.Type == contract.EventVisionClipObservationV1 {
			if summarySeen {
				t.Fatal("progressive observation was published after final summary")
			}
			assertNoRawVisionData(t, event.Payload)
		}
		if event.Type == contract.EventVisionClipSummaryV1 {
			summarySeen = true
			if summaryEvent == nil {
				summaryEvent = event
			}
			if event.TrackID != "" && event.TrackID != "clip-no-human" {
				trackIDs[event.TrackID] = struct{}{}
			}
		}
	}
	if summaryEvent == nil {
		t.Fatal("Core did not retain replay summary")
	}
	assertNoRawVisionData(t, summaryEvent.Payload)
	var backend contract.VisionClipBackendDiagnostic
	backendBytes, _ := json.Marshal(summaryEvent.Payload["backend"])
	if err := json.Unmarshal(backendBytes, &backend); err != nil {
		t.Fatal(err)
	}
	if err := backend.Validate(); err != nil {
		t.Fatalf("Core summary backend diagnostic invalid: %v", err)
	}
	var metrics contract.VisionClipMetrics
	metricsBytes, _ := json.Marshal(summaryEvent.Payload["metrics"])
	if err := json.Unmarshal(metricsBytes, &metrics); err != nil {
		t.Fatalf("invalid vision wall metrics: %v", err)
	}
	if err := metrics.Validate(); err != nil {
		t.Fatalf("vision wall metrics validation failed: %v", err)
	}

	if err := replayBus.app.actionDispatcher.Dispatch(contract.Action{Type: "push", Device: "dry-run-device", Command: "notify"}, automationContext(summaryEvent.ID)); err != nil {
		t.Fatal(err)
	}
	actionExec := replayBus.actionExec
	actionService := &actions.Service{Bus: replayBus.actions, Deduper: actions.NewDeduper(), Executor: actionExec,
		ExecutionMode: actions.ExecutionDryRun, EnforceExecutionMode: true, Now: func() time.Time { return time.Now().UTC() }}
	deadline := time.After(5 * time.Second)
	for len(replayBus.app.state.ActionResultsList()) == 0 {
		select {
		case message := <-replayBus.actions.SubscribeChannel("actions"):
			if message.Type == contract.EventActionRequest {
				actionService.HandleMessage(context.Background(), message)
			}
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatal("timed out waiting for replay dry_run action")
		}
	}
	if len(actionExec.requests) != 0 {
		t.Fatalf("replay reached physical executor: %#v", actionExec.requests)
	}

	cognitiveInput, err := cognitive.BuildInput(context.Background(), "replay-cognitive-request", cognitive.Task{
		ID: "replay-cognitive-task", Kind: "event_reasoning", RequestedCapabilities: []string{cognitive.CapabilityEventReasoning},
	}, cognitive.StateFrame{SchemaVersion: cognitive.StateFrameSchemaVersion, Revision: replayBus.app.coreRevision.Load(), CapturedAt: when,
		CurrentEvent: cognitive.StateEventFromContract(summaryEvent), System: cognitive.StateSystem{DangerLevel: "advisory"}},
		cognitive.ActionCatalog{SchemaVersion: cognitive.ActionCatalogSchemaVersion, Revision: 1}, cognitive.DeterministicStateEncoder{})
	if err != nil {
		t.Fatal(err)
	}
	cognitiveOutput, err := cognitive.NewScheduler(cognitive.DefaultRegistry()).Run(context.Background(), cognitiveInput)
	if err != nil || !cognitiveOutput.AdvisoryOnly || len(cognitiveOutput.ExecutableActions) != 0 {
		t.Fatalf("replay cognitive output crossed shadow boundary: %#v err=%v", cognitiveOutput, err)
	}

	humanDetections := backend.DetectionsTotal
	tracksFinal := len(trackIDs)
	if expectation.MinimumHumanDetections > humanDetections || expectation.MinimumTracks > tracksFinal || (expectation.BackendStatus != "" && expectation.BackendStatus != backend.Status) || expectation.PhysicalActionExecuted {
		t.Fatalf("replay expectation failed: backend=%#v human_detections=%d tracks=%d expectation=%#v", backend, humanDetections, tracksFinal, expectation)
	}

	summary := map[string]any{
		"clip_id": job.ID, "episode_id": job.EpisodeID, "python_worker_real": true,
		"vision_model_real": backend.RealModel, "detector_backend": backend.Name, "detector_status": backend.Status,
		"frames_sampled": backend.FramesSampled, "human_detections": humanDetections, "tracks_final": tracksFinal,
		"observations": len(observations), "worker_latency_ms": backend.LatencyMS,
		"first_observation_wall_ms": metrics.FirstObservationWallMS, "vision_wall_latency_ms": metrics.VisionWallLatencyMS,
		"detector_compute_sum_ms": metrics.DetectorComputeSumMS, "metrics": metrics,
		"priority_hint": summaryEvent.Payload["priority_hint"],
		"first_observation_priority": func() any {
			if len(observations) == 0 {
				return nil
			}
			return observations[0]["priority_hint"]
		}(),
		"reason_codes":             summaryEvent.Payload["reason_codes"],
		"priority_timeline":        summaryEvent.Payload["priority_timeline"],
		"physical_action_executed": false, "cognitive_mode": "advisory_shadow",
	}
	writeReplayJSON(t, filepath.Join(outDir, "summary.json"), summary)
	writeReplayJSON(t, filepath.Join(outDir, "summary.contract.json"), summaryEvent.Payload)
	writeReplayJSONL(t, filepath.Join(outDir, "observations.jsonl"), observations)
	traceLines := []map[string]any{
		{"stage": "clip", "clip_id": job.ID, "episode_id": job.EpisodeID},
		{"stage": "vision.observations", "count": len(observations), "last_sequence": lastObservationSequence},
		{"stage": "vision.summary", "event_id": summaryEvent.ID, "event_type": summaryEvent.Type, "backend": backend},
		{"stage": "cognitive", "mode": "advisory_shadow", "advisory_only": cognitiveOutput.AdvisoryOnly},
		{"stage": "action", "execution_mode": string(actions.ExecutionDryRun), "physical_action_executed": false},
	}
	writeReplayJSONL(t, filepath.Join(outDir, "trace.jsonl"), traceLines)
	report := fmt.Sprintf("# Vision Clip V1 priority replay\n\n- clip: `%s`\n- episode: `%s`\n- detector: `%s` (`%s`)\n- topology class: `%v`\n- first observation priority: `%v`\n- first observation wall time: %.3f ms\n- final vision wall time: %.3f ms\n- detector compute sum (cumulative NPU): %.3f ms\n- frames sampled: `%d`\n- frames by priority: `%v`\n- tracks final: `%d`\n- progressive observations: `%d`\n- priority timeline entries: `%d`\n- enrichments executed: `%d`\n- enrichments avoided: `%d`\n- priority evictions: `%d`\n- priority starvation: `%d`\n- physical action executed: `false`\n- cognitive mode: `advisory_shadow`\n", job.ID, job.EpisodeID, backend.Name, backend.Status, summaryEvent.Payload["topology_class"], summary["first_observation_priority"], metrics.FirstObservationWallMS, metrics.VisionWallLatencyMS, metrics.DetectorComputeSumMS, backend.FramesSampled, metrics.FramesByPriority, tracksFinal, len(observations), len(observations), metrics.EnrichmentsExecuted, metrics.EnrichmentsAvoided, metrics.PriorityEvictions, metrics.PriorityStarvation)
	if err := os.WriteFile(filepath.Join(outDir, "report.md"), []byte(report), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("REPLAY_REPORT_DIR %s", outDir)
	t.Logf("REPLAY_SUMMARY %s", string(mustJSON(t, summary)))
}

type replayBusRuntime struct {
	server     *bus.Server
	core       *bus.Client
	discovery  *bus.Client
	actions    *bus.Client
	app        *coreApp
	stop       chan struct{}
	cancel     context.CancelFunc
	coreDone   chan error
	actionExec *hermeticActionExecutor
}

func newReplayBusRuntime(t *testing.T) *replayBusRuntime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bus.sock")
	server := bus.NewServer(path)
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Start() }()
	var coreClient, discoveryClient, actionClient *bus.Client
	var err error
	for attempt := 0; attempt < 100; attempt++ {
		coreClient, err = bus.NewClient(path, "core")
		if err == nil {
			discoveryClient, err = bus.NewClient(path, "discovery")
		}
		if err == nil {
			actionClient, err = bus.NewClient(path, "actions")
		}
		if err == nil {
			break
		}
		if coreClient != nil {
			_ = coreClient.Close()
		}
		if discoveryClient != nil {
			_ = discoveryClient.Close()
		}
		if actionClient != nil {
			_ = actionClient.Close()
		}
		coreClient, discoveryClient, actionClient = nil, nil, nil
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	app, _ := newTestCoreApp(t)
	app.bus = coreClient
	app.snapshotPublisher.Bus = coreClient
	app.actionDispatcher.Bus = coreClient
	app.actionDispatcher.Now = func() time.Time { return time.Now().UTC() }
	stop := make(chan struct{})
	app.processStop = stop
	ctx, cancel := context.WithCancel(context.Background())
	coreDone := make(chan error, 1)
	app.startBackgroundLoops()
	go func() { coreDone <- app.runBusLoopContext(ctx) }()
	executor := &hermeticActionExecutor{}
	return &replayBusRuntime{server: server, core: coreClient, discovery: discoveryClient, actions: actionClient, app: app, stop: stop, cancel: cancel, coreDone: coreDone, actionExec: executor}
}

func (r *replayBusRuntime) close() {
	if r == nil {
		return
	}
	r.cancel()
	close(r.stop)
	r.app.lifecycleWG.Wait()
	_ = r.core.Close()
	_ = r.discovery.Close()
	_ = r.actions.Close()
	_ = r.server.Close()
	select {
	case <-r.coreDone:
	case <-time.After(time.Second):
	}
}

func startReplayWorker(t *testing.T, socket string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(source)))
	script := filepath.Join(root, "services", "vision-worker", "worker.py")
	cmd := exec.Command("python3", script)
	cmd.Dir = filepath.Dir(script)
	cmd.Env = append(os.Environ(), "SYNORA_VISION_SOCKET="+socket, "SYNORA_VISION_DEBUG=0", "SYNORA_VISION_CLIP_V1_ENABLED=1", "SYNORA_VISION_CLIP_V1_DETECTOR_MODE=real_shadow", "SYNORA_VISION_FACE_ENABLED=0", "SYNORA_VISION_PLATE_ENABLED=0", "SYNORA_VISION_SENSITIVE_OBJECTS_ENABLED=0", "PYTHONUNBUFFERED=1")
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd, &logs
}

func replayMultipartRequest(t *testing.T, path, cameraID, clipID string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("clip", clipID)
	if err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := io.Copy(part, file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	_ = file.Close()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/vision", &body)
	req.Header.Set("X-Synora-Device", cameraID)
	req.Header.Set("X-Synora-Clip-ID", strings.TrimSuffix(clipID, filepath.Ext(clipID)))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func writeReplayJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeReplayJSONL(t *testing.T, path string, values []map[string]any) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for _, value := range values {
		data, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
}

func assertNoRawVisionData(t *testing.T, payload map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	for _, forbidden := range []string{"\"frame\"", "\"image\"", "\"embedding\"", "\"crop\"", "\"roi\""} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("raw vision field leaked to Core payload: %s", forbidden)
		}
	}
}

func firstReplayValue(envName, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return value
	}
	return fallback
}

func automationContext(eventID string) automation.ActionContext {
	return automation.ActionContext{SourceEventID: eventID}
}

func readReplayExpectation(path string) (replayExpectation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return replayExpectation{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var expectation replayExpectation
	if err := decoder.Decode(&expectation); err != nil {
		return replayExpectation{}, err
	}
	if expectation.MinimumHumanDetections < 0 || expectation.MinimumTracks < 0 {
		return replayExpectation{}, fmt.Errorf("minimums must be non-negative")
	}
	if expectation.BackendStatus != "" {
		switch expectation.BackendStatus {
		case "ok", "unavailable", "failed", "timeout":
		default:
			return replayExpectation{}, fmt.Errorf("unsupported backend_status %q", expectation.BackendStatus)
		}
	}
	return expectation, nil
}
