package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/device"
	"synora/internal/engine/contracts"
	"synora/internal/engine/graph"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

func TestCriticalSeedMutationPersistsBackupAndPreservesRealCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cge_critical_chains.yaml")
	seed := engineCriticalSeed("persisted-seed", "vision.unknown", 0.72)
	if err := graph.SaveCriticalSeeds(path, []contracts.CriticalSeed{seed}); err != nil {
		t.Fatal(err)
	}
	engineInstance := newConfigurationTestEngine()
	if err := engineInstance.LoadCriticalSeeds(path); err != nil {
		t.Fatal(err)
	}

	analysis := engineInstance.Analyze(&contract.Event{
		ID: "evt-real-seed", Type: contract.EventVisionUnknown, Source: "test",
		DeviceID: "cam_01", NodeID: "entry", Timestamp: time.Now().UTC(),
	}, state.NewStore())
	if analysis == nil || analysis.DangerAssessment == nil || analysis.DangerAssessment.MatchedSeedID != seed.ID || analysis.DangerAssessment.RiskLevel == "" {
		t.Fatalf("critical match metadata missing from danger assessment: %#v", analysis)
	}
	before := engineBehaviorForSeed(t, engineInstance, seed.ID)
	if before.RealCount != 1 {
		t.Fatalf("real seed match was not counted: %#v", before)
	}

	dangerScore := 0.84
	forbidden := []string{"door.unlock", "disable_camera"}
	updated, err := engineInstance.PatchCriticalSeed(seed.ID, contracts.CriticalSeedPatch{
		DangerScore:      &dangerScore,
		ForbiddenActions: &forbidden,
	})
	if err != nil {
		t.Fatalf("patch critical seed: %v", err)
	}
	if updated.DangerScore != dangerScore || updated.Version <= seed.Version || updated.UpdatedAt.IsZero() {
		t.Fatalf("unexpected updated seed: %#v", updated)
	}
	after := engineBehaviorForSeed(t, engineInstance, seed.ID)
	if after.RealCount != before.RealCount || after.DangerScore != dangerScore {
		t.Fatalf("patch changed real stats: before=%#v after=%#v", before, after)
	}

	backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "cge_critical_chains.*.yaml"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("critical seed backup missing: backups=%v err=%v", backups, err)
	}
	deleted, err := engineInstance.DeleteCriticalSeed(seed.ID)
	if err != nil {
		t.Fatalf("delete critical seed: %v", err)
	}
	if deleted.Enabled || deleted.DeletedAt == nil {
		t.Fatalf("delete must be soft: %#v", deleted)
	}
	countBefore := engineBehaviorForSeed(t, engineInstance, seed.ID).RealCount
	engineInstance.Analyze(&contract.Event{
		ID: "evt-after-delete", Type: contract.EventVisionUnknown, Source: "test",
		DeviceID: "cam_01", NodeID: "entry", Timestamp: time.Now().UTC().Add(time.Second),
	}, state.NewStore())
	behavior := engineBehaviorForSeed(t, engineInstance, seed.ID)
	if behavior.Enabled || behavior.RealCount != countBefore {
		t.Fatalf("deleted seed remained active or lost counters: %#v", behavior)
	}
}

func TestCriticalSeedCreateSupportsEmptyInstallAndRejectsDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "etc", "synora", "cge_critical_chains.yaml")
	engineInstance := newConfigurationTestEngine()
	engineInstance.SetCriticalSeedPath(path)
	seed := engineCriticalSeed("created-seed", "vision.motion", 0.70)
	created, err := engineInstance.CreateCriticalSeed(seed, false)
	if err != nil {
		t.Fatalf("create critical seed: %v", err)
	}
	if created.ID != seed.ID || created.Version != graph.CriticalSeedVersion || created.CreatedAt.IsZero() {
		t.Fatalf("unexpected created seed: %#v", created)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("critical seed file was not created: %v", err)
	}
	if _, err := engineInstance.CreateCriticalSeed(seed, false); err == nil {
		t.Fatal("duplicate critical seed id should be rejected")
	}
	low := engineCriticalSeed("low-created-seed", "vision.motion", 0.60)
	if _, err := engineInstance.CreateCriticalSeed(low, true); err != nil {
		t.Fatalf("create explicitly allowed low-score seed: %v", err)
	}
	if _, err := graph.LoadCriticalSeeds(path); err != nil {
		t.Fatalf("persisted low-score seed must remain loadable after restart: %v", err)
	}
}

func TestCriticalSeedWriteFailureLeavesMemoryUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cge.yaml")
	seed := engineCriticalSeed("rollback-seed", "vision.motion", 0.70)
	if err := graph.SaveCriticalSeeds(path, []contracts.CriticalSeed{seed}); err != nil {
		t.Fatal(err)
	}
	engineInstance := newConfigurationTestEngine()
	if err := engineInstance.LoadCriticalSeeds(path); err != nil {
		t.Fatal(err)
	}
	blockingFile := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	engineInstance.SetCriticalSeedPath(filepath.Join(blockingFile, "cge.yaml"))
	changed := 0.91
	if _, err := engineInstance.PatchCriticalSeed(seed.ID, contracts.CriticalSeedPatch{DangerScore: &changed}); err == nil {
		t.Fatal("write through a non-directory should fail")
	}
	current, ok := engineInstance.CriticalSeed(seed.ID)
	if !ok || current.DangerScore != seed.DangerScore {
		t.Fatalf("memory changed despite failed durable write: %#v", current)
	}
}

func TestApprovedLearnedBehaviorGuidesDecisionWithoutDispatch(t *testing.T) {
	engineInstance := newConfigurationTestEngine()
	seed := engineCriticalSeed("guidance-seed", contract.EventVisionMotion, 0.66)
	engineInstance.ApplyCriticalSeeds([]contracts.CriticalSeed{seed})
	behavior := engineBehaviorForSeed(t, engineInstance, seed.ID)
	risk := 0.91
	actions := []map[string]any{{"action": "light.turn_on"}}
	if _, err := engineInstance.PatchLearnedBehavior(behavior.ID, contracts.LearnedBehaviorPatch{
		RiskOverride:    &risk,
		ProposedActions: &actions,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := engineInstance.ApproveLearnedBehavior(behavior.ID, nil); err != nil {
		t.Fatal(err)
	}

	result := engineInstance.Analyze(&contract.Event{
		ID: "evt-guided", Type: contract.EventVisionMotion, Source: "test",
		DeviceID: "cam_01", NodeID: "entry", Timestamp: time.Now().UTC(),
	}, state.NewStore())
	if result == nil || result.Decision == nil || result.Decision.Score < risk {
		t.Fatalf("approved behavior did not raise decision safety floor: %#v", result)
	}
	if _, err := engineInstance.DisableLearnedBehavior(behavior.ID); err != nil {
		t.Fatal(err)
	}
	unguided := engineInstance.Analyze(&contract.Event{
		ID: "evt-disabled", Type: contract.EventVisionMotion, Source: "test",
		DeviceID: "cam_01", NodeID: "entry", Timestamp: time.Now().UTC().Add(time.Second),
	}, state.NewStore())
	if unguided == nil || unguided.Decision == nil || unguided.Decision.Score >= risk {
		t.Fatalf("disabled behavior still guided decision: %#v", unguided)
	}
}

func TestApplyUserValidationApprovesAndForbidsBehaviorActions(t *testing.T) {
	engineInstance := newConfigurationTestEngine()
	seed := engineCriticalSeed("validation-seed", contract.EventVisionMotion, 0.70)
	seed.ProposedActions = []string{"light.turn_on"}
	engineInstance.ApplyCriticalSeeds([]contracts.CriticalSeed{seed})
	behavior := engineBehaviorForSeed(t, engineInstance, seed.ID)

	if err := engineInstance.ApplyUserValidation(contract.ValidationRequest{
		ID: "validation-approve", BehaviorID: behavior.ID,
		Type: contract.ValidationTypeBehaviorApproval, Status: contract.ValidationStatusApproved,
		Enabled: true,
	}); err != nil {
		t.Fatalf("apply approval validation: %v", err)
	}
	approved, _ := engineInstance.LearnedBehavior(behavior.ID)
	if approved.Status != contracts.LearnedBehaviorApproved || !approved.Enabled {
		t.Fatalf("behavior not approved through validation: %#v", approved)
	}

	if err := engineInstance.ApplyUserValidation(contract.ValidationRequest{
		ID: "validation-forbid", BehaviorID: behavior.ID,
		Type: contract.ValidationTypeActionFeedback, Status: contract.ValidationStatusCorrected,
		Correction: map[string]any{"forbidden_action": "light.turn_on"}, Enabled: true,
	}); err != nil {
		t.Fatalf("apply forbidden action feedback: %v", err)
	}
	corrected, _ := engineInstance.LearnedBehavior(behavior.ID)
	if corrected.Status != contracts.LearnedBehaviorDisabled || corrected.Enabled || len(corrected.ForbiddenActions) != 1 {
		t.Fatalf("approved behavior remained active after forbidden action feedback: %#v", corrected)
	}
}

func TestApplyUserValidationFeedbackIsIdempotentByValidationID(t *testing.T) {
	engineInstance := newConfigurationTestEngine()
	seed := engineCriticalSeed("feedback-seed", contract.EventVisionMotion, 0.70)
	engineInstance.ApplyCriticalSeeds([]contracts.CriticalSeed{seed})
	behavior := engineBehaviorForSeed(t, engineInstance, seed.ID)
	validation := contract.ValidationRequest{
		ID: "validation-false-positive", BehaviorID: behavior.ID,
		Type: contract.ValidationTypeFalsePositive, Status: contract.ValidationStatusCorrected,
		Enabled: true,
	}
	if err := engineInstance.ApplyUserValidation(validation); err != nil {
		t.Fatal(err)
	}
	if err := engineInstance.ApplyUserValidation(validation); err != nil {
		t.Fatal(err)
	}
	updated, _ := engineInstance.LearnedBehavior(behavior.ID)
	if updated.UserFeedback.FalsePositiveCount != 1 || len(updated.UserFeedback.ValidationIDs) != 1 {
		t.Fatalf("validation feedback was applied more than once: %#v", updated.UserFeedback)
	}
}

func TestShouldPersistDangerAssessmentDelegatesDomainPolicy(t *testing.T) {
	assessment := &contract.DangerAssessment{
		ID: "danger-1", EventType: contract.EventVisionWeapon,
		Score: 0.90, Level: 5, RiskLevel: "critical", Category: contract.DangerCategorySecurity,
	}
	if !ShouldPersistDangerAssessment(assessment) {
		t.Fatal("critical assessment should be persistable")
	}
	assessment.EventType = contract.EventDiscoveryWorkerCrashed
	if ShouldPersistDangerAssessment(assessment) {
		t.Fatal("discovery worker assessment should be excluded")
	}
}

func newConfigurationTestEngine() *Engine {
	return NewEngine(
		&topology.Topology{Nodes: map[string]*topology.Node{}},
		device.NewRegistry(),
		nil,
	)
}

func engineCriticalSeed(id string, eventType string, dangerScore float64) contracts.CriticalSeed {
	return contracts.CriticalSeed{
		ID: id, Name: id, DangerScore: dangerScore,
		RiskLevel: "high", ExpectedState: "suspicious",
		Sequence:           []contracts.CriticalSeedStep{{EventType: eventType}},
		RequiresValidation: true, Enabled: true,
	}
}

func engineBehaviorForSeed(t *testing.T, engineInstance *Engine, seedID string) contracts.LearnedBehavior {
	t.Helper()
	for _, behavior := range engineInstance.LearnedBehaviors() {
		if behavior.CriticalSeedID == seedID {
			return behavior
		}
	}
	t.Fatalf("behavior for seed %q missing: %#v", seedID, engineInstance.LearnedBehaviors())
	return contracts.LearnedBehavior{}
}
