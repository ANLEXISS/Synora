package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func testSegment(index int, final bool) VisionSegmentReadyV1 {
	digest := sha256.Sum256([]byte("segment-content"))
	hash := hex.EncodeToString(digest[:])
	start := time.Date(2026, 1, 1, 0, 0, index, 0, time.UTC)
	return VisionSegmentReadyV1{
		SchemaVersion: EventVisionSegmentReadyV1, CameraID: "cam-entry", NodeID: "entry",
		EpisodeID: "episode-1", SegmentID: DeterministicVisionSegmentID("cam-entry", "episode-1", index, hash),
		SegmentIndex: index, StartedAt: start, EndedAt: start.Add(time.Second), IsFinal: final,
		TopologyClass: VisionTopologyProtectedInterior, Trigger: "motion", MediaRef: "local://segments/segment.mp4", ContentSHA256: hash,
	}
}

func TestVisionSegmentReadyV1IsDeterministicAndStrict(t *testing.T) {
	segment := testSegment(0, false)
	if err := segment.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := DeterministicVisionSegmentID(segment.CameraID, segment.EpisodeID, segment.SegmentIndex, strings.ToUpper(segment.ContentSHA256)); got != segment.SegmentID {
		t.Fatalf("deterministic id changed with hash case: %s", got)
	}
	segment.MediaRef = "https://example.invalid/segment.mp4"
	if err := segment.Validate(); err == nil {
		t.Fatal("remote media reference accepted")
	}
	segment = testSegment(0, false)
	segment.ContentSHA256 = strings.Repeat("0", 64)
	if err := segment.Validate(); err == nil {
		t.Fatal("content hash change accepted without deterministic id change")
	}
}

