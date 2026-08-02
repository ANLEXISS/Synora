package shadowworkflow

import (
	"context"
	"testing"
	"time"
)

func TestQualificationCircuitClosedOpenHalfOpenSuccessAndFailure(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAuthorizationBoundary
	cfg.ConsecutiveFailureLimit = 2
	cfg.CircuitResetAfter = time.Minute
	cfg.MaxProcessingDuration = 2 * time.Second
	clock := newQualificationClock()
	provider := newQualificationCapabilityProvider()
	authorization := &qualificationAuthorizationProvider{available: true, err: ErrProviderUnavailable}
	r, err := NewRuntime(context.Background(), cfg, clock, nil, provider, authorization)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	for index, eventID := range []string{"breaker-failure-1", "breaker-failure-2"} {
		if r.TrySubmit(testInput(clock.Now(), eventID)).Status != SubmitAccepted {
			t.Fatalf("%s not accepted", eventID)
		}
		expected := uint64(index + 1)
		waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesFailed >= expected })
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CircuitState == string(circuitOpen) })
	if result := r.TrySubmit(testInput(clock.Now(), "breaker-open-rejection")); result.Status != SubmitCircuitOpen {
		t.Fatalf("open result=%+v", result)
	}

	clock.Advance(time.Minute)
	authorization.setFailure(nil)
	authorization.setAllow(true)
	if result := r.TrySubmit(testInput(clock.Now(), "breaker-half-open-success")); result.Status != SubmitAccepted {
		t.Fatalf("half-open probe=%+v", result)
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool {
		return status.CyclesSucceeded > 0 && status.CircuitState == string(circuitClosed)
	})
	if status.State != StateRunning {
		t.Fatalf("closed state=%+v", status)
	}

	authorization.setFailure(ErrProviderUnavailable)
	for index, eventID := range []string{"breaker-reopen-1", "breaker-reopen-2"} {
		if r.TrySubmit(testInput(clock.Now(), eventID)).Status != SubmitAccepted {
			t.Fatalf("%s not accepted", eventID)
		}
		expected := uint64(3 + index)
		waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesFailed >= expected })
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CircuitState == string(circuitOpen) })
	clock.Advance(time.Minute)
	if result := r.TrySubmit(testInput(clock.Now(), "breaker-half-open-failure")); result.Status != SubmitAccepted {
		t.Fatalf("half-open failure probe=%+v", result)
	}
	status = waitForQualification(t, r, func(status StatusSnapshot) bool { return status.State == StateCircuitOpen && status.CyclesFailed >= 5 })
	if status.State != StateCircuitOpen {
		t.Fatalf("reopened state=%+v", status)
	}
}
