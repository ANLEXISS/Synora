package contract

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	PriorityLow      = 25
	PriorityNormal   = 50
	PriorityHigh     = 75
	PriorityCritical = 100
)

/*
EVENT TYPES
*/

const (

	// EventVisionIdentity reports that a known resident or identity was recognized by vision.
	EventVisionIdentity = "vision.identity"
	// EventVisionUnknown reports a detected person or subject with no known identity.
	EventVisionUnknown = "vision.unknown"
	// EventVisionUncertain reports a low-confidence identity or ambiguous visual classification.
	EventVisionUncertain = "vision.uncertain"
	// EventVisionMotion reports motion detected by the vision pipeline.
	EventVisionMotion = "vision.motion"
	// EventVisionWeapon reports a potential weapon detection.
	EventVisionWeapon = "vision.weapon"
	// EventVisionFall reports a potential person fall detection.
	EventVisionFall = "vision.fall"
	// EventVisionFight reports a potential fight detection.
	EventVisionFight = "vision.fight"
	// EventVisionTamper reports camera obstruction, movement, or tampering.
	EventVisionTamper = "vision.tamper"

	// Device events
	EventDeviceTrigger = "device.trigger"
	// EventDeviceOffline reports that a device or camera is no longer reachable.
	EventDeviceOffline = "device.offline"

	// Discovery events
	EventDiscoveryCameraOnline  = "discovery.camera.online"
	EventDiscoveryCameraOffline = "discovery.camera.offline"
	EventDiscoveryWorkerStarted = "discovery.worker.started"
	EventDiscoveryWorkerStopped = "discovery.worker.stopped"
	EventDiscoveryWorkerCrashed = "discovery.worker.crashed"

	// System events
	// EventSystemStateChanged reports a state transition published by the Core.
	EventSystemStateChanged = "system.state.changed"
	EventSystemPresence     = "system.presence.updated"
	EventSystemUnknown      = "system.unknown"

	// EventActionRequest asks the Actions service to execute an action.
	EventActionRequest = "action.request"
	// EventActionResult reports the outcome of an action request.
	EventActionResult = "action.result"
	// EventAutomationAction is the temporary legacy action command emitted by older automations.
	EventAutomationAction = "automation.action"
)

/*
EVENT STRUCT
*/

type Event struct {
	ID string `json:"id,omitempty"`

	Type string `json:"type"`

	Source string `json:"source"`

	Timestamp time.Time `json:"timestamp,omitempty"`

	Payload map[string]any `json:"payload,omitempty"`

	DeviceID string `json:"device_id,omitempty"`
	NodeID   string `json:"node_id,omitempty"`

	Identity   string  `json:"identity,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`

	Priority int    `json:"priority,omitempty"`
	GroupKey string `json:"group_key,omitempty"`
	TrackID  string `json:"track_id,omitempty"`
	ClipID   string `json:"clip_id,omitempty"`

	ValidationRequired bool   `json:"validation_required,omitempty"`
	ValidationReason   string `json:"validation_reason,omitempty"`
}

type eventJSON struct {
	ID                 string         `json:"id,omitempty"`
	Type               string         `json:"type"`
	Source             string         `json:"source"`
	Timestamp          time.Time      `json:"timestamp,omitempty"`
	Payload            map[string]any `json:"payload,omitempty"`
	DeviceID           string         `json:"device_id,omitempty"`
	NodeID             string         `json:"node_id,omitempty"`
	Identity           string         `json:"identity,omitempty"`
	Confidence         float64        `json:"confidence,omitempty"`
	Priority           int            `json:"priority,omitempty"`
	GroupKey           string         `json:"group_key,omitempty"`
	TrackID            string         `json:"track_id,omitempty"`
	ClipID             string         `json:"clip_id,omitempty"`
	ValidationRequired bool           `json:"validation_required,omitempty"`
	ValidationReason   string         `json:"validation_reason,omitempty"`
}

func (e *Event) UnmarshalJSON(data []byte) error {
	var decoded eventJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	applyLegacyEventFields(data, &decoded)

	*e = Event{
		ID:                 decoded.ID,
		Type:               decoded.Type,
		Source:             decoded.Source,
		Timestamp:          decoded.Timestamp,
		Payload:            decoded.Payload,
		DeviceID:           decoded.DeviceID,
		NodeID:             decoded.NodeID,
		Identity:           decoded.Identity,
		Confidence:         decoded.Confidence,
		Priority:           decoded.Priority,
		GroupKey:           decoded.GroupKey,
		TrackID:            decoded.TrackID,
		ClipID:             decoded.ClipID,
		ValidationRequired: decoded.ValidationRequired,
		ValidationReason:   decoded.ValidationReason,
	}
	return nil
}

func applyLegacyEventFields(data []byte, decoded *eventJSON) {
	legacy := struct {
		DeviceID           string `json:"DeviceID,omitempty"`
		NodeID             string `json:"NodeID,omitempty"`
		GroupKey           string `json:"GroupKey,omitempty"`
		TrackID            string `json:"TrackID,omitempty"`
		ClipID             string `json:"ClipID,omitempty"`
		ValidationRequired bool   `json:"ValidationRequired,omitempty"`
		ValidationReason   string `json:"ValidationReason,omitempty"`
	}{}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return
	}
	if decoded.DeviceID == "" {
		decoded.DeviceID = legacy.DeviceID
	}
	if decoded.NodeID == "" {
		decoded.NodeID = legacy.NodeID
	}
	if decoded.GroupKey == "" {
		decoded.GroupKey = legacy.GroupKey
	}
	if decoded.TrackID == "" {
		decoded.TrackID = legacy.TrackID
	}
	if decoded.ClipID == "" {
		decoded.ClipID = legacy.ClipID
	}
	if !decoded.ValidationRequired {
		decoded.ValidationRequired = legacy.ValidationRequired
	}
	if decoded.ValidationReason == "" {
		decoded.ValidationReason = legacy.ValidationReason
	}
}

/*
EVENT WINDOW
*/

type EventWindow struct {
	NodeID string

	Events []*Event

	LastUpdate time.Time

	Score float64

	VisionKnown     int
	VisionUnknown   int
	VisionUncertain int
}

/*
TYPE HELPERS
*/

func IsVisionEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "vision.")
}

func IsDeviceEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "device.")
}

func IsSystemEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "system.")
}

/*
NORMALIZE EVENT TYPE
*/

func NormalizeEventType(raw string) string {

	raw = strings.TrimSpace(strings.ToLower(raw))

	if raw == "" {
		return EventSystemUnknown
	}

	switch raw {

	case
		EventVisionIdentity,
		EventVisionUnknown,
		EventVisionUncertain,
		EventVisionWeapon,
		EventVisionFall,
		EventVisionFight,
		EventVisionTamper,
		EventVisionMotion,
		EventDeviceTrigger,
		EventDeviceOffline,
		EventDiscoveryCameraOnline,
		EventDiscoveryCameraOffline,
		EventDiscoveryWorkerStarted,
		EventDiscoveryWorkerStopped,
		EventDiscoveryWorkerCrashed,
		EventSystemStateChanged,
		EventSystemPresence,
		EventAutomationAction,
		EventActionRequest:
		return raw

	case EventActionResult:

		return raw

	case "identity":
		return EventVisionIdentity

	case "unknown":
		return EventVisionUnknown

	case "motion":
		return EventVisionMotion

	case "trigger":
		return EventDeviceTrigger

	}

	if strings.Contains(raw, ".") {
		return raw
	}

	return "system." + raw
}

/*
EVENT PRIORITY
*/

func EventPriority(eventType string) int {

	eventType = NormalizeEventType(eventType)

	switch eventType {

	case EventVisionWeapon,
		EventVisionFight:
		return PriorityCritical

	case EventVisionTamper,
		EventVisionFall,
		EventDeviceOffline,
		EventDiscoveryCameraOffline,
		EventDiscoveryWorkerCrashed:
		return PriorityHigh

	case EventVisionUnknown,
		EventVisionUncertain,
		EventVisionIdentity,
		EventVisionMotion,
		EventDeviceTrigger,
		EventDiscoveryCameraOnline,
		EventDiscoveryWorkerStarted,
		EventDiscoveryWorkerStopped,
		EventAutomationAction,
		EventActionRequest,
		EventActionResult:
		return PriorityNormal

	default:
		return PriorityLow
	}
}
