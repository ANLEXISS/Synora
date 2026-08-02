package shadowworkflow

import (
	"context"
	"testing"
	"time"
)

func TestQualificationAppendFailurePublishesNothing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.MaxProcessingDuration = 2 * time.Second
	store := &qualificationStore{base: newMemoryStore()}
	clock := newQualificationClock()
	r := newInjectedRuntime(t, cfg, clock, store, nil, nil)
	store.setAppendBefore(true)
	if r.TrySubmit(testInput(clock.Now(), "append-failure")).Status != SubmitAccepted {
		t.Fatal("event not accepted")
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool {
		return status.CyclesFailed == 1 && status.LastErrorCode == "transaction.durability_failure"
	})
	if status.WorkflowRevision != 0 || status.CommitsSucceeded != 0 || status.LastErrorCode != "transaction.durability_failure" {
		t.Fatalf("append failure state=%+v", status)
	}
}

func TestQualificationMappingAndAuthorizationQuotasFailBeforeCommit(t *testing.T) {
	clock := newQualificationClock()
	for _, testCase := range []struct {
		name  string
		depth PipelineDepth
		set   func(*Config)
		cap   CapabilityInputProvider
		auth  AuthorizationInputProvider
	}{
		{name: "mapping", depth: DepthCapabilityMapping, set: func(cfg *Config) { cfg.MaxMappingsPerCycle = 1 }, cap: newQualificationCapabilityProvider()},
		{name: "authorization", depth: DepthAuthorizationBoundary, set: func(cfg *Config) { cfg.MaxAuthorizationsPerCycle = 1 }, cap: newQualificationCapabilityProvider(), auth: &qualificationAuthorizationProvider{allow: true, available: true}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Enabled = true
			cfg.PipelineDepth = testCase.depth
			cfg.MaxProcessingDuration = 2 * time.Second
			testCase.set(&cfg)
			r, err := NewRuntime(context.Background(), cfg, clock, nil, testCase.cap, testCase.auth)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close(context.Background())
			if r.TrySubmit(testInput(clock.Now(), "quota-"+testCase.name)).Status != SubmitAccepted {
				t.Fatal("event not accepted")
			}
			status := waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesFailed == 1 })
			if status.WorkflowRevision != 0 || status.CommitsSucceeded != 0 || status.LastErrorCode != "workflow_error" {
				t.Fatalf("quota state=%+v metrics=%v", status, r.Metrics())
			}
		})
	}
}
