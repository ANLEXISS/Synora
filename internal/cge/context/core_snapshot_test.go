package context

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func testCoreSnapshot(at time.Time) CoreContextSnapshot {
	value := CoreContextSnapshot{
		CapturedAtUnixNano: at.UnixNano(), HomeMode: string(HouseModeHome), SystemState: "idle",
		Residents: []ResidentContext{{ResidentFingerprint: "sha256:resident", PresenceCode: "present", CurrentNodeCode: "entry", ConfidencePermille: 900, LastSeenUnixNano: at.UnixNano(), FreshnessCode: string(FreshnessFresh)}},
		Devices:   []DeviceContext{{DeviceFingerprint: "sha256:device", NodeCode: "entry", DeviceKind: "sensor", HealthCode: "online", Online: true, LastSeenUnixNano: at.UnixNano(), FreshnessCode: string(FreshnessFresh)}},
		Cameras:   []CameraContext{{CameraFingerprint: "sha256:camera", NodeCode: "entry", Online: true, HealthCode: "online", StreamAvailable: true, DetectionAvailable: true, LastSeenUnixNano: at.UnixNano(), FreshnessCode: string(FreshnessFresh)}},
		Topology:  CoreTopologyContext{Revision: "sha256:topology", Nodes: []Node{{ID: "entry", Kind: NodeRoom}}, ObservationNode: "entry"},
		Freshness: ContextFreshness{Overall: FreshnessFresh, Residents: FreshnessFresh, Devices: FreshnessFresh, Cameras: FreshnessFresh, Topology: FreshnessFresh},
	}
	return value.WithFingerprint()
}

func TestCoreContextSnapshotMarkersAndRedaction(t *testing.T) {
	at := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	snapshot := testCoreSnapshot(at)
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("snapshot validation: %v", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range []string{"SENSITIVE-RESIDENT-NAME", "SENSITIVE-RESIDENT-ID", "SENSITIVE-DEVICE-ID", "SENSITIVE-CAMERA-ID", "SENSITIVE-IP", "SENSITIVE-TOKEN"} {
		if string(encoded) == sentinel || contains(string(encoded), sentinel) {
			t.Fatalf("snapshot leaked sentinel %q: %s", sentinel, encoded)
		}
	}
	if !snapshot.Markers.ReadOnlySnapshot || !snapshot.Markers.Expurgated || !snapshot.Markers.DoesNotOwnCoreState || !snapshot.Markers.DoesNotModifyCoreState || !snapshot.Markers.NoSecurityMeaning {
		t.Fatalf("unsafe snapshot markers: %+v", snapshot.Markers)
	}
	clone := snapshot.Clone()
	clone.Residents[0].PresenceCode = "absent"
	if snapshot.Residents[0].PresenceCode != "present" {
		t.Fatal("snapshot clone shares mutable resident storage")
	}
}

func TestCoreContextSnapshotFingerprintAndFrameAreDeterministic(t *testing.T) {
	at := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	first := testCoreSnapshot(at)
	second := first.Clone()
	if first.Fingerprint != second.Fingerprint || !reflect.DeepEqual(first, second) {
		t.Fatalf("snapshot fingerprint is not stable: first=%+v second=%+v", first, second)
	}
	frame, err := first.Frame(SnapshotRequest{ObservationID: "event-1", ObservedAt: at, NodeID: "entry"})
	if err != nil {
		t.Fatalf("frame conversion: %v", err)
	}
	if frame.Occupancy != OccupancyOccupied || frame.HouseMode != HouseModeHome || frame.SnapshotFingerprint != first.Fingerprint || frame.FreshnessCode != string(FreshnessFresh) {
		t.Fatalf("live context was not reflected in frame: %+v", frame)
	}
}

func TestContextFreshnessPolicyAndClassification(t *testing.T) {
	policy := DefaultContextFreshnessPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	if policy.Fingerprint() == "" || policy.Fingerprint() != DefaultContextFreshnessPolicy().Fingerprint() {
		t.Fatalf("freshness fingerprint unstable: %q", policy.Fingerprint())
	}
	at := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	if ClassifyFreshness(at, at, policy.ResidentFreshFor, policy.ResidentStaleAfter) != FreshnessFresh || ClassifyFreshness(at, at.Add(-5*time.Minute), policy.ResidentFreshFor, policy.ResidentStaleAfter) != FreshnessAging || ClassifyFreshness(at, at.Add(-20*time.Minute), policy.ResidentFreshFor, policy.ResidentStaleAfter) != FreshnessStale || ClassifyFreshness(at, time.Time{}, policy.ResidentFreshFor, policy.ResidentStaleAfter) != FreshnessUnknown {
		t.Fatal("freshness classification does not distinguish fresh, aging, stale and unknown")
	}
	invalid := policy
	invalid.DeviceFreshFor = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero freshness threshold accepted")
	}
}

func TestCoreContextSnapshotProviderContractHonorsCancellation(t *testing.T) {
	provider := staticCoreSnapshotProvider{snapshot: testCoreSnapshot(time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Snapshot(ctx, SnapshotRequest{ObservationID: "event", ObservedAt: time.Now().UTC()}); err == nil {
		t.Fatal("cancelled context was accepted")
	}
}

type staticCoreSnapshotProvider struct{ snapshot CoreContextSnapshot }

func (p staticCoreSnapshotProvider) Resolve(context.Context, string, time.Time, string) (Frame, error) {
	return Frame{}, nil
}

func (p staticCoreSnapshotProvider) Snapshot(ctx context.Context, _ SnapshotRequest) (CoreContextSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return CoreContextSnapshot{}, err
	}
	return p.snapshot.Clone(), nil
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
