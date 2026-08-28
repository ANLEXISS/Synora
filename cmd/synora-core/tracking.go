package main

import (
	"strings"
	"time"

	"synora/internal/state"
	"synora/internal/stateapply"
	"synora/pkg/contract"
)

func (a *coreApp) acceptedResidentIdentity(event *contract.Event) bool {
	if a == nil || event == nil || contract.NormalizeEventType(event.Type) != contract.EventVisionIdentity {
		return false
	}
	residentID := strings.TrimSpace(event.ResidentID)
	if residentID == "" && legacyIdentityPayload(event) {
		candidate := strings.TrimSpace(event.Identity)
		if resident, exists := a.residents[candidate]; exists && resident != nil {
			residentID = candidate
		}
	}
	resident, ok := a.residents[residentID]
	if residentID == "" || !ok || resident == nil || event.Confidence < stateapply.MinResidentIdentityConfidence {
		return false
	}
	current, exists := a.state.PresenceState(residentID)
	if !exists || current == nil || current.State != "present" {
		return event.Confidence >= 0.60
	}
	return event.Confidence >= 0.40
}

func (a *coreApp) canonicalizeVisionIdentifiers(event *contract.Event) {
	if a == nil || event == nil || contract.NormalizeEventType(event.Type) != contract.EventVisionIdentity {
		return
	}
	residentID := strings.TrimSpace(event.ResidentID)
	if residentID == "" && legacyIdentityPayload(event) {
		candidate := strings.TrimSpace(event.Identity)
		if resident, exists := a.residents[candidate]; exists && resident != nil {
			residentID = candidate
			event.ResidentID = candidate
		}
	}
	if residentID == "" {
		return
	}
	event.Identity = residentID
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	event.Payload["resident_id"] = residentID
	event.Payload["identity"] = residentID
}

func legacyIdentityPayload(event *contract.Event) bool {
	if event == nil || len(event.Payload) == 0 {
		return true
	}
	_, hasResidentID := event.Payload["resident_id"]
	_, hasTransportID := event.Payload["event_id"]
	return !hasResidentID && !hasTransportID
}

func (a *coreApp) updateVisionTracking(event *contract.Event) {
	if a == nil || a.state == nil || event == nil {
		return
	}
	now := event.Timestamp.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	switch contract.NormalizeEventType(event.Type) {
	case contract.EventVisionIdentity:
		if !a.acceptedResidentIdentity(event) {
			return
		}
		residentID := strings.TrimSpace(event.ResidentID)
		entityID := state.EntityTrackID(event.TrackID, event.SequenceKey, event.ActivationID, event.DeviceID, event.NodeID)
		if entityID != "" {
			if entity, exists := a.state.EntityTrack(entityID); exists && entity != nil && entity.ResidentID != "" && entity.ResidentID != residentID {
				// A detector track cannot be rebound to a second resident within
				// the same camera/activation boundary without a new track.
				return
			}
		}
		if current, ok := a.state.ResidentTrack(residentID); ok && current != nil && !a.validTrackMovement(current.LastNodeID, event.NodeID) {
			// Keep the last valid location. The observation is retained in the
			// event journal but cannot teleport a resident across the topology.
			if presence, exists := a.state.PresenceState(residentID); exists && presence != nil {
				presence.Location = current.LastNodeID
				a.state.SetPresence(presence)
			}
			return
		}
		presence := stateapply.ApplyVisionIdentityForResidents(a.state, event, a.residents)
		if presence == nil {
			return
		}
		track := &state.ResidentTrack{
			ResidentID: residentID, LastNodeID: strings.TrimSpace(event.NodeID),
			LastDeviceID: strings.TrimSpace(event.DeviceID), LastTrackID: strings.TrimSpace(event.TrackID),
			LastEventID: strings.TrimSpace(event.ID), ActivationID: strings.TrimSpace(event.ActivationID),
			SequenceKey: strings.TrimSpace(event.SequenceKey), Epoch: strings.TrimSpace(event.Epoch),
			Confidence: event.Confidence, LastSeen: now, UpdatedAt: now,
			ExpiresAt: now.Add(state.DefaultExpirationConfig().Tracks),
		}
		a.state.SetResidentTrack(track)
		if entityID != "" {
			entity, ok := a.state.EntityTrack(entityID)
			if !ok || entity == nil {
				entity = &state.EntityTrack{ID: entityID, CreatedAt: now}
			}
			entity.TrackID = strings.TrimSpace(event.TrackID)
			entity.NodeID, entity.DeviceID = strings.TrimSpace(event.NodeID), strings.TrimSpace(event.DeviceID)
			entity.ActivationID, entity.SequenceKey, entity.Epoch = strings.TrimSpace(event.ActivationID), strings.TrimSpace(event.SequenceKey), strings.TrimSpace(event.Epoch)
			entity.ResidentID, entity.Kind = residentID, "resident"
			entity.Confidence, entity.UpdatedAt, entity.LastSeen = event.Confidence, now, now
			entity.ExpiresAt = now.Add(state.DefaultExpirationConfig().Tracks)
			a.state.SetEntityTrack(entity)
		}
	case contract.EventVisionUnknown, contract.EventVisionUncertain:
		a.updateAnonymousTrack(event, now)
	}
}

func (a *coreApp) updateAnonymousTrack(event *contract.Event, now time.Time) {
	id := state.EntityTrackID(event.TrackID, event.SequenceKey, event.ActivationID, event.DeviceID, event.NodeID)
	if id == "" {
		return
	}
	current, exists := a.state.EntityTrack(id)
	if !exists || current == nil {
		current = &state.EntityTrack{ID: id, TrackID: strings.TrimSpace(event.TrackID), CreatedAt: now}
	} else if current.ResidentID != "" {
		// Once a track is bound to a resident, an anonymous/uncertain update
		// cannot silently turn it into a second person.
		return
	}
	kind := "unknown"
	if contract.NormalizeEventType(event.Type) == contract.EventVisionUncertain {
		kind = "uncertain"
	}
	candidate := strings.TrimSpace(event.ResidentID)
	current.NodeID, current.DeviceID = strings.TrimSpace(event.NodeID), strings.TrimSpace(event.DeviceID)
	current.ActivationID, current.SequenceKey, current.Epoch = strings.TrimSpace(event.ActivationID), strings.TrimSpace(event.SequenceKey), strings.TrimSpace(event.Epoch)
	current.Kind, current.CandidateResidentID = kind, candidate
	current.Confidence, current.UpdatedAt, current.LastSeen = event.Confidence, now, now
	current.ExpiresAt = now.Add(state.DefaultExpirationConfig().Tracks)
	a.state.SetEntityTrack(current)

	if contract.NormalizeEventType(event.Type) == contract.EventVisionUnknown {
		clusterID := "unknown_presence:" + id
		cluster, ok := a.state.Cluster(clusterID)
		if !ok || cluster == nil {
			cluster = &state.Cluster{ID: clusterID, NodeID: event.NodeID, Type: "unknown_presence", CreatedAt: now}
		}
		cluster.Score = event.Confidence
		cluster.UpdatedAt, cluster.ExpiresAt = now, now.Add(state.DefaultExpirationConfig().Clusters)
		cluster.EventIDs = appendUniqueID(cluster.EventIDs, event.ID)
		a.state.SetCluster(cluster)
	}
}

func (a *coreApp) entityIDForEvent(event *contract.Event) string {
	if event == nil || a == nil || a.state == nil {
		return ""
	}
	id := state.EntityTrackID(event.TrackID, event.SequenceKey, event.ActivationID, event.DeviceID, event.NodeID)
	if entity, ok := a.state.EntityTrack(id); ok && entity != nil {
		return entity.ID
	}
	return id
}

func appendUniqueID(values []string, id string) []string {
	if strings.TrimSpace(id) == "" {
		return values
	}
	for _, value := range values {
		if value == id {
			return values
		}
	}
	values = append(values, id)
	if len(values) > 100 {
		values = values[len(values)-100:]
	}
	return values
}

func (a *coreApp) validTrackMovement(previous, next string) bool {
	previous, next = strings.TrimSpace(previous), strings.TrimSpace(next)
	if previous == "" || next == "" || previous == next || a == nil || a.topology == nil || len(a.topology.Nodes) == 0 {
		return true
	}
	left, leftOK := a.topology.Nodes[previous]
	right, rightOK := a.topology.Nodes[next]
	if !leftOK || !rightOK || left == nil || right == nil {
		return false
	}
	graphHasLinks := false
	for _, node := range a.topology.Nodes {
		if node != nil && (node.Parent != nil || len(node.Neighbors) > 0 || len(node.Connect) > 0) {
			graphHasLinks = true
			break
		}
	}
	if !graphHasLinks {
		// A one-node/fixture topology has no movement information and cannot
		// disprove a camera transition.
		return true
	}
	if left.Parent == right || right.Parent == left {
		return true
	}
	for _, neighbor := range left.Neighbors {
		if neighbor != nil && neighbor.ID == right.ID {
			return true
		}
	}
	for _, neighborID := range left.Connect {
		if strings.TrimSpace(neighborID) == right.ID {
			return true
		}
	}
	for _, neighborID := range right.Connect {
		if strings.TrimSpace(neighborID) == left.ID {
			return true
		}
	}
	return false
}

func (a *coreApp) finalizeVisionActivation(event *contract.Event) {
	if a == nil || event == nil || a.state == nil {
		return
	}
	activationID := strings.TrimSpace(event.ActivationID)
	if activationID == "" {
		activationID = strings.TrimSpace(payloadString(event.Payload, "activation_id"))
		if activationID == "" {
			activationID = strings.TrimSpace(payloadString(event.Payload, "activation"))
		}
		if activationID == "" {
			activationID = strings.TrimSpace(payloadString(event.Payload, "session_id"))
		}
	}
	if activationID != "" {
		a.state.DeleteEntityTracksByActivation(activationID)
	}
}
