package shadowworkflow

import (
	"context"
	"testing"
	"time"
)

func TestQualificationStatusSnapshotIsDefensivelyCloned(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MaxProcessingDuration = 2 * time.Second
	clock := newQualificationClock()
	r, err := NewRuntime(context.Background(), cfg, clock, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if r.TrySubmit(testInput(clock.Now(), "status-clone")).Status != SubmitAccepted {
		t.Fatal("event not accepted")
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
	first := r.Status()
	first.FreshLayerCounts["episode"] = 999
	first.StaleLayerCounts["episode"] = 999
	first.RecoveryWarnings = append(first.RecoveryWarnings, "secret-marker")
	second := r.Status()
	if second.FreshLayerCounts["episode"] == 999 || second.StaleLayerCounts["episode"] == 999 {
		t.Fatalf("status map escaped: %+v", second)
	}
	for _, warning := range second.RecoveryWarnings {
		if warning == "secret-marker" {
			t.Fatalf("status warning slice escaped: %+v", second)
		}
	}
}
