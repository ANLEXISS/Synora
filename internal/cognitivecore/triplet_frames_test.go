package cognitivecore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestTripletFramesBuildDeterministicallyFromUniversalStore(t *testing.T) {
	store := NewUniversalStore()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := goldenSnapshotV2()
	base.CapturedAt, base.Topology = at, contract.VisionTopologyRestrictedThreshold
	base.Security.Armed = true
	base.Episode.Phase, base.Episode.SegmentCount, base.Episode.GapCount = VisionPhaseConfirmed, 3, 1
	base.Presence.HumanPresent, base.Vision.HumanConfirmed = false, false
	v3 := CognitiveSnapshotV3{CapturedAt: at, BaseV2: base, Vision: VisionSignalsV3{PoseStatus: PoseV3Unavailable, PoseQuality: 0, Posture: PostureV3Unknown, FallState: FallV3Unknown, CameraHealthStatus: CameraHealthV3Offline}}
	if _, err := store.Commit(Commit{Event: contract.Event{ID: "triplet-test-event", Type: contract.EventVisionEvidenceV1, Source: "discovery", Timestamp: at}, SnapshotV3: &v3, CommittedAt: at}); err != nil {
		t.Fatal(err)
	}
	first, err := BuildTripletFramesV1(store)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildTripletFramesV1(store)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatal("replayed Store state yielded different triplet frames")
	}
	if first.Event.Presence.Human != "unknown" || first.Event.Evidence.PoseAvailability != "unavailable" {
		t.Fatalf("missing Vision became negative evidence: %+v", first.Event)
	}
	if first.Context.Episode.SegmentCount != 3 || first.Context.Episode.GapCount != 1 || first.Context.Episode.FactCount != 1 {
		t.Fatalf("context did not derive from Store ledger: %+v", first.Context.Episode)
	}
	if first.Arbitration.EventBranch.Availability != "unavailable" || first.Arbitration.ContextBranch.Availability != "unavailable" || first.Arbitration.System.ActionPossible {
		t.Fatalf("future MLP branches or action capability are ambiguous: %+v", first.Arbitration)
	}
	for _, forbidden := range []string{"\"bbox\"", "\"crop\"", "\"keypoints\"", "\"biometric_embedding\"", "\"identity\"", "\"local_track_id\"", "\"camera_id\"", "\"url\"", "\"local_path\""} {
		if strings.Contains(string(a), forbidden) {
			t.Fatalf("triplet frame leaked %s", forbidden)
		}
	}
}

func TestTripletBuilderRejectsMissingStoreSnapshot(t *testing.T) {
	if _, err := BuildTripletFramesV1(NewUniversalStore()); err == nil {
		t.Fatal("triplet frame built without persisted V3 snapshot")
	}
}

func TestContextFrameEncodesActionFailureAndCooldownWithoutHiddenMemory(t *testing.T) {
	snapshot := CognitiveSnapshot{CapturedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Topology: contract.VisionTopologyProtectedInterior, ActionResults: []ActionResultFact{{Failed: true}}}
	vision := CognitiveSnapshotV3{CapturedAt: snapshot.CapturedAt, BaseV2: goldenSnapshotV2(), Vision: VisionSignalsV3{PoseStatus: PoseV3Unavailable, Posture: PostureV3Unknown, FallState: FallV3Unknown, CameraHealthStatus: CameraHealthV3Offline}}
	vision.BaseV2.Communication.CooldownActive = true
	frame, err := BuildContextFrameV1(snapshot, nil, []CognitiveSnapshotV3{vision}, true)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Actions.LastResult != "failed" || frame.Actions.FailureCount != 1 || !frame.Actions.Cooldown || !frame.Actions.Pending || frame.Availability.CameraCoverage != "unavailable" {
		t.Fatalf("action/camera context missing: %+v", frame)
	}
	if len(frame.Availability.UnavailableModules) != 2 || frame.Availability.UnavailableModules[0] != "camera_health" || frame.Availability.UnavailableModules[1] != "vision_pose" {
		t.Fatalf("unavailable module mask is not deterministic: %+v", frame.Availability.UnavailableModules)
	}
}
