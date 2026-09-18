package vision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

type segmentTestProcessor struct{ calls []int }

func (p *segmentTestProcessor) ProcessSegment(_ context.Context, segment contract.VisionSegmentReadyV1) ([]Event, error) {
	p.calls = append(p.calls, segment.SegmentIndex)
	return []Event{
		{Type: contract.EventVisionClipObservationV1, Payload: map[string]any{
			"schema_version": contract.EventVisionClipObservationV1, "clip_id": segment.SegmentID,
			"episode_id": segment.EpisodeID, "camera_id": segment.CameraID, "node_id": segment.NodeID,
			"zone": segment.TopologyClass, "topology_class": segment.TopologyClass, "trigger": segment.Trigger,
			"observed_at": segment.EndedAt, "sequence": 1, "priority_hint": contract.VisionPriorityP1,
			"priority_state": "candidate", "tracks": []any{}, "priority_timeline": []any{},
			"reason_codes": []any{"human_detected"}, "backend": map[string]any{"status": "unavailable", "real_model": false},
		}},
		{Type: contract.EventVisionClipSummaryV1, TrackID: "human-0", Payload: map[string]any{
			"schema": contract.EventVisionClipSummaryV1, "episode_id": segment.EpisodeID, "clip_id": segment.SegmentID,
			"camera_id": segment.CameraID, "topology": map[string]any{"node_id": segment.NodeID, "zone": segment.TopologyClass},
			"topology_class": segment.TopologyClass, "trigger": map[string]any{"reason": segment.Trigger, "started_at": segment.StartedAt},
			"priority_hint": contract.VisionPriorityP1, "priority_state": "confirmed", "reason_codes": []any{"human_detected"},
		}},
	}, nil
}

func TestEpisodeRuntimeV1OrderingIdempotencyGapAndFinalClosure(t *testing.T) {
	processor := &segmentTestProcessor{}
	cfg := DefaultSegmentRuntimeConfig()
	cfg.Enabled = true
	cfg.ReorderWindowSegments = 1
	runtime, err := NewEpisodeRuntimeV1(cfg, processor)
	if err != nil {
		t.Fatal(err)
	}
	first := testRuntimeSegment(0, false)
	second := testRuntimeSegment(1, true)
	if _, err := runtime.Ingest(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Ingest(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Finalized || len(result.Events) != 3 || len(processor.calls) != 2 {
		t.Fatalf("unexpected ordered closure result: %#v calls=%v", result, processor.calls)
	}
	if result.Events[0].Payload["sequence"] != 1 || result.Events[1].Payload["sequence"] != 2 {
		t.Fatalf("observation sequence was not rewritten: %#v", result.Events)
	}
	duplicate, err := runtime.Ingest(context.Background(), first)
	if err != nil || !duplicate.Duplicate || len(processor.calls) != 2 {
		t.Fatalf("identical duplicate was not coalesced: %#v err=%v", duplicate, err)
	}
	divergent := first
	divergent.ContentSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	divergent.SegmentID = contract.DeterministicVisionSegmentID(divergent.CameraID, divergent.EpisodeID, divergent.SegmentIndex, divergent.ContentSHA256)
	if _, err := runtime.Ingest(context.Background(), divergent); !errors.Is(err, ErrSegmentDuplicateHash) {
		t.Fatalf("divergent duplicate error=%v", err)
	}
}

func TestEpisodeRuntimeV1GapAndRestartContinuity(t *testing.T) {
	processor := &segmentTestProcessor{}
	cfg := DefaultSegmentRuntimeConfig()
	cfg.Enabled = true
	cfg.ReorderWindowSegments = 1
	cfg.StatePath = filepath.Join(t.TempDir(), "episode-state.json")
	runtime, err := NewEpisodeRuntimeV1(cfg, processor)
	if err != nil {
		t.Fatal(err)
	}
	far := testRuntimeSegment(3, true)
	result, err := runtime.Ingest(context.Background(), far)
	if err != nil || !result.GapDetected || result.GapReason != "segment_gap_detected" {
		t.Fatalf("gap was not explicit: %#v err=%v", result, err)
	}
	if _, err := NewEpisodeRuntimeV1(cfg, processor); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewEpisodeRuntimeV1(cfg, processor)
	if err != nil {
		t.Fatal(err)
	}
	result, err = loaded.Ingest(context.Background(), testRuntimeSegment(0, false))
	if err != nil || !result.ContinuityReset {
		t.Fatalf("restart continuity was not exposed: %#v err=%v", result, err)
	}
}

func testRuntimeSegment(index int, final bool) contract.VisionSegmentReadyV1 {
	digest := sha256.Sum256([]byte("runtime-segment-content"))
	hash := hex.EncodeToString(digest[:])
	segment := contract.VisionSegmentReadyV1{
		SchemaVersion: contract.EventVisionSegmentReadyV1, CameraID: "cam-entry", NodeID: "entry",
		EpisodeID: "episode-runtime", SegmentIndex: index, IsFinal: final,
		StartedAt:     time.Date(2026, 1, 1, 0, 0, index, 0, time.UTC),
		TopologyClass: contract.VisionTopologyProtectedInterior, Trigger: "motion",
		MediaRef: "local://segments/runtime.mp4", ContentSHA256: hash,
	}
	segment.SegmentID = contract.DeterministicVisionSegmentID(segment.CameraID, segment.EpisodeID, index, segment.ContentSHA256)
	segment.EndedAt = segment.StartedAt.Add(time.Second)
	return segment
}
