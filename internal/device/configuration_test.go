package device

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synora/pkg/contract"
)

func TestDeviceConfigurationPreservesPrivateAndUnknownYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.yaml")
	initial := `devices:
  - id: cam_01
    type: camera
    room: house.entry
    secret: keep-me
    capabilities: [vision, infrared]
    network: {ip: 10.0.0.2}
    vendor_extension: {credential: hidden, mode: local}
    config: {stream: main, api_token: hidden, endpoints: [{api_token: nested-hidden}]}
`
	if err := os.WriteFile(path, []byte(initial), 0o640); err != nil {
		t.Fatal(err)
	}
	items, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Enabled || !items[0].Trusted || items[0].NodeID != "house.entry" {
		t.Fatalf("legacy defaults were not applied: %#v", items)
	}
	registry := NewRegistry(path)
	registry.Register(items)
	node := "house.hall"
	updated, err := registry.Patch("cam_01", contract.DevicePatch{NodeID: &node})
	if err != nil {
		t.Fatal(err)
	}
	if updated.NodeID != node {
		t.Fatalf("updated=%#v", updated)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"secret: keep-me", "vendor_extension:", "credential: hidden", "network:"} {
		if !strings.Contains(string(written), expected) {
			t.Fatalf("durable update lost %q:\n%s", expected, written)
		}
	}
	publicJSON, err := json.Marshal(updated.PublicView())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"keep-me", "credential", "api_token", "nested-hidden", "10.0.0.2"} {
		if strings.Contains(string(publicJSON), forbidden) {
			t.Fatalf("public view leaked %q: %s", forbidden, publicJSON)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "devices.*.yaml"))
	if len(backups) != 1 {
		t.Fatalf("backups=%v", backups)
	}
}

func TestDeviceRegistryCRUDAndTopologyDetachAreTransactional(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.yaml")
	if err := os.WriteFile(path, []byte("devices: []\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(path)
	created, err := registry.Create(Device{ID: "light_1", Type: contract.DeviceTypeLight, NodeID: "house.entry"})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Enabled || created.NodeID != "house.entry" {
		t.Fatalf("created=%#v", created)
	}
	if _, err := registry.Create(Device{ID: "light_1", Type: contract.DeviceTypeLight}); contract.APIErrorCode(err) != contract.ErrorDuplicateID {
		t.Fatalf("duplicate err=%v code=%s", err, contract.APIErrorCode(err))
	}
	items, err := registry.MoveMissingNodesToUnlocated(map[string]bool{"house.other": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].NodeID != UnlocatedNodeID {
		t.Fatalf("detached=%#v", items)
	}
	deleted, err := registry.SoftDelete("light_1")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Enabled || deleted.DeletedAt == nil {
		t.Fatalf("deleted=%#v", deleted)
	}
}

func TestDeviceRegistryWriteFailureRollsBackMemory(t *testing.T) {
	registry := NewRegistry(t.TempDir()) // a directory cannot be replaced by a config file
	_, err := registry.Create(Device{ID: "sensor_1", Type: contract.DeviceTypeSensor})
	if err == nil {
		t.Fatal("expected persistence failure")
	}
	if _, exists := registry.Get("sensor_1"); exists {
		t.Fatal("failed create changed live registry")
	}
}

func TestDeviceValidationRejectsInvalidTypeAndCapabilities(t *testing.T) {
	if err := Validate(Device{ID: "bad", Type: "spaceship"}); contract.APIErrorCode(err) != contract.ErrorValidationFailed {
		t.Fatalf("type error=%v", err)
	}
	if err := Validate(Device{ID: "cam", Type: contract.DeviceTypeCamera, Capabilities: []string{"vision", "vision"}}); contract.APIErrorCode(err) != contract.ErrorValidationFailed {
		t.Fatalf("capability error=%v", err)
	}
}
