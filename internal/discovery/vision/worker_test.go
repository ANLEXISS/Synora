package vision

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"synora/pkg/contract"
)

type clipProcessorFunc func(*ClipJob) (*WorkerResponse, error)

func (f clipProcessorFunc) Process(job *ClipJob) (*WorkerResponse, error) { return f(job) }

type clipMessagePublisher struct {
	messages []contract.Message
	err      error
}

func (p *clipMessagePublisher) Send(message contract.Message) error {
	if p.err != nil {
		return p.err
	}
	p.messages = append(p.messages, message)
	return nil
}

func TestRunClipWorkerPublishesLifecycleAndStableVisionEventID(t *testing.T) {
	publisher := &clipMessagePublisher{}
	processor := clipProcessorFunc(func(job *ClipJob) (*WorkerResponse, error) {
		return &WorkerResponse{Events: []Event{{Type: contract.EventVisionUnknown, Payload: map[string]any{"clip_id": "spoofed"}}}}, nil
	})
	job := &ClipJob{ID: "clip-1", CameraID: "cam-1", ActivationID: "activation-1", SequenceKey: "sequence-1", NodeID: "entry", Path: "/tmp/clip-1.mp4"}
	if err := RunClipWorker(processor, publisher, job); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 4 || publisher.messages[0].Type != contract.EventClipProcessing || publisher.messages[1].Type != contract.EventVisionUnknown || publisher.messages[2].Type != contract.EventClipProcessed || publisher.messages[3].Type != contract.EventVisionEnd {
		t.Fatalf("unexpected lifecycle messages: %#v", publisher.messages)
	}
	var payload map[string]any
	if err := json.Unmarshal(publisher.messages[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["clip_id"] != "clip-1" || payload["event_id"] != "clip-1:event:0:vision.unknown" || payload["activation_id"] != "activation-1" {
		t.Fatalf("vision metadata not stable: %#v", payload)
	}
}

func TestRunClipWorkerAttemptDefersTerminalFailureUntilPoolExhaustion(t *testing.T) {
	publisher := &clipMessagePublisher{}
	wantErr := errors.New("transient")
	if err := RunClipWorkerAttempt(clipProcessorFunc(func(*ClipJob) (*WorkerResponse, error) {
		return nil, wantErr
	}), publisher, &ClipJob{ID: "clip-retry", CameraID: "cam-1", ActivationID: "activation-1"}); !errors.Is(err, wantErr) {
		t.Fatalf("attempt error=%v", err)
	}
	if len(publisher.messages) != 1 || publisher.messages[0].Type != contract.EventClipProcessing {
		t.Fatalf("retryable attempt published terminal failure: %#v", publisher.messages)
	}
	if err := PublishClipFailure(publisher, &ClipJob{ID: "clip-retry", CameraID: "cam-1", ActivationID: "activation-1"}, "vision_processing_failed"); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 3 || publisher.messages[1].Type != contract.EventClipFailed || publisher.messages[2].Type != contract.EventVisionEnd {
		t.Fatalf("permanent failure lifecycle=%#v", publisher.messages)
	}
}

func TestRunClipWorkerPublishesFailureAndDoesNotClaimProcessed(t *testing.T) {
	publisher := &clipMessagePublisher{}
	errExpected := errors.New("decoder failed")
	err := RunClipWorker(clipProcessorFunc(func(*ClipJob) (*WorkerResponse, error) { return nil, errExpected }), publisher, &ClipJob{ID: "clip-1", CameraID: "cam-1"})
	if !errors.Is(err, errExpected) {
		t.Fatalf("expected processor error, got %v", err)
	}
	if len(publisher.messages) != 2 || publisher.messages[0].Type != contract.EventClipProcessing || publisher.messages[1].Type != contract.EventClipFailed {
		t.Fatalf("unexpected failure lifecycle: %#v", publisher.messages)
	}
}

func TestRunClipWorkerPublishesVersionedClipSummaryToCore(t *testing.T) {
	publisher := &clipMessagePublisher{}
	processor := clipProcessorFunc(func(job *ClipJob) (*WorkerResponse, error) {
		return &WorkerResponse{Events: []Event{{
			Type: contract.EventVisionClipSummaryV1, TrackID: "track-1",
			Payload: validSummaryPayload("clip-v1", "cam-1", "episode-1", "front", "exterior", "motion.sensor.front", "track-1"),
		}}}, nil
	})
	job := validV1Job()
	if err := RunClipWorker(processor, publisher, job); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 3 || publisher.messages[1].Type != contract.EventVisionClipSummaryV1 {
		t.Fatalf("unexpected messages: %#v", publisher.messages)
	}
	var payload map[string]any
	if err := json.Unmarshal(publisher.messages[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["schema"] != contract.EventVisionClipSummaryV1 || payload["clip_id"] != "clip-v1" || payload["track_id"] != "track-1" {
		t.Fatalf("summary metadata not authoritative: %#v", payload)
	}
}

func TestRunClipWorkerPublishesProgressiveObservationBeforeSummary(t *testing.T) {
	publisher := &clipMessagePublisher{}
	processor := clipProcessorFunc(func(job *ClipJob) (*WorkerResponse, error) {
		return &WorkerResponse{Events: []Event{
			{Type: contract.EventVisionClipObservationV1, Payload: map[string]any{
				"schema_version": contract.EventVisionClipObservationV1, "clip_id": job.ID, "episode_id": job.EpisodeID, "camera_id": job.CameraID,
				"node_id": job.NodeID, "zone": job.Zone, "trigger": job.TriggerReason, "observed_at": job.StartedAt,
				"sequence": 1, "tracks": []any{map[string]any{"track_id": "human-0", "subject_type": "human", "confidence": .8, "state": "candidate", "detection_count": 1}},
				"backend":        map[string]any{"status": "ok", "real_model": true},
				"topology_class": contract.VisionTopologyUnknown, "priority_hint": contract.VisionPriorityP4,
				"reason_codes": []string{"human_detected", "unknown_topology"}, "priority_timeline": []any{},
			}},
			{Type: contract.EventVisionClipSummaryV1, TrackID: "track-1", Payload: validSummaryPayload("clip-v1", "cam-1", "episode-1", "front", "exterior", "motion.sensor.front", "track-1")},
		}}, nil
	})
	job := validV1Job()
	if err := RunClipWorker(processor, publisher, job); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 4 || publisher.messages[1].Type != contract.EventVisionClipObservationV1 || publisher.messages[2].Type != contract.EventVisionClipSummaryV1 {
		t.Fatalf("unexpected progressive messages: %#v", publisher.messages)
	}
}

func TestRunClipWorkerRejectsDuplicateObservationSequence(t *testing.T) {
	publisher := &clipMessagePublisher{}
	observation := map[string]any{
		"schema_version": contract.EventVisionClipObservationV1, "clip_id": "clip-v1", "episode_id": "episode-1", "camera_id": "cam-1",
		"node_id": "front", "zone": "exterior", "trigger": "motion.sensor.front", "observed_at": "2026-09-17T12:00:00Z", "sequence": 1,
		"tracks": []any{}, "backend": map[string]any{"status": "ok", "real_model": true},
		"topology_class": contract.VisionTopologyUnknown, "priority_hint": contract.VisionPriorityP4,
		"reason_codes": []string{"backend_error"}, "priority_timeline": []any{},
	}
	err := RunClipWorker(clipProcessorFunc(func(*ClipJob) (*WorkerResponse, error) {
		return &WorkerResponse{Events: []Event{{Type: contract.EventVisionClipObservationV1, Payload: observation}, {Type: contract.EventVisionClipObservationV1, Payload: observation}}}, nil
	}), publisher, validV1Job())
	if err == nil || len(publisher.messages) != 2 || publisher.messages[1].Type != contract.EventClipFailed {
		t.Fatalf("duplicate observation was accepted: err=%v messages=%#v", err, publisher.messages)
	}
}

func validV1Job() *ClipJob {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	return &ClipJob{ID: "clip-v1", CameraID: "cam-1", NodeID: "front", Zone: "exterior", EpisodeID: "episode-1", TriggerReason: "motion.sensor.front", StartedAt: at, Pipeline: "clip-v1"}
}

func validSummaryPayload(clipID, cameraID, episodeID, nodeID, zone, reason, trackID string) map[string]any {
	return map[string]any{
		"schema": contract.EventVisionClipSummaryV1, "clip_id": clipID, "camera_id": cameraID, "episode_id": episodeID,
		"topology":          map[string]any{"node_id": nodeID, "zone": zone},
		"topology_class":    contract.VisionTopologyUnknown,
		"trigger":           map[string]any{"reason": reason, "started_at": "2026-09-17T12:00:00Z"},
		"track":             map[string]any{"id": trackID, "subject_type": "human", "first_seen_at": "2026-09-17T12:00:01Z", "last_seen_at": "2026-09-17T12:00:02Z", "confidence": .8},
		"identity":          map[string]any{"status": "uncertain", "confidence": .2, "embedding_ref": nil},
		"plate":             map[string]any{"status": "not_available", "confidence": 0, "value_ref": nil},
		"sensitive_objects": map[string]any{"status": "not_available", "detections": []any{}},
		"media":             map[string]any{"clip_ref": "local://clips/clip-v1", "best_roi_refs": []string{"local://clips/clip-v1/roi/1"}},
		"backend":           map[string]any{"name": "existing_detector", "model_version": "yolov8.rknn", "real_model": true, "status": "ok", "frames_sampled": 2, "detections_total": 2, "latency_ms": 1.5, "non_human_ignored": 0},
		"priority_hint":     contract.VisionPriorityP4, "reason_codes": []string{"human_detected", "unknown_topology"}, "priority_timeline": []any{},
	}
}

func TestRunClipWorkerRejectsInvalidV1ContractWithoutBusinessPublication(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"spoof camera", func(p map[string]any) { p["camera_id"] = "cam-spoof" }},
		{"spoof clip", func(p map[string]any) { p["clip_id"] = "clip-spoof" }},
		{"spoof episode", func(p map[string]any) { p["episode_id"] = "episode-spoof" }},
		{"spoof node", func(p map[string]any) { p["node_id"] = "node-spoof" }},
		{"spoof topology", func(p map[string]any) { p["topology"].(map[string]any)["node_id"] = "node-spoof" }},
		{"spoof trigger", func(p map[string]any) { p["trigger"].(map[string]any)["reason"] = "trigger-spoof" }},
		{"invalid schema", func(p map[string]any) { p["schema"] = "synora.vision.clip-summary/v9" }},
		{"non-local reference", func(p map[string]any) { p["media"].(map[string]any)["clip_ref"] = "https://example.invalid/raw" }},
		{"invalid identity status", func(p map[string]any) { p["identity"].(map[string]any)["status"] = "maybe" }},
		{"invalid topology", func(p map[string]any) { p["topology"].(map[string]any)["zone"] = "" }},
		{"out of range score", func(p map[string]any) { p["track"].(map[string]any)["confidence"] = 2.0 }},
		{"invalid time order", func(p map[string]any) { p["track"].(map[string]any)["first_seen_at"] = "2026-09-17T12:00:03Z" }},
		{"raw embedding field", func(p map[string]any) { p["embedding"] = []float64{1, 2, 3} }},
		{"raw image field", func(p map[string]any) { p["image"] = "data:image/png;base64,raw" }},
		{"unauthorized event", func(p map[string]any) { _ = p }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			publisher := &clipMessagePublisher{}
			typ := contract.EventVisionClipSummaryV1
			payload := validSummaryPayload("clip-v1", "cam-1", "episode-1", "front", "exterior", "motion.sensor.front", "track-1")
			if tc.name == "unauthorized event" {
				typ = "vision.unknown"
			}
			tc.mutate(payload)
			err := RunClipWorker(clipProcessorFunc(func(*ClipJob) (*WorkerResponse, error) {
				return &WorkerResponse{Events: []Event{{Type: typ, TrackID: "track-1", Payload: payload}}}, nil
			}), publisher, validV1Job())
			if err == nil {
				t.Fatal("invalid V1 response accepted")
			}
			if len(publisher.messages) != 2 || publisher.messages[0].Type != contract.EventClipProcessing || publisher.messages[1].Type != contract.EventClipFailed {
				t.Fatalf("unexpected messages=%#v", publisher.messages)
			}
			for _, message := range publisher.messages {
				if message.Type == contract.EventVisionClipSummaryV1 || message.Type == contract.EventVisionPreliminaryAlertV1 || message.Type == contract.EventClipProcessed {
					t.Fatalf("business event published after invalid contract: %#v", publisher.messages)
				}
			}
		})
	}
}
