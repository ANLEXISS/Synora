package cognitivecore

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSnapshotEncoderV3AppendsWithoutChangingV2Prefix(t *testing.T) {
	base := goldenSnapshotV2()
	v3 := CognitiveSnapshotV3{
		CapturedAt: base.CapturedAt,
		BaseV2:     base,
		Vision:     VisionSignalsV3{PoseStatus: PoseV3Available, PoseQuality: .9, Posture: PostureV3Ground, FallState: FallV3Candidate, ImmobilitySeconds: 4, RiskStatus: RiskConfirmed, RiskKind: RiskKindFirearm, RiskPersistence: RiskPersistencePersistent, RiskPersistenceSeconds: 4, RiskObservationCount: 3, RiskQualitySufficient: true, PoseObservationCount: 3, PoseSampled: true},
	}
	encoded, err := (SnapshotEncoderV3{}).Encode(context.Background(), v3)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := (SnapshotEncoderV2{}).Encode(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	for i := range v2.Values {
		if encoded.Values[i] != v2.Values[i] {
			t.Fatalf("V3 changed immutable V2 prefix at %d: got %v want %v", i, encoded.Values[i], v2.Values[i])
		}
	}
	if encoded.Shape != [1]int{CognitiveVectorSizeV3} || encoded.Values[73] != 1 || encoded.Values[79] != 1 || encoded.Values[33] != 4.0/120.0 {
		t.Fatalf("unexpected V3 extension: shape=%v values[73]=%v values[79]=%v ground_prefix=%v", encoded.Shape, encoded.Values[73], encoded.Values[79], encoded.Values[33])
	}
	if err := encoded.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotEncoderV3PoseGoldenVectors(t *testing.T) {
	cases := []struct {
		name   string
		vision VisionSignalsV3
		index  int
	}{
		{"unavailable", VisionSignalsV3{PoseStatus: PoseV3Unavailable, Posture: PostureV3Unknown, FallState: FallV3Unknown}, 64},
		{"low_quality", VisionSignalsV3{PoseStatus: PoseV3LowQuality, PoseQuality: .3, Posture: PostureV3Unknown, FallState: FallV3None}, 66},
		{"upright", VisionSignalsV3{PoseStatus: PoseV3Available, PoseQuality: .9, Posture: PostureV3Upright, FallState: FallV3None}, 69},
		{"seated", VisionSignalsV3{PoseStatus: PoseV3Available, PoseQuality: .9, Posture: PostureV3Seated, FallState: FallV3None}, 70},
		{"ground", VisionSignalsV3{PoseStatus: PoseV3Available, PoseQuality: .9, Posture: PostureV3Ground, FallState: FallV3None, ImmobilitySeconds: 2}, 71},
		{"candidate", VisionSignalsV3{PoseStatus: PoseV3Available, PoseQuality: .9, Posture: PostureV3Ground, FallState: FallV3Candidate, ImmobilitySeconds: 2}, 73},
		{"recovery", VisionSignalsV3{PoseStatus: PoseV3Available, PoseQuality: .9, Posture: PostureV3Upright, FallState: FallV3None, RecoveryObserved: true}, 69},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			base := goldenSnapshotV2()
			base.Vision.FallState = FallNone
			encoded, err := (SnapshotEncoderV3{}).Encode(context.Background(), CognitiveSnapshotV3{CapturedAt: base.CapturedAt, BaseV2: base, Vision: item.vision})
			if err != nil {
				t.Fatal(err)
			}
			if encoded.Values[item.index] != 1 {
				t.Fatalf("feature %d = %v, want one-hot", item.index, encoded.Values[item.index])
			}
		})
	}
}

func TestSnapshotV3UsesOneImmobilityDefinitionForEveryPosture(t *testing.T) {
	for _, posture := range []string{PostureV3Upright, PostureV3Seated, PostureV3Ground} {
		base := goldenSnapshotV2()
		encoded, err := (SnapshotEncoderV3{}).Encode(context.Background(), CognitiveSnapshotV3{
			CapturedAt: base.CapturedAt,
			BaseV2:     base,
			Vision:     VisionSignalsV3{PoseStatus: PoseV3Available, Posture: posture, FallState: FallV3None, ImmobilitySeconds: 7},
		})
		if err != nil {
			t.Fatal(err)
		}
		if encoded.Values[33] != 7.0/120.0 {
			t.Fatalf("posture %q changed immobility semantics: got %v", posture, encoded.Values[33])
		}
	}
}

func TestSnapshotV3RejectsFallWithoutAvailablePose(t *testing.T) {
	v := CognitiveSnapshotV3{CapturedAt: time.Unix(1, 0).UTC(), BaseV2: goldenSnapshotV2(), Vision: VisionSignalsV3{PoseStatus: PoseV3Unavailable, FallState: FallV3Confirmed}}
	if err := v.Validate(); err == nil || !strings.Contains(err.Error(), "available pose") {
		t.Fatalf("expected explicit pose gate, got %v", err)
	}
}
