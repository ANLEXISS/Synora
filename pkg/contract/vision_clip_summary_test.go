package contract

import (
	"strings"
	"testing"
	"time"
)

func TestVisionClipSummaryV1Validation(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	clipRef := "local://clips/clip-1"
	summary := VisionClipSummary{
		Schema: EventVisionClipSummaryV1, EpisodeID: "episode-1", ClipID: "clip-1", CameraID: "cam-1",
		Topology: VisionClipTopology{NodeID: "front", Zone: "exterior"}, Trigger: VisionClipTrigger{Reason: "motion.sensor.front", StartedAt: at},
		Track:    VisionClipTrack{ID: "track-1", SubjectType: "human", FirstSeenAt: at, LastSeenAt: at.Add(time.Second), Confidence: .8},
		Identity: VisionClipIdentity{Status: "uncertain", Confidence: .2}, Plate: VisionClipPlate{Status: "not_available"},
		Sensitive: VisionSensitiveObjects{Status: "not_available"}, Media: VisionClipMedia{ClipRef: &clipRef, BestROIRefs: []string{"local://clips/clip-1/roi/1"}},
	}
	if err := summary.Validate(); err != nil {
		t.Fatal(err)
	}
	clipRef = "https://example.invalid/raw"
	if err := summary.Validate(); err == nil || !strings.Contains(err.Error(), "clip reference") {
		t.Fatalf("expected local reference rejection, got %v", err)
	}
}
