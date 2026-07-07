package main

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"synora/internal/automation"
	"synora/internal/device"
	"synora/internal/engine"
	"synora/internal/event"
	"synora/internal/ingest"
	snapshotpkg "synora/internal/snapshot"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

type memoryCoreBus struct {
	mu       sync.Mutex
	messages []contract.Message
	incoming chan contract.Message
}

func newMemoryCoreBus() *memoryCoreBus {
	return &memoryCoreBus{incoming: make(chan contract.Message, 16)}
}

func (b *memoryCoreBus) Send(msg contract.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, msg)
	return nil
}

func (b *memoryCoreBus) SubscribeChannel(string) <-chan contract.Message {
	return b.incoming
}

func (b *memoryCoreBus) messagesOfType(messageType string) []contract.Message {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]contract.Message, 0)
	for _, msg := range b.messages {
		if msg.Type == messageType {
			out = append(out, msg)
		}
	}
	return out
}

func TestCriticalFlowIdentityUpdatesStateSnapshotAndDispatchesAction(t *testing.T) {
	app, bus := newTestCoreApp(t)

	if err := app.automation.Add(automation.Rule{
		ID:        "identity-action",
		EventType: contract.EventVisionIdentity,
		Actions: []contract.Action{{
			Device:  "light-entry",
			Command: "on",
		}},
	}); err != nil {
		t.Fatalf("add automation: %v", err)
	}

	now := time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC)
	app.processEvent(&contract.Event{
		ID:         "evt-identity",
		Type:       contract.EventVisionIdentity,
		Source:     "vision-worker",
		Timestamp:  now,
		DeviceID:   "cam_01",
		Identity:   "alexis",
		Confidence: 0.96,
		Payload:    map[string]any{"identity": "alexis", "confidence": 0.96},
	})

	identity, ok := app.state.Identity("alexis")
	if !ok || identity == nil {
		t.Fatalf("expected alexis identity in state store")
	}
	if identity.State != "present" || identity.LastSeen.IsZero() {
		t.Fatalf("unexpected identity state: %#v", identity)
	}

	presence, ok := app.state.PresenceState("alexis")
	if !ok || presence == nil {
		t.Fatalf("expected alexis presence in state store")
	}
	if presence.State != "present" || !presence.LastSeen.Equal(now) {
		t.Fatalf("unexpected presence state: %#v", presence)
	}

	public := contract.PublicSnapshotFromCoreState(app.snapshotBuilder.CoreState())
	resident := findByID(public.Residents, "alexis")
	if resident == nil {
		t.Fatalf("alexis missing from public snapshot residents: %#v", public.Residents)
	}
	if resident["state"] != "present" || resident["last_seen"] == nil {
		t.Fatalf("public snapshot resident not present: %#v", resident)
	}

	actionRequests := bus.messagesOfType(contract.EventActionRequest)
	if len(actionRequests) != 1 {
		t.Fatalf("expected one action.request, got %d messages=%#v", len(actionRequests), bus.messages)
	}
	var request contract.ActionRequest
	if err := json.Unmarshal(actionRequests[0].Payload, &request); err != nil {
		t.Fatalf("unmarshal action request: %v", err)
	}
	if request.Action.Device != "light-entry" || request.Action.Command != "on" {
		t.Fatalf("unexpected action request: %#v", request)
	}
}

func TestCriticalFlowUnknownProducesDecisionAndSnapshotEvent(t *testing.T) {
	app, bus := newTestCoreApp(t)

	now := time.Date(2026, 7, 6, 10, 1, 0, 0, time.UTC)
	app.processEvent(&contract.Event{
		ID:         "evt-unknown",
		Type:       contract.EventVisionUnknown,
		Source:     "vision-worker",
		Timestamp:  now,
		DeviceID:   "cam_01",
		Confidence: 0.72,
		Payload:    map[string]any{"confidence": 0.72},
	})

	decisions := bus.messagesOfType("engine.decision")
	if len(decisions) != 1 {
		t.Fatalf("expected one engine decision, got %d messages=%#v", len(decisions), bus.messages)
	}
	var decision contract.Decision
	if err := json.Unmarshal(decisions[0].Payload, &decision); err != nil {
		t.Fatalf("unmarshal decision: %v", err)
	}
	if decision.EventID != "evt-unknown" || decision.NodeID != "entry" {
		t.Fatalf("unexpected decision: %#v", decision)
	}

	deviceState, ok := app.state.DeviceState("cam_01")
	if !ok || deviceState == nil || !deviceState.Online {
		t.Fatalf("expected cam_01 online after unknown event, got %#v", deviceState)
	}

	public := contract.PublicSnapshotFromCoreState(app.snapshotBuilder.CoreState())
	eventView := findByID(public.Events, "evt-unknown")
	if eventView == nil {
		t.Fatalf("unknown event missing from public snapshot events: %#v", public.Events)
	}
	if eventView["type"] != contract.EventVisionUnknown {
		t.Fatalf("unexpected snapshot event: %#v", eventView)
	}
}

func TestCriticalFlowMotionUpdatesDeviceAndSnapshotEvent(t *testing.T) {
	app, bus := newTestCoreApp(t)

	now := time.Date(2026, 7, 6, 10, 1, 30, 0, time.UTC)
	app.processEvent(&contract.Event{
		ID:        "evt-motion",
		Type:      contract.EventVisionMotion,
		Source:    "vision-worker",
		Timestamp: now,
		DeviceID:  "cam_01",
		Payload:   map[string]any{"motion": true},
	})

	if len(bus.messagesOfType("engine.decision")) != 1 {
		t.Fatalf("expected one engine decision, got messages=%#v", bus.messages)
	}

	deviceState, ok := app.state.DeviceState("cam_01")
	if !ok || deviceState == nil || !deviceState.Online || !deviceState.LastSeen.Equal(now) {
		t.Fatalf("unexpected cam_01 state after motion: %#v", deviceState)
	}

	public := contract.PublicSnapshotFromCoreState(app.snapshotBuilder.CoreState())
	eventView := findByID(public.Events, "evt-motion")
	if eventView == nil || eventView["type"] != contract.EventVisionMotion {
		t.Fatalf("motion event missing from public snapshot: %#v", public.Events)
	}
}

func TestCriticalFlowDeviceOfflineUpdatesState(t *testing.T) {
	app, _ := newTestCoreApp(t)

	now := time.Date(2026, 7, 6, 10, 2, 0, 0, time.UTC)
	app.processEvent(&contract.Event{
		ID:        "evt-offline",
		Type:      contract.EventDeviceOffline,
		Source:    "discovery",
		Timestamp: now,
		DeviceID:  "cam_01",
		Payload:   map[string]any{},
	})

	deviceState, ok := app.state.DeviceState("cam_01")
	if !ok || deviceState == nil {
		t.Fatalf("expected cam_01 device state")
	}
	if deviceState.Online {
		t.Fatalf("expected cam_01 offline, got %#v", deviceState)
	}

	public := contract.PublicSnapshotFromCoreState(app.snapshotBuilder.CoreState())
	deviceView := findByID(public.Devices, "cam_01")
	if deviceView == nil || deviceView["online"] != false || deviceView["last_seen"] == nil {
		t.Fatalf("unexpected public device view: %#v", deviceView)
	}
}

func newTestCoreApp(t *testing.T) (*coreApp, *memoryCoreBus) {
	t.Helper()

	bus := newMemoryCoreBus()
	devices := device.NewRegistry()
	devices.Register([]device.DeviceConfig{
		{ID: "cam_01", Type: "camera", Room: "entry", NodeID: "entry", Role: "access_control"},
		{ID: "cam_02", Type: "camera", Room: "salon", NodeID: "salon"},
		{ID: "cam_03", Type: "camera", Room: "child_room", NodeID: "child_room"},
		{ID: "cam_04", Type: "camera", Room: "guest_room", NodeID: "guest_room"},
		{ID: "cam_05", Type: "camera", Room: "remote_room", NodeID: "remote_room"},
	})

	topo := &topology.Topology{
		Nodes: map[string]*topology.Node{
			"entry":       {ID: "entry", Name: "Entry", Type: topology.NodeRoom},
			"salon":       {ID: "salon", Name: "Salon", Type: topology.NodeRoom},
			"child_room":  {ID: "child_room", Name: "Child Room", Type: topology.NodeRoom},
			"guest_room":  {ID: "guest_room", Name: "Guest Room", Type: topology.NodeRoom},
			"remote_room": {ID: "remote_room", Name: "Remote Room", Type: topology.NodeRoom},
		},
	}
	residents := map[string]*topology.Resident{
		"alexis": {ID: "alexis", Name: "Alexis", Role: "resident"},
		"carole": {ID: "carole", Name: "Carole", Role: "resident"},
	}
	store := state.NewStore()
	automationEngine := automation.NewEngine(t.TempDir() + "/automations.yaml")
	metrics := &coreMetrics{sourceLastSeen: map[string]time.Time{}}

	app := &coreApp{
		bus:        bus,
		engine:     engine.NewEngine(topo, devices, residents),
		automation: automationEngine,
		device:     devices,
		topology:   topo,
		residents:  residents,
		state:      store,
		eventStore: event.NewStore(20),
		metrics:    metrics,
		ingest: &ingest.Queue{
			Parser: ingest.Parser{Devices: devices},
		},
	}
	app.snapshotBuilder = &snapshotpkg.Builder{
		Mu:         &app.mu,
		State:      app.state,
		Devices:    app.device,
		Topology:   app.topology,
		Residents:  app.residents,
		Automation: app.automation,
		Events:     app.eventStore,
		Metrics:    app.metrics,
	}
	app.snapshotPublisher = snapshotpkg.Publisher{
		Builder: app.snapshotBuilder,
		Bus:     bus,
		Now:     func() time.Time { return time.Date(2026, 7, 6, 10, 3, 0, 0, time.UTC) },
	}
	app.actionDispatcher = automation.Dispatcher{
		Bus:    bus,
		Source: "core",
		Target: "actions",
		Now:    func() time.Time { return time.Date(2026, 7, 6, 10, 4, 0, 0, time.UTC) },
		NewID:  func() string { return "msg-action-test" },
	}
	app.seedState()

	return app, bus
}

func findByID(items []map[string]any, id string) map[string]any {
	for _, item := range items {
		if item["id"] == id {
			return item
		}
	}
	return nil
}
