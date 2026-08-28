package danger

import (
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestDangerScoringKnownResidentNormal(t *testing.T) {
	assessment := AssessEvent(testEvent(contract.EventVisionIdentity, "entry", 0.92), Context{
		DeviceRole: "access_control",
		Now:        testTime(10),
	})

	if assessment.Level != 1 || assessment.Category != contract.DangerCategoryActivity || assessment.ValidationRequired {
		t.Fatalf("unexpected assessment: %#v", assessment)
	}
	assertAction(t, assessment, contract.SystemActionUpdatePresence)
}

func TestDangerScoringUnknownEntrance(t *testing.T) {
	assessment := AssessEvent(testEvent(contract.EventVisionUnknown, "entry", 0.72), Context{
		DeviceRole: "access_control",
		Now:        testTime(12),
	})

	if assessment.Level != 5 || assessment.Category != contract.DangerCategorySecurity || !assessment.ValidationRequired {
		t.Fatalf("unexpected assessment: %#v", assessment)
	}
	assertAction(t, assessment, contract.SystemActionCreateValidation)
	assertAction(t, assessment, contract.SystemActionStoreEvidence)
	assertAction(t, assessment, contract.SystemActionRecordClipIfAvailable)
}

func TestDangerScoringUnknownEntranceNight(t *testing.T) {
	assessment := AssessEvent(testEvent(contract.EventVisionUnknown, "entry", 0.72), Context{
		DeviceRole: "access_control",
		Now:        testTime(23),
	})

	if assessment.Level != 5 || assessment.Score < 0.80 || !assessment.ValidationRequired {
		t.Fatalf("unexpected assessment: %#v", assessment)
	}
}

func TestDangerScoringWeaponAndFall(t *testing.T) {
	weapon := AssessEvent(testEvent(contract.EventVisionWeapon, "entry", 0.88), Context{Now: testTime(12)})
	if weapon.Level != 5 || weapon.Category != contract.DangerCategorySecurity || !weapon.ValidationRequired {
		t.Fatalf("unexpected weapon assessment: %#v", weapon)
	}
	assertAction(t, weapon, contract.SystemActionSetIntrusionState)
	assertAction(t, weapon, contract.SystemActionCreateAlert)

	fall := AssessEvent(testEvent(contract.EventVisionFall, "child_room", 0.84), Context{Now: testTime(12)})
	if fall.Level != 5 || fall.Category != contract.DangerCategoryMedicalEmergency || !fall.ValidationRequired {
		t.Fatalf("unexpected fall assessment: %#v", fall)
	}
	assertAction(t, fall, contract.SystemActionSetEmergencyState)
}

func TestDangerScoringTamperWorkerCrashAndCameraOffline(t *testing.T) {
	tamper := AssessEvent(testEvent(contract.EventVisionTamper, "entry", 0.82), Context{Now: testTime(12)})
	if tamper.Level != 4 || tamper.Category != contract.DangerCategorySecurity {
		t.Fatalf("unexpected tamper assessment: %#v", tamper)
	}

	crash := AssessEvent(testEvent(contract.EventDiscoveryWorkerCrashed, "", 0), Context{Now: testTime(12)})
	if crash.Category != contract.DangerCategorySystemHealth || crash.ValidationRequired {
		t.Fatalf("worker crash should be system health without user validation: %#v", crash)
	}
	assertAction(t, crash, contract.SystemActionSuppressNoise)

	hostapd := AssessEvent(testEvent("hostapd.degraded", "", 0), Context{Now: testTime(12)})
	if hostapd.Category != contract.DangerCategorySystemHealth || hostapd.ValidationRequired {
		t.Fatalf("hostapd degraded should be system health without validation: %#v", hostapd)
	}

	offline := AssessEvent(testEvent(contract.EventDiscoveryCameraOffline, "entry", 0), Context{
		DeviceRole: "access_control",
		Now:        testTime(12),
	})
	if offline.Level < 2 {
		t.Fatalf("camera offline access control should be at least level 2: %#v", offline)
	}
}

func TestDangerScoringUncertainLowConfidenceCreatesValidation(t *testing.T) {
	assessment := AssessEvent(testEvent(contract.EventVisionUncertain, "salon", 0.42), Context{Now: testTime(12)})
	if assessment.Level != 2 || assessment.Category != contract.DangerCategoryIdentity || !assessment.ValidationRequired {
		t.Fatalf("unexpected uncertain assessment: %#v", assessment)
	}
}

func TestSecurityMachineGoldenTimelineInputs(t *testing.T) {
	now := testTime(12)
	cases := []struct {
		name       string
		eventType  string
		role       string
		repetition int
		eventAt    time.Time
		wantLevel  int
		wantState  string
		wantReason string
	}{
		{name: "known resident", eventType: contract.EventVisionIdentity, role: "access_control", wantLevel: 1, wantState: "activity", wantReason: "known_resident"},
		{name: "unknown intrusion", eventType: contract.EventVisionUnknown, role: "access_control", wantLevel: 5, wantState: "intrusion", wantReason: "unknown_identity"},
		{name: "camera disappearance", eventType: contract.EventDiscoveryCameraOffline, role: "access_control", wantLevel: 3, wantState: "suspicious", wantReason: "device_offline"},
		{name: "repeated motion", eventType: contract.EventVisionMotion, role: "access_control", repetition: 3, wantLevel: 2, wantState: "activity", wantReason: "repeated_motion_sensitive_zone"},
		{name: "authorized reset", eventType: contract.EventSystemStateReset, role: "access_control", wantLevel: 0, wantState: "idle", wantReason: "informational_event"},
		{name: "out of order observation", eventType: contract.EventVisionUnknown, role: "access_control", eventAt: now.Add(-time.Minute), wantLevel: 5, wantState: "intrusion", wantReason: "unknown_identity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := tc.eventAt
			if at.IsZero() {
				at = now
			}
			event := testEvent(tc.eventType, "entry", 0.9)
			event.Timestamp = at
			assessment := AssessEvent(event, Context{
				DeviceRole:      tc.role,
				RepetitionCount: tc.repetition,
				Now:             now,
			})
			if assessment.Level != tc.wantLevel || assessment.ExpectedState != tc.wantState {
				t.Fatalf("assessment level/state=(%d,%q), want (%d,%q): %#v", assessment.Level, assessment.ExpectedState, tc.wantLevel, tc.wantState, assessment)
			}
			if !containsString(assessment.Reasons, tc.wantReason) {
				t.Fatalf("assessment reasons=%v, want %q", assessment.Reasons, tc.wantReason)
			}
		})
	}
}

func TestSimulatedAssessmentMarked(t *testing.T) {
	event := testEvent(contract.EventVisionUnknown, "entry", 0.72)
	event.Payload["metadata"] = map[string]any{"simulated": true, "dry_run": true}
	assessment := AssessEvent(event, Context{DeviceRole: "access_control", Now: testTime(12)})

	if !assessment.Simulated {
		t.Fatalf("assessment should be simulated: %#v", assessment)
	}
	for _, action := range assessment.RecommendedSystemActions {
		if !action.Simulated || !action.DryRun {
			t.Fatalf("system action should inherit simulation flags: %#v", action)
		}
	}
}

func TestManualRiskPreservesRequestedLevelThroughProfileMultipliers(t *testing.T) {
	event := testEvent(contract.EventManualRisk, "entry", 0)
	event.Payload["danger_level"] = "high"
	assessment := AssessEvent(event, Context{
		Now:               testTime(12),
		ProfileEnabled:    true,
		GlobalSensitivity: 0.9,
		NightMultiplier:   2,
		ArmedMultiplier:   2,
		HomeMode:          "armed",
	})
	if assessment.Level != 4 || assessment.RiskLevel != "high" || assessment.Score != 0.75 {
		t.Fatalf("manual high was rewritten: %#v", assessment)
	}

	event.Payload["danger_level"] = "critical"
	assessment = AssessEvent(event, Context{Now: testTime(12), ProfileEnabled: true, GlobalSensitivity: 0.9})
	if assessment.Level != 5 || assessment.RiskLevel != "critical" || assessment.Score != 0.95 {
		t.Fatalf("manual critical was rewritten: %#v", assessment)
	}
}

func TestDangerAssessmentCarriesPersistenceMetadata(t *testing.T) {
	assessment := AssessEvent(testEvent(contract.EventVisionWeapon, "entry", 0.90), Context{Now: testTime(12)})
	if assessment.RiskLevel == "" || assessment.ExpectedState == "" || assessment.LastSeen.IsZero() {
		t.Fatalf("danger persistence metadata missing: %#v", assessment)
	}
	if !contract.IsPersistableDangerAssessment(&assessment) {
		t.Fatalf("critical danger should be persistable: %#v", assessment)
	}

	low := AssessEvent(testEvent(contract.EventVisionIdentity, "entry", 0.95), Context{Now: testTime(12)})
	if contract.IsPersistableDangerAssessment(&low) {
		t.Fatalf("low danger should not be persisted: %#v", low)
	}
	worker := AssessEvent(testEvent(contract.EventDiscoveryWorkerCrashed, "", 0), Context{Now: testTime(12)})
	if contract.IsPersistableDangerAssessment(&worker) {
		t.Fatalf("discovery worker health must not be persisted as danger: %#v", worker)
	}
}

func testEvent(eventType string, nodeID string, confidence float64) *contract.Event {
	return &contract.Event{
		ID:         "evt-" + eventType,
		Type:       eventType,
		Source:     "test",
		Timestamp:  testTime(12),
		DeviceID:   "cam_01",
		NodeID:     nodeID,
		Confidence: confidence,
		Payload:    map[string]any{"confidence": confidence},
	}
}

func testTime(hour int) time.Time {
	return time.Date(2026, 7, 9, hour, 0, 0, 0, time.UTC)
}

func assertAction(t *testing.T, assessment contract.DangerAssessment, actionType string) {
	t.Helper()
	for _, action := range assessment.RecommendedSystemActions {
		if action.Type == actionType {
			return
		}
	}
	t.Fatalf("assessment missing action %s: %#v", actionType, assessment.RecommendedSystemActions)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
