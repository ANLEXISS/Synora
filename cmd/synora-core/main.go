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

	started := time.Now()
	chainRole := eventpkg.ClassifyEventForChain(event)

	log.Printf(
		"core: engine analyze event=%s type=%s",
		event.ID,
		event.Type,
	)

	var result *engine.Result
	if chainRole == eventpkg.ChainRoleContextual {
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

	stateChanged := stateapply.Apply(a.state, result, stateapply.Callbacks{
		SyncPresence: a.syncResidentPresence,
	})
	if presence := stateapply.ApplyVisionIdentity(a.state, event); presence != nil {
		a.syncResidentPresence(presence)
	}

	if result != nil &&
		result.Decision != nil {

		for _, request := range a.automation.EvaluateRequests(
			event,
			result.Decision,
		) {

			if err := a.actionDispatcher.DispatchRequest(request); err != nil {
				log.Println("core: action dispatch error", err)
			}
		}

		a.publishEvent(
			"engine.decision",
			result.Decision,
			result.Decision.Priority,
		)
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
