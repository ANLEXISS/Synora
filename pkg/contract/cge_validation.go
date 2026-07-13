package contract

import (
	"strings"
	"time"
)

const ValidationTestModeControlledReal = "controlled_real_test"

type CGEValidationEventRequest struct {
	EventType       string  `json:"event_type"`
	DeviceID        string  `json:"device_id,omitempty"`
	NodeID          string  `json:"node_id,omitempty"`
	Identity        string  `json:"identity,omitempty"`
	Confidence      float64 `json:"confidence,omitempty"`
	DangerLevelHint string  `json:"danger_level_hint,omitempty"`
	Learn           *bool   `json:"learn,omitempty"`
	Reason          string  `json:"reason,omitempty"`
}

func (r CGEValidationEventRequest) LearnEnabled(inherited bool) bool {
	if r.Learn != nil {
		return *r.Learn
	}
	return inherited
}

func (r CGEValidationEventRequest) NormalizedEventType() string {
	switch strings.ToLower(strings.TrimSpace(r.EventType)) {
	case "motion.detected":
		return EventVisionMotion
	case "weapon.detected":
		return EventVisionWeapon
	case "fall.detected":
		return EventVisionFall
	case "camera.offline":
		return EventDiscoveryCameraOffline
	case "camera.tampered":
		return EventVisionTamper
	case "camera.online":
		return EventDiscoveryCameraOnline
	default:
		return NormalizeEventType(r.EventType)
	}
}

type CGEValidationSequenceRequest struct {
	Events []CGEValidationEventRequest `json:"events"`
	Learn  bool                        `json:"learn,omitempty"`
	Reason string                      `json:"reason,omitempty"`
}

type CGEValidationHistoryItem struct {
	ValidationID string    `json:"validation_id"`
	EventID      string    `json:"event_id"`
	EventType    string    `json:"event_type"`
	Timestamp    time.Time `json:"timestamp"`
	DeviceID     string    `json:"device_id,omitempty"`
	NodeID       string    `json:"node_id,omitempty"`
	ChainID      string    `json:"chain_id,omitempty"`
	Learn        bool      `json:"learn"`
	Reason       string    `json:"reason,omitempty"`
	SourceType   string    `json:"source_type"`
	TestMode     string    `json:"test_mode"`
}
