package actionpolicy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestStoreLoadsSafeDefaultsWhenAbsent(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "action_policy.yaml"))
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	policy := store.Get()
	if len(policy.Levels[contract.DangerMediumHigh].Actions) == 0 {
		t.Fatal("medium_high default missing")
	}
	for _, action := range policy.Levels[contract.DangerCritical].Actions {
		if action.Command == "siren" && action.Enabled {
			t.Fatal("siren must be disabled by default")
		}
	}
}

func TestPatchRejectsUnknownCommandAndWritesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "action_policy.yaml")
	store := NewStore(path)
	if _, err := store.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update([]byte(`{"levels":{"high":{"actions":[{"id":"bad","command":"explode","enabled":true,"priority":10}]}}}`)); err == nil {
		t.Fatal("unknown command accepted")
	}
	if _, err := store.Update([]byte(`{"levels":{"high":{"enabled":false}}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "action_policy.*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) == 0 {
		t.Fatal("expected backup before replacement")
	}
}

func TestEvaluateBlocksDisabledActionAndMatchesSecurityCondition(t *testing.T) {
	store := NewStore("")
	policy := store.Get()
	level := policy.Levels[contract.DangerHigh]
	level.Actions = append(level.Actions, contract.ActionPolicyEntry{ID: "armed-only", Command: "notify.whatsapp", Target: "owner", Enabled: true, Conditions: []contract.Condition{{Field: "security.armed", Op: "==", Value: true}}})
	policy.Levels[contract.DangerHigh] = level
	// Store is intentionally local-only for this unit test; update through the
	// public behavior is covered by persistence tests above.
	store.policy = policy
	decision := &contract.Decision{DangerLevel: string(contract.DangerHigh), DangerScore: .8}
	items := store.Evaluate(contract.DangerHigh, &contract.Event{}, decision, contract.DefaultSecurityModeState(time.Time{}))
	if len(items) == 0 {
		t.Fatal("expected policy actions")
	}
	if items[len(items)-1].BlockedReason != "condition_not_met" {
		t.Fatalf("condition should block while home/disarmed: %#v", items[len(items)-1])
	}
}
