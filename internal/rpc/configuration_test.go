package rpc

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"synora/internal/automation"
	"synora/internal/device"
	"synora/internal/event"
	"synora/internal/snapshot"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

func TestDeviceConfigurationCRUDIsDurableAndPublic(t *testing.T) {
	store := state.NewStore()
	registry := device.NewRegistry()
	notifications := []string{}
	builder := &snapshot.Builder{
		Mu: &sync.RWMutex{}, State: store, Devices: registry,
		Topology:  &topology.Topology{Nodes: map[string]*topology.Node{}},
		Residents: map[string]*topology.Resident{}, Events: event.NewStore(10),
	}
	server := NewServer(Config{
		State: store, Devices: registry, Snapshot: builder,
		DevicePath: filepath.Join(t.TempDir(), "devices.yaml"),
		NotifyMutation: func(kind string, id string) { notifications = append(notifications, kind+":"+id) },
	})

	createdAny, err := server.Handler("device.create")(rpcMessage(`{
		"id":"front_camera","name":"Front camera","type":"camera",
		"capabilities":["vision","motion_detection"],
		"config":{"stream":"main","api_token":"must-not-leak"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	created := createdAny.(contract.DeviceView)
	if created.NodeID != device.UnlocatedNodeID || !created.Enabled {
		t.Fatalf("unexpected created device: %#v", created)
	}
	if _, leaked := created.Config["api_token"]; leaked {
		t.Fatalf("device secret leaked: %#v", created.Config)
	}
	if len(notifications) != 1 || notifications[0] != "device.updated:front_camera" {
		t.Fatalf("device mutation did not signal snapshot update: %#v", notifications)
	}
	if _, err := server.Handler("device.create")(rpcMessage(`{"id":"front_camera","type":"camera"}`)); contract.APIErrorCode(err) != contract.ErrorDuplicateID {
		t.Fatalf("duplicate error=%v code=%s", err, contract.APIErrorCode(err))
	}

	updatedAny, err := server.Handler("device.update")(mutationMessage("front_camera", `{"node_id":"house.entry","enabled":false}`))
	if err != nil {
		t.Fatal(err)
	}
	updated := updatedAny.(contract.DeviceView)
	if updated.NodeID != "house.entry" || updated.Enabled {
		t.Fatalf("unexpected updated device: %#v", updated)
	}
	deletedAny, err := server.Handler("device.delete")(deleteMessage("front_camera"))
	if err != nil {
		t.Fatal(err)
	}
	deleted := deletedAny.(contract.DeviceView)
	if deleted.Enabled || deleted.DeletedAt == nil {
		t.Fatalf("device was not soft deleted: %#v", deleted)
	}
	if _, ok := store.DeviceState("front_camera"); !ok {
		t.Fatal("soft delete removed runtime device history")
	}
	public := contract.PublicSnapshotFromCoreState(builder.CoreState())
	if len(public.Devices) != 1 || public.Devices[0]["id"] != "front_camera" || public.Devices[0]["enabled"] != false {
		t.Fatalf("device missing from public snapshot: %#v", public.Devices)
	}
}

func TestAutomationConfigurationCRUDAndSafety(t *testing.T) {
	path := filepath.Join(t.TempDir(), "automations.yaml")
	engine := automation.NewEngine(path)
	server := NewServer(Config{Automation: engine, AutomationPath: path})

	createdAny, err := server.Handler("automation.create")(rpcMessage(`{
		"id":"entry_light","name":"Entry light","trigger":{"event_type":"vision.motion"},
		"actions":[{"type":"light.turn_on","target":"entry_light"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	created := createdAny.(map[string]any)
	if created["id"] != "entry_light" || created["enabled"] != true {
		t.Fatalf("unexpected automation: %#v", created)
	}

	updatedAny, err := server.Handler("automation.update")(mutationMessage("entry_light", `{
		"enabled":false,"actions":[{"type":"light.turn_off","target":"entry_light"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if updatedAny.(map[string]any)["enabled"] != false {
		t.Fatalf("automation patch did not disable: %#v", updatedAny)
	}

	_, err = server.Handler("automation.create")(rpcMessage(`{
		"id":"unsafe_unlock","enabled":true,"trigger":{"event_type":"sensor.door.open"},
		"actions":[{"type":"door.unlock","target":"front_door"}]
	}`))
	if contract.APIErrorCode(err) != contract.ErrorUnsafeAutomation {
		t.Fatalf("unsafe unlock error=%v code=%s", err, contract.APIErrorCode(err))
	}
	_, err = server.Handler("automation.create")(rpcMessage(`{
		"id":"emergency","trigger":{"event_type":"vision.fall"},
		"actions":[{"type":"emergency_call"}]
	}`))
	if contract.APIErrorCode(err) != contract.ErrorForbiddenAction {
		t.Fatalf("emergency action error=%v code=%s", err, contract.APIErrorCode(err))
	}

	deletedAny, err := server.Handler("automation.delete")(deleteMessage("entry_light"))
	if err != nil {
		t.Fatal(err)
	}
	deleted := deletedAny.(map[string]any)
	if deleted["enabled"] != false || deleted["deleted_at"] == nil {
		t.Fatalf("automation was not soft deleted: %#v", deleted)
	}
}

func TestTopologyDeleteKeepsDevicesAndDisablesDependentAutomations(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, "topology.yaml")
	devicePath := filepath.Join(dir, "devices.yaml")
	automationPath := filepath.Join(dir, "automations.yaml")
	initial, err := topology.FromConfig(contract.TopologyConfig{
		Version: topology.TopologyConfigVersion,
		Nodes: []contract.TopologyNode{
			{ID: "house", Name: "House", Type: "house"},
			{ID: "house.entry", Name: "Entry", Type: "room", Parent: "house"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := topology.Save(topologyPath, initial); err != nil {
		t.Fatal(err)
	}
	registry := device.NewRegistry(devicePath)
	registry.Register([]device.DeviceConfig{{ID: "camera", Type: "camera", NodeID: "house.entry"}})
	if err := device.Save(devicePath, registry.Ordered()); err != nil {
		t.Fatal(err)
	}
	automations := automation.NewEngine(automationPath)
	if _, err := automations.Create(automation.Rule{
		ID: "entry_rule", Enabled: true,
		Trigger: contract.AutomationTrigger{EventType: "vision.motion", NodeID: "house.entry"},
		Actions: []automation.AutomationAction{{Type: "light.turn_on", Target: "entry", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	store := state.NewStore()
	residents := map[string]*topology.Resident{"owner": {ID: "owner", Name: "Owner", Role: "owner", Enabled: true}}
	builder := &snapshot.Builder{
		Mu: &sync.RWMutex{}, State: store, Devices: registry, Topology: initial,
		Residents: residents, Automation: automations, Events: event.NewStore(10),
	}
	server := NewServer(Config{
		State: store, Devices: registry, Automation: automations, Snapshot: builder,
		TopologyPath: topologyPath, DevicePath: devicePath, AutomationPath: automationPath,
	})
	server.syncDeviceConfigState(mustDevice(t, registry, "camera"))

	deletedAny, err := server.Handler("topology.delete")(rpcMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	deleted := deletedAny.(map[string]any)
	if nodes, ok := deleted["nodes"].([]any); !ok || len(nodes) != 0 {
		t.Fatalf("topology was not reset: %#v", deleted)
	}
	kept, ok := registry.Get("camera")
	if !ok || kept.NodeID != device.UnlocatedNodeID {
		t.Fatalf("device lost or not moved: %#v ok=%v", kept, ok)
	}
	rule, ok := automations.Get("entry_rule")
	if !ok || rule.Enabled || rule.ConfigError != "topology_node_missing" {
		t.Fatalf("dependent automation not invalidated: %#v", rule)
	}
	if len(builder.Residents) != 1 {
		t.Fatalf("topology delete removed residents: %#v", builder.Residents)
	}
	public := contract.PublicSnapshotFromCoreState(builder.CoreState())
	if len(public.Devices) != 1 || public.Devices[0]["node_id"] != device.UnlocatedNodeID {
		t.Fatalf("unlocated device missing from snapshot: %#v", public.Devices)
	}

	before := builder.Topology.ConfigView()
	_, err = server.Handler("topology.replace")(rpcMessage(`{
		"nodes":[{"id":"room","type":"room"},{"id":"room","type":"room"}],"links":[]
	}`))
	if contract.APIErrorCode(err) != contract.ErrorDuplicateID {
		t.Fatalf("invalid topology error=%v code=%s", err, contract.APIErrorCode(err))
	}
	after := builder.Topology.ConfigView()
	if len(before.Nodes) != len(after.Nodes) {
		t.Fatalf("invalid topology changed live topology: before=%#v after=%#v", before, after)
	}
}

func mustDevice(t *testing.T, registry *device.Registry, id string) *device.Device {
	t.Helper()
	value, ok := registry.Get(id)
	if !ok {
		t.Fatalf("device %q missing", id)
	}
	return value
}

func TestResidentConfigurationCRUDPreservesRuntimeIdentity(t *testing.T) {
	store := state.NewStore()
	store.SetIdentity(&state.IdentityState{ID: "guest_1", State: "present"})
	store.SetPresence(&state.PresenceState{ID: "guest_1", ResidentID: "guest_1", State: "present"})
	builder := &snapshot.Builder{
		Mu: &sync.RWMutex{}, State: store, Devices: device.NewRegistry(),
		Topology:  &topology.Topology{Nodes: map[string]*topology.Node{}},
		Residents: map[string]*topology.Resident{}, Events: event.NewStore(10),
	}
	server := NewServer(Config{
		State: store, Snapshot: builder, Devices: builder.Devices,
		ResidentsPath: filepath.Join(t.TempDir(), "residents.yaml"),
	})

	createdAny, err := server.Handler("residents.create")(rpcMessage(`{
		"id":"guest_1","name":"Guest","role":"guest",
		"contact":{"email":"guest@example.test"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	created := createdAny.(contract.ResidentView)
	if !created.Enabled || created.Trusted {
		t.Fatalf("unexpected guest defaults: %#v", created)
	}

	updatedAny, err := server.Handler("resident.update")(mutationMessage("guest_1", `{
		"name":"Guest updated","contact":{"phone":"+33123456789"},"enabled":false
	}`))
	if err != nil {
		t.Fatal(err)
	}
	updated := updatedAny.(contract.ResidentView)
	if updated.Name != "Guest updated" || updated.Contact.Phone == "" || updated.Enabled {
		t.Fatalf("unexpected resident update: %#v", updated)
	}

	deletedAny, err := server.Handler("resident.delete")(deleteMessage("guest_1"))
	if err != nil {
		t.Fatal(err)
	}
	deleted := deletedAny.(contract.ResidentView)
	if deleted.DeletedAt == nil || deleted.Enabled {
		t.Fatalf("resident was not soft deleted: %#v", deleted)
	}
	if _, ok := store.Identity("guest_1"); !ok {
		t.Fatal("resident soft delete removed identity history")
	}
	if _, ok := store.PresenceState("guest_1"); !ok {
		t.Fatal("resident soft delete removed presence history")
	}
	public := contract.PublicSnapshotFromCoreState(builder.CoreState())
	if len(public.Residents) != 1 || public.Residents[0]["id"] != "guest_1" {
		t.Fatalf("resident missing from public snapshot: %#v", public.Residents)
	}
	if _, exposed := public.Residents[0]["contact"]; exposed {
		t.Fatalf("public snapshot exposed resident contact: %#v", public.Residents[0])
	}
}

func rpcMessage(payload string) contract.Message {
	return contract.Message{Type: "test", Payload: json.RawMessage(payload), Timestamp: time.Now().UTC()}
}

func mutationMessage(id string, patch string) contract.Message {
	payload, _ := json.Marshal(MutationPayload{ID: id, Data: json.RawMessage(patch)})
	return contract.Message{Type: "test", Payload: payload}
}

func deleteMessage(id string) contract.Message {
	payload, _ := json.Marshal(DeletePayload{ID: id})
	return contract.Message{Type: "test", Payload: payload}
}
