package cognitivecore

import (
	"context"
	"reflect"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestVisionEvidenceFrameGoldenVector(t *testing.T) {
	frame := VisionEvidenceFrame{
		CapturedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Security:       VisionEvidenceFrameSecurity{Armed: true, Known: true},
		Presence:       VisionEvidenceFramePresence{HumanPresent: true, TrackCount: 3, TrackConfirmed: true},
		TopologyClass:  contract.VisionTopologyProtectedInterior,
		Priority:       contract.VisionPriorityP1,
		PriorityOrigin: VisionPriorityOriginVision,
		EpisodePhase:   VisionPhaseConfirmed,
		Enrichment:     VisionEnrichmentUncertain,
		Continuity:     VisionEvidenceFrameContinuity{SecondsSinceFirstObservation: 150, SecondsSinceLastObservation: 30, SegmentCount: 4, GapCount: 2, CalmSeconds: 60},
		Quality:        VisionEvidenceFrameQuality{RealDetection: true, ReplaySimulation: true, ObservationCount: 5, AggregateConfidence: .75},
		CoEvidence:     VisionEvidenceFrameCoEvidence{AccessState: VisionAccessForced, Movement: true, SensorEvidence: true, AlarmState: VisionAlarmTriggered},
	}
	encoded, err := (VisionEvidenceEncoder{}).Encode(context.Background(), frame)
	if err != nil {
		t.Fatal(err)
	}
	want := [VisionEvidenceSize]float32{
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
	if !reflect.DeepEqual(encoded.FeatureNames, VisionEvidenceFeatureNames) {
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

func TestVisionEvidenceFrameMissingFieldsHaveStableDefaults(t *testing.T) {
	encoded, err := (VisionEvidenceEncoder{}).Encode(context.Background(), VisionEvidenceFrame{})
	if err != nil {
		t.Fatal(err)
	}
	want := [VisionEvidenceSize]float32{}
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

func TestVisionEvidenceFrameRejectsVisionP0(t *testing.T) {
	_, err := (VisionEvidenceEncoder{}).Encode(context.Background(), VisionEvidenceFrame{
		Priority:       contract.VisionPriorityP0,
		PriorityOrigin: VisionPriorityOriginVision,
	})
	if err == nil {
		t.Fatal("Vision-created P0 was accepted")
	}
	if _, err := (VisionEvidenceEncoder{}).Encode(context.Background(), VisionEvidenceFrame{
		Priority:       contract.VisionPriorityP0,
		PriorityOrigin: VisionPriorityOriginCore,
	}); err != nil {
		t.Fatalf("Core P0 was rejected: %v", err)
	}
}

func TestVisionEvidenceFrameEncodeDoesNotAllocateVariableVector(t *testing.T) {
	frame := VisionEvidenceFrame{CapturedAt: time.Unix(1, 0).UTC(), TopologyClass: contract.VisionTopologyUnknown, Priority: contract.VisionPriorityP4, PriorityOrigin: VisionPriorityOriginVision}
	encoder := VisionEvidenceEncoder{}
	ctx := context.Background()
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = encoder.Encode(ctx, frame)
	})
	if allocs != 0 {
		t.Fatalf("Vision fixed-vector encode allocated %v objects per run", allocs)
	}
}
