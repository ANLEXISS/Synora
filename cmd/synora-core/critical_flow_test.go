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
	"synora/internal/simulation"
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
		Enabled:   true,
		EventType: contract.EventVisionIdentity,
		Actions: []automation.AutomationAction{{
			ID:      "action-light-entry",
			Enabled: true,
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
	assessment := latestDanger(public)
	if assessment == nil || assessment["level"] != float64(3) || assessment["category"] != contract.DangerCategorySecurity {
		t.Fatalf("unknown should expose danger assessment: %#v", public.CGE)
	}
	if !dangerHasAction(assessment, contract.SystemActionCreateValidation) {
		t.Fatalf("unknown danger should recommend validation: %#v", assessment)
	}
}

func TestCriticalFlowProcessesSimulatedLabEventFromBusMessage(t *testing.T) {
	app, _ := newTestCoreApp(t)
	run := simulation.BuildRun("Unknown at entrance", "unknown_at_entrance", simulation.ModeDryRun, simulation.GeneratedBySynoraLab, nil)
	msg, err := simulation.BuildMessage(simulation.EventBuildOptions{
		Type:       contract.EventVisionUnknown,
		Source:     "lab",
		SourceType: contract.SourceSimulator,
		DeviceID:   "cam_01",
		CameraID:   "cam_01",
		NodeID:     "entry",
		Run:        &run,
		ScenarioID: "unknown_at_entrance",
		StepID:     "unknown_first",
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("build simulated message: %v", err)
	}

	parsed, accepted := app.ingest.Ingest(msg)
	if !accepted || parsed == nil {
		t.Fatalf("simulated message was not accepted: parsed=%#v accepted=%v", parsed, accepted)
	}
	var event *contract.Event
	select {
	case event = <-app.normalQueue:
	case <-time.After(time.Second):
		t.Fatal("simulated event was not queued")
	}
	app.processEvent(event)

	if event.Source != "lab" || event.DeviceID != "cam_01" {
		t.Fatalf("transport source and device payload should be distinct: %#v", event)
	}
	metadata, _ := event.Payload["metadata"].(map[string]any)
	if metadata["simulated"] != true || metadata["test_run_id"] != run.ID || metadata["scenario_step_id"] != "unknown_first" {
		t.Fatalf("simulation metadata missing from event payload: %#v", event.Payload)
	}

	public := contract.PublicSnapshotFromCoreState(app.snapshotBuilder.CoreState())
	eventView := findByID(public.Events, event.ID)
	if eventView == nil {
		t.Fatalf("simulated event missing from public snapshot: %#v", public.Events)
	}
	payload, _ := eventView["payload"].(map[string]any)
	publicMetadata, _ := payload["metadata"].(map[string]any)
	if publicMetadata["simulated"] != true || publicMetadata["test_run_id"] != run.ID {
		t.Fatalf("simulation metadata missing from public snapshot: %#v", eventView)
	}
	if metric, _ := public.Metrics["event_processed"].(int64); metric == 0 {
		t.Fatalf("metrics.event_processed should increase: %#v", public.Metrics)
	}
}

func TestCriticalFlowUnknownAtEntranceKeepsAllSimulatedSteps(t *testing.T) {
	app, _ := newTestCoreApp(t)
	app.ingest.Rate = event.NewRateController(2*time.Second, 750*time.Millisecond)
	scenario, ok := simulation.ScenarioByID("unknown_at_entrance")
	if !ok {
		t.Fatal("unknown_at_entrance scenario missing")
	}
	run := simulation.BuildRun(scenario.Name, scenario.ID, simulation.ModeDryRun, simulation.GeneratedBySynoraLab, nil)
	messages, err := simulation.BuildEventsForScenario(run, scenario, simulation.EventBuildOptions{
		DeviceID: "cam_01",
		CameraID: "cam_01",
		NodeID:   "zoneA.L0.entree",
		DryRun:   true,
		Now:      time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("build scenario messages: %v", err)
	}

	for i, msg := range messages {
		msg.Timestamp = time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC).Add(time.Duration(i) * 500 * time.Millisecond)
		parsed, accepted := app.ingest.Ingest(msg)
		if !accepted || parsed == nil {
			t.Fatalf("step %d should be accepted by ingest/rate: parsed=%#v accepted=%v", i, parsed, accepted)
		}
		var queued *contract.Event
		select {
		case queued = <-app.normalQueue:
		case <-time.After(time.Second):
			t.Fatalf("step %d was not queued", i)
		}
		app.processEvent(queued)
	}

	public := contract.PublicSnapshotFromCoreState(app.snapshotBuilder.CoreState())
	simulated := simulatedEventsForRun(public, run.ID)
	if len(simulated) != 3 {
		t.Fatalf("unknown_at_entrance should expose 3 simulated events, got %d events=%#v", len(simulated), public.Events)
	}
	if metricInt(public.Metrics["event_processed"]) < 3 {
		t.Fatalf("event_processed should count accepted core events, metrics=%#v", public.Metrics)
	}

	unknownGroupKeys := map[string]bool{}
	for _, item := range simulated {
		if item["type"] == contract.EventVisionUnknown {
			unknownGroupKeys[stringValue(item["group_key"])] = true
		}
	}
	if len(unknownGroupKeys) != 2 {
		t.Fatalf("simulated unknown steps should have distinct group keys: %#v", unknownGroupKeys)
	}
	sequences := cgeItems(public, "sequences")
	if sequence := findCGEItemBySignature(sequences, "vision.unknown > vision.motion > vision.unknown"); sequence == nil || metricInt(sequence["count"]) != 1 || metricInt(sequence["simulated_count"]) != 1 {
		t.Fatalf("CGE sequence should be visible in PublicSnapshot: %#v", public.CGE)
	}
}

func TestCriticalFlowProductionDuplicateIsDedupedBeforeSnapshot(t *testing.T) {
	app, _ := newTestCoreApp(t)
	app.ingest.Rate = event.NewRateController(2*time.Second, 750*time.Millisecond)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		body, err := json.Marshal(map[string]any{
			"device_id":  "cam_01",
			"camera_id":  "cam_01",
			"node_id":    "entry",
			"identity":   "unknown",
			"confidence": 0.72,
		})
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		_, accepted := app.ingest.Ingest(contract.Message{
			Type:      contract.EventVisionUnknown,
			Kind:      contract.KindEvent,
			Source:    "vision",
			Target:    "core",
			Timestamp: now.Add(time.Duration(i) * time.Second),
			Payload:   body,
		})
		if i == 1 {
			if accepted {
				t.Fatal("second production duplicate should be deduped")
			}
			continue
		}
		if !accepted {
			t.Fatal("first production event should be accepted")
		}
		queued := <-app.normalQueue
		app.processEvent(queued)
	}

	public := contract.PublicSnapshotFromCoreState(app.snapshotBuilder.CoreState())
	if len(public.Events) != 1 {
		t.Fatalf("production duplicate should expose one event, got %#v", public.Events)
	}
	if metricInt(public.Metrics["event_processed"]) != 1 {
		t.Fatalf("event_processed should count accepted events after dedup: %#v", public.Metrics)
	}
}

func TestCriticalFlowDiscoveryWorkerCrashedDoesNotCreateUserValidation(t *testing.T) {
	app, _ := newTestCoreApp(t)
	app.processEvent(&contract.Event{
		ID:        "evt-worker-crashed",
		Type:      contract.EventDiscoveryWorkerCrashed,
		Source:    "discovery",
		Timestamp: time.Date(2026, 7, 6, 10, 1, 0, 0, time.UTC),
		Payload:   map[string]any{"status": "backoff", "error": "missing cv2"},
	})

	public := contract.PublicSnapshotFromCoreState(app.snapshotBuilder.CoreState())
	if len(public.Validations) != 0 {
		t.Fatalf("worker crash should not create user validation: %#v", public.Validations)
	}
	eventView := findByID(public.Events, "evt-worker-crashed")
	if eventView == nil || eventView["validation_required"] == true {
		t.Fatalf("worker crash should remain visible without validation: %#v", eventView)
	}
	assessment := latestDanger(public)
	if assessment == nil || assessment["category"] != contract.DangerCategorySystemHealth || assessment["validation_required"] == true {
		t.Fatalf("worker crash should expose system health danger without validation: %#v", public.CGE)
	}
	if !dangerHasAction(assessment, contract.SystemActionSuppressNoise) {
		t.Fatalf("worker crash should recommend suppress_noise: %#v", assessment)
	}
}

func simulatedEventsForRun(public contract.PublicSnapshot, runID string) []map[string]any {
	out := []map[string]any{}
	for _, item := range public.Events {
		payload, _ := item["payload"].(map[string]any)
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata["test_run_id"] == runID {
			out = append(out, item)
		}
	}
	return out
}

func metricInt(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func stringValue(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func cgeItems(public contract.PublicSnapshot, key string) []map[string]any {
	raw, _ := public.CGE[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if mapped, ok := item.(map[string]any); ok {
			out = append(out, mapped)
		}
	}
	return out
}

func latestDanger(public contract.PublicSnapshot) map[string]any {
	items := cgeItems(public, "danger_assessments")
	if len(items) == 0 {
		return nil
	}
	return items[len(items)-1]
}

func dangerHasAction(assessment map[string]any, actionType string) bool {
	raw, _ := assessment["recommended_system_actions"].([]any)
	for _, item := range raw {
		mapped, _ := item.(map[string]any)
		if mapped["type"] == actionType {
			return true
		}
	}
	return false
}

func findCGEItemBySignature(items []map[string]any, signature string) map[string]any {
	for _, item := range items {
		if item["signature"] == signature {
			return item
		}
	}
	return nil
}

func TestCriticalFlowStoresActionResultInPublicSnapshot(t *testing.T) {
	app, bus := newTestCoreApp(t)
	startedAt := time.Date(2026, 7, 6, 10, 2, 0, 0, time.UTC)
	finishedAt := startedAt.Add(150 * time.Millisecond)

	app.processEvent(&contract.Event{
		ID:        "evt-action-result",
		Type:      contract.EventActionResult,
		Source:    "actions",
		Timestamp: finishedAt,
		Payload: map[string]any{
			"id":          "ares-1",
			"request_id":  "act-1",
			"action_id":   "act-1",
			"status":      "success",
			"started_at":  startedAt.Format(time.RFC3339Nano),
			"finished_at": finishedAt.Format(time.RFC3339Nano),
		},
	})

	public := contract.PublicSnapshotFromCoreState(app.snapshotBuilder.CoreState())
	if len(public.ActionResults) != 1 {
		t.Fatalf("expected public action result, got %#v", public.ActionResults)
	}
	if public.ActionResults[0]["request_id"] != "act-1" || public.ActionResults[0]["status"] != "success" {
		t.Fatalf("unexpected action result snapshot: %#v", public.ActionResults[0])
	}
	if len(bus.messagesOfType("engine.decision")) != 0 {
		t.Fatalf("action.result should not go through CGE decisions: %#v", bus.messages)
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
		bus:          bus,
		engine:       engine.NewEngine(topo, devices, residents),
		automation:   automationEngine,
		device:       devices,
		topology:     topo,
		residents:    residents,
		state:        store,
		eventStore:   event.NewStore(20),
		metrics:      metrics,
		highPriority: make(chan *contract.Event, 8),
		normalQueue:  make(chan *contract.Event, 8),
		ingest: &ingest.Queue{
			Parser: ingest.Parser{Devices: devices},
		},
	}
	app.ingest.High = app.highPriority
	app.ingest.Normal = app.normalQueue
	app.snapshotBuilder = &snapshotpkg.Builder{
		Mu:         &app.mu,
		State:      app.state,
		Devices:    app.device,
		Topology:   app.topology,
		Residents:  app.residents,
		Automation: app.automation,
		Events:     app.eventStore,
		Metrics:    app.metrics,
		CGE:        app.engine,
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
