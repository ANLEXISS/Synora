package shadowworkflow

import (
	"context"
	"testing"
	"time"

	"synora/internal/cge/durableworkflow"
)

func TestQualificationFullPipelineWithSyntheticProviders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAuthorizationBoundary
	cfg.MaxProcessingDuration = 2 * time.Second
	capabilityProvider := newQualificationCapabilityProvider()
	authorizationProvider := &qualificationAuthorizationProvider{allow: true, available: true}
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, capabilityProvider, authorizationProvider)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if result := r.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "full-pipeline-event")); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
	if status.CyclesSucceeded != 1 || status.CommitsSucceeded != 1 {
		t.Fatalf("status=%+v metrics=%v", status, r.Metrics())
	}
	state := r.CoordinatorSnapshot()
	if len(state.Episodes) != 1 {
		t.Fatalf("episodes=%d", len(state.Episodes))
	}
	episode := state.Episodes[0]
	for _, layer := range []durableworkflow.LayerKind{durableworkflow.LayerEpisode, durableworkflow.LayerSituationFacts, durableworkflow.LayerSituationHypotheses, durableworkflow.LayerEvidenceDiscrimination, durableworkflow.LayerAdvisoryRequests, durableworkflow.LayerCapabilityMapping, durableworkflow.LayerAuthorizationBoundary} {
		if episode.Freshness[layer] != durableworkflow.FreshnessFresh {
			t.Fatalf("layer %s freshness=%s", layer, episode.Freshness[layer])
		}
	}
	if len(episode.CapabilityMappings) == 0 || len(episode.AuthorizationAssessments) == 0 {
		t.Fatalf("mapping/authentication layers were not populated: mappings=%d authorizations=%d", len(episode.CapabilityMappings), len(episode.AuthorizationAssessments))
	}
	for _, assessment := range episode.AuthorizationAssessments {
		if assessment.AuthorizationEligible {
			for _, candidate := range assessment.Candidates {
				if candidate.Eligible {
					if candidate.Status != "eligible" {
						t.Fatalf("invalid eligible status=%s", candidate.Status)
					}
				}
			}
		}
	}
	if status.WorkflowDigest == "" || state.Digest != status.WorkflowDigest {
		t.Fatalf("digest mismatch status=%s state=%s", status.WorkflowDigest, state.Digest)
	}
}
