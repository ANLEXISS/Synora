package shadowworkflow

import (
	"context"
	"testing"
	"time"

	"synora/internal/cge/durableworkflow"
)

func TestQualificationMissingCapabilityProviderSkipsWithoutFabrication(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthCapabilityMapping
	cfg.MaxProcessingDuration = 2 * time.Second
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if result := r.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "missing-capability-provider")); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
	if status.CyclesSucceeded != 1 || r.Metrics()["mapping_skipped"] == 0 {
		t.Fatalf("status=%+v metrics=%v", status, r.Metrics())
	}
	state := r.CoordinatorSnapshot()
	if len(state.Episodes) != 1 || len(state.Episodes[0].CapabilityMappings) != 0 {
		t.Fatalf("fabricated mappings: %+v", state.Episodes)
	}
	if state.Episodes[0].Freshness[durableworkflow.LayerCapabilityMapping] == durableworkflow.FreshnessFresh {
		t.Fatalf("missing provider unexpectedly produced fresh mapping layer")
	}
}

func TestQualificationMissingAuthorizationProviderSkipsWithoutAllow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAuthorizationBoundary
	cfg.MaxProcessingDuration = 2 * time.Second
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, newQualificationCapabilityProvider(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if result := r.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "missing-authorization-provider")); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
	if status.CyclesSucceeded != 1 || r.Metrics()["authorization_skipped"] == 0 {
		t.Fatalf("status=%+v metrics=%v", status, r.Metrics())
	}
	state := r.CoordinatorSnapshot()
	if len(state.Episodes) != 1 || len(state.Episodes[0].AuthorizationAssessments) != 0 {
		t.Fatalf("fabricated authorization assessment: %+v", state.Episodes)
	}
}

func TestQualificationAuthorizationDefaultDenyIntegrated(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAuthorizationBoundary
	cfg.MaxProcessingDuration = 2 * time.Second
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, newQualificationCapabilityProvider(), &qualificationAuthorizationProvider{available: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if result := r.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "default-deny")); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
	if status.CyclesSucceeded != 1 {
		t.Fatalf("status=%+v", status)
	}
	state := r.CoordinatorSnapshot()
	if len(state.Episodes) != 1 || len(state.Episodes[0].AuthorizationAssessments) == 0 {
		t.Fatalf("authorization assessment absent")
	}
	deniedByDefaultAssessment := false
	for _, assessment := range state.Episodes[0].AuthorizationAssessments {
		for _, candidate := range assessment.Candidates {
			if candidate.Status == "denied_by_default" {
				deniedByDefaultAssessment = true
			}
		}
		if assessment.AuthorizationEligible {
			t.Fatalf("default deny bypassed: %+v", assessment)
		}
	}
	if !deniedByDefaultAssessment {
		t.Fatal("no compatible mapping reached integrated default deny")
	}
}
