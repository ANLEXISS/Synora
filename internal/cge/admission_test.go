package cge

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/shadowworkflow"
	"synora/pkg/contract"
)

func TestDefaultShadowEventAdmissionPolicyIsClosedCanonicalAndDefensive(t *testing.T) {
	defaultPolicy := DefaultShadowEventAdmissionPolicy()
	if got := defaultPolicy.AllowedEventTypes(); !reflect.DeepEqual(got, []string{
		contract.EventVisionIdentity, contract.EventVisionUncertain, contract.EventVisionUnknown,
	}) {
		t.Fatalf("default admission policy changed: %v", got)
	}
	const wantFingerprint = "shadow-event-admission-policy-v1:11fccc139860e454fd3587ce2aa08a690ddac39521e509af98ab05420dcdcc96"
	if defaultPolicy.Fingerprint() != wantFingerprint {
		t.Fatalf("default policy fingerprint=%q, want %q", defaultPolicy.Fingerprint(), wantFingerprint)
	}
	ordered, err := NewShadowEventAdmissionPolicy([]string{contract.EventVisionUnknown, contract.EventVisionIdentity, contract.EventVisionUncertain})
	if err != nil {
		t.Fatalf("canonical policy: %v", err)
	}
	if ordered.Fingerprint() != defaultPolicy.Fingerprint() {
		t.Fatalf("order changed fingerprint: default=%q reordered=%q", defaultPolicy.Fingerprint(), ordered.Fingerprint())
	}
	clone := ordered.AllowedEventTypes()
	clone[0] = contract.EventVisionWeapon
	if reflect.DeepEqual(clone, ordered.AllowedEventTypes()) {
		t.Fatal("allowed event type snapshot is not defensive")
	}
	for name, input := range map[string][]string{
		"empty":       nil,
		"blank":       {"  "},
		"duplicate":   {contract.EventVisionIdentity, contract.EventVisionIdentity},
		"unknown":     {"vision.not-a-contract-event"},
		"mixed_blank": {contract.EventVisionIdentity, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewShadowEventAdmissionPolicy(input); !errors.Is(err, ErrInvalidShadowAdmissionPolicy) {
				t.Fatalf("policy input %v error=%v", input, err)
			}
		})
	}
}

func TestShadowEventAdmissionMatrixCoversContractEvents(t *testing.T) {
	matrix := DefaultShadowEventAdmissionMatrix()
	known := knownShadowEventTypes()
	if len(matrix) != len(known) {
		t.Fatalf("matrix length=%d, known contract events=%d", len(matrix), len(known))
	}
	seen := make(map[string]bool, len(matrix))
	for _, entry := range matrix {
		if seen[entry.EventType] {
			t.Fatalf("duplicate matrix event %q", entry.EventType)
		}
		seen[entry.EventType] = true
		if _, ok := known[entry.EventType]; !ok {
			t.Fatalf("matrix contains unknown event %q", entry.EventType)
		}
	}
	for eventType := range known {
		if !seen[eventType] {
			t.Fatalf("contract event %q missing from admission matrix", eventType)
		}
	}
	for _, want := range []struct {
		eventType   string
		disposition ShadowEventDisposition
	}{
		{contract.EventVisionIdentity, ShadowEventAdmitted},
		{contract.EventVisionUnknown, ShadowEventAdmitted},
		{contract.EventVisionUncertain, ShadowEventAdmitted},
		{contract.EventVisionWeapon, ShadowEventHistoricalOnly},
		{contract.EventVisionMotion, ShadowEventHistoricalOnly},
		{contract.EventActionRequest, ShadowEventIgnoredByDesign},
		{contract.EventSystemUnknown, ShadowEventUnknown},
	} {
		var got ShadowEventDisposition
		for _, entry := range matrix {
			if entry.EventType == want.eventType {
				got = entry.Disposition
			}
		}
		if got != want.disposition {
			t.Fatalf("matrix[%q]=%q, want %q", want.eventType, got, want.disposition)
		}
	}
}

func TestShadowAdmissionAdaptationDistinguishesIgnoredAndInvalid(t *testing.T) {
	at := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		event      Event
		eligible   bool
		adapted    bool
		reasonCode string
		wantErr    bool
	}{
		{name: "accepted", event: shadowEvent("identity-1", contract.EventVisionIdentity, at), eligible: true, adapted: true},
		{name: "historical_only", event: shadowEvent("weapon-1", contract.EventVisionWeapon, at), reasonCode: ReasonEventTypeNotAllowlisted},
		{name: "missing_id", event: Event{Type: contract.EventVisionIdentity, Timestamp: at}, eligible: true, wantErr: true},
		{name: "invalid_timestamp", event: Event{ID: "identity-2", Type: contract.EventVisionIdentity}, eligible: true, wantErr: true},
		{name: "invalid_type", event: Event{ID: "unknown-type", Type: "", Timestamp: at}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AdaptEvent(tc.event)
			if tc.wantErr {
				if err == nil || !errors.Is(err, ErrShadowAdaptation) {
					t.Fatalf("expected adaptation error, got result=%#v err=%v", got, err)
				}
				return
			}
			if err != nil || got.Eligible != tc.eligible || got.Adapted != tc.adapted || got.ReasonCode != tc.reasonCode {
				t.Fatalf("adaptation=%#v err=%v", got, err)
			}
		})
	}
}

func TestShadowAdmissionMapsSubmitResultsToClosedCodes(t *testing.T) {
	cases := []struct {
		status shadowworkflow.SubmitStatus
		state  shadowworkflow.RuntimeState
		want   ShadowAdmissionCode
	}{
		{shadowworkflow.SubmitAccepted, shadowworkflow.StateRunning, ShadowAdmissionAccepted},
		{shadowworkflow.SubmitQueueFull, shadowworkflow.StateRunning, ShadowAdmissionQueueFull},
		{shadowworkflow.SubmitStopped, shadowworkflow.StateStopping, ShadowAdmissionStopping},
		{shadowworkflow.SubmitStopped, shadowworkflow.StateStopped, ShadowAdmissionStopped},
		{shadowworkflow.SubmitStopped, shadowworkflow.StateRecoveryFailed, ShadowAdmissionUnavailable},
		{shadowworkflow.SubmitDisabled, shadowworkflow.StateDisabled, ShadowAdmissionDisabled},
		{shadowworkflow.SubmitRejected, shadowworkflow.StateRunning, ShadowAdmissionInvalid},
		{shadowworkflow.SubmitCircuitOpen, shadowworkflow.StateCircuitOpen, ShadowAdmissionUnavailable},
		{shadowworkflow.SubmitStorageLimit, shadowworkflow.StateStorageLimitReached, ShadowAdmissionUnavailable},
	}
	for _, tc := range cases {
		if got := mapSubmitStatus(tc.status, tc.state); got != tc.want {
			t.Errorf("mapSubmitStatus(%q,%q)=%q, want %q", tc.status, tc.state, got, tc.want)
		}
	}
}

func TestShadowAdmissionStatusIsSafeUnderConcurrentObservation(t *testing.T) {
	engine := NewShadowEngine()
	const workers = 16
	const perWorker = 20
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for index := 0; index < perWorker; index++ {
				_, _ = engine.Observe(context.Background(), shadowEvent("event", contract.EventVisionIdentity, time.Now().UTC()))
				_ = engine.AdmissionStatus()
			}
		}(worker)
	}
	wg.Wait()
	status := engine.AdmissionStatus()
	if status.DisabledTotal != workers*perWorker || status.LastCode != ShadowAdmissionDisabled || !status.HistoricalAuthorityUnchanged || !status.NoActionProduced {
		t.Fatalf("concurrent admission status=%+v", status)
	}
	metrics := engine.AdmissionMetrics()
	if metrics["cge_shadow_admission_workflow_disabled_total"] != workers*perWorker {
		t.Fatalf("concurrent admission metrics=%v", metrics)
	}
}

func TestShadowAdmissionInvalidEventIsNotSubmittedToWorkflow(t *testing.T) {
	root := t.TempDir()
	config := enabledShadowConfig(root, true)
	config.Workflow.Enabled = true
	config.Workflow.StoreMode = shadowworkflow.StoreMemory
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)}, quietShadowLogger())
	if err != nil {
		t.Fatalf("create shadow workflow: %v", err)
	}
	defer engine.Close()
	if _, err := engine.Observe(context.Background(), Event{ID: "invalid-identity", Type: contract.EventVisionIdentity}); !errors.Is(err, ErrShadowAdaptation) {
		t.Fatalf("invalid admitted event error=%v", err)
	}
	admission := engine.AdmissionStatus()
	workflow := engine.WorkflowStatus()
	if admission.LastCode != ShadowAdmissionInvalid || admission.InvalidTotal != 1 || workflow.Accepted != 0 || workflow.CommitsSucceeded != 0 || workflow.EpisodeCount != 0 {
		t.Fatalf("invalid event crossed workflow boundary: admission=%+v workflow=%+v", admission, workflow)
	}
}
