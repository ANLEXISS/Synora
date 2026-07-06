package state

import "testing"

func TestDeviceCollectionAlias(t *testing.T) {
	store := NewStore()
	store.SetDeviceState(&DeviceState{ID: "cam_01", Type: "camera"})

	if _, ok := store.Snapshot("devices")["cam_01"]; !ok {
		t.Fatal("devices snapshot should include device state")
	}

	if _, ok := store.Snapshot("device")["cam_01"]; !ok {
		t.Fatal("device alias snapshot should include device state")
	}

	store.Delete("device", "cam_01")

	if _, ok := store.DeviceState("cam_01"); ok {
		t.Fatal("device alias delete should remove device state")
	}
}
