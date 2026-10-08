package discovery

import (
	"testing"
	"time"

	"synora/internal/discovery/vision"
)

func TestClassifyVisionWorkerRunningWithMissingModelsIsDegradedButActive(t *testing.T) {
	status, reason := classifyVisionWorkerStatus(vision.WorkerSnapshot{
		Status: vision.WorkerStatusRunning,
		PID:    1234,
	}, true)
	if status != "degraded" || reason != "running with missing models" {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
}

func TestClassifyVisionWorkerStoppedIsUnavailable(t *testing.T) {
	status, reason := classifyVisionWorkerStatus(vision.WorkerSnapshot{
		Status: vision.WorkerStatusStopped,
	}, false)
	if status != "unavailable" || reason != vision.WorkerStatusStopped {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
}

func TestClassifyVisionWorkerDegradedCapabilitiesIsDegraded(t *testing.T) {
	status, reason := classifyVisionWorkerStatus(vision.WorkerSnapshot{
		Status:           vision.WorkerStatusRunning,
		CapabilityStatus: "degraded",
		CapabilityError:  "ArcFace unavailable",
	}, false)
	if status != "degraded" || reason != "ArcFace unavailable" {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
}

func TestEdgeManifestConversionPreservesAggregateFactsOrRejects(t *testing.T) {
	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	manifest := EdgeTrackManifestV1{
		SchemaVersion: EdgeTrackManifestSchemaV1, CameraID: "camera-safe", EpisodeID: "episode-safe",
		TopologyClass: "protected_interior", TriggerClass: "human", TriggerConfidence: .88,
		TrackingStatus: "ok", StartedAt: start.Format(time.RFC3339), EndedAt: start.Add(4 * time.Second).Format(time.RFC3339),
		TrackCount: 2, ConfirmedTrackCount: 1, ObservationCount: 4, SegmentCount: 3, GapCount: 1,
		PriorityReason: []string{"human_confirmed"}, Metrics: map[string]any{"confidence": .88, "quality": .75, "supported_seconds": 3.5},
	}
	evidence, err := edgeManifestEvidence("opaque-message-id", manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.Validate(); err != nil {
		t.Fatalf("converted Evidence V1 invalid: %v", err)
	}
	if evidence.Topology != manifest.TopologyClass || evidence.Presence.HumanTrackCount != 2 || evidence.Presence.ConfirmedHumanTracks != 1 ||
		evidence.Presence.Human.Confidence != .88 || evidence.Presence.Human.Quality != .75 || evidence.Presence.Human.Support.GapCount != 1 ||
		evidence.Media.Support.ValidEvaluations != 3 || evidence.Media.Support.SupportedSeconds != 3.5 || len(evidence.PriorityReasons) != 1 {
		t.Fatalf("aggregate facts were not preserved: %+v", evidence)
	}
	manifest.EvidenceRefs = []string{"media-reference-not-allowed"}
	if _, err := edgeManifestEvidence("opaque-message-id", manifest); err == nil {
		t.Fatal("unrepresentable evidence reference was silently converted")
	}
	manifest.EvidenceRefs = nil
	manifest.Metrics["unknown_semantic"] = "value"
	if _, err := edgeManifestEvidence("opaque-message-id", manifest); err == nil {
		t.Fatal("unknown active metric was silently dropped")
	}
}
