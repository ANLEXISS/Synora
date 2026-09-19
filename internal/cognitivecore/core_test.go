package cognitivecore

import (
	"context"
	"testing"
	"time"

	"synora/pkg/contract"
)

type fakeMLP struct {
	output MLPOutput
}

func (f fakeMLP) Run(context.Context, EncodedSnapshot, CognitiveSnapshot) (MLPOutput, map[string]float64, error) {
	return f.output, map[string]float64{"danger": 0.1, "incident": 0.1, "task": 0.1, "action": 0.1}, nil
}

func TestCognitiveSnapshotFeatureOrderAndDimension(t *testing.T) {
	encoded, err := (SnapshotEncoder{}).Encode(context.Background(), CognitiveSnapshot{SchemaVersion: SnapshotSchemaVersion, CapturedAt: time.Unix(10, 0).UTC(), Topology: contract.VisionTopologyUnknown, Episode: EpisodeFacts{Phase: "initial"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := encoded.Validate(); err != nil {
		t.Fatal(err)
	}
	if encoded.Shape[0] != CognitiveVectorSize || encoded.FeatureNames[42] != "vision.security.armed" {
		t.Fatalf("unexpected full snapshot contract: %#v", encoded)
	}
}

func TestCoreUnavailableModelIsFailClosedAndIdempotent(t *testing.T) {
	store := NewUniversalStore()
	core := &Core{Store: store, MLP: UnavailableMLP{}, Gate: SafetyGate{DryRun: true}, Now: func() time.Time { return time.Unix(20, 0).UTC() }}
	event := contract.Event{ID: "event-1", Type: "sensor.motion", Source: "discovery", Timestamp: time.Unix(20, 0).UTC(), Payload: map[string]any{"movement": true}}
	first, err := core.Process(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if first.Commit.Decision.Status != "unavailable" || first.Commit.Decision.Mode != "active_dry_run" || first.Result.Action != nil {
		t.Fatalf("model absence was not fail-closed: %#v", first.Commit)
	}
	second, err := core.Process(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Result.Duplicate || store.Revision() != first.Result.Revision {
		t.Fatalf("duplicate reopened processing: first=%#v second=%#v", first.Result, second.Result)
	}
}

func TestCoreUsesAbstractActionAndKeepsPhysicalExecutionFalse(t *testing.T) {
	store := NewUniversalStore()
	core := &Core{Store: store, MLP: fakeMLP{output: MLPOutput{DangerLabel: "high", DangerScore: 0.9, Task: "respond", Action: ActionIntent{Action: "lock", Capability: "lock", Topology: contract.VisionTopologyProtectedInterior}}}, Gate: SafetyGate{DryRun: true}, Now: func() time.Time { return time.Unix(30, 0).UTC() }}
	event := contract.Event{ID: "vision-1", Type: contract.EventVisionSegmentReadyV1, Source: "discovery", Timestamp: time.Unix(30, 0).UTC(), Payload: map[string]any{"topology": contract.VisionTopologyProtectedInterior, "human_present": true, "track_count": 1, "track_confirmed": true, "priority": contract.VisionPriorityP1, "segment_count": 1, "confidence": 0.9}}
	result, err := core.Process(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if result.Commit.Decision.Action.Status != "allowed_dry_run" || result.Commit.Decision.Action.Proposed.Action != "lock" || result.Commit.Decision.Action.PhysicalActionExecuted {
		t.Fatalf("unexpected abstract action result: %#v", result.Commit.Decision.Action)
	}
	if len(store.ActionOutbox()) != 1 || !store.ActionOutbox()[0].DryRun {
		t.Fatalf("dry-run action was not persisted in outbox: %#v", store.ActionOutbox())
	}
}

func TestSafetyGateBlocksCapabilityWithoutChoosingAnotherAction(t *testing.T) {
	assessment := (SafetyGate{DryRun: true, Capabilities: map[string]bool{"lock": false}, AllowedTopologies: map[string]bool{contract.VisionTopologyProtectedInterior: true}}).Apply(MLPOutput{Action: ActionIntent{Action: "lock", Capability: "lock"}}, CognitiveSnapshot{Topology: contract.VisionTopologyProtectedInterior}, time.Unix(1, 0).UTC())
	if assessment.Status != "blocked" || assessment.Proposed.Action != "lock" || len(assessment.Reasons) != 1 || assessment.Reasons[0] != "capability_unavailable" {
		t.Fatalf("Safety Gate replaced or lost the MLP intent: %#v", assessment)
	}
}

func TestVisionCannotCreateP0(t *testing.T) {
	store := NewUniversalStore()
	core := &Core{Store: store, MLP: UnavailableMLP{}, Gate: SafetyGate{DryRun: true}}
	_, err := core.Process(context.Background(), contract.Event{ID: "p0", Type: contract.EventVisionSegmentReadyV1, Source: "discovery", Timestamp: time.Now().UTC(), Payload: map[string]any{"priority": contract.VisionPriorityP0}})
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot().VisionEvidence[11]; got != 0 || store.Snapshot().VisionEvidence[15] != 1 {
		t.Fatalf("Vision P0 was not demoted to P4: %#v", store.Snapshot().VisionEvidence)
	}
}
