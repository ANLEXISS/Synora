package rpc

import (
	"testing"
	"time"

	"synora/internal/state"
	"synora/pkg/contract"
)

func TestSystemHealthExposesCoreRecoveryWithoutClaimingHealthy(t *testing.T) {
	store := state.NewStore()
	store.SetSystemState(state.SystemState{
		LifecycleState:     "recovering",
		LifecycleReason:    "state restore pending",
		LifecycleUpdatedAt: time.Date(2026, 8, 22, 19, 0, 0, 0, time.UTC),
		Ready:              false,
		Healthy:            false,
		RecoveryComplete:   false,
	})
	server := &Server{state: store}
	health := server.mergeStateRuntimeHealth(contract.RuntimeHealth{Status: "ok"})
	component, ok := health.Components["core_recovery"]
	if !ok || component.Status != "recovering" || component.Active || health.Status != "degraded" {
		t.Fatalf("recovery health was overstated: health=%#v component=%#v", health, component)
	}
}
