package situation

import (
	"fmt"
	"strings"
	"time"

	"synora/internal/engine/contracts"
)

const (
	TypeResidentPresent = "resident.present"
	TypeResidentSeen    = "resident.seen"
	TypeUnknownPresence = "unknown.presence"
	TypeThreatWeapon    = "threat.weapon"
	TypeSafetyFall      = "safety.fall"
	TypeCameraTamper    = "camera.tamper"
	TypeDeviceOffline   = "device.offline"
)

type Situation = contracts.Situation

func Analyze(
	event *contracts.Event,
	decision contracts.DecisionResult,
	now time.Time,
) []Situation {
	if event == nil {
		return nil
	}
	if now.IsZero() {
		now = event.Timestamp
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	situationType, ok := situationType(event)
	if !ok {
		return nil
	}

	s := Situation{
		ID:         situationID(event, situationType),
		Type:       situationType,
		Severity:   severity(event, decision),
		NodeID:     nodeID(event),
		ResidentID: residentID(event),
		DeviceID:   deviceID(event),
		ClipID:     metadataString(event.Metadata, "clip_id"),
		Evidence:   evidence(event, decision),
		CreatedAt:  now,
	}
	if isTransient(situationType) {
		expiresAt := now.Add(45 * time.Second)
		s.ExpiresAt = &expiresAt
	}
	return []Situation{s}
}

func situationType(event *contracts.Event) (string, bool) {
	eventType := normalizedEventType(event)
	switch eventType {
	case "vision.identity", "vision.id.seen":
		if residentID(event) == "" {
			return TypeResidentSeen, true
		}
		return TypeResidentPresent, true
	case "vision.unknown", "vision.id.lost":
		return TypeUnknownPresence, true
	case "vision.weapon", "vision.weapon.detected", "vision.weapon.firearm", "vision.weapon.knife":
		return TypeThreatWeapon, true
	case "vision.fall", "vision.pose.fallen":
		return TypeSafetyFall, true
	case "vision.tamper", "vision.camera.tampered":
		return TypeCameraTamper, true
	case "device.offline", "vision.camera.offline":
		return TypeDeviceOffline, true
	default:
		return "", false
	}
}

func normalizedEventType(event *contracts.Event) string {
	if event == nil {
		return ""
	}
	if rawType := metadataString(event.Metadata, "raw_type"); rawType != "" {
		return strings.ToLower(strings.TrimSpace(rawType))
	}
	return strings.ToLower(strings.TrimSpace(event.Type))
}

func severity(event *contracts.Event, decision contracts.DecisionResult) contracts.Severity {
	if decision.Level != "" {
		return decision.Level
	}
	if event != nil && event.Severity != "" {
		return event.Severity
	}
	return contracts.SeverityInfo
}

func nodeID(event *contracts.Event) string {
	if event == nil {
		return ""
	}
	if event.TopologyNode != "" {
		return event.TopologyNode
	}
	return metadataString(event.Metadata, "node_id")
}

func residentID(event *contracts.Event) string {
	if event == nil {
		return ""
	}
	if event.SubjectType == contracts.SubjectResident && event.SubjectID != "" {
		return event.SubjectID
	}
	return metadataString(event.Metadata, "resident_id", "identity", "best_match")
}

func deviceID(event *contracts.Event) string {
	if event == nil {
		return ""
	}
	if event.TargetType == contracts.SubjectDevice && event.TargetID != "" {
		return event.TargetID
	}
	if event.SubjectType == contracts.SubjectDevice && event.SubjectID != "" {
		return event.SubjectID
	}
	return metadataString(event.Metadata, "device_id", "camera_id", "camera", "device")
}

func evidence(event *contracts.Event, decision contracts.DecisionResult) []string {
	items := make([]string, 0, 4+len(decision.Evidence)+len(decision.Reasons))
	if event != nil && event.ID != "" {
		items = append(items, "event:"+event.ID)
	}
	if event != nil && event.Type != "" {
		items = append(items, "event_type:"+event.Type)
	}
	items = append(items, decision.Evidence...)
	for _, reason := range decision.Reasons {
		if reason != "" {
			items = append(items, "reason:"+reason)
		}
	}
	if len(items) == 0 {
		items = append(items, "situation:mapped")
	}
	return items
}

func situationID(event *contracts.Event, situationType string) string {
	eventID := ""
	if event != nil {
		eventID = strings.TrimSpace(event.ID)
	}
	if eventID == "" {
		eventID = "event"
	}
	return fmt.Sprintf("sit_%s_%s", sanitizeIDPart(situationType), sanitizeIDPart(eventID))
}

func sanitizeIDPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func isTransient(situationType string) bool {
	switch situationType {
	case TypeResidentPresent, TypeResidentSeen, TypeUnknownPresence:
		return true
	default:
		return false
	}
}

func metadataString(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch current := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(current); trimmed != "" {
				return trimmed
			}
		case fmt.Stringer:
			if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
