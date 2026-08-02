package state

import (
	"testing"
	"time"
)

func TestContextSnapshotIsDefensiveAndBounded(t *testing.T) {
	store := NewStore()
	at := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store.SetDeviceState(&DeviceState{ID: "device-a", Type: "sensor", LastSeen: at, UpdatedAt: at, Online: true})
	store.SetCameraState(&CameraState{ID: "camera-a", LastSeen: at, UpdatedAt: at, Online: true})
	store.SetPresence(&PresenceState{ID: "resident-a", ResidentID: "resident-a", State: "present", LastSeen: at, UpdatedAt: at})
	snapshot := store.ContextSnapshot()
	if len(snapshot.Devices) != 1 || len(snapshot.Cameras) != 1 || len(snapshot.Presence) != 1 {
		t.Fatalf("bounded context snapshot=%+v", snapshot)
	}
	snapshot.Devices[0].Online = false
	snapshot.Cameras[0].Online = false
	snapshot.Presence[0].State = "absent"
	current := store.ContextSnapshot()
	if !current.Devices[0].Online || !current.Cameras[0].Online || current.Presence[0].State != "present" {
		t.Fatal("context snapshot exposed mutable Store state")
	}
}
