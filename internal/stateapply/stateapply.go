package stateapply

import (
	"log"
	"strings"
	"time"

	"synora/internal/device"
	"synora/internal/engine"
	"synora/internal/idgen"
	"synora/internal/state"
	"synora/pkg/contract"
)

type Callbacks struct {
	SyncPresence func(*state.PresenceState)
}

const MinResidentIdentityConfidence = 0.50

// ApplyVisionIdentity is the runtime presence boundary for known vision
// identities. It deliberately does not touch the residents configuration map.
func ApplyVisionIdentity(store *state.Store, event *contract.Event) *state.PresenceState {
	if store == nil || event == nil || contract.NormalizeEventType(event.Type) != contract.EventVisionIdentity {
		return nil
	}
	identity := strings.TrimSpace(event.Identity)
	if identity == "" || strings.EqualFold(identity, "unknown") || strings.EqualFold(identity, "uncertain") || event.Confidence < MinResidentIdentityConfidence {
		return nil
	}
	now := event.Timestamp.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createdAt := now
	if current, ok := store.PresenceState(identity); ok && current != nil && !current.CreatedAt.IsZero() {
		createdAt = current.CreatedAt
	}
	presence := &state.PresenceState{
		ID:         identity,
		ResidentID: identity,
		Location:   strings.TrimSpace(event.NodeID),
		Confidence: event.Confidence,
		State:      "present",
		CreatedAt:  createdAt,
		UpdatedAt:  now,
		LastSeen:   now,
		ExpiresAt:  now.Add(state.DefaultPresenceTTL),
	}
	store.SetPresence(presence)

	identityCreatedAt := now
	if current, ok := store.Identity(identity); ok && current != nil && !current.CreatedAt.IsZero() {
		identityCreatedAt = current.CreatedAt
	}
	store.SetIdentity(&state.IdentityState{
		ID:           identity,
		LastNodeID:   strings.TrimSpace(event.NodeID),
		LastDeviceID: strings.TrimSpace(event.DeviceID),
		Confidence:   event.Confidence,
		State:        "present",
		CreatedAt:    identityCreatedAt,
		UpdatedAt:    now,
		LastSeen:     now,
		ExpiresAt:    now.Add(state.DefaultPresenceTTL),
	})
	return presence
}

func TouchDeviceState(store *state.Store, registry *device.Registry, event *contract.Event) {
	if store == nil || registry == nil || event == nil || event.DeviceID == "" {
		return
	}
	staticDevice, ok := registry.Get(event.DeviceID)
	if !ok || staticDevice == nil {
		return
	}
	now := event.Timestamp
	if now.IsZero() {
		now = time.Now().UTC()
	}
	current, _ := store.DeviceState(event.DeviceID)
	if current == nil {
		current = &state.DeviceState{ID: event.DeviceID, CreatedAt: now}
	}
	current.Type = staticDevice.Type
	current.Role = staticDevice.Role
	current.Room = staticDevice.Room
	current.NodeID = staticDevice.NodeID
	current.LastSeen = now
	current.LastEventID = event.ID
	current.UpdatedAt = now
	if event.Type == contract.EventDeviceOffline || event.Type == contract.EventDiscoveryCameraOffline {
		current.Online = false
	} else {
		current.Online = true
		current.ActivityCount++
	}
	store.SetDeviceState(current)

	if staticDevice.Type == "camera" {
		cameraState, _ := store.CameraState(event.DeviceID)
		if cameraState == nil {
			cameraState = &state.CameraState{ID: event.DeviceID, CreatedAt: now}
		}
		cameraState.NodeID = staticDevice.NodeID
		cameraState.Online = current.Online
		cameraState.LastSeen = now
		cameraState.UpdatedAt = now
		store.SetCameraState(cameraState)
	}
}

func Apply(store *state.Store, result *engine.Result, callbacks Callbacks) bool {
	if store == nil || result == nil {
		return false
	}
	for _, nodeState := range result.NodeStates {
		store.SetNodeState(nodeState)
	}
	if result.Identity != nil {
		store.SetIdentity(result.Identity)
	}
	if result.Presence != nil {
		store.SetPresence(result.Presence)
		if callbacks.SyncPresence != nil {
			callbacks.SyncPresence(result.Presence)
		}
	}
	if result.Clip != nil {
		store.SetClip(result.Clip)
		if cameraState, ok := store.CameraState(result.Clip.CameraID); ok && cameraState != nil {
			cameraState.LastClipID = result.Clip.ID
			cameraState.UpdatedAt = result.Clip.UpdatedAt
			store.SetCameraState(cameraState)
		}
	}
	if result.DangerAssessment != nil {
		store.AddDangerAssessment(result.DangerAssessment)
	}
	if validation := buildValidationRequest(result); validation != nil {
		store.SetValidation(validation)
	}

	changed := false
	if result.System != nil {
		current := store.SystemState()
		changed = current.LastState != result.System.LastState || current.IntrusionActive != result.System.IntrusionActive
		store.SetSystemState(*result.System)
	}
	if changed {
		log.Printf("core: system state changed -> %s", result.System.LastState)
	}

	for _, nodeState := range result.NodeStates {
		log.Printf("core: node %s danger_score=%.2f", nodeState.NodeID, nodeState.DangerScore)
		store.SetNodeState(nodeState)
	}

	return changed
}

func buildValidationRequest(result *engine.Result) *contract.ValidationRequest {
	if result == nil || result.Decision == nil {
		return nil
	}
	if result.DangerAssessment != nil && !result.DangerAssessment.ValidationRequired {
		return nil
	}
	if result.DangerAssessment == nil && !result.Decision.ValidationRequired {
		return nil
	}

	decision := result.Decision
	now := decision.Timestamp.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	situationID := ""
	evidence := []string(nil)
	if result.DangerAssessment != nil {
		evidence = append(evidence, result.DangerAssessment.Evidence...)
	}
	if len(result.Situations) > 0 {
		situationID = result.Situations[0].ID
		evidence = append(evidence, result.Situations[0].Evidence...)
	}
	if len(evidence) == 0 && strings.TrimSpace(decision.Reason) != "" {
		evidence = append(evidence, "reason:"+strings.TrimSpace(decision.Reason))
	}
	if len(evidence) == 0 && decision.EventID != "" {
		evidence = append(evidence, "event:"+decision.EventID)
	}

	proposedIdentity := ""
	if result.Identity != nil {
		proposedIdentity = result.Identity.ID
	} else if result.Presence != nil {
		proposedIdentity = result.Presence.ResidentID
	}

	return &contract.ValidationRequest{
		ID:               validationID(decision),
		DecisionID:       decision.ID,
		EventID:          decision.EventID,
		SituationID:      situationID,
		Reason:           validationReason(result),
		Evidence:         evidence,
		ProposedIdentity: proposedIdentity,
		NodeID:           decision.NodeID,
		ClipID:           decision.ClipID,
		Status:           contract.ValidationStatusPending,
		Enabled:          true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func validationReason(result *engine.Result) string {
	if result == nil {
		return ""
	}
	if result.DangerAssessment != nil {
		return firstNonEmpty(result.DangerAssessment.ValidationReason, result.DangerAssessment.Explanation)
	}
	if result.Decision != nil {
		return firstNonEmpty(result.Decision.ValidationReason, result.Decision.Reason)
	}
	return ""
}

func validationID(decision *contract.Decision) string {
	if decision == nil {
		return idgen.New("validation")
	}
	switch {
	case strings.TrimSpace(decision.ID) != "":
		return "validation-" + strings.TrimSpace(decision.ID)
	case strings.TrimSpace(decision.EventID) != "":
		return "validation-" + strings.TrimSpace(decision.EventID)
	default:
		return idgen.New("validation")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
