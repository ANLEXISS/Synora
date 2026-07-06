package stateapply

import (
	"log"
	"time"

	"synora/internal/device"
	"synora/internal/engine"
	"synora/internal/state"
	"synora/pkg/contract"
)

type Callbacks struct {
	SyncPresence func(*state.PresenceState)
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
	if event.Type == contract.EventDeviceOffline {
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
