// Package adapter contains Synora boundary translations for the Cognitive Graph
// Engine. It is the only engine package that knows how to normalize Synora
// events, create CGE events, and build state updates consumed by the core app.
package adapter

import (
	"strings"
	"time"

	"synora/internal/device"
	cgecontracts "synora/internal/engine/contracts"
	"synora/internal/idgen"
	"synora/internal/state"
	"synora/pkg/contract"
)

const (
	StateAbsent   = "absent"
	StateEntering = "entering"
	StatePresent  = "present"
	StateLeaving  = "leaving"
)

type Result struct {
	Decision         *contract.Decision
	NodeStates       map[string]*state.NodeState
	Identity         *state.IdentityState
	Presence         *state.PresenceState
	Clip             *state.ClipState
	System           *state.SystemState
	Situations       []cgecontracts.Situation
	DangerAssessment *contract.DangerAssessment
}

func NormalizeEvent(event *contract.Event, registry *device.Registry) time.Time {
	now := event.Timestamp.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
		event.Timestamp = now
	}

	event.Type = contract.NormalizeEventType(event.Type)

	if event.Payload == nil {
		event.Payload = map[string]any{}
	}

	if event.Priority == 0 {
		event.Priority = contract.EventPriority(event.Type)
	}

	if event.DeviceID == "" {
		event.DeviceID = resolveDeviceID(event, event.Payload)
	}

	if event.NodeID == "" && registry != nil && event.DeviceID != "" {
		if device, ok := registry.Get(event.DeviceID); ok && device != nil {
			event.NodeID = device.NodeID
		}
	}

	if event.Identity == "" {
		event.Identity = strings.TrimSpace(resolvePayloadString(event.Payload, "identity", "resident_id"))
	}

	if event.Confidence == 0 {
		event.Confidence = asFloat(event.Payload["confidence"])
	}

	if event.GroupKey == "" {
		event.GroupKey = strings.Join([]string{
			event.Type,
			event.Source,
			event.DeviceID,
			event.NodeID,
			event.Identity,
		}, "|")
	}
	if event.TrackID == "" {
		event.TrackID = resolvePayloadString(event.Payload, "track_id")
	}
	if event.ClipID == "" {
		event.ClipID = resolvePayloadString(event.Payload, "clip_id")
	}

	return now
}

func ToCGEEvent(
	event *contract.Event,
	store *state.Store,
	now time.Time,
) *cgecontracts.Event {
	subjectType, subjectID := subjectFromEvent(event)
	targetType, targetID := targetFromEvent(event)

	metadata := cloneMetadata(event.Payload)
	metadata["source"] = event.Source
	metadata["raw_type"] = event.Type
	metadata["device_id"] = event.DeviceID
	metadata["node_id"] = event.NodeID
	metadata["clip_id"] = event.ClipID
	metadata["priority"] = event.Priority
	metadata["hour"] = now.Hour()
	metadata["weekday"] = int(now.Weekday())
	metadata["house_state"] = houseState(store)

	return &cgecontracts.Event{
		ID:           event.ID,
		Type:         mapEventType(event.Type, event.Payload),
		SubjectType:  subjectType,
		SubjectID:    subjectID,
		TargetType:   targetType,
		TargetID:     targetID,
		TopologyNode: event.NodeID,
		Timestamp:    now,
		TrackID:      event.TrackID,
		Confidence:   event.Confidence,
		Severity:     severityFromPriority(event.Priority),
		Metadata:     metadata,
	}
}

func ToSynoraDecision(
	event *contract.Event,
	result cgecontracts.DecisionResult,
	now time.Time,
) *contract.Decision {
	stateValue := systemStateFromDecision(result)
	reason := strings.Join(result.Reasons, ",")
	if reason == "" {
		reason = string(result.Level)
	}

	return &contract.Decision{
		ID:             idgen.New("dec"),
		Type:           "engine.decision",
		Source:         "core",
		Timestamp:      now,
		Priority:       priorityFromSeverity(result.Level),
		EventID:        event.ID,
		Score:          result.DecisionScore,
		EffectiveScore: result.DecisionScore,
		Alert:          result.Level == cgecontracts.SeverityHigh || result.Level == cgecontracts.SeverityCritical,
		Reason:         reason,
		State:          stateValue,
		NodeID:         event.NodeID,
		ClipID:         event.ClipID,
		TrackID:        event.TrackID,
		GroupKey:       event.GroupKey,
		SequenceKey:    result.SequenceKey,
		GraphUsed:      result.GraphUsed,

		ValidationRequired: result.ValidationRequired,
		ValidationReason:   result.ValidationReason,
	}
}

func BuildResult(
	event *contract.Event,
	store *state.Store,
	decisionResult cgecontracts.DecisionResult,
	now time.Time,
	assessment *contract.DangerAssessment,
) *Result {
	event.ValidationRequired = decisionResult.ValidationRequired
	event.ValidationReason = decisionResult.ValidationReason
	decision := ToSynoraDecision(event, decisionResult, now)
	if assessment != nil {
		decision.State = systemStateFromAssessment(decisionResult, assessment)
		decision.Alert = assessment.Level >= 4
	}

	return &Result{
		Decision:         decision,
		NodeStates:       buildNodeStates(event, decisionResult, now),
		Identity:         buildIdentity(event, now),
		Presence:         buildPresence(event, now),
		Clip:             buildClip(event, now),
		System:           buildSystemState(store, decisionResult, assessment, now),
		Situations:       decisionResult.Situations,
		DangerAssessment: assessment,
	}
}

func buildNodeStates(
	event *contract.Event,
	result cgecontracts.DecisionResult,
	now time.Time,
) map[string]*state.NodeState {
	if event.NodeID == "" {
		return nil
	}
	return map[string]*state.NodeState{
		event.NodeID: {
			NodeID:      event.NodeID,
			DangerScore: result.DecisionScore,
			LastEventID: event.ID,
			LastSeen:    now,
			UpdatedAt:   now,
		},
	}
}

func buildIdentity(event *contract.Event, now time.Time) *state.IdentityState {
	identity := normalizedIdentity(event.Identity)
	if identity == "" {
		return nil
	}
	return &state.IdentityState{
		ID:           identity,
		LastNodeID:   event.NodeID,
		LastDeviceID: event.DeviceID,
		Confidence:   event.Confidence,
		State:        StatePresent,
		CreatedAt:    now,
		UpdatedAt:    now,
		LastSeen:     now,
		ExpiresAt:    now.Add(45 * time.Second),
	}
}

func buildPresence(event *contract.Event, now time.Time) *state.PresenceState {
	identity := normalizedIdentity(event.Identity)
	if identity == "" {
		return nil
	}
	return &state.PresenceState{
		ID:         identity,
		ResidentID: identity,
		Location:   event.NodeID,
		Confidence: event.Confidence,
		State:      StatePresent,
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeen:   now,
		ExpiresAt:  now.Add(45 * time.Second),
	}
}

func buildClip(event *contract.Event, now time.Time) *state.ClipState {
	if event.Payload == nil {
		return nil
	}

	path := strings.TrimSpace(resolvePayloadString(event.Payload, "clip_path"))
	if path == "" {
		return nil
	}

	cameraID := event.DeviceID
	if cameraID == "" {
		cameraID = resolvePayloadString(event.Payload, "camera", "camera_id")
	}

	clipID := event.ClipID
	if clipID == "" {
		clipID = idgen.New("clip")
		event.ClipID = clipID
	}

	return &state.ClipState{
		ID:        clipID,
		CameraID:  cameraID,
		EventID:   event.ID,
		Path:      path,
		Start:     now,
		End:       now,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(5 * time.Minute),
	}
}

func buildSystemState(
	store *state.Store,
	result cgecontracts.DecisionResult,
	assessment *contract.DangerAssessment,
	now time.Time,
) *state.SystemState {
	current := store.SystemState()
	next := current
	next.LastState = systemStateFromAssessment(result, assessment)
	next.IntrusionActive = next.LastState == "intrusion"
	next.EmergencyActive = next.LastState == "emergency"

	if next.LastState != current.LastState {
		next.LastStateTime = now
	}
	if next.IntrusionActive {
		if next.IntrusionTime.IsZero() {
			next.IntrusionTime = now
		}
	} else {
		next.IntrusionTime = time.Time{}
	}
	if next.EmergencyActive {
		if next.EmergencyTime.IsZero() {
			next.EmergencyTime = now
		}
	} else {
		next.EmergencyTime = time.Time{}
	}

	return &next
}

func subjectFromEvent(event *contract.Event) (cgecontracts.SubjectType, string) {
	if identity := normalizedIdentity(event.Identity); identity != "" {
		return cgecontracts.SubjectResident, identity
	}
	if bestMatch := normalizedIdentity(resolvePayloadString(event.Payload, "best_match")); bestMatch != "" {
		return cgecontracts.SubjectResident, bestMatch
	}
	if event.DeviceID != "" {
		return cgecontracts.SubjectDevice, event.DeviceID
	}
	if event.Source != "" {
		return cgecontracts.SubjectSystem, event.Source
	}
	return cgecontracts.SubjectUnknown, "unknown"
}

func targetFromEvent(event *contract.Event) (cgecontracts.SubjectType, string) {
	if event.DeviceID != "" {
		return cgecontracts.SubjectDevice, event.DeviceID
	}
	if event.NodeID != "" {
		return cgecontracts.SubjectSystem, event.NodeID
	}
	return cgecontracts.SubjectSystem, "house"
}

func mapEventType(eventType string, payload map[string]any) string {
	switch contract.NormalizeEventType(eventType) {
	case "vision.identity":
		return "vision.id.seen"
	case "vision.unknown":
		return "vision.id.lost"
	case "vision.uncertain":
		return "vision.id.uncertain"
	case "vision.motion":
		return "vision.motion.detected"
	case "vision.fall":
		return "vision.pose.fallen"
	case "vision.tamper":
		return "vision.camera.tampered"
	case "vision.weapon", "vision.weapon.detected":
		return mapWeaponEvent(payload)
	case "device.offline":
		return "vision.camera.offline"
	default:
		return contract.NormalizeEventType(eventType)
	}
}

func mapWeaponEvent(payload map[string]any) string {
	weapon := strings.ToLower(strings.TrimSpace(resolvePayloadString(payload, "weapon", "weapon_type")))
	switch weapon {
	case "firearm", "gun":
		return "vision.weapon.firearm"
	case "knife":
		return "vision.weapon.knife"
	default:
		return "vision.weapon.detected"
	}
}

func systemStateFromDecision(result cgecontracts.DecisionResult) string {
	switch result.Level {
	case cgecontracts.SeverityCritical:
		return "intrusion"
	case cgecontracts.SeverityHigh:
		return "suspicious"
	case cgecontracts.SeverityMedium, cgecontracts.SeverityLow:
		return "activity"
	default:
		return "idle"
	}
}

func systemStateFromAssessment(result cgecontracts.DecisionResult, assessment *contract.DangerAssessment) string {
	if assessment == nil {
		return systemStateFromDecision(result)
	}
	if assessment.Category == contract.DangerCategoryMedicalEmergency && assessment.Level >= 5 {
		return "emergency"
	}
	if assessment.Level >= 5 {
		return "intrusion"
	}
	if assessment.Level >= 3 {
		return "suspicious"
	}
	if assessment.Level >= 1 {
		return "activity"
	}
	return "idle"
}

func priorityFromSeverity(level cgecontracts.Severity) int {
	switch level {
	case cgecontracts.SeverityCritical:
		return contract.PriorityCritical
	case cgecontracts.SeverityHigh:
		return contract.PriorityHigh
	case cgecontracts.SeverityMedium:
		return contract.PriorityNormal
	case cgecontracts.SeverityLow:
		return contract.PriorityLow
	default:
		return contract.PriorityLow
	}
}

func severityFromPriority(priority int) cgecontracts.Severity {
	switch {
	case priority >= contract.PriorityCritical:
		return cgecontracts.SeverityCritical
	case priority >= contract.PriorityHigh:
		return cgecontracts.SeverityHigh
	case priority >= contract.PriorityNormal:
		return cgecontracts.SeverityMedium
	case priority >= contract.PriorityLow:
		return cgecontracts.SeverityLow
	default:
		return cgecontracts.SeverityInfo
	}
}

func houseState(store *state.Store) string {
	if store == nil {
		return string(cgecontracts.HouseStateUnknown)
	}
	current := store.SystemState()
	switch current.LastState {
	case "idle":
		return string(cgecontracts.HouseStateEmpty)
	default:
		return string(cgecontracts.HouseStateOccupied)
	}
}

func normalizedIdentity(identity string) string {
	identity = strings.TrimSpace(identity)
	switch strings.ToLower(identity) {
	case "", "unknown", "uncertain":
		return ""
	default:
		return identity
	}
}

func resolveDeviceID(event *contract.Event, payload map[string]any) string {
	if event.DeviceID != "" {
		return event.DeviceID
	}
	value := resolvePayloadString(payload, "device", "camera", "camera_id", "device_id")
	if value != "" {
		return value
	}
	return strings.TrimSpace(event.Source)
}

func resolvePayloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func asFloat(value any) float64 {
	switch current := value.(type) {
	case float64:
		return current
	case float32:
		return float64(current)
	case int:
		return float64(current)
	case int64:
		return float64(current)
	default:
		return 0
	}
}

func cloneMetadata(source map[string]any) map[string]any {
	out := make(map[string]any, len(source)+8)
	for key, value := range source {
		out[key] = value
	}
	return out
}
