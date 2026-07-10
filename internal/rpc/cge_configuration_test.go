package rpc

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"synora/internal/automation"
	"synora/internal/device"
	"synora/internal/engine"
	cgecontracts "synora/internal/engine/contracts"
	"synora/internal/event"
	"synora/internal/snapshot"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

func TestCriticalSeedCRUDPersistsWithoutChangingRealCounters(t *testing.T) {
	dir := t.TempDir()
	seedPath := filepath.Join(dir, "cge_critical_chains.yaml")
	engineInstance := newRPCConfigurationEngine()
	engineInstance.SetCriticalSeedPath(seedPath)
	store := state.NewStore(state.WithPersistencePath(filepath.Join(dir, "state.json")))
	server := NewServer(Config{State: store, CGE: engineInstance})

	createdAny, err := server.Handler("cge.critical_seed.create")(rpcMessage(`{
		"id":"entry_intrusion","name":"Entry intrusion","enabled":true,
		"danger_score":0.82,"risk_level":"high","expected_state":"intrusion",
		"sequence":[{"event_type":"vision.unknown"}],
		"proposed_actions":["notify_user"],"forbidden_actions":["door.unlock"],
		"requires_validation":true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	created := createdAny.(map[string]any)
	if created["id"] != "entry_intrusion" || created["danger_score"] != 0.82 {
		t.Fatalf("unexpected seed: %#v", created)
	}
	behavior := behaviorForSeed(t, engineInstance, "entry_intrusion")
	if behavior.RealCount != 0 {
		t.Fatalf("seed creation changed real_count: %#v", behavior)
	}

	updatedAny, err := server.Handler("cge.critical_seed.update")(mutationMessage("entry_intrusion", `{
		"danger_score":0.91,"forbidden_actions":["door.unlock","disable_camera"]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	updated := updatedAny.(map[string]any)
	if updated["danger_score"] != 0.91 {
		t.Fatalf("seed danger score not patched: %#v", updated)
	}
	behavior = behaviorForSeed(t, engineInstance, "entry_intrusion")
	if behavior.RealCount != 0 || len(behavior.ForbiddenActions) != 2 {
		t.Fatalf("seed patch lost counters/actions: %#v", behavior)
	}

	backups, err := filepath.Glob(filepath.Join(dir, "backups", "cge_critical_chains.*.yaml"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("critical seed backup missing: %v err=%v", backups, err)
	}

	deletedAny, err := server.Handler("cge.critical_seed.delete")(deleteMessage("entry_intrusion"))
	if err != nil {
		t.Fatal(err)
	}
	deleted := deletedAny.(map[string]any)
	if deleted["enabled"] != false || deleted["deleted_at"] == nil {
		t.Fatalf("critical seed was not soft deleted: %#v", deleted)
	}
	behavior = behaviorForSeed(t, engineInstance, "entry_intrusion")
	if behavior.Enabled || behavior.Status != cgecontracts.LearnedBehaviorDisabled || behavior.RealCount != 0 {
		t.Fatalf("seed behavior not safely disabled: %#v", behavior)
	}
}

func TestCriticalSeedLowScoreAndForbiddenActions(t *testing.T) {
	engineInstance := newRPCConfigurationEngine()
	engineInstance.SetCriticalSeedPath(filepath.Join(t.TempDir(), "critical.yaml"))
	server := NewServer(Config{State: state.NewStore(), CGE: engineInstance})
	low := `{
		"id":"low","name":"Low","enabled":true,"danger_score":0.5,
		"risk_level":"medium","expected_state":"suspicious",
		"sequence":[{"event_type":"vision.motion"}],"requires_validation":true
	}`
	if _, err := server.Handler("cge.critical_seed.create")(rpcMessage(low)); contract.APIErrorCode(err) != contract.ErrorValidationFailed {
		t.Fatalf("low seed error=%v code=%s", err, contract.APIErrorCode(err))
	}
	var fields map[string]any
	_ = json.Unmarshal([]byte(low), &fields)
	fields["allow_low_score"] = true
	allowed, _ := json.Marshal(fields)
	if _, err := server.Handler("cge.critical_seed.create")(rpcMessage(string(allowed))); err != nil {
		t.Fatalf("explicit low score should be accepted: %v", err)
	}
	if _, err := server.Handler("cge.critical_seed.create")(rpcMessage(`{
		"id":"unsafe","name":"Unsafe","enabled":true,"danger_score":0.9,
		"risk_level":"critical","expected_state":"intrusion",
		"sequence":[{"event_type":"vision.unknown"}],
		"proposed_actions":["door.unlock"],"requires_validation":true
	}`)); contract.APIErrorCode(err) != contract.ErrorForbiddenAction {
		t.Fatalf("unsafe seed error=%v code=%s", err, contract.APIErrorCode(err))
	}
}

func TestLearnedBehaviorGuidancePersistsAndProtectsCounters(t *testing.T) {
	dir := t.TempDir()
	engineInstance := newRPCConfigurationEngine()
	engineInstance.SetCriticalSeedPath(filepath.Join(dir, "critical.yaml"))
	store := state.NewStore(state.WithPersistencePath(filepath.Join(dir, "state.json")))
	server := NewServer(Config{State: store, CGE: engineInstance})
	if _, err := server.Handler("cge.critical_seed.create")(rpcMessage(`{
		"id":"guide","name":"Guide","enabled":true,"danger_score":0.8,
		"risk_level":"high","expected_state":"suspicious",
		"sequence":[{"event_type":"vision.unknown"}],
		"proposed_actions":["notify_user"],"forbidden_actions":["door.unlock"],
		"requires_validation":true
	}`)); err != nil {
		t.Fatal(err)
	}
	behavior := behaviorForSeed(t, engineInstance, "guide")
	id := behavior.ID

	if _, err := server.Handler("cge.learned_behavior.update")(mutationMessage(id, `{"real_count":99}`)); contract.APIErrorCode(err) != contract.ErrorValidationFailed {
		t.Fatalf("counter patch error=%v code=%s", err, contract.APIErrorCode(err))
	}
	if _, err := server.Handler("cge.learned_behavior.update")(mutationMessage(id, `{
		"proposed_actions":[{"action":"door.unlock"}],"user_notes":"never unlock"
	}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Handler("cge.learned_behavior.action")(behaviorActionMessage(id, "approve", `{}`)); contract.APIErrorCode(err) != contract.ErrorForbiddenAction {
		t.Fatalf("forbidden approval error=%v code=%s", err, contract.APIErrorCode(err))
	}
	if _, err := server.Handler("cge.learned_behavior.update")(mutationMessage(id, `{
		"proposed_actions":[{"action":"notify_user"}],"forbidden_actions":["door.unlock"]
	}`)); err != nil {
		t.Fatal(err)
	}
	approvedAny, err := server.Handler("cge.learned_behavior.action")(behaviorActionMessage(id, "approve", `{"requires_validation":true}`))
	if err != nil {
		t.Fatal(err)
	}
	approved := approvedAny.(map[string]any)
	if approved["status"] != cgecontracts.LearnedBehaviorApproved || approved["enabled"] != true {
		t.Fatalf("behavior not approved: %#v", approved)
	}
	if _, ok := store.BehaviorOverride(id); !ok {
		t.Fatal("learned behavior override was not persisted")
	}
	if current, _ := engineInstance.LearnedBehavior(id); current.RealCount != behavior.RealCount {
		t.Fatalf("guidance changed real_count: before=%d after=%d", behavior.RealCount, current.RealCount)
	}

	if _, err := server.Handler("cge.learned_behavior.action")(behaviorActionMessage(id, "reject", `{}`)); err != nil {
		t.Fatal(err)
	}
	rejected, _ := engineInstance.LearnedBehavior(id)
	if rejected.Enabled || rejected.Status != cgecontracts.LearnedBehaviorRejected {
		t.Fatalf("behavior not rejected: %#v", rejected)
	}
	if _, err := server.Handler("cge.learned_behavior.action")(behaviorActionMessage(id, "reset", `{}`)); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.BehaviorOverride(id); ok {
		t.Fatal("reset did not remove persisted overrides")
	}
	forgottenAny, err := server.Handler("cge.learned_behavior.delete")(deleteMessage(id))
	if err != nil {
		t.Fatal(err)
	}
	if forgottenAny.(map[string]any)["forgotten"] != true {
		t.Fatalf("behavior was not forgotten: %#v", forgottenAny)
	}
}

func TestUserValidationGuidesBehaviorAndPersistsCorrection(t *testing.T) {
	dir := t.TempDir()
	engineInstance := newRPCConfigurationEngine()
	engineInstance.SetCriticalSeedPath(filepath.Join(dir, "critical.yaml"))
	store := state.NewStore(state.WithPersistencePath(filepath.Join(dir, "state.json")))
	server := NewServer(Config{State: store, CGE: engineInstance})
	if _, err := server.Handler("cge.critical_seed.create")(rpcMessage(`{
		"id":"validation_seed","name":"Validation seed","enabled":true,"danger_score":0.8,
		"risk_level":"high","expected_state":"suspicious",
		"sequence":[{"event_type":"vision.unknown"}],"proposed_actions":["notify_user"],
		"requires_validation":true
	}`)); err != nil {
		t.Fatal(err)
	}
	behavior := behaviorForSeed(t, engineInstance, "validation_seed")
	createdAny, err := server.Handler("validations.create")(rpcMessage(fmt.Sprintf(`{
		"id":"approve-guide","behavior_id":%q,"type":"behavior_approval",
		"status":"approved","notes":"confirmed"
	}`, behavior.ID)))
	if err != nil {
		t.Fatal(err)
	}
	created := createdAny.(*contract.ValidationRequest)
	if created.Correction == nil || created.UpdatedAt.IsZero() {
		t.Fatalf("validation not normalized/persisted: %#v", created)
	}
	approved, _ := engineInstance.LearnedBehavior(behavior.ID)
	if approved.Status != cgecontracts.LearnedBehaviorApproved {
		t.Fatalf("validation did not approve behavior: %#v", approved)
	}
	if _, err := server.Handler("validations.create")(rpcMessage(fmt.Sprintf(`{
		"id":"false-positive","behavior_id":%q,"type":"false_positive",
		"status":"corrected","correction":{"reason":"pet"}
	}`, behavior.ID))); err != nil {
		t.Fatal(err)
	}
	after, _ := engineInstance.LearnedBehavior(behavior.ID)
	if after.UserFeedback.FalsePositiveCount != 1 || after.ConfidenceOverride == nil {
		t.Fatalf("false positive did not guide confidence: %#v", after)
	}
	stored, ok := store.Validation("false-positive")
	if !ok || stored.Correction["reason"] != "pet" {
		t.Fatalf("validation correction not persisted: %#v", stored)
	}
}

func TestDangerAssessmentRPCIsFilteredAndBounded(t *testing.T) {
	store := state.NewStore()
	evidence := make([]string, 25)
	actions := make([]contract.SystemActionRecommendation, 15)
	for i := range evidence {
		evidence[i] = fmt.Sprintf("evidence-%02d", i)
	}
	for i := range actions {
		actions[i] = contract.SystemActionRecommendation{Type: "notify", Reason: fmt.Sprintf("reason-%02d", i)}
	}
	store.AddDangerAssessment(&contract.DangerAssessment{
		ID: "danger-1", EventType: contract.EventVisionWeapon, Score: 0.91, Level: 5,
		RiskLevel: "critical", ExpectedState: "intrusion", Category: contract.DangerCategorySecurity,
		Evidence: evidence, RecommendedSystemActions: actions, CreatedAt: time.Now().UTC(),
	})
	store.AddDangerAssessment(&contract.DangerAssessment{
		ID: "low", EventType: contract.EventVisionUnknown, Score: 0.64, Level: 3,
		RiskLevel: "medium_high", Category: contract.DangerCategorySecurity,
	})
	store.AddDangerAssessment(&contract.DangerAssessment{
		ID: "worker", EventType: contract.EventDiscoveryWorkerCrashed, Score: 0.99, Level: 5,
		RiskLevel: "critical", Category: contract.DangerCategorySystemHealth,
	})
	builder := &snapshot.Builder{
		Mu: &sync.RWMutex{}, State: store, Devices: device.NewRegistry(),
		Topology:  &topology.Topology{Nodes: map[string]*topology.Node{}},
		Residents: map[string]*topology.Resident{}, Automation: automation.NewEngine(filepath.Join(t.TempDir(), "auto.yaml")),
		Events: event.NewStore(10),
	}
	server := NewServer(Config{State: store, Snapshot: builder})
	listedAny, err := server.Handler("cge.danger_assessments")(rpcMessage(`{"limit":100}`))
	if err != nil {
		t.Fatal(err)
	}
	listed := listedAny.([]map[string]any)
	if len(listed) != 1 || listed[0]["id"] != "danger-1" {
		t.Fatalf("danger list not filtered: %#v", listed)
	}
	if _, exposed := listed[0]["evidence"]; exposed {
		t.Fatalf("compact danger list exposed evidence: %#v", listed[0])
	}
	detailAny, err := server.Handler("cge.danger_assessment")(rpcMessage(`{"id":"danger-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	detail := detailAny.(map[string]any)
	if len(detail["evidence"].([]string)) != 20 || len(detail["recommended_actions"].([]map[string]any)) != 10 {
		t.Fatalf("danger detail not bounded: %#v", detail)
	}
}

func newRPCConfigurationEngine() *engine.Engine {
	return engine.NewEngine(
		&topology.Topology{Nodes: map[string]*topology.Node{}},
		device.NewRegistry(), nil,
	)
}

func behaviorForSeed(t *testing.T, engineInstance *engine.Engine, seedID string) cgecontracts.LearnedBehavior {
	t.Helper()
	for _, item := range engineInstance.LearnedBehaviors() {
		if item.CriticalSeedID == seedID {
			return item
		}
	}
	t.Fatalf("behavior for seed %q not found", seedID)
	return cgecontracts.LearnedBehavior{}
}

func behaviorActionMessage(id string, action string, body string) contract.Message {
	payload, _ := json.Marshal(learnedBehaviorActionRequest{
		ID: id, Action: action, Data: json.RawMessage(body),
	})
	return contract.Message{Type: "test", Payload: payload}
}
