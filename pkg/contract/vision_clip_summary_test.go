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
		Topology: VisionClipTopology{NodeID: "front", Zone: "exterior"}, TopologyClass: VisionTopologyProtectedInterior, Trigger: VisionClipTrigger{Reason: "motion.sensor.front", StartedAt: at},
		Track:    VisionClipTrack{ID: "track-1", SubjectType: "human", FirstSeenAt: at, LastSeenAt: at.Add(time.Second), Confidence: .8},
		Identity: VisionClipIdentity{Status: "uncertain", Confidence: .2}, Plate: VisionClipPlate{Status: "not_available"},
		Sensitive: VisionSensitiveObjects{Status: "not_available"}, Media: VisionClipMedia{ClipRef: &clipRef, BestROIRefs: []string{"local://clips/clip-1/roi/1"}},
		Backend:      VisionClipBackendDiagnostic{Name: "existing_detector", ModelVersion: "yolov8.rknn", RealModel: true, Status: "ok", FramesSampled: 2, DetectionsTotal: 2, LatencyMS: 1.5},
		PriorityHint: VisionPriorityP1, PriorityState: "confirmed", ReasonCodes: []string{"human_detected", "protected_interior"},
	}
	if err := summary.Validate(); err != nil {
		t.Fatal(err)
	}
	clipRef = "https://example.invalid/raw"
	if err := summary.Validate(); err == nil || !strings.Contains(err.Error(), "clip reference") {
		t.Fatalf("expected local reference rejection, got %v", err)
	}
}

func TestVisionClipObservationV1ValidationAndRawDataRejection(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	observation := VisionClipObservation{
		SchemaVersion: EventVisionClipObservationV1, ClipID: "clip-1", EpisodeID: "episode-1", CameraID: "cam-1",
		NodeID: "front", Zone: "exterior", TopologyClass: VisionTopologyProtectedInterior, Trigger: "motion.sensor.front", ObservedAt: at, Sequence: 1,
		Tracks:       []VisionClipObservationTrack{{ID: "human-0", SubjectType: "human", Confidence: .8, State: "candidate", DetectionCount: 1}},
		Backend:      VisionClipObservationBackend{Status: "ok", RealModel: true},
		PriorityHint: VisionPriorityP1, PriorityState: "candidate", ReasonCodes: []string{"human_detected", "protected_interior"},
	}
	if err := observation.Validate(); err != nil {
		t.Fatal(err)
	}
	observation.Tracks[0].State = "recognized"
	if err := observation.Validate(); err == nil {
		t.Fatal("invalid enrichment state accepted")
	}
}

func TestVisionClipMetricsRejectInvalidWallOrderingAndInflight(t *testing.T) {
	metrics := VisionClipMetrics{VisionWallLatencyMS: 10, FirstObservationWallMS: 11}
	if err := metrics.Validate(); err == nil {
		t.Fatal("first observation after final result accepted")
	}
	metrics = VisionClipMetrics{PeakFramesInFlight: 4}
	if err := metrics.Validate(); err == nil {
		t.Fatal("inflight bound violation accepted")
	}
}

func TestVisionPriorityAndTopologyValidation(t *testing.T) {
	if !ValidVisionTopologyClass(VisionTopologyProtectedInterior) || ValidVisionTopologyClass("camera-name") {
		t.Fatal("topology class validation is not strict")
	}
	observation := VisionClipObservation{
		SchemaVersion: EventVisionClipObservationV1, ClipID: "clip-1", EpisodeID: "episode-1", CameraID: "cam-1",
		NodeID: "entry", Zone: "door", TopologyClass: VisionTopologyRestrictedThreshold,
		Trigger: "motion", ObservedAt: time.Now().UTC(), Sequence: 1,
		Tracks:       []VisionClipObservationTrack{{ID: "human-0", SubjectType: "human", Confidence: .9, State: "candidate", DetectionCount: 1}},
		Backend:      VisionClipObservationBackend{Status: "ok", RealModel: true},
		PriorityHint: VisionPriorityP1, PriorityState: "candidate", ReasonCodes: []string{"human_detected", "restricted_threshold"},
	}
	if err := observation.Validate(); err != nil {
		t.Fatal(err)
	}
	observation.PriorityHint = VisionPriorityP0
	if err := observation.Validate(); err == nil {
		t.Fatal("Vision worker accepted Core-only P0")
	}
}
