package cognitivecore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synora/internal/facegallery"
	"synora/pkg/contract"
)

func TestVisionEvidenceV1MapsOnlyExistingV3Offsets(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "pkg", "contract", "testdata", "v1", "vision-evidence-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	pose := raw["pose"].(map[string]any)
	pose["availability"], pose["posture"], pose["quality"], pose["posture_confidence"], pose["transition_to_ground_confidence"] = "evaluated", "upright", .7, .8, .37
	pose["support"].(map[string]any)["valid_evaluations"] = float64(4)
	encodedEvidence, _ := json.Marshal(raw)
	evidence, err := contract.DecodeVisionEvidenceV1(encodedEvidence)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := CognitiveSnapshotV3{CapturedAt: evidence.WindowEnd, BaseV2: goldenSnapshotV2(), Vision: VisionSignalsV3{PoseStatus: PoseV3Unavailable, Posture: PostureV3Unknown, FallState: FallV3Unknown}}
	mapped, err := snapshot.ApplyVisionEvidenceV1(evidence)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := (SnapshotEncoderV3{}).Encode(context.Background(), mapped)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.Shape[0] != 86 || encoded.FeatureNames != CognitiveFeatureNamesV3 {
		t.Fatal("V3 dimension or feature order changed")
	}
	if encoded.Values[27] != .7 || encoded.Values[64+3] != 1 || encoded.Values[68+1] != 1 || encoded.Values[84] != .125 || encoded.Values[85] != 1 {
		t.Fatalf("existing V3 pose offsets not mapped as documented: [%v %v %v %v %v]", encoded.Values[27], encoded.Values[67], encoded.Values[69], encoded.Values[84], encoded.Values[85])
	}
	if mapped.VisionEvidence == nil || mapped.VisionEvidence.Pose.TransitionToGroundConfidence != .37 {
		t.Fatal("unmapped continuous evidence was not retained")
	}
}

func TestSyntheticFaceGalleryEvidenceTraversesCoreV3WithoutIdentityOrGateOverride(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "pkg", "contract", "testdata", "v1", "vision-evidence-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := contract.DecodeVisionEvidenceV1(body)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Provenance = "simulated_test"
	evidence.SimulatedCamera = true
	evidence, err = facegallery.ApplySyntheticResult(evidence, facegallery.SemanticResult{Result: "recognized"})
	if err != nil {
		t.Fatal(err)
	}
	encodedEvidence, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedEvidence), "resident_ref") || strings.Contains(string(encodedEvidence), "embedding") || strings.Contains(string(encodedEvidence), "score") || evidence.Face.Confidence != 0 {
		t.Fatal("synthetic semantic evidence contains private identifiers or scores")
	}
	snapshot, err := (CognitiveSnapshotV3{CapturedAt: evidence.WindowEnd, BaseV2: goldenSnapshotV2(), SimulatedCamera: true, VisionStatus: "unavailable", VisionEvidenceSource: "simulated_test_worker"}).ApplyVisionEvidenceV1(evidence)
	if err != nil {
		t.Fatal(err)
	}
	vector, err := (SnapshotEncoderV3{}).Encode(context.Background(), snapshot)
	if err != nil || vector.Shape[0] != 86 || vector.FeatureNames != CognitiveFeatureNamesV3 {
		t.Fatalf("face evidence changed the frozen V3 vector: shape=%v err=%v", vector.Shape, err)
	}
	core := &CoreV3{Store: NewUniversalStore(), Encoder: SnapshotEncoderV3{}, MLP: fixedMLPV3{}, ActiveDryRun: true, Now: func() time.Time { return evidence.WindowEnd }}
	event := contract.Event{ID: "synthetic-face-core-event", Type: contract.EventVisionEvidenceV1, Source: "discovery", Timestamp: evidence.WindowEnd, Payload: map[string]any{"vision_evidence": evidence}}
	result, err := core.Process(context.Background(), event, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if result.Commit.DecisionV3 == nil || result.Commit.DecisionV3.Mode != "active_dry_run" || result.Commit.DecisionV3.PhysicalActionExecuted || result.Commit.DecisionV3.Action.Status != "blocked" || result.Result.Action != nil {
		t.Fatalf("synthetic face signal altered dry-run action guarantees: decision=%+v result=%+v", result.Commit.DecisionV3, result.Result)
	}
	if result.Commit.SnapshotV3 == nil || result.Commit.SnapshotV3.VisionEvidence == nil || result.Commit.SnapshotV3.VisionEvidence.Face.Result != "recognized" {
		t.Fatal("redacted semantic Evidence V1 did not remain in Store")
	}
}

func TestVisionEvidenceV1PreservesSeatedAndReclinedWithoutFallInference(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "pkg", "contract", "testdata", "v1", "vision-evidence-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, posture := range []string{contract.VisionPostureSeated, contract.VisionPostureReclined} {
		t.Run(posture, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Fatal(err)
			}
			pose := raw["pose"].(map[string]any)
			pose["availability"], pose["posture"], pose["posture_confidence"], pose["quality"] = "evaluated", posture, .8, .75
			pose["support"] = map[string]any{"valid_evaluations": 2, "continuity": "continuous", "supported_seconds": 1.0, "gap_count": 0}
			pose["immobility_seconds"] = 0.0
			encoded, _ := json.Marshal(raw)
			evidence, err := contract.DecodeVisionEvidenceV1(encoded)
			if err != nil {
				t.Fatal(err)
			}
			mapped, err := (CognitiveSnapshotV3{CapturedAt: evidence.WindowEnd, BaseV2: goldenSnapshotV2()}).ApplyVisionEvidenceV1(evidence)
			if err != nil {
				t.Fatal(err)
			}
			if mapped.VisionEvidence.Pose.Posture != posture || mapped.Vision.Posture != posture {
				t.Fatalf("pose distinction lost: evidence=%q snapshot=%q", mapped.VisionEvidence.Pose.Posture, mapped.Vision.Posture)
			}
			if !mapped.VisionEvidenceProjection.NotEncodedInSnapshotV3 {
				t.Fatal("unencoded evidence projection was not diagnosed")
			}
			if posture == contract.VisionPostureReclined {
				found := false
				for _, fact := range mapped.VisionEvidenceProjection.Facts {
					found = found || fact == "pose.posture.reclined"
				}
				if !found {
					t.Fatalf("reclined projection diagnostic missing: %#v", mapped.VisionEvidenceProjection)
				}
			}
			if mapped.Vision.FallState == FallV3Candidate || mapped.Vision.FallState == FallV3Confirmed {
				t.Fatalf("posture was interpreted as fall: %#v", mapped.Vision)
			}
			vector, err := (SnapshotEncoderV3{}).Encode(context.Background(), mapped)
			if err != nil {
				t.Fatal(err)
			}
			if vector.Shape[0] != 86 || vector.FeatureNames != CognitiveFeatureNamesV3 {
				t.Fatal("frozen 86D feature contract changed")
			}
		})
	}
}

type fixedMLPV3 struct{}

func (fixedMLPV3) RunV3(context.Context, EncodedSnapshotV3, CognitiveSnapshotV3) (MLPOutputV3, map[string]float64, error) {
	return MLPOutputV3{
		DangerLabel: "critical",
		Incident:    "anomaly",
		Task:        "notify_security",
		Action:      "announce",
		Communication: CommunicationIntentV3{
			Intent: CommunicationNeutralPresenceNoticeV3,
		},
		ModelVersion: "test-v3",
	}, map[string]float64{"shared_backbone": 0}, nil
}

func TestCoreV3UsesIndependentGateAndPersistsCandidateSnapshot(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	base := goldenSnapshotV2()
	base.Communication = CommunicationCapabilitiesV2{AnnounceAvailable: true, TTSStatus: TTSAvailable}
	evidenceEvent := evidenceEventForTest(t, "v3-core-test")
	body, _ := json.Marshal(evidenceEvent.Payload)
	evidence, err := contract.DecodeVisionEvidenceV1(body)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := CognitiveSnapshotV3{CapturedAt: base.CapturedAt, BaseV2: base}
	snapshot, err = snapshot.ApplyVisionEvidenceV1(evidence)
	if err != nil {
		t.Fatal(err)
	}
	core := &CoreV3{Store: store, MLP: fixedMLPV3{}, ActiveDryRun: true, Now: func() time.Time { return time.Unix(2000, 0).UTC() }}
	event := contract.Event{ID: "v3-core-test", Type: contract.EventVisionEvidenceV1, Source: "discovery", Timestamp: snapshot.CapturedAt}
	result, err := core.Process(context.Background(), event, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if result.Commit.DecisionV3 == nil || result.Commit.DecisionV3.Mode != "active_dry_run" {
		t.Fatalf("missing V3 dry-run decision: %#v", result.Commit.DecisionV3)
	}
	if result.Commit.DecisionV3.PhysicalActionExecuted || result.Commit.DecisionV3.Communication.PhysicalAudioPlayed {
		t.Fatal("V3 candidate executed a physical action or audio")
	}
	if stored, ok := store.SnapshotV3(); !ok || stored.Revision != result.Result.Revision {
		t.Fatalf("V3 snapshot was not persisted: ok=%v snapshot=%#v", ok, stored)
	}
	reopened, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stored, ok := reopened.SnapshotV3(); !ok || stored.Revision != result.Result.Revision {
		t.Fatalf("V3 snapshot was not recovered: ok=%v snapshot=%#v", ok, stored)
	}
}
