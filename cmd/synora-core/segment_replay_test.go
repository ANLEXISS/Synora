package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synora/internal/actions"
	"synora/internal/automation"
	"synora/internal/cognitive"
	"synora/internal/discovery/vision"
	"synora/pkg/contract"
)

type segmentReplayManifest struct {
	Path    string                        `json:"path"`
	Segment contract.VisionSegmentReadyV1 `json:"segment"`
}

type segmentReplayManifestFile struct {
	Segments []segmentReplayManifest `json:"segments"`
}

func TestV1SegmentReplay(t *testing.T) {
	manifestPath := strings.TrimSpace(os.Getenv("SYNORA_SEGMENT_MANIFEST"))
	if manifestPath == "" {
		t.Skip("SYNORA_SEGMENT_MANIFEST is not set")
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest segmentReplayManifestFile
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil || len(manifest.Segments) == 0 {
		t.Fatalf("invalid segment manifest: %v", err)
	}
	outDir := strings.TrimSpace(os.Getenv("SYNORA_REPLAY_OUT"))
	if outDir == "" {
		outDir = t.TempDir()
	}
	if err := os.MkdirAll(outDir, 0700); err != nil {
		t.Fatal(err)
	}

	replayBus := newReplayBusRuntime(t)
	defer replayBus.close()
	workerSocket := filepath.Join(t.TempDir(), "vision-worker.sock")
	worker, logs := startReplayWorker(t, workerSocket)
	defer func() {
		_ = worker.Process.Kill()
		_ = worker.Wait()
	}()
	probe, err := dialEventually(workerSocket, 10*time.Second)
	if err != nil {
		t.Fatalf("worker did not start: %v logs=%s", err, logs.String())
	}
	_ = probe.Close()
	workerRuntime := vision.NewRuntimeWithManagerAndSocketTimeout(nil, workerSocket, 2*time.Minute)
	defer workerRuntime.Close()

	processor := &segmentReplayProcessor{runtime: workerRuntime, paths: make(map[string]string, len(manifest.Segments))}
	for _, item := range manifest.Segments {
		if err := item.Segment.Validate(); err != nil {
			t.Fatalf("invalid segment %d: %v", item.Segment.SegmentIndex, err)
		}
		processor.paths[item.Segment.SegmentID] = item.Path
	}
	cfg := vision.DefaultSegmentRuntimeConfig()
	cfg.Enabled = true
	cfg.StatePath = filepath.Join(t.TempDir(), "episode-runtime.json")
	cfg.ReorderWindowSegments = 4
	runtimeV1, err := vision.NewEpisodeRuntimeV1(cfg, processor)
	if err != nil {
		t.Fatal(err)
	}

	var observations []map[string]any
	var canonicalFinalSummary map[string]any
	var finalSegment contract.VisionSegmentReadyV1
	var runtimeMetrics vision.SegmentRuntimeMetrics
	for _, item := range manifest.Segments {
		result, ingestErr := runtimeV1.Ingest(context.Background(), item.Segment)
		if ingestErr != nil {
			t.Fatalf("segment %d ingest failed: %v logs=%s", item.Segment.SegmentIndex, ingestErr, logs.String())
		}
		if item.Segment.IsFinal {
			finalSegment = item.Segment
		}
		for _, event := range result.Events {
			if event.Type == contract.EventVisionClipObservationV1 {
				assertNoRawVisionData(t, event.Payload)
				observations = append(observations, event.Payload)
			}
			if event.Type == contract.EventVisionClipSummaryV1 {
				canonicalFinalSummary = event.Payload
			}
		}
		if err := vision.PublishSegmentRuntimeResult(replayBus.discovery, item.Segment, result); err != nil {
			t.Fatalf("segment %d publish failed: %v", item.Segment.SegmentIndex, err)
		}
		runtimeMetrics = result.Metrics
	}
	if finalSegment.SegmentID == "" {
		t.Fatal("manifest has no final segment")
	}

	var summaryEvent *contract.Event
	waitHermetic(t, "segment replay final summary", func() bool {
		for _, event := range replayBus.app.eventStore.List() {
			if event != nil && event.Type == contract.EventVisionClipSummaryV1 {
				summaryEvent = event
				return true
			}
		}
		return false
	})
	if summaryEvent == nil {
		t.Fatal("Core did not retain final segment summary")
	}
	assertNoRawVisionData(t, summaryEvent.Payload)
	var summary contract.VisionClipSummary
	if canonicalFinalSummary == nil {
		t.Fatal("runtime did not retain canonical final summary")
	}
	if data, err := json.Marshal(canonicalFinalSummary); err != nil {
		t.Fatal(err)
	} else if decoded, err := contract.DecodeVisionClipSummary(data); err != nil {
		t.Fatalf("final summary contract invalid: %v", err)
	} else {
		summary = decoded
	}
	if summary.ClipID != finalSegment.EpisodeID+":final" {
		t.Fatalf("final summary was not episode-scoped: %s", summary.ClipID)
	}

	trackIDs := map[string]struct{}{}
	candidateSeen, confirmedSeen := false, false
	for _, observation := range observations {
		state, _ := observation["priority_state"].(string)
		candidateSeen = candidateSeen || state == "candidate"
		confirmedSeen = confirmedSeen || state == "confirmed"
		for _, raw := range observationTracks(observation) {
			if id, ok := raw["track_id"].(string); ok {
				trackIDs[id] = struct{}{}
			}
		}
	}
	if !candidateSeen || !confirmedSeen {
		t.Fatalf("expected candidate then confirmed P1 state, observations=%v", observations)
	}
	if len(trackIDs) > 1 {
		t.Fatalf("track continuity was not preserved: %v", trackIDs)
	}

	if err := replayBus.app.actionDispatcher.Dispatch(contract.Action{Type: "push", Device: "dry-run-device", Command: "notify"}, automation.ActionContext{SourceEventID: summaryEvent.ID}); err != nil {
		t.Fatal(err)
	}
	actionService := &actions.Service{Bus: replayBus.actions, Deduper: actions.NewDeduper(), Executor: replayBus.actionExec,
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
			t.Fatal("timed out waiting for dry-run action")
		}
	}
	if len(replayBus.actionExec.requests) != 0 {
		t.Fatalf("physical action was executed: %#v", replayBus.actionExec.requests)
	}

	cognitiveInput, err := cognitive.BuildInput(context.Background(), "segment-cognitive-request", cognitive.Task{
		ID: "segment-cognitive-task", Kind: "event_reasoning", RequestedCapabilities: []string{cognitive.CapabilityEventReasoning},
	}, cognitive.StateFrame{SchemaVersion: cognitive.StateFrameSchemaVersion, Revision: replayBus.app.coreRevision.Load(), CapturedAt: time.Now().UTC(),
		CurrentEvent: cognitive.StateEventFromContract(summaryEvent), System: cognitive.StateSystem{DangerLevel: "advisory"}},
		cognitive.ActionCatalog{SchemaVersion: cognitive.ActionCatalogSchemaVersion, Revision: 1}, cognitive.DeterministicStateEncoder{})
	if err != nil {
		t.Fatal(err)
	}
	cognitiveOutput, err := cognitive.NewScheduler(cognitive.DefaultRegistry()).Run(context.Background(), cognitiveInput)
	if err != nil || !cognitiveOutput.AdvisoryOnly || len(cognitiveOutput.ExecutableActions) != 0 {
		t.Fatalf("cognitive shadow boundary crossed: %#v err=%v", cognitiveOutput, err)
	}

	var metrics contract.VisionClipMetrics
	metricsBytes, _ := json.Marshal(summaryEvent.Payload["metrics"])
	if err := json.Unmarshal(metricsBytes, &metrics); err != nil {
		t.Fatal(err)
	}
	if err := metrics.Validate(); err != nil {
		t.Fatalf("final metrics invalid: %v", err)
	}
	result := map[string]any{
		"segment_simulation": true, "camera_transport_real": false,
		"camera_id": finalSegment.CameraID, "episode_id": finalSegment.EpisodeID,
		"segments": len(manifest.Segments), "observations": len(observations),
		"candidate_seen": candidateSeen, "confirmed_seen": confirmedSeen,
		"track_continuity": len(trackIDs) <= 1, "final_summary_count": 1,
		"priority_metrics": map[string]any{"frames_by_priority": metrics.FramesByPriority, "priority_evictions": metrics.PriorityEvictions, "priority_starvation": metrics.PriorityStarvation,
			"trigger_to_candidate_ms": runtimeMetrics.TriggerToCandidateMS, "trigger_to_confirmed_ms": runtimeMetrics.TriggerToConfirmedMS,
			"segments_processed": runtimeMetrics.SegmentsProcessed, "segments_coalesced": runtimeMetrics.SegmentsCoalesced, "segment_gaps": runtimeMetrics.SegmentGaps},
		"physical_action_executed": false, "cognitive_mode": "advisory_shadow",
	}
	writeReplayJSON(t, filepath.Join(outDir, "summary.json"), result)
	writeReplayJSON(t, filepath.Join(outDir, "summary.contract.json"), canonicalFinalSummary)
	writeReplayJSONL(t, filepath.Join(outDir, "observations.jsonl"), observations)
	writeReplayJSONL(t, filepath.Join(outDir, "trace.jsonl"), []map[string]any{
		{"stage": "discovery.segment-ready", "count": len(manifest.Segments), "segment_simulation": true},
		{"stage": "episode-runtime", "ordered": true, "track_continuity": len(trackIDs) <= 1},
		{"stage": "core.final-summary", "count": 1, "candidate_seen": candidateSeen, "confirmed_seen": confirmedSeen},
		{"stage": "cognitive.shadow", "advisory_only": cognitiveOutput.AdvisoryOnly, "executable_actions": 0},
		{"stage": "action.dry-run", "physical_action_executed": false},
	})
	report := fmt.Sprintf("# Vision segment-ready V1 replay\n\n- segment simulation: `true`\n- camera transport real: `false`\n- episode: `%s`\n- segments: `%d`\n- candidate seen: `%t`\n- confirmed seen: `%t`\n- track continuity: `%t`\n- final summaries: `1`\n- cognitive mode: `advisory_shadow`\n- physical action executed: `false`\n", finalSegment.EpisodeID, len(manifest.Segments), candidateSeen, confirmedSeen, len(trackIDs) <= 1)
	if err := os.WriteFile(filepath.Join(outDir, "report.md"), []byte(report), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("SEGMENT_REPLAY_REPORT_DIR %s", outDir)
}

type segmentReplayProcessor struct {
	runtime *vision.Runtime
	paths   map[string]string
}

func (p *segmentReplayProcessor) ProcessSegment(ctx context.Context, segment contract.VisionSegmentReadyV1) ([]vision.Event, error) {
	path := p.paths[segment.SegmentID]
	if path == "" {
		return nil, fmt.Errorf("no replay path for %s", segment.SegmentID)
	}
	return p.runtime.ProcessSegment(ctx, segment, path)
}

func observationTracks(payload map[string]any) []map[string]any {
	raw, _ := payload["tracks"].([]any)
	tracks := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if track, ok := item.(map[string]any); ok {
			tracks = append(tracks, track)
		}
	}
	return tracks
}
