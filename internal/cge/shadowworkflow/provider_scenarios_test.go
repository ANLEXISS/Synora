package shadowworkflow

import (
	"context"
	"testing"
	"time"

	"synora/internal/cge/authorizationboundary"
)

func TestQualificationSyntheticAuthorizationProviderScenarios(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		mode       qualificationGrantMode
		wantStatus authorizationboundary.AuthorizationEligibilityStatus
	}{
		{name: "external confirmation", mode: qualificationGrantConfirmation, wantStatus: authorizationboundary.EligibilityRequiresExternalConfirmation},
		{name: "valid grant", mode: qualificationGrantValid, wantStatus: authorizationboundary.EligibilityEligible},
		{name: "expired grant", mode: qualificationGrantExpired, wantStatus: authorizationboundary.EligibilityRequiresExternalConfirmation},
		{name: "revoked grant", mode: qualificationGrantRevoked, wantStatus: authorizationboundary.EligibilityRequiresExternalConfirmation},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Enabled = true
			cfg.PipelineDepth = DepthAuthorizationBoundary
			cfg.MaxProcessingDuration = 2 * time.Second
			at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			provider := &qualificationAuthorizationProvider{available: true, grantMode: testCase.mode}
			r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, newQualificationCapabilityProvider(), provider)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close(context.Background())
			if result := r.TrySubmit(testInput(at, "synthetic-provider-"+string(testCase.mode))); result.Status != SubmitAccepted {
				t.Fatalf("submit=%+v", result)
			}
			waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
			found := false
			for _, assessment := range r.CoordinatorSnapshot().Episodes[0].AuthorizationAssessments {
				for _, candidate := range assessment.Candidates {
					if candidate.Status == testCase.wantStatus {
						found = true
					}
				}
			}
			if !found {
				t.Fatalf("expected status %s in provider scenario", testCase.wantStatus)
			}
		})
	}
}
