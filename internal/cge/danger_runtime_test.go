package cge

import (
	"math"
	"testing"
	"time"

	eventpkg "synora/internal/event"
	"synora/internal/state"
	"synora/pkg/contract"
)

func runtimeTestConfig() contract.DangerDecayConfig {
	return contract.DangerDecayConfig{Enabled: true, TickSeconds: 5, WindowMinutes: 30, HalfLifeMinutes: 10, IdleBelowScore: .25, IdleStableSeconds: 300, DowngradeStableSeconds: 1, LockIntrusionUntilReset: true}
}

func testChain(t *testing.T, at time.Time, score float64, level string) (*state.Store, *eventpkg.ChainManager) {
	t.Helper()
	store := state.NewStore()
	manager := eventpkg.NewChainManager(eventpkg.DefaultChainConfig())
	manager.Process(&contract.Event{ID: "event-1", Type: contract.EventVisionUnknown, Source: "vision", Timestamp: at, Payload: map[string]any{"activation_id": "activation-1"}}, &contract.ChainEvaluation{EventID: "event-1", Timestamp: at, State: "suspicious", DangerLevel: level, DangerScore: score, Reasons: []string{"unknown person"}})
	return store, manager
}

func TestDangerRuntimeIsoltedDangerDecaysWithTime(t *testing.T) {
	t0 := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	store, chains := testChain(t, t0, .8, "high")
	runtime := NewDangerRuntime(runtimeTestConfig())
	runtime.Recompute(store, chains, t0, true)
	result := runtime.Recompute(store, chains, t0.Add(10*time.Minute), false)
	if math.Abs(result.CurrentScore-.4) > .01 {
		t.Fatalf("score after half-life=%.3f, want about .4", result.CurrentScore)
	}
	runtime.Recompute(store, chains, t0.Add(20*time.Minute), false)
	result = runtime.Recompute(store, chains, t0.Add(30*time.Minute+time.Second), false)
	if result.CurrentScore != 0 || result.CurrentLevel != string(contract.DangerNone) {
		t.Fatalf("expired score=%v level=%s", result.CurrentScore, result.CurrentLevel)
	}
}

func TestDangerRuntimeNewEventRaisesImmediately(t *testing.T) {
	t0 := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	store, chains := testChain(t, t0, .3, "low")
	runtime := NewDangerRuntime(runtimeTestConfig())
	runtime.Recompute(store, chains, t0, true)
	chains.Process(&contract.Event{ID: "event-2", Type: contract.EventVisionWeapon, Source: "vision", Timestamp: t0.Add(10 * time.Minute), Payload: map[string]any{"activation_id": "activation-2"}}, &contract.ChainEvaluation{EventID: "event-2", Timestamp: t0.Add(10 * time.Minute), State: "intrusion", DangerLevel: "critical", DangerScore: .95})
	result := runtime.Recompute(store, chains, t0.Add(10*time.Minute), false)
	if result.CurrentScore < .9 || result.CurrentLevel != string(contract.DangerCritical) {
		t.Fatalf("new event did not raise danger: %#v", result)
	}
}

func TestDangerRuntimeManualRiskExpiresWithoutDecay(t *testing.T) {
	t0 := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	store := state.NewStore()
	current := store.SystemState()
	current.ManualRiskActive = true
	current.ManualRiskLevel = "high"
	current.ManualRiskScore = .75
	current.ManualRiskExpiresAt = t0.Add(5 * time.Minute)
	store.SetSystemState(current)
	runtime := NewDangerRuntime(runtimeTestConfig())
	if got := runtime.Recompute(store, nil, t0, true); got.CurrentScore != .75 {
		t.Fatalf("manual score=%v", got.CurrentScore)
	}
	if got := runtime.Recompute(store, nil, t0.Add(4*time.Minute), false); got.CurrentScore != .75 {
		t.Fatalf("manual score decayed before expiration=%v", got.CurrentScore)
	}
	current = store.SystemState()
	current.ManualRiskActive = false
	current.ManualRiskExpiresAt = time.Time{}
	store.SetSystemState(current)
	if got := runtime.Recompute(store, nil, t0.Add(6*time.Minute), false); got.CurrentScore != 0 {
		t.Fatalf("expired manual score=%v", got.CurrentScore)
	}
}

func TestDangerRuntimeIntrusionLockAndHysteresis(t *testing.T) {
	t0 := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	store, chains := testChain(t, t0, .8, "high")
	runtime := NewDangerRuntime(runtimeTestConfig())
	runtime.Recompute(store, chains, t0, true)
	// The score is already below the high threshold, but must not downgrade
	// before the configured stable interval.
	result := runtime.Recompute(store, chains, t0.Add(10*time.Minute+500*time.Millisecond), false)
	if result.CurrentLevel != string(contract.DangerHigh) {
		t.Fatalf("hysteresis downgraded too early to %s", result.CurrentLevel)
	}
	result = runtime.Recompute(store, chains, t0.Add(10*time.Minute+2*time.Second), false)
	if result.CurrentLevel == string(contract.DangerHigh) {
		t.Fatalf("hysteresis did not downgrade after stability")
	}
	current := store.SystemState()
	current.IntrusionActive = true
	store.SetSystemState(current)
	result = runtime.Recompute(store, chains, t0.Add(31*time.Minute), false)
	if !result.Locked || result.CurrentLevel != string(contract.DangerCritical) || result.CurrentState != "intrusion" {
		t.Fatalf("intrusion lock lost: %#v", result)
	}
}

func TestDangerRuntimeRestartRecomputeDropsExpiredDanger(t *testing.T) {
	t0 := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	store, chains := testChain(t, t0, .8, "high")
	old := store.SystemState()
	old.LastState, old.DangerLevel, old.DangerScore, old.DangerKnown = "suspicious", "high", .8, true
	store.SetSystemState(old)
	runtime := NewDangerRuntime(runtimeTestConfig())
	result := runtime.Recompute(store, chains, t0.Add(31*time.Minute), true)
	if result.CurrentScore != 0 || result.CurrentLevel != string(contract.DangerNone) || result.CurrentState != "idle" {
		t.Fatalf("restart restored expired danger: %#v", result)
	}
}
