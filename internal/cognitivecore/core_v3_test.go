package cognitivecore

import (
	"context"
	"testing"
	"time"

	"synora/pkg/contract"
)

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
	snapshot := CognitiveSnapshotV3{
		CapturedAt: base.CapturedAt,
		BaseV2:     base,
		Vision: VisionSignalsV3{
			PoseStatus:      PoseV3Unavailable,
			Posture:         PostureV3Unknown,
			FallState:       FallV3None,
			RiskStatus:      RiskSuspected,
			RiskKind:        RiskKindOther,
			RiskPersistence: RiskPersistenceIsolated,
		},
	}
	core := &CoreV3{Store: store, MLP: fixedMLPV3{}, ActiveDryRun: true, Now: func() time.Time { return time.Unix(2000, 0).UTC() }}
	event := contract.Event{ID: "v3-core-test", Type: contract.EventVisionEnrichmentV3, Source: "discovery.v3-test", Timestamp: snapshot.CapturedAt}
	result, err := core.Process(context.Background(), event, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if result.Commit.DecisionV3 == nil || result.Commit.DecisionV3.Mode != "active_dry_run" {
		t.Fatalf("missing V3 dry-run decision: %#v", result.Commit.DecisionV3)
	}
	if result.Commit.DecisionV3.Communication.Status != "blocked" || result.Commit.DecisionV3.Action.Status != "blocked" {
		t.Fatalf("high-risk announce was not independently blocked: %#v", result.Commit.DecisionV3)
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
