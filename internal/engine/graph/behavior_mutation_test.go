package graph

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"synora/internal/engine/contracts"
)

func TestLearnedBehaviorApprovalRejectsConflictingAndUnsafeActions(t *testing.T) {
	memory, behavior := learnedBehaviorFixture(t)
	originalRealCount := behavior.RealCount
	originalSimulatedCount := behavior.SimulatedCount

	proposed := []map[string]any{{"action": "light.turn_on"}}
	forbidden := []string{"light.turn_on"}
	patched, err := memory.PatchLearnedBehavior(behavior.ID, contracts.LearnedBehaviorPatch{
		ProposedActions:  &proposed,
		ForbiddenActions: &forbidden,
	})
	if err != nil {
		t.Fatalf("patch behavior: %v", err)
	}
	if patched.RealCount != originalRealCount || patched.SimulatedCount != originalSimulatedCount {
		t.Fatalf("patch changed raw counters: before=%#v after=%#v", behavior, patched)
	}
	if _, err := memory.ApproveLearnedBehavior(behavior.ID, nil); !errors.Is(err, ErrForbiddenAction) {
		t.Fatalf("approve conflict error = %v, want forbidden action", err)
	}

	forbidden = nil
	proposed = []map[string]any{{"action": "door.unlock"}}
	if _, err := memory.PatchLearnedBehavior(behavior.ID, contracts.LearnedBehaviorPatch{
		ProposedActions:  &proposed,
		ForbiddenActions: &forbidden,
	}); err != nil {
		t.Fatalf("store unapproved unsafe proposal: %v", err)
	}
	if _, err := memory.ApproveLearnedBehavior(behavior.ID, nil); !errors.Is(err, ErrForbiddenAction) {
		t.Fatalf("approve unsafe action error = %v, want forbidden action", err)
	}
}

func TestLearnedBehaviorLifecycleControlsAutomaticMatching(t *testing.T) {
	memory, behavior := learnedBehaviorFixture(t)
	proposed := []map[string]any{{"action": "light.turn_on"}}
	notes := "Validated after observing the entrance routine."
	patched, err := memory.PatchLearnedBehavior(behavior.ID, contracts.LearnedBehaviorPatch{
		ProposedActions: &proposed,
		UserNotes:       &notes,
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := memory.ApproveLearnedBehavior(behavior.ID, nil)
	if err != nil {
		t.Fatalf("approve behavior: %v", err)
	}
	if approved.Status != contracts.LearnedBehaviorApproved || !approved.Enabled || approved.UserNotes != notes {
		t.Fatalf("unexpected approved behavior: %#v patched=%#v", approved, patched)
	}

	base := time.Date(2026, 7, 10, 13, 0, 0, 0, time.UTC)
	first := simulatedCGEEvent("vision.unknown", "run-approved", "first", base)
	second := simulatedCGEEvent("vision.motion", "run-approved", "second", base.Add(time.Second))
	memory.LearnEvent(first)
	if matched := memory.MatchApprovedBehavior(first); matched != nil {
		t.Fatalf("partial signature should not match: %#v", matched)
	}
	memory.LearnEvent(second)
	matched := memory.MatchApprovedBehavior(second)
	if matched == nil || matched.ID != behavior.ID {
		t.Fatalf("approved behavior should match, got %#v", matched)
	}
	rejected, err := memory.RejectLearnedBehavior(behavior.ID)
	if err != nil || rejected.Status != contracts.LearnedBehaviorRejected || rejected.Enabled {
		t.Fatalf("reject behavior = %#v err=%v", rejected, err)
	}
	rejectedFirst := simulatedCGEEvent("vision.unknown", "run-rejected", "first", base.Add(2*time.Second))
	rejectedSecond := simulatedCGEEvent("vision.motion", "run-rejected", "second", base.Add(3*time.Second))
	memory.LearnEvent(rejectedFirst)
	memory.LearnEvent(rejectedSecond)
	if matched := memory.MatchApprovedBehavior(rejectedSecond); matched != nil {
		t.Fatalf("rejected behavior must not match: %#v", matched)
	}
	if _, err := memory.ApproveLearnedBehavior(behavior.ID, nil); err != nil {
		t.Fatalf("reapprove behavior: %v", err)
	}

	disabled, err := memory.DisableLearnedBehavior(behavior.ID)
	if err != nil || disabled.Status != contracts.LearnedBehaviorDisabled || disabled.Enabled {
		t.Fatalf("disable behavior = %#v err=%v", disabled, err)
	}
	third := simulatedCGEEvent("vision.unknown", "run-disabled", "first", base.Add(4*time.Second))
	fourth := simulatedCGEEvent("vision.motion", "run-disabled", "second", base.Add(5*time.Second))
	memory.LearnEvent(third)
	memory.LearnEvent(fourth)
	if matched := memory.MatchApprovedBehavior(fourth); matched != nil {
		t.Fatalf("disabled behavior must not match: %#v", matched)
	}

	forgotten, err := memory.ForgetLearnedBehavior(behavior.ID)
	if err != nil || !forgotten.Forgotten || forgotten.Enabled {
		t.Fatalf("forget behavior = %#v err=%v", forgotten, err)
	}
	reset, err := memory.ResetLearnedBehavior(behavior.ID)
	if err != nil || reset.Status != contracts.LearnedBehaviorObserving || !reset.Enabled || reset.Forgotten || reset.UserNotes != "" {
		t.Fatalf("reset behavior = %#v err=%v", reset, err)
	}
}

func TestLearnedBehaviorOverridesRoundTripWithoutRawCounters(t *testing.T) {
	memory, behavior := learnedBehaviorFixture(t)
	status := contracts.LearnedBehaviorApproved
	notes := "owner approved"
	risk := 0.81
	enabled := true
	actions := []map[string]any{{"action": "light.turn_on"}}
	override := contracts.LearnedBehaviorOverride{
		BehaviorID:      behavior.ID,
		Status:          &status,
		UserNotes:       &notes,
		RiskOverride:    &risk,
		Enabled:         &enabled,
		ProposedActions: &actions,
	}
	if err := memory.ApplyLearnedBehaviorOverrides([]contracts.LearnedBehaviorOverride{override}); err != nil {
		t.Fatalf("apply overrides: %v", err)
	}
	exported := memory.ExportLearnedBehaviorOverrides()
	if len(exported) != 1 || exported[0].BehaviorID != behavior.ID || exported[0].RiskOverride == nil || *exported[0].RiskOverride != risk {
		t.Fatalf("unexpected exported overrides: %#v", exported)
	}
	data, err := json.Marshal(exported[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, protected := range []string{"real_count", "simulated_count", "critical_seed_count", "count", "confidence"} {
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload[protected]; ok {
			t.Fatalf("override exposes raw field %q: %s", protected, data)
		}
	}

	restored := NewGraphMemory()
	if err := restored.ApplyLearnedBehaviorOverrides(exported); err != nil {
		t.Fatal(err)
	}
	learnBehaviorSequence(t, restored)
	restoredBehavior, ok := restored.LearnedBehavior(behavior.ID)
	if !ok || restoredBehavior.Status != contracts.LearnedBehaviorApproved || restoredBehavior.RiskOverride == nil || *restoredBehavior.RiskOverride != risk {
		t.Fatalf("pending override was not applied to learned behavior: %#v", restoredBehavior)
	}
}

func TestPendingUnsafeApprovedOverrideIsDisabledWhenBehaviorAppears(t *testing.T) {
	probe := NewGraphMemory()
	learnBehaviorSequence(t, probe)
	var behaviorID string
	for _, behavior := range probe.LearnedBehaviors() {
		if behavior.TriggerSequenceSignature == "vision.unknown > vision.motion" {
			behaviorID = behavior.ID
			break
		}
	}
	if behaviorID == "" {
		t.Fatal("fixture behavior id missing")
	}
	status := contracts.LearnedBehaviorApproved
	enabled := true
	actions := []map[string]any{{"action": "door.unlock"}}
	memory := NewGraphMemory()
	if err := memory.ApplyLearnedBehaviorOverrides([]contracts.LearnedBehaviorOverride{{
		BehaviorID: behaviorID, Status: &status, Enabled: &enabled, ProposedActions: &actions,
	}}); err != nil {
		t.Fatalf("pending override should be retained until its behavior exists: %v", err)
	}
	learnBehaviorSequence(t, memory)
	behavior, ok := memory.LearnedBehavior(behaviorID)
	if !ok || behavior.Enabled || behavior.Status != contracts.LearnedBehaviorDisabled {
		t.Fatalf("unsafe pending approval should fail closed: %#v", behavior)
	}
}

func TestLearnedBehaviorPatchJSONRejectsRawCounters(t *testing.T) {
	for _, payload := range []string{
		`{"real_count": 99}`,
		`{"count": 99}`,
		`{"confidence": 1}`,
		`{"unexpected": true}`,
	} {
		var patch contracts.LearnedBehaviorPatch
		if err := json.Unmarshal([]byte(payload), &patch); err == nil {
			t.Fatalf("patch %s should be rejected", payload)
		}
	}
}

func TestLearnedBehaviorFalsePositiveStoresSeparateConfidenceOverride(t *testing.T) {
	memory, behavior := learnedBehaviorFixture(t)
	rawConfidence := behavior.Confidence
	updated, err := memory.ApplyLearnedBehaviorFeedback(behavior.ID, contracts.FeedbackFalsePositive)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Confidence != rawConfidence {
		t.Fatalf("raw confidence changed: got=%v want=%v", updated.Confidence, rawConfidence)
	}
	if updated.ConfidenceOverride == nil || *updated.ConfidenceOverride >= rawConfidence {
		t.Fatalf("false positive should lower separate override: %#v", updated)
	}
	if updated.UserFeedback.FalsePositiveCount != 1 || !updated.RequiresValidation {
		t.Fatalf("feedback marker missing: %#v", updated.UserFeedback)
	}
}

func learnedBehaviorFixture(t *testing.T) (*GraphMemory, contracts.LearnedBehavior) {
	t.Helper()
	memory := NewGraphMemory()
	learnBehaviorSequence(t, memory)
	for _, behavior := range memory.LearnedBehaviors() {
		if behavior.TriggerSequenceSignature == "vision.unknown > vision.motion" && behavior.Origin != OriginCriticalSeed {
			return memory, behavior
		}
	}
	t.Fatalf("learned behavior missing: %#v", memory.LearnedBehaviors())
	return nil, contracts.LearnedBehavior{}
}

func learnBehaviorSequence(t *testing.T, memory *GraphMemory) {
	t.Helper()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	for run := 0; run < 3; run++ {
		runID := "run-behavior-" + string(rune('a'+run))
		memory.LearnEvent(simulatedCGEEvent("vision.unknown", runID, "first", base.Add(time.Duration(run*2)*time.Second)))
		memory.LearnEvent(simulatedCGEEvent("vision.motion", runID, "second", base.Add(time.Duration(run*2+1)*time.Second)))
	}
}
