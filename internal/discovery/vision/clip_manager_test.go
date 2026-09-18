package vision

import (
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestClipManagerAssignsFixedWindowAndEpisodeContinuity(t *testing.T) {
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	m, err := NewClipManager(10*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.Open("clip-1", "cam-1", "front", "exterior", "motion.sensor.front", "track-1", base)
	if err != nil {
		t.Fatal(err)
	}
	if !first.EndsAt.Equal(base.Add(10 * time.Second)) {
		t.Fatalf("ends_at=%s", first.EndsAt)
	}
	second, err := m.Open("clip-2", "cam-1", "front", "exterior", "motion.sensor.front", "", base.Add(12*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.EpisodeID != first.EpisodeID {
		t.Fatalf("episode continuity lost: %s != %s", second.EpisodeID, first.EpisodeID)
	}
	third, err := m.Open("clip-3", "cam-1", "front", "exterior", "motion.sensor.front", "track-1", base.Add(40*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if third.EpisodeID == second.EpisodeID {
		t.Fatal("distant clip reused episode")
	}
}

func TestClipManagerContinuityRequiresSameCameraNodeZoneAndTimeWindow(t *testing.T) {
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	m, err := NewClipManager(10*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.Open("clip-1", "cam-1", "front", "exterior", "motion", "worker-track", base)
	if err != nil {
		t.Fatal(err)
	}
	lateSameTrack, err := m.Open("clip-2", "cam-1", "front", "exterior", "motion", "worker-track", base.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if lateSameTrack.EpisodeID == first.EpisodeID {
		t.Fatal("late worker track reused an expired episode")
	}
	otherCamera, err := m.Open("clip-3", "cam-2", "front", "exterior", "motion", "worker-track", base.Add(32*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	otherZone, err := m.Open("clip-4", "cam-1", "front", "interior", "motion", "worker-track", base.Add(34*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if otherCamera.EpisodeID == lateSameTrack.EpisodeID || otherZone.EpisodeID == lateSameTrack.EpisodeID {
		t.Fatal("camera or zone change reused an episode")
	}
}

func TestClipManagerOnlyLearnsTrackFromValidatedSummaryWithinEpisode(t *testing.T) {
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	m, err := NewClipManager(10*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.Open("clip-1", "cam-1", "front", "exterior", "motion", "worker-track", base)
	if err != nil {
		t.Fatal(err)
	}
	summary := contract.VisionClipSummary{
		Schema: contract.EventVisionClipSummaryV1, EpisodeID: first.EpisodeID, ClipID: first.ClipID, CameraID: first.CameraID,
		Topology: contract.VisionClipTopology{NodeID: first.NodeID, Zone: first.Zone}, TopologyClass: contract.VisionTopologyUnknown,
		Trigger:  contract.VisionClipTrigger{Reason: first.TriggerReason, StartedAt: first.StartedAt},
		Track:    contract.VisionClipTrack{ID: "worker-track", SubjectType: "human", FirstSeenAt: base, LastSeenAt: base.Add(time.Second), Confidence: .8},
		Identity: contract.VisionClipIdentity{Status: "uncertain"}, Plate: contract.VisionClipPlate{Status: "not_available"},
		Sensitive:    contract.VisionSensitiveObjects{Status: "not_available"},
		Backend:      contract.VisionClipBackendDiagnostic{Name: "existing_detector", ModelVersion: "yolov8.rknn", RealModel: true, Status: "ok", FramesSampled: 1, DetectionsTotal: 1},
		PriorityHint: contract.VisionPriorityP4, ReasonCodes: []string{"human_detected", "unknown_topology"},
	}
	if err := m.ObserveSummary(summary); err != nil {
		t.Fatal(err)
	}
	second, err := m.Open("clip-2", "cam-1", "front", "exterior", "motion", "worker-track", base.Add(12*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.EpisodeID != first.EpisodeID {
		t.Fatalf("validated summary did not preserve nearby episode: %s != %s", second.EpisodeID, first.EpisodeID)
	}
	restarted, err := NewClipManager(10*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	newEpisode, err := restarted.Open("clip-3", "cam-1", "front", "exterior", "motion", "worker-track", base.Add(13*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if newEpisode.EpisodeID == first.EpisodeID {
		t.Fatal("restart reused an in-memory episode")
	}
}
