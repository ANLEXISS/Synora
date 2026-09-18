package cognitive

import (
	"context"
	"reflect"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestStateFrameV5GoldenVector(t *testing.T) {
	frame := StateFrameV5{
		CapturedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Security:       StateFrameV5Security{Armed: true, Known: true},
		Presence:       StateFrameV5Presence{HumanPresent: true, TrackCount: 3, TrackConfirmed: true},
		TopologyClass:  contract.VisionTopologyProtectedInterior,
		Priority:       contract.VisionPriorityP1,
		PriorityOrigin: V5PriorityOriginVision,
		EpisodePhase:   V5PhaseConfirmed,
		Enrichment:     V5EnrichmentUncertain,
		Continuity:     StateFrameV5Continuity{SecondsSinceFirstObservation: 150, SecondsSinceLastObservation: 30, SegmentCount: 4, GapCount: 2, CalmSeconds: 60},
		Quality:        StateFrameV5Quality{RealDetection: true, ReplaySimulation: true, ObservationCount: 5, AggregateConfidence: .75},
		CoEvidence:     StateFrameV5CoEvidence{AccessState: V5AccessForced, Movement: true, SensorEvidence: true, AlarmState: V5AlarmTriggered},
	}
	encoded, err := (V5StateEncoder{}).Encode(context.Background(), frame)
	if err != nil {
		t.Fatal(err)
	}
	want := [EncoderV5Size]float32{
		1, 0, 1, 1, .1875, 1,
		0, 0, 0, 1, 0,
		0, 1, 0, 0, 0,
		0, 0, 1, 0,
		0, 0, 1, 0, 0,
		.5, .1, .125, .125, .2,
		1, 1, .15625, .75,
		0, 0, 0, 1, 1, 1, 0, 0, 0, 1,
	}
	if encoded.Values != want {
		t.Fatalf("golden vector changed:\n got=%v\nwant=%v", encoded.Values, want)
	}
	if err := encoded.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(encoded.FeatureNames, EncoderV5FeatureNames) {
		t.Fatal("feature order is not the frozen order")
	}
	first, err := frame.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	second, err := frame.Normalized().Fingerprint()
	if err != nil || first != second {
		t.Fatalf("fingerprint is not deterministic: %q %q %v", first, second, err)
	}
}

func TestStateFrameV5MissingFieldsHaveStableDefaults(t *testing.T) {
	encoded, err := (V5StateEncoder{}).Encode(context.Background(), StateFrameV5{})
	if err != nil {
		t.Fatal(err)
	}
	want := [EncoderV5Size]float32{}
	want[10] = 1 // unknown topology
	want[15] = 1 // P4 default priority
	want[16] = 1 // initial phase
	want[20] = 1 // unavailable enrichment
	want[34] = 1 // unknown access
	want[40] = 1 // unknown alarm
	if encoded.Values != want {
		t.Fatalf("unexpected absent-field defaults: got=%v want=%v", encoded.Values, want)
	}
}

func TestStateFrameV5RejectsVisionP0(t *testing.T) {
	_, err := (V5StateEncoder{}).Encode(context.Background(), StateFrameV5{
		Priority:       contract.VisionPriorityP0,
		PriorityOrigin: V5PriorityOriginVision,
	})
	if err == nil {
		t.Fatal("Vision-created P0 was accepted")
	}
	if _, err := (V5StateEncoder{}).Encode(context.Background(), StateFrameV5{
		Priority:       contract.VisionPriorityP0,
		PriorityOrigin: V5PriorityOriginCore,
	}); err != nil {
		t.Fatalf("Core P0 was rejected: %v", err)
	}
}

func TestStateFrameV5DoesNotChangeV4Contract(t *testing.T) {
	if StateEncoderSchemaVersion != "state-encoder/v4" || EncoderV4Size != 477 || EncoderV4Version != "4.0.0" {
		t.Fatalf("V4 contract changed: schema=%s size=%d version=%s", StateEncoderSchemaVersion, EncoderV4Size, EncoderV4Version)
	}
}

func TestStateFrameV5EncodeDoesNotAllocateVariableVector(t *testing.T) {
	frame := StateFrameV5{CapturedAt: time.Unix(1, 0).UTC(), TopologyClass: contract.VisionTopologyUnknown, Priority: contract.VisionPriorityP4, PriorityOrigin: V5PriorityOriginVision}
	encoder := V5StateEncoder{}
	ctx := context.Background()
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = encoder.Encode(ctx, frame)
	})
	if allocs != 0 {
		t.Fatalf("V5 fixed-vector encode allocated %v objects per run", allocs)
	}
}
