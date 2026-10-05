package cognitivecore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func goldenSnapshotV2() CognitiveSnapshotV2 {
	return CognitiveSnapshotV2{
		CapturedAt: time.Unix(1_700_000_000, 0).UTC(),
		Security:   SecurityFacts{Armed: true, Known: true},
		Presence:   PresenceFacts{HumanPresent: true, TrackConfirmed: true, TrackCount: 2, KnownResidentCount: 1},
		Topology:   "protected_interior",
		Episode:    EpisodeFacts{Phase: VisionPhaseConfirmed, SegmentCount: 3, GapCount: 1, SecondsSinceFirst: 12, SecondsSinceLast: 2, CalmSeconds: 0.5},
		Sensors:    SensorFacts{Movement: true, AccessState: VisionAccessForced, AlarmState: VisionAlarmTriggered, ObservationCount: 4},
		Vision: VisionSignalsV2{
			HumanConfirmed: true, PoseStatus: PoseAvailable, PoseQuality: 0.8, Posture: PostureLying,
			TransitionToGround: true, ImmobilitySeconds: 4, FallState: FallProbable, FallQualitySufficient: true,
			RiskStatus: RiskConfirmed, RiskKind: RiskKindFirearm, RiskConfidence: 0.9, IdentityStatus: IdentityUncertain,
			RealDetection: true, AggregateConfidence: 0.88, ObservationCount: 3, EdgeTrackingOK: true,
		},
		Communication: CommunicationCapabilitiesV2{AnnounceAvailable: true, TTSStatus: TTSAvailable, CooldownRemainingSeconds: 30},
	}
}

func TestSnapshotEncoderV2GoldenVector(t *testing.T) {
	encoded, err := (SnapshotEncoderV2{}).Encode(context.Background(), goldenSnapshotV2())
	if err != nil {
		t.Fatal(err)
	}
	if encoded.Shape != [1]int{CognitiveVectorSizeV2} || len(encoded.FeatureNames) != CognitiveVectorSizeV2 {
		t.Fatalf("unexpected V2 shape: %#v", encoded.Shape)
	}
	checks := map[int]float32{
		0: 1, 3: 1, 5: 0.125, 6: 1, 10: 1, 14: 1, 16: 3.0 / 32.0,
		24: 1, 25: 1, 26: 1, 27: 0.8, 30: 1, 32: 1, 33: 4.0 / 120.0,
		37: 1, 38: 1, 43: 1, 45: 1, 49: 1, 52: 1, 53: 1, 54: 1, 55: 0.25, 58: 0.88,
	}
	for index, expected := range checks {
		if encoded.Values[index] != expected {
			t.Errorf("golden feature %d = %v, want %v", index, encoded.Values[index], expected)
		}
	}
	if err := encoded.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotV2HasNoRawVisionData(t *testing.T) {
	body, err := json.Marshal(goldenSnapshotV2().Normalized())
	if err != nil {
		t.Fatal(err)
	}
	encoded := strings.ToLower(string(body))
	for _, forbidden := range []string{`"bbox"`, `"crop"`, `"embedding"`, `"local_track_id"`, `"media"`} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("V2 snapshot contains forbidden field %s: %s", forbidden, encoded)
		}
	}
}
