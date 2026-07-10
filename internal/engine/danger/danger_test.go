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

	if assessment.Level != 3 || assessment.Category != contract.DangerCategorySecurity || !assessment.ValidationRequired {
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

	if assessment.Level != 4 || assessment.Score < 0.80 || !assessment.ValidationRequired {
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
