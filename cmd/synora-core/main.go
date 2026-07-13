package main

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"synora/internal/automation"
	"synora/internal/bus"
	"synora/internal/cge"
	"synora/internal/device"
	"synora/internal/engine"
	eventpkg "synora/internal/event"
	"synora/internal/idgen"
	"synora/internal/ingest"
	corerpc "synora/internal/rpc"
	snapshotpkg "synora/internal/snapshot"
	"synora/internal/state"
	"synora/internal/stateapply"
	"synora/internal/topology"
	"synora/pkg/contract"
)

const (
	defaultCGECriticalChainsPath     = "/etc/synora/cge_critical_chains.yaml"
	developmentCGECriticalChainsPath = "configs/cge_critical_chains.yaml"
	defaultCGEProfilePath            = "/etc/synora/cge_profile.yaml"
	defaultCGEFeedbackPath           = "/var/lib/synora/cge/feedback.json"
)

type coreMetrics struct {
	mu sync.RWMutex
	// eventProcessed counts events accepted by Core after ingest validation and rate/dedupe.
	eventProcessed     int64
	lastEngineLatency  time.Duration
	totalEngineLatency time.Duration
	sourceLastSeen     map[string]time.Time
}

type coreApp struct {
	mu sync.RWMutex

	snapshotPending atomic.Bool

	bus        coreBus
	engine     *engine.Engine
	automation *automation.Engine
	device     *device.Registry

	topology  *topology.Topology
	residents map[string]*topology.Resident

	state      *state.Store
	eventStore *eventpkg.Store
	chains     *eventpkg.ChainManager
	rate       *eventpkg.RateController
	metrics    *coreMetrics

	highPriority      chan *contract.Event
	normalQueue       chan *contract.Event
	ingest            *ingest.Queue
	rpc               *corerpc.Server
	snapshotBuilder   *snapshotpkg.Builder
	snapshotPublisher snapshotpkg.Publisher
	actionDispatcher  automation.Dispatcher
}

type coreBus interface {
	Send(contract.Message) error
	SubscribeChannel(string) <-chan contract.Message
}

func main() {
	log.Println("starting synora core")

	busPath := getenv("SYNORA_BUS", "/run/synora/bus.sock")
	topologyPath := getenv("SYNORA_TOPOLOGY", "/etc/synora/topology.yaml")
	residentsPath := getenv("SYNORA_RESIDENTS", "/etc/synora/residents.yaml")
	devicePath := getenv("SYNORA_DEVICE", "/etc/synora/devices.yaml")
	automationPath := getenv("SYNORA_AUTOMATION", "/etc/synora/automations.yaml")
	securityPath := getenv("SYNORA_SECURITY", "/etc/synora/security.yaml")
	cgeProfilePath := getenv("SYNORA_CGE_PROFILE", defaultCGEProfilePath)
	cgeFeedbackPath := getenv("SYNORA_CGE_FEEDBACK", defaultCGEFeedbackPath)
	statePath := getenv("SYNORA_STATE_PATH", "")
	if statePath == "" {
		statePath = state.DefaultStatePath()
	}

	busClient, err := bus.NewClient(busPath, "core")
	if err != nil {
		log.Fatal(err)
	}

	topologyInstance := &topology.Topology{Nodes: map[string]*topology.Node{}}
	if err := topology.Load(topologyPath, topologyInstance); err != nil {
		log.Println("topology load warning:", err)
	}

	residents, err := topology.LoadResidents(residentsPath)
	if err != nil {
		log.Println("residents load warning:", err)
		residents = map[string]*topology.Resident{}
	}

	deviceRegistry := device.NewRegistry()
	if configs, err := device.Load(devicePath); err != nil {
		log.Println("device load warning:", err)
	} else {
		deviceRegistry.Register(configs)
	}

	automationEngine := automation.NewEngine(automationPath)
	if err := automationEngine.Load(); err != nil {
		log.Println("automation load warning:", err)
	}

	engineInstance := engine.NewEngine(topologyInstance, deviceRegistry, residents)
	profileStore := cge.NewProfileStore(cgeProfilePath)
	if profile, exists, err := profileStore.Load(); err != nil {
		log.Println("cge security profile load warning:", err)
	} else if exists {
		engineInstance.SetSecurityProfile(&profile)
	}
	if loadedPath, err := loadCGECriticalChains(engineInstance); err != nil {
		log.Println("cge critical chains load warning:", err)
	} else if loadedPath == "" {
		log.Println("cge critical chains load warning: no configuration file found")
	} else {
		log.Println("cge critical chains loaded:", loadedPath)
	}
	stateStore := state.NewStore()
	eventStore := eventpkg.NewStore(200)
	chainManager := eventpkg.NewChainManager(eventpkg.ChainConfigFromEnvironment(os.Getenv))
	chainManager.SetDeviceRegistry(deviceRegistry)
	if profile := engineInstance.SecurityProfile(); profile != nil {
		chainManager.SetSignificantInactivityTimeout(time.Duration(profile.SignificantInactivityTimeoutSeconds) * time.Second)
	}
	feedbackStore := cge.NewFeedbackStore(cgeFeedbackPath)
	if err := feedbackStore.Load(); err != nil {
		log.Println("cge feedback load warning:", err)
	}
	rateController := eventpkg.NewRateController(2*time.Second, 750*time.Millisecond)

	app := &coreApp{
		bus:          busClient,
		engine:       engineInstance,
		automation:   automationEngine,
		device:       deviceRegistry,
		topology:     topologyInstance,
		residents:    residents,
		state:        stateStore,
		eventStore:   eventStore,
		chains:       chainManager,
		rate:         rateController,
		metrics:      &coreMetrics{sourceLastSeen: map[string]time.Time{}},
		highPriority: make(chan *contract.Event, 128),
		normalQueue:  make(chan *contract.Event, 512),
	}
	app.snapshotBuilder = &snapshotpkg.Builder{
		Mu:         &app.mu,
		State:      app.state,
		Devices:    app.device,
		Topology:   app.topology,
		Residents:  app.residents,
		Automation: app.automation,
		Events:     app.eventStore,
		Chains:     app.chains,
		Metrics:    app.metrics,
		CGE:        app.engine,
	}
	app.snapshotPublisher = snapshotpkg.Publisher{
		Builder: app.snapshotBuilder,
		Bus:     app.bus,
	}
	app.actionDispatcher = automation.Dispatcher{
		Bus:    app.bus,
		Source: "core",
		Target: "actions",
	}
	app.ingest = &ingest.Queue{
		Parser: ingest.Parser{Devices: app.device},
		Rate:   app.rate,
		High:   app.highPriority,
		Normal: app.normalQueue,
	}
	app.rpc = corerpc.NewServer(corerpc.Config{
		Bus:            app.bus,
		State:          app.state,
		Events:         app.eventStore,
		Chains:         app.chains,
		Devices:        app.device,
		Automation:     app.automation,
		Snapshot:       app.snapshotBuilder,
		Metrics:        app.metrics,
		TopologyPath:   topologyPath,
		ResidentsPath:  residentsPath,
		DevicePath:     devicePath,
		AutomationPath: automationPath,
		SecurityPath:   securityPath,
		CGEProfile:     profileStore,
		CGEFeedback:    feedbackStore,
		PublishEvent:   app.publishEvent,
		UpdateTopology: app.setTopology,
		CGE:            app.engine,
		NotifyMutation: app.notifyConfigMutation,
		IngestEvent:    app.ingestRuntimeEvent,
	})

	app.seedState()
	app.state.SetPersistence(state.NewFilePersistence(statePath))
	summary, err := app.state.LoadPersisted()
	if err != nil {
		log.Println("state persistence load warning:", err)
	}
	chainManager.AttachState(stateStore)
	// Reconcile persisted runtime presence before publishing the first
	// snapshot. Expiration is evaluated against wall-clock time after a
	// restart, while Cleanup preserves each resident's last_seen value.
	if expired := app.state.Cleanup(time.Now().UTC(), state.DefaultExpirationConfig()); len(expired.Deleted) > 0 {
		for _, residentID := range expired.Deleted["presence"] {
			app.clearResidentPresence(residentID)
		}
	}
	for residentID := range app.residents {
		if presence, ok := app.state.PresenceState(residentID); ok && presence != nil && presence.State == engine.StatePresent {
			app.syncResidentPresence(presence)
		}
	}
	app.eventStore.Load(app.state.RecentEventsList())
	if err := app.rpc.RestoreLearnedBehaviorOverrides(); err != nil {
		log.Println("cge learned behavior overrides restore warning:", err)
	}
	log.Printf(
		"state restored events=%d clips=%d validations=%d action_results=%d danger=%d",
		summary.Events,
		summary.Clips,
		summary.Validations,
		summary.ActionResults,
		summary.Danger,
	)
	app.publishStateSnapshot()
	go app.processLoop()
	go app.manualRiskLoop()
	go app.cleanupLoop()
	go app.chainLoop()

	if err := app.runBusLoop(); err != nil {
		log.Fatal(err)
	}
}

func (a *coreApp) seedState() {
	now := time.Now().UTC()
	for _, device := range a.device.List() {
		if device == nil {
			continue
		}
		a.state.SetDeviceState(&state.DeviceState{
			ID:        device.ID,
			Type:      device.Type,
			Role:      device.Role,
			Room:      device.Room,
			NodeID:    device.NodeID,
			Online:    false,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if device.Type == "camera" {
			a.state.SetCameraState(&state.CameraState{
				ID:        device.ID,
				NodeID:    device.NodeID,
				Online:    false,
				CreatedAt: now,
				UpdatedAt: now,
			})
		}
	}
	for id := range a.residents {
		a.state.SetPresence(&state.PresenceState{
			ID:         id,
			ResidentID: id,
			State:      engine.StateAbsent,
			CreatedAt:  now,
			UpdatedAt:  now,
			ExpiresAt:  now,
		})
		a.state.DeletePresence(id)
	}
	current := a.state.SystemState()
	current.LastState = "idle"
	current.LastStateTime = now
	current.IntrusionActive = false
	current.EmergencyActive = false
	a.state.SetSystemState(current)
}

func (a *coreApp) runBusLoop() error {
	log.Println("core bus loop started")
	msgCh := a.bus.SubscribeChannel("core")
	for msg := range msgCh {
		log.Printf(
			"core: received message type=%s kind=%s source=%s",
			msg.Type,
			msg.Kind,
			msg.Source,
		)
		a.metrics.touchSource(msg.Source)
		switch msg.Kind {
		case contract.KindRPC, contract.KindCommand:
			a.rpc.Handle(msg)
		case contract.KindEvent:
			a.ingest.Ingest(msg)
		default:
			log.Println("core: unknown message kind", msg.Kind)
		}
	}
	return nil
}

func (a *coreApp) processLoop() {
	for {
		select {
		case event := <-a.highPriority:
			a.processEvent(event)
		default:
			select {
			case event := <-a.highPriority:
				a.processEvent(event)
			case event := <-a.normalQueue:
				a.processEvent(event)
			}
		}
	}
}

func (a *coreApp) processEvent(event *contract.Event) {

	if event == nil {
		return
	}

	stateapply.TouchDeviceState(a.state, a.device, event)
	a.recordRuntimeEvent(event)

	a.eventStore.Add(event)
	a.state.SetRecentEvents(a.eventStore.List())

	if event.Type == contract.EventActionResult {
		a.engine.ObserveActionResult(event)
		a.storeActionResult(event)
		a.metrics.record(event.Source, 0)
		a.triggerSnapshot()
		return
	}

	if event.Type == contract.EventSystemStateChanged || event.Type == contract.EventSystemPresence {
		log.Printf("core: stored lifecycle event=%s type=%s category=%s", event.ID, event.Type, contract.EventCategory(event.Type))
		a.metrics.record(event.Source, 0)
		a.triggerSnapshot()
		return
	}
	if event.Type == contract.EventSecurityModeChanged {
		a.applyAutomationContext(event)
	}

	started := time.Now()
	chainRole := eventpkg.ClassifyEventForChain(event)

	log.Printf(
		"core: engine analyze event=%s type=%s",
		event.ID,
		event.Type,
	)

	var result *engine.Result
	if event.Type == contract.EventSecurityModeChanged {
		// Mode changes provide automation context; they are not CGE danger input.
		result = nil
	} else if chainRole == eventpkg.ChainRoleContextual {
		// Keep the legacy engine.decision notification for existing consumers;
		// this path only observes graph continuity and does not run CGE danger,
		// validation, state-transition, or action-planning evaluation.
		result = a.engine.ObserveContext(event, a.state)
	} else if chainRole != eventpkg.ChainRoleIgnored {
		result = a.engine.Analyze(event, a.state)
	}
	if result != nil && result.Decision != nil && result.DangerAssessment == nil && !contract.IsUserValidationCandidate(event.Type) {
		result.Decision.ValidationRequired = false
		result.Decision.ValidationReason = ""
		event.ValidationRequired = false
		event.ValidationReason = ""
	}
	if event.SequenceKey == "" && result != nil && result.Decision != nil {
		event.SequenceKey = result.Decision.SequenceKey
	}

	if result != nil &&
		result.Decision != nil {

		log.Printf(
			"core: decision=%s priority=%d",
			result.Decision.Type,
			result.Decision.Priority,
		)
	}

	latency := time.Since(started)

	previousSystemState := a.state.SystemState()
	stateChanged := stateapply.Apply(a.state, result, stateapply.Callbacks{
		SyncPresence: a.syncResidentPresence,
	})
	if isManualRiskEvent(event) {
		stateChanged = a.applyManualRiskState(event, stateChanged, previousSystemState)
	} else if result != nil && result.DangerAssessment != nil && !eventIsSimulated(event) {
		current := a.state.SystemState()
		current.ManualRiskActive = false
		current.ManualRiskTest = false
		current.ManualRiskLevel = ""
		current.ManualRiskScore = 0
		current.ManualRiskExpiresAt = time.Time{}
		a.state.SetSystemState(current)
	}
	if presence := stateapply.ApplyVisionIdentity(a.state, event); presence != nil {
		a.syncResidentPresence(presence)
	}
	a.applyAutomationContext(event)

	if (result != nil && result.Decision != nil) || event.Type == contract.EventSecurityModeChanged {
		decision := (*contract.Decision)(nil)
		if result != nil {
			decision = result.Decision
		}
		if decision == nil {
			decision = a.automationContextDecision(event)
		}
		requests := a.automation.EvaluateRequests(event, decision)
		if result != nil && result.Decision != nil && len(requests) == 0 && result.DangerAssessment != nil && result.DangerAssessment.Level >= 3 {
			result.Decision.ActionDecision = "blocked"
			result.Decision.BlockedActions = appendUniqueString(result.Decision.BlockedActions, "no_matching_automation")
			if eventIsSimulated(event) {
				result.Decision.BlockedActions = appendUniqueString(result.Decision.BlockedActions, "simulated_input")
			}
			log.Printf("core: action blocked event_id=%s reason=%s", event.ID, strings.Join(result.Decision.BlockedActions, ","))
		} else if len(requests) > 0 {
			if result != nil && result.Decision != nil {
				result.Decision.ActionDecision = "requested"
			}
			current := a.state.SystemState()
			current.BlockingReasons = []string{}
			a.state.SetSystemState(current)
		}

		for _, request := range requests {

			if err := a.actionDispatcher.DispatchRequest(request); err != nil {
				if result != nil && result.Decision != nil {
					result.Decision.ActionDecision = "blocked"
					result.Decision.BlockedActions = appendUniqueString(result.Decision.BlockedActions, "action_service_unavailable")
				}
				log.Printf("core: action blocked event_id=%s reason=action_service_unavailable err=%v", event.ID, err)
			} else {
				current := a.state.SystemState()
				current.LastActionRequestAt = time.Now().UTC()
				a.state.SetSystemState(current)
			}
		}
		if result != nil && result.Decision != nil && len(result.Decision.BlockedActions) > 0 {
			current := a.state.SystemState()
			current.BlockingReasons = append([]string(nil), result.Decision.BlockedActions...)
			for _, reason := range result.Decision.BlockedActions {
				current.BlockedActionsRecent = append(current.BlockedActionsRecent, map[string]any{
					"reason": reason, "event_id": event.ID, "timestamp": time.Now().UTC(),
				})
			}
			if len(current.BlockedActionsRecent) > 20 {
				current.BlockedActionsRecent = current.BlockedActionsRecent[len(current.BlockedActionsRecent)-20:]
			}
			a.state.SetSystemState(current)
		}

		if result != nil && result.Decision != nil {
			a.publishEvent(
				"engine.decision",
				result.Decision,
				result.Decision.Priority,
			)
		}
	}

	if stateChanged {

		a.publishEvent(
			contract.EventSystemStateChanged,
			a.state.SystemState(),
			event.Priority,
		)
	}

	a.metrics.record(
		event.Source,
		latency,
	)

	if a.chains != nil {
		a.publishChainUpdates(a.chains.Process(event, chainEvaluation(event, result)))
	}

	a.triggerSnapshot()
}

func (a *coreApp) recordRuntimeEvent(event *contract.Event) {
	if a == nil || a.state == nil || event == nil {
		return
	}
	current := a.state.SystemState()
	if !eventIsSimulated(event) && eventpkg.ClassifyEventForChain(event) != eventpkg.ChainRoleIgnored {
		current.LastRealEventAt = event.Timestamp.UTC()
	}
	if contract.NormalizeEventType(event.Type) == contract.EventActionResult {
		current.LastActionAt = event.Timestamp.UTC()
	}
	switch contract.NormalizeEventType(event.Type) {
	case contract.EventActionServiceStarted:
		if current.RuntimeComponents == nil {
			current.RuntimeComponents = map[string]string{}
		}
		current.RuntimeComponents["actions"] = "ok"
		if current.RuntimeComponentInfo == nil {
			current.RuntimeComponentInfo = map[string]string{}
		}
		current.RuntimeComponentInfo["actions"] = "bus client registered"
	case contract.EventDiscoveryVisionWorkerUnavailable, contract.EventDiscoveryNetworkDegraded, contract.EventRuntimeComponentFlapping, contract.EventRuntimeModelMissing, contract.EventDiscoveryVisionIngressStatus:
		current.Degraded = true
		current.DegradationReasons = appendUniqueString(current.DegradationReasons, event.Type)
		if current.RuntimeComponents == nil {
			current.RuntimeComponents = map[string]string{}
		}
		current.RuntimeComponents["discovery"] = "degraded"
		component := "discovery"
		status := "degraded"
		if event.Type == contract.EventDiscoveryVisionWorkerUnavailable {
			component, status = "vision_worker", "unavailable"
		} else if event.Type == contract.EventDiscoveryVisionIngressStatus {
			component, status = "vision_ingress", payloadString(event.Payload, "status")
		} else if event.Type == contract.EventRuntimeModelMissing {
			component, status = "models", "degraded"
		}
		if status == "" {
			status = "degraded"
		}
		current.RuntimeComponents[component] = status
		if current.RuntimeComponentInfo == nil {
			current.RuntimeComponentInfo = map[string]string{}
		}
		if reason := payloadString(event.Payload, "reason"); reason != "" {
			current.RuntimeComponentInfo[component] = reason
		}
	case contract.EventDiscoveryRuntimeStatus:
		applyDiscoveryRuntimeStatus(&current, event.Payload)
	case contract.EventDiscoveryWorkerStarted, contract.EventDiscoveryCameraOnline:
		current.DegradationReasons = removeString(current.DegradationReasons, contract.EventDiscoveryVisionWorkerUnavailable)
		if current.RuntimeComponents == nil {
			current.RuntimeComponents = map[string]string{}
		}
		current.RuntimeComponents["vision_worker"] = "ok"
		current.Degraded = len(current.DegradationReasons) > 0
	}
	a.state.SetSystemState(current)
}

func (a *coreApp) applyAutomationContext(event *contract.Event) {
	if a == nil || a.state == nil || event == nil {
		return
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	system := a.state.SystemState()
	event.Payload["security"] = map[string]any{"mode": system.Security.Mode, "armed": system.Security.Armed}
	event.Payload["occupancy"] = map[string]any{"expected": system.Security.ExpectedOccupancy}
	event.Payload["manual_risk"] = map[string]any{"active": system.ManualRiskActive}
	event.Payload["current_state"] = system.LastState
	event.Payload["danger_source"] = system.DangerSource
}

func (a *coreApp) automationContextDecision(event *contract.Event) *contract.Decision {
	system := a.state.SystemState()
	return &contract.Decision{
		ID: event.ID, EventID: event.ID, Type: event.Type, Source: "system",
		Timestamp: event.Timestamp, Priority: event.Priority, State: system.LastState,
		DangerLevel: system.DangerLevel, DangerScore: system.DangerScore,
		DangerSource: system.DangerSource, EffectiveScore: system.DangerScore,
	}
}

func applyDiscoveryRuntimeStatus(current *state.SystemState, payload map[string]any) {
	if current == nil {
		return
	}
	if current.RuntimeComponents == nil {
		current.RuntimeComponents = map[string]string{}
	}
	if current.RuntimeComponentInfo == nil {
		current.RuntimeComponentInfo = map[string]string{}
	}
	if current.RuntimeModels == nil {
		current.RuntimeModels = map[string]string{}
	}
	for _, component := range []string{"discovery", "network"} {
		if value := payloadString(payload, component); value != "" {
			current.RuntimeComponents[component] = value
		}
	}
	for _, component := range []string{"vision_worker", "vision_ingress"} {
		if nested, ok := payload[component].(map[string]any); ok {
			if value := payloadString(nested, "status"); value != "" {
				current.RuntimeComponents[component] = value
			}
			if reason := payloadString(nested, "reason"); reason != "" {
				current.RuntimeComponentInfo[component] = reason
			}
		}
	}
	if models, ok := payload["models"].(map[string]any); ok {
		for name, value := range models {
			if item, ok := value.(map[string]any); ok {
				if status := payloadString(item, "status"); status != "" {
					current.RuntimeModels[name] = status
				}
			}
		}
	}
	if status := payloadString(payload, "status"); status != "" {
		current.RuntimeComponents["discovery"] = status
	}
	current.Degraded = current.RuntimeComponents["discovery"] == "degraded" || current.RuntimeComponents["vision_worker"] != "ok" || current.RuntimeComponents["vision_ingress"] != "ok"
}

func (a *coreApp) ingestRuntimeEvent(event *contract.Event) {
	if a == nil || a.ingest == nil || event == nil {
		return
	}
	body, err := json.Marshal(event.Payload)
	if err != nil {
		return
	}
	a.ingest.Ingest(contract.Message{
		Type:      event.Type,
		Kind:      contract.KindEvent,
		Source:    event.Source,
		Timestamp: event.Timestamp,
		Priority:  event.Priority,
		Payload:   body,
	})
}

func isManualRiskEvent(event *contract.Event) bool {
	return event != nil && contract.NormalizeEventType(event.Type) == contract.EventManualRisk
}

func (a *coreApp) applyManualRiskState(event *contract.Event, changed bool, previous state.SystemState) bool {
	if a == nil || a.state == nil || event == nil {
		return changed
	}
	test := eventIsSimulated(event)
	current := a.state.SystemState()
	if test {
		current = previous
	}
	level := strings.ToLower(strings.TrimSpace(payloadString(event.Payload, "danger_level")))
	duration := int(payloadNumber(event.Payload, "duration_seconds"))
	if duration <= 0 {
		duration = 60
	}
	current.ManualRiskActive = true
	current.ManualRiskTest = test
	current.ManualRiskLevel = level
	current.ManualRiskExpiresAt = event.Timestamp.UTC().Add(time.Duration(duration) * time.Second)
	current.ManualRiskScore = manualRiskScore(level)
	if test {
		a.state.SetSystemState(current)
		return changed && previous.LastState != current.LastState
	}
	current.DangerSource = "manual"
	current.DangerKnown = true
	switch level {
	case "critical":
		current.DangerLevel, current.DangerScore = string(contract.DangerCritical), 0.95
		current.LastState = "intrusion"
		current.IntrusionActive = true
		current.IntrusionTime = event.Timestamp.UTC()
	case "high":
		current.DangerLevel, current.DangerScore = string(contract.DangerHigh), 0.75
		current.LastState = "suspicious"
		current.IntrusionActive = false
	case "medium", "low":
		if level == "medium" {
			current.DangerLevel, current.DangerScore = string(contract.DangerMedium), 0.50
		} else {
			current.DangerLevel, current.DangerScore = string(contract.DangerLow), 0.25
		}
		current.LastState = "activity"
		current.IntrusionActive = false
	}
	current.LastStateTime = event.Timestamp.UTC()
	a.state.SetSystemState(current)
	return changed || current.LastState != "idle"
}

func manualRiskScore(level string) float64 {
	switch level {
	case "critical":
		return 0.95
	case "high":
		return 0.75
	case "medium":
		return 0.50
	case "low":
		return 0.25
	default:
		return 0
	}
}

func (a *coreApp) manualRiskLoop() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for now := range ticker.C {
		changed := a.expireManualRisk(now.UTC())
		if changed {
			current := a.state.SystemState()
			a.publishEvent(contract.EventSystemStateChanged, current, contract.PriorityNormal)
			if a.chains != nil {
				a.publishChainUpdates(a.chains.CloseManualRiskChains(now.UTC()))
			}
			a.triggerSnapshot()
		}
		if a.expireSecurityMode(now.UTC()) {
			a.triggerSnapshot()
		}
	}
}

func (a *coreApp) expireSecurityMode(now time.Time) bool {
	if a == nil || a.state == nil {
		return false
	}
	current := a.state.SystemState()
	if current.Security.Mode == contract.SecurityModeHome || current.Security.ExpiresAt == nil || now.Before(*current.Security.ExpiresAt) {
		return false
	}
	old := current.Security
	next := contract.DefaultSecurityModeState(now)
	next.Reason = "security mode duration expired"
	current.Security = next
	current.Armed = false
	a.state.SetSystemState(current)
	if err := a.state.SaveNow(); err != nil {
		log.Printf("core: security mode persistence warning: %v", err)
	}
	payload := map[string]any{"old_mode": old.Mode, "new_mode": next.Mode, "armed": next.Armed, "expected_occupancy": next.ExpectedOccupancy, "source": "system", "reason": next.Reason, "security": next}
	a.publishEvent(contract.EventSecurityModeChanged, payload, contract.PriorityNormal)
	a.ingestRuntimeEvent(&contract.Event{ID: idgen.New("evt"), Type: contract.EventSecurityModeChanged, Source: "system", Timestamp: now, Payload: payload, Priority: contract.PriorityNormal})
	return true
}

func (a *coreApp) expireManualRisk(now time.Time) bool {
	if a == nil || a.state == nil {
		return false
	}
	current := a.state.SystemState()
	if !current.ManualRiskActive || current.ManualRiskExpiresAt.IsZero() || now.Before(current.ManualRiskExpiresAt) {
		return false
	}
	current.ManualRiskActive = false
	wasTest := current.ManualRiskTest
	current.ManualRiskTest = false
	current.ManualRiskLevel = ""
	current.ManualRiskScore = 0
	current.ManualRiskExpiresAt = time.Time{}
	if wasTest {
		a.state.SetSystemState(current)
		return true
	}
	if chain := a.highestActiveRealChainExcludingManual(); chain != nil {
		current.LastState = chain.CurrentState
		if current.LastState == "" {
			current.LastState = "suspicious"
		}
		current.LastStateTime = now.UTC()
		current.DangerLevel = string(chain.DangerLevel)
		current.DangerScore = chain.DangerScore
		if current.DangerScore <= 0 {
			current.DangerScore = chain.MaxDangerScore
		}
		current.DangerKnown = true
		current.DangerSource = "real"
		current.IntrusionActive = current.LastState == "intrusion"
		a.state.SetSystemState(current)
		return true
	}
	current.PreviousState = current.LastState
	current.LastState = "idle"
	current.LastStateTime = now.UTC()
	current.DangerLevel = string(contract.DangerNone)
	current.DangerScore = 0
	current.DangerKnown = true
	current.DangerSource = "none"
	current.IntrusionActive = false
	current.IntrusionTime = time.Time{}
	a.state.SetSystemState(current)
	return true
}

func (a *coreApp) highestActiveRealChainExcludingManual() *contract.EventChain {
	if a == nil || a.chains == nil {
		return nil
	}
	simulated := false
	var highest *contract.EventChain
	for _, chain := range a.chains.List(eventpkg.ChainFilter{Status: string(contract.EventChainOpen), Simulated: &simulated}) {
		if chain == nil || manualRiskChain(chain) {
			continue
		}
		if highest == nil || dangerRankForState(chain.DangerLevel) > dangerRankForState(highest.DangerLevel) {
			highest = chain
		}
	}
	return highest
}

func manualRiskChain(chain *contract.EventChain) bool {
	if chain == nil {
		return false
	}
	for _, eventType := range chain.SignificantEventTypes {
		if contract.NormalizeEventType(eventType) == contract.EventManualRisk {
			return true
		}
	}
	for _, recent := range chain.RecentEvents {
		if contract.NormalizeEventType(recent.Type) == contract.EventManualRisk {
			return true
		}
	}
	return false
}

func dangerRankForState(level contract.DangerLevel) int {
	switch level {
	case contract.DangerCritical:
		return 5
	case contract.DangerHigh:
		return 4
	case contract.DangerMedium:
		return 3
	case contract.DangerLow:
		return 2
	default:
		return 0
	}
}

func payloadString(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return value
	}
	return ""
}

func payloadNumber(payload map[string]any, key string) float64 {
	switch value := payload[key].(type) {
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case float64:
		return value
	default:
		return 0
	}
}

func eventIsSimulated(event *contract.Event) bool {
	if event == nil || event.Payload == nil {
		return false
	}
	if metadata, ok := event.Payload["metadata"].(map[string]any); ok {
		if value, ok := metadata["simulated"].(bool); ok && value {
			return true
		}
	}
	value, _ := event.Payload["simulated"].(bool)
	return value
}

func appendUniqueString(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func removeString(values []string, value string) []string {
	out := values[:0]
	for _, current := range values {
		if current != value {
			out = append(out, current)
		}
	}
	return out
}

func (a *coreApp) chainLoop() {
	if a.chains == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for now := range ticker.C {
		a.publishChainUpdates(a.chains.CloseInactive(now.UTC()))
	}
}

func (a *coreApp) publishChainUpdates(updates []eventpkg.ChainUpdate) {
	for _, update := range updates {
		if update.Chain == nil {
			continue
		}
		a.publishEvent(update.Type, chainEventPayload(update.Chain), contract.PriorityNormal)
	}
	if len(updates) > 0 {
		if a.state != nil {
			if err := a.state.SaveNow(); err != nil {
				log.Printf("core: event chain state persistence warning: %v", err)
			}
		}
		a.triggerSnapshot()
	}
}

func chainEventPayload(chain *contract.EventChain) map[string]any {
	return map[string]any{
		"chain_id":                  chain.ID,
		"status":                    chain.Status,
		"current_state":             chain.CurrentState,
		"danger_level":              chain.DangerLevel,
		"danger_score":              chain.DangerScore,
		"summary":                   chain.Summary,
		"events_count":              chain.EventsCount,
		"significant_events_count":  chain.SignificantEventsCount,
		"contextual_events_count":   chain.ContextualEventsCount,
		"motion_count":              chain.MotionCount,
		"updated_at":                chain.UpdatedAt,
		"last_significant_event_at": chain.LastSignificantEventAt,
	}
}

func chainEvaluation(event *contract.Event, result *engine.Result) *contract.ChainEvaluation {
	if event == nil || result == nil || result.Decision == nil {
		return nil
	}
	evaluation := &contract.ChainEvaluation{
		EventID:       event.ID,
		Timestamp:     event.Timestamp,
		State:         result.Decision.State,
		DangerScore:   result.Decision.EffectiveScore,
		EngineVersion: "synora-cge-v1",
		Reasons:       []string{result.Decision.Reason},
	}
	if result.DangerAssessment != nil {
		evaluation.DangerLevel = result.DangerAssessment.RiskLevel
		evaluation.DangerScore = result.DangerAssessment.Score
		evaluation.Reasons = append([]string(nil), result.DangerAssessment.Reasons...)
		for _, action := range result.DangerAssessment.RecommendedSystemActions {
			if action.Type != "" {
				evaluation.RecommendedActions = append(evaluation.RecommendedActions, action.Type)
			}
		}
	}
	evaluation.ActionDecision = result.Decision.ActionDecision
	evaluation.BlockedActions = append([]string(nil), result.Decision.BlockedActions...)
	if len(evaluation.Reasons) == 1 && evaluation.Reasons[0] == "" {
		evaluation.Reasons = nil
	}
	return evaluation
}

func (a *coreApp) storeActionResult(event *contract.Event) {
	if event == nil || event.Payload == nil {
		return
	}
	body, err := json.Marshal(event.Payload)
	if err != nil {
		log.Println("core: action result marshal error", err)
		return
	}
	var result contract.ActionResult
	if err := json.Unmarshal(body, &result); err != nil {
		log.Println("core: action result decode error", err)
		return
	}
	if result.Timestamp.IsZero() {
		result.Timestamp = event.Timestamp
	}
	if result.Source == "" {
		result.Source = event.Source
	}
	a.state.SetActionResult(&result)
}

func (a *coreApp) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	cfg := state.DefaultExpirationConfig()
	for range ticker.C {
		result := a.state.Cleanup(time.Now().UTC(), cfg)
		if len(result.Deleted) == 0 {
			continue
		}
		for _, residentID := range result.Deleted["presence"] {
			a.clearResidentPresence(residentID)
		}
		a.publishEvent("system.lifecycle.cleaned", result, contract.PriorityLow)
		a.triggerSnapshot()
	}
}

func (a *coreApp) publishEvent(eventType string, payload any, priority int) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Println("core: publish marshal error", err)
		return
	}
	msg := contract.Message{
		ID:        idgen.New("msg"),
		Type:      contract.NormalizeEventType(eventType),
		Kind:      contract.KindEvent,
		Source:    "core",
		Timestamp: time.Now().UTC(),
		Priority:  priority,
		Payload:   body,
	}
	if err := a.bus.Send(msg); err != nil {
		log.Println("core: publish bus error", err)
	}
}

func (a *coreApp) publishStateSnapshot() {
	a.snapshotPublisher.PublishStateSnapshot()
}

func (a *coreApp) notifyConfigMutation(kind string, id string) {
	if strings.TrimSpace(kind) == "" {
		kind = "config.updated"
	}
	a.publishEvent(kind, map[string]any{"id": id}, contract.PriorityNormal)
	a.triggerSnapshot()
}

func (a *coreApp) triggerSnapshot() {

	if a.snapshotPending.Swap(true) {
		return
	}

	go func() {

		time.Sleep(250 * time.Millisecond)

		a.publishStateSnapshot()

		a.snapshotPending.Store(false)

	}()
}

func (a *coreApp) syncResidentPresence(presence *state.PresenceState) {
	if presence == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	resident, ok := a.residents[presence.ResidentID]
	if !ok || resident == nil {
		return
	}
	resident.Presence = &topology.Presence{
		ResidentID: presence.ResidentID,
		Location:   presence.Location,
		LastSeen:   presence.LastSeen.UnixMilli(),
		Confidence: presence.Confidence,
	}
}

func (a *coreApp) clearResidentPresence(residentID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	resident, ok := a.residents[residentID]
	if !ok || resident == nil {
		return
	}
	if presence, exists := a.state.PresenceState(residentID); exists && presence != nil {
		// Keep the config-side convenience projection aligned with the runtime
		// record. In particular, an absent resident still has a last_seen.
		resident.Presence = &topology.Presence{
			ResidentID: presence.ResidentID,
			Location:   presence.Location,
			LastSeen:   presence.LastSeen.UnixMilli(),
			Confidence: presence.Confidence,
		}
		return
	}
	resident.Presence = nil
}

func (a *coreApp) setTopology(value *topology.Topology) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.topology = value
	a.engine.Topology = value
}

func (m *coreMetrics) touchSource(source string) {
	if strings.TrimSpace(source) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sourceLastSeen[source] = time.Now().UTC()
}

func (m *coreMetrics) record(source string, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventProcessed++
	m.lastEngineLatency = latency
	m.totalEngineLatency += latency
	if strings.TrimSpace(source) != "" {
		m.sourceLastSeen[source] = time.Now().UTC()
	}
}

func (m *coreMetrics) SourceStatus(source string, staleAfter time.Duration) map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	lastSeen, ok := m.sourceLastSeen[source]
	if !ok {
		return map[string]any{"status": "unknown"}
	}
	status := "ok"
	if time.Since(lastSeen) > staleAfter {
		status = "stale"
	}
	return map[string]any{"status": status, "last_seen": lastSeen}
}

func (m *coreMetrics) Snapshot(store *state.Store) map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	average := 0.0
	if m.eventProcessed > 0 {
		average = float64(m.totalEngineLatency.Milliseconds()) / float64(m.eventProcessed)
	}
	return map[string]any{
		"event_processed":    m.eventProcessed,
		"engine_latency_ms":  m.lastEngineLatency.Milliseconds(),
		"engine_avg_latency": average,
		"state_store_size":   store.Size(),
		"active_tracks":      store.ActiveTracks(),
		"active_clusters":    store.ActiveClusters(),
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func loadCGECriticalChains(engineInstance *engine.Engine) (string, error) {
	return engineInstance.LoadCriticalSeedsFirstExisting(
		cgeCriticalChainsPath(),
		developmentCGECriticalChainsPath,
	)
}

func cgeCriticalChainsPath() string {
	return getenv("SYNORA_CGE_CRITICAL_CHAINS", defaultCGECriticalChainsPath)
}
