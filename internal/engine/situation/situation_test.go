package situation

import (
	"testing"
	"time"

	"synora/internal/engine/contracts"
)

func TestAnalyzeMapsEventsToSituations(t *testing.T) {
	tests := []struct {
		name          string
		eventType     string
		rawType       string
		subjectType   contracts.SubjectType
		subjectID     string
		targetType    contracts.SubjectType
		targetID      string
		wantType      string
		wantResident  string
		wantDeviceID  string
		wantExpiresAt bool
	}{
		{
			name:          "identity",
			eventType:     "vision.id.seen",
			rawType:       "vision.identity",
			subjectType:   contracts.SubjectResident,
			subjectID:     "alexis",
			targetType:    contracts.SubjectDevice,
			targetID:      "cam_01",
			wantType:      TypeResidentPresent,
			wantResident:  "alexis",
			wantDeviceID:  "cam_01",
			wantExpiresAt: true,
		},
		{
			name:          "unknown",
			eventType:     "vision.id.lost",
			rawType:       "vision.unknown",
			targetType:    contracts.SubjectDevice,
			targetID:      "cam_02",
			wantType:      TypeUnknownPresence,
			wantDeviceID:  "cam_02",
			wantExpiresAt: true,
		},
		{
			name:         "weapon",
			eventType:    "vision.weapon.firearm",
			rawType:      "vision.weapon",
			targetType:   contracts.SubjectDevice,
			targetID:     "cam_03",
			wantType:     TypeThreatWeapon,
			wantDeviceID: "cam_03",
		},
		{
			name:      "fall",
			eventType: "vision.pose.fallen",
			rawType:   "vision.fall",
			wantType:  TypeSafetyFall,
		},
		{
			name:      "tamper",
			eventType: "vision.camera.tampered",
			rawType:   "vision.tamper",
			wantType:  TypeCameraTamper,
		},
		{
			name:         "offline",
			eventType:    "vision.camera.offline",
			rawType:      "device.offline",
			targetType:   contracts.SubjectDevice,
			targetID:     "cam_04",
			wantType:     TypeDeviceOffline,
			wantDeviceID: "cam_04",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			situations := Analyze(&contracts.Event{
				ID:           "evt-" + tt.name,
				Type:         tt.eventType,
				SubjectType:  tt.subjectType,
				SubjectID:    tt.subjectID,
				TargetType:   tt.targetType,
				TargetID:     tt.targetID,
				TopologyNode: "entry",
				Timestamp:    testTime(),
				Severity:     contracts.SeverityHigh,
				Metadata: map[string]any{
					"raw_type": tt.rawType,
				},
			}, contracts.DecisionResult{
				Level: contracts.SeverityCritical,
			}, testTime())

			if len(situations) != 1 {
				t.Fatalf("expected one situation, got %#v", situations)
			}
			got := situations[0]
			if got.Type != tt.wantType {
				t.Fatalf("situation type mismatch: got %q want %q", got.Type, tt.wantType)
			}
			if got.Severity != contracts.SeverityCritical {
				t.Fatalf("severity should come from decision, got %q", got.Severity)
			}
			if got.NodeID != "entry" {
				t.Fatalf("node_id mismatch: %#v", got)
			}
			if got.ResidentID != tt.wantResident {
				t.Fatalf("resident_id mismatch: got %q want %q", got.ResidentID, tt.wantResident)
			}
			if got.DeviceID != tt.wantDeviceID {
				t.Fatalf("device_id mismatch: got %q want %q", got.DeviceID, tt.wantDeviceID)
			}
			if (got.ExpiresAt != nil) != tt.wantExpiresAt {
				t.Fatalf("expires_at presence mismatch: %#v", got)
			}
		})
	}
}

func TestAnalyzePropagatesClipID(t *testing.T) {
	situations := Analyze(&contracts.Event{
		ID:           "evt-clip",
		Type:         "vision.weapon.detected",
		TopologyNode: "entry",
		Metadata: map[string]any{
			"clip_id": "clip-123",
		},
	}, contracts.DecisionResult{}, testTime())

	if len(situations) != 1 {
		t.Fatalf("expected one situation, got %#v", situations)
	}
	if situations[0].ClipID != "clip-123" {
		t.Fatalf("clip_id mismatch: %#v", situations[0])
	}
}

func TestAnalyzeAlwaysAddsEvidence(t *testing.T) {
	situations := Analyze(&contracts.Event{
		Type: "vision.camera.tampered",
	}, contracts.DecisionResult{}, testTime())

	if len(situations) != 1 {
		t.Fatalf("expected one situation, got %#v", situations)
	}
	if len(situations[0].Evidence) == 0 {
		t.Fatalf("expected non-empty evidence: %#v", situations[0])
	}
}

func testTime() time.Time {
	return time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC)
}
