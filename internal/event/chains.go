package event

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"synora/internal/device"
	"synora/internal/idgen"
	"synora/internal/state"
	"synora/pkg/contract"
)

type ChainRole string

const (
	ChainRoleSignificant ChainRole = "significant"
	ChainRoleContextual  ChainRole = "contextual"
	ChainRoleIgnored     ChainRole = "ignored_for_chain"
)

type ChainConfig struct {
	SignificantInactivityTimeout time.Duration
	MotionExtendsChain           bool
	MotionCreatesChain           bool
	ContextualCoalesceWindow     time.Duration
	RecentEventsLimitPerChain    int
	EvaluationsLimitPerChain     int
	NearbyMatchWindow            time.Duration
}

func DefaultChainConfig() ChainConfig {
	return ChainConfig{
		SignificantInactivityTimeout: 30 * time.Second,
		MotionExtendsChain:           false,
		MotionCreatesChain:           false,
		ContextualCoalesceWindow:     5 * time.Second,
		RecentEventsLimitPerChain:    100,
		EvaluationsLimitPerChain:     50,
		NearbyMatchWindow:            30 * time.Second,
	}
}

// ChainConfigFromEnvironment provides an operational override for the
// cge.event_chains defaults without introducing a second Discovery config.
// Duration values use Go syntax, for example "30s" or "5s".
func ChainConfigFromEnvironment(getenv func(string) string) ChainConfig {
	config := DefaultChainConfig()
	if getenv == nil {
		return config
	}
	if value := strings.TrimSpace(getenv("SYNORA_CGE_SIGNIFICANT_INACTIVITY_TIMEOUT")); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			config.SignificantInactivityTimeout = parsed
		}
	}
	if value := strings.TrimSpace(getenv("SYNORA_CGE_CONTEXTUAL_COALESCE_WINDOW")); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			config.ContextualCoalesceWindow = parsed
		}
	}
	if value := strings.TrimSpace(getenv("SYNORA_CGE_RECENT_EVENTS_LIMIT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			config.RecentEventsLimitPerChain = parsed
		}
	}
	if value := strings.TrimSpace(getenv("SYNORA_CGE_EVALUATIONS_LIMIT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			config.EvaluationsLimitPerChain = parsed
		}
	}
	if value := strings.TrimSpace(getenv("SYNORA_CGE_MOTION_EXTENDS_CHAIN")); value != "" {
		config.MotionExtendsChain = value == "1" || strings.EqualFold(value, "true")
	}
	if value := strings.TrimSpace(getenv("SYNORA_CGE_MOTION_CREATES_CHAIN")); value != "" {
		config.MotionCreatesChain = value == "1" || strings.EqualFold(value, "true")
	}
	return config.normalize()
}

func (c ChainConfig) normalize() ChainConfig {
	defaults := DefaultChainConfig()
	if c.SignificantInactivityTimeout <= 0 {
		c.SignificantInactivityTimeout = defaults.SignificantInactivityTimeout
	}
	if c.ContextualCoalesceWindow <= 0 {
		c.ContextualCoalesceWindow = defaults.ContextualCoalesceWindow
	}
	if c.RecentEventsLimitPerChain <= 0 {
		c.RecentEventsLimitPerChain = defaults.RecentEventsLimitPerChain
	}
	if c.EvaluationsLimitPerChain <= 0 {
		c.EvaluationsLimitPerChain = defaults.EvaluationsLimitPerChain
	}
	if c.NearbyMatchWindow <= 0 {
		c.NearbyMatchWindow = defaults.NearbyMatchWindow
	}
	return c
}

// ClassifyEventForChain is the single policy boundary between raw events and
// chain aggregation. Unknown events are deliberately ignored by chain logic.
func ClassifyEventForChain(event *contract.Event) ChainRole {
	if event == nil {
		return ChainRoleIgnored
	}
	switch contract.NormalizeEventType(event.Type) {
	case contract.EventVisionIdentity, contract.EventVisionUnknown,
		contract.EventVisionUncertain, contract.EventVisionWeapon,
		contract.EventVisionFall, contract.EventVisionFight,
		contract.EventVisionTamper, contract.EventDeviceOffline,
		contract.EventDiscoveryCameraOffline, "camera.offline",
		contract.EventManualRisk,
		"door.opened", "window.opened", "sensor.door.open", "sensor.window.open",
		"presence.changed", "security.armed_changed", "automation.action_failed":
		return ChainRoleSignificant
	case contract.EventVisionMotion, "camera.heartbeat", "camera.clip_received",
		"camera.clip_started", "camera.clip_finished", "stream.started",
		"stream.frame", "sensor.noise", "vision.end":
		return ChainRoleContextual
	default:
		return ChainRoleIgnored
	}
}

type ChainFilter struct {
	Status    string
	Limit     int
	Since     time.Time
	Severity  string
	Simulated *bool
}

type ChainUpdate struct {
	Type  string
	Chain *contract.EventChain
}

type ChainManager struct {
	mu sync.RWMutex

	config ChainConfig
	now    func() time.Time

	chains                map[string]*contract.EventChain
	memories              map[string]*contract.CriticalChainMemory
	critical              map[string]bool
	lastContextualPublish map[string]time.Time
	store                 *state.Store
	devices               *device.Registry
}

// AttachState makes the chain manager a live projection of the Core
// StateStore. Existing persisted chains are loaded before new events arrive.
func (m *ChainManager) AttachState(store *state.Store) {
	if m == nil || store == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
	for _, chain := range store.EventChainsList() {
		if chain != nil {
			m.chains[chain.ID] = chain
			m.critical[chain.ID] = chain.Critical
		}
	}
	for _, memory := range store.CriticalChainMemoriesList() {
		if memory != nil {
			m.memories[memory.TemplateID] = memory
		}
	}
}

func (m *ChainManager) syncChainLocked(chain *contract.EventChain) {
	if m.store != nil {
		m.store.SetEventChain(chain)
	}
}

func (m *ChainManager) syncMemoryLocked(memory *contract.CriticalChainMemory) {
	if m.store != nil {
		m.store.SetCriticalChainMemory(memory)
	}
}

func NewChainManager(config ChainConfig) *ChainManager {
	return &ChainManager{
		config:                config.normalize(),
		now:                   func() time.Time { return time.Now().UTC() },
		chains:                make(map[string]*contract.EventChain),
		memories:              make(map[string]*contract.CriticalChainMemory),
		critical:              make(map[string]bool),
		lastContextualPublish: make(map[string]time.Time),
	}
}

func (m *ChainManager) SetNow(now func() time.Time) {
	if m == nil || now == nil {
		return
	}
	m.mu.Lock()
	m.now = now
	m.mu.Unlock()
}

func (m *ChainManager) SetDeviceRegistry(registry *device.Registry) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.devices = registry
	m.mu.Unlock()
}

func (m *ChainManager) Config() ChainConfig {
	if m == nil {
		return DefaultChainConfig()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *ChainManager) SetSignificantInactivityTimeout(timeout time.Duration) {
	if m == nil || timeout <= 0 {
		return
	}
	m.mu.Lock()
	m.config.SignificantInactivityTimeout = timeout
	m.mu.Unlock()
}

func (m *ChainManager) ApplyChainFeedback(chainID string, feedback contract.CgeChainFeedback) (*contract.CriticalChainMemory, error) {
	if m == nil {
		return nil, fmt.Errorf("event chain manager unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	chain, ok := m.chains[strings.TrimSpace(chainID)]
	if !ok || chain == nil {
		return nil, fmt.Errorf("event chain %q not found", chainID)
	}
	var memory *contract.CriticalChainMemory
	for _, candidate := range m.memories {
		if candidate == nil || candidate.RepresentativeChainID == chain.ID {
			memory = candidate
			break
		}
		for _, id := range candidate.RecentChainIDs {
			if id == chain.ID {
				memory = candidate
				break
			}
		}
		if memory != nil {
			break
		}
	}
	if memory == nil {
		return nil, nil
	}
	memory.FeedbackCount++
	memory.LastFeedbackAt = time.Now().UTC()
	scope := feedback.Scope
	if scope == "" {
		scope = contract.CgeFeedbackApplyToSimilar
	}
	outcome := feedback.FinalOutcome
	if outcome == "" {
		switch feedback.CorrectionType {
		case contract.CgeCorrectionFalsePositive, contract.CgeCorrectionReactionTooStrong:
			outcome = contract.CgeOutcomeFalsePositive
		case contract.CgeCorrectionFalseNegative, contract.CgeCorrectionReactionTooWeak:
			outcome = contract.CgeOutcomeRealIncident
		default:
			outcome = contract.CgeOutcomeNormal
		}
	}
	memory.Outcomes = appendUniqueString(memory.Outcomes, string(outcome))
	if scope == contract.CgeFeedbackCaseOnly {
		m.syncMemoryLocked(memory)
		return cloneMemory(memory), nil
	}
	for _, action := range feedback.PreferredActions {
		memory.RecommendedActions = appendUniqueString(memory.RecommendedActions, action)
	}
	switch outcome {
	case contract.CgeOutcomeFalsePositive, contract.CgeOutcomeNormal:
		memory.Confidence = maxFloat(0, memory.Confidence*0.8)
	case contract.CgeOutcomeRealIncident:
		memory.Confidence = minFloat(1, memory.Confidence+(1-memory.Confidence)*0.2)
	}
	m.syncMemoryLocked(memory)
	return cloneMemory(memory), nil
}

func appendUniqueString(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func maxFloat(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

func (m *ChainManager) Process(event *contract.Event, evaluation *contract.ChainEvaluation) []ChainUpdate {
	if m == nil || event == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enrichNodeIDLocked(event)

	at := event.Timestamp.UTC()
	if at.IsZero() {
		at = m.now().UTC()
		event.Timestamp = at
	}
	updates := m.closeInactiveLocked(at)
	role := ClassifyEventForChain(event)
	if role == ChainRoleIgnored {
		return updates
	}
	if role == ChainRoleContextual {
		chain := m.findCompatibleLocked(event, at)
		if chain == nil && !(event.Type == contract.EventVisionMotion && m.config.MotionCreatesChain) {
			return updates
		}
		if chain == nil {
			chain = newChain(event, at)
			m.chains[chain.ID] = chain
			m.syncChainLocked(chain)
			updates = append(updates, ChainUpdate{Type: "event.chain.created", Chain: cloneChain(chain)})
		}
		if m.attachContextualLocked(chain, event, at) {
			updates = append(updates, ChainUpdate{Type: "event.chain.updated", Chain: cloneChain(chain)})
		}
		return updates
	}

	chain := m.findCompatibleLocked(event, at)
	created := false
	if chain == nil {
		chain = newChain(event, at)
		m.chains[chain.ID] = chain
		m.syncChainLocked(chain)
		created = true
	}
	if role == ChainRoleContextual {
		if m.attachContextualLocked(chain, event, at) {
			updates = append(updates, ChainUpdate{Type: "event.chain.updated", Chain: cloneChain(chain)})
		}
		return appendCreated(updates, chain, created)
	}

	appendSignificantLocked(chain, event, at, evaluation, m.config)
	m.syncChainLocked(chain)
	if isCriticalChain(chain, event, evaluation) {
		chain.Critical = true
		m.upsertCriticalMemoryLocked(chain, at)
	}
	if created {
		updates = append(updates, ChainUpdate{Type: "event.chain.created", Chain: cloneChain(chain)})
	}
	updates = append(updates, ChainUpdate{Type: "event.chain.updated", Chain: cloneChain(chain)})
	return updates
}

func (m *ChainManager) enrichNodeIDLocked(event *contract.Event) {
	if event == nil || event.NodeID != "" || event.DeviceID == "" || m.devices == nil {
		return
	}
	if node, _ := event.Payload["node_id"].(string); strings.TrimSpace(node) != "" {
		event.NodeID = strings.TrimSpace(node)
		return
	}
	if node, _ := event.Payload["node"].(string); strings.TrimSpace(node) != "" {
		event.NodeID = strings.TrimSpace(node)
		return
	}
	value, ok := m.devices.Get(event.DeviceID)
	if !ok || value == nil || value.NodeID == "" {
		return
	}
	event.NodeID = value.NodeID
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	node, _ := event.Payload["node_id"].(string)
	if strings.TrimSpace(node) == "" {
		event.Payload["node_id"] = event.NodeID
	}
}

func appendCreated(updates []ChainUpdate, chain *contract.EventChain, created bool) []ChainUpdate {
	if created {
		return append(updates, ChainUpdate{Type: "event.chain.created", Chain: cloneChain(chain)})
	}
	return updates
}

func (m *ChainManager) CloseInactive(now time.Time) []ChainUpdate {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if now.IsZero() {
		now = m.now().UTC()
	}
	return m.closeInactiveLocked(now.UTC())
}

func (m *ChainManager) closeInactiveLocked(now time.Time) []ChainUpdate {
	updates := make([]ChainUpdate, 0)
	for _, chain := range m.chains {
		if chain == nil || chain.Status != contract.EventChainOpen || chain.LastSignificantEventAt.IsZero() {
			continue
		}
		if now.Sub(chain.LastSignificantEventAt) < m.config.SignificantInactivityTimeout {
			continue
		}
		closedAt := chain.LastSignificantEventAt.Add(m.config.SignificantInactivityTimeout)
		if closedAt.After(now) {
			closedAt = now
		}
		chain.Status = contract.EventChainClosed
		chain.ClosedAt = &closedAt
		chain.ClosedReason = "significant_inactivity_timeout"
		chain.UpdatedAt = closedAt
		m.syncChainLocked(chain)
		if chain.Critical {
			m.upsertCriticalMemoryLocked(chain, closedAt)
		}
		updates = append(updates, ChainUpdate{Type: "event.chain.closed", Chain: cloneChain(chain)})
	}
	return updates
}

func newChain(event *contract.Event, at time.Time) *contract.EventChain {
	chain := &contract.EventChain{
		ID:              idgen.New("chain"),
		Status:          contract.EventChainOpen,
		ActivationID:    event.ActivationID,
		SequenceKey:     event.SequenceKey,
		StartedAt:       at,
		UpdatedAt:       at,
		LastEventAt:     at,
		PrimaryDeviceID: event.DeviceID,
		PrimaryNodeID:   event.NodeID,
		DangerLevel:     contract.DangerNone,
		MaxDangerLevel:  contract.DangerNone,
		Title:           chainTitle(event),
		CreatedBy:       event.Source,
	}
	applyEventMetadata(chain, event)
	return chain
}

func (m *ChainManager) findCompatibleLocked(event *contract.Event, at time.Time) *contract.EventChain {
	open := make([]*contract.EventChain, 0)
	for _, chain := range m.chains {
		if chain == nil || chain.Status != contract.EventChainOpen {
			continue
		}
		open = append(open, chain)
	}
	sort.Slice(open, func(i, j int) bool { return open[i].UpdatedAt.After(open[j].UpdatedAt) })
	for _, chain := range open {
		eventTestRunID := eventTestRun(event)
		if eventTestRunID != "" && chain.TestRunID != "" && eventTestRunID != chain.TestRunID {
			continue
		}
		if event.ActivationID != "" && chain.ActivationID == event.ActivationID {
			return chain
		}
		if event.SequenceKey != "" && chain.SequenceKey == event.SequenceKey {
			return chain
		}
		if event.TrackID != "" && contains(chain.TrackIDs, event.TrackID) {
			return chain
		}
		if event.DeviceID != "" && event.DeviceID == chain.PrimaryDeviceID &&
			event.NodeID == chain.PrimaryNodeID && near(at, chain.LastEventAt, m.config.NearbyMatchWindow) {
			return chain
		}
	}
	return nil
}

func eventTestRun(event *contract.Event) string {
	if event == nil {
		return ""
	}
	metadata, _ := event.Payload["metadata"].(map[string]any)
	return stringValue(metadataValue(metadata, "test_run_id"))
}

func near(a, b time.Time, window time.Duration) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	delta := a.Sub(b)
	if delta < 0 {
		delta = -delta
	}
	return delta <= window
}

func appendSignificantLocked(chain *contract.EventChain, event *contract.Event, at time.Time, evaluation *contract.ChainEvaluation, config ChainConfig) {
	chain.Status = contract.EventChainOpen
	chain.ClosedAt = nil
	chain.ClosedReason = ""
	chain.LastEventAt = at
	chain.LastSignificantEventAt = at
	chain.UpdatedAt = at
	chain.EventsCount++
	chain.SignificantEventsCount++
	chain.SignificantEventTypes = appendUnique(chain.SignificantEventTypes, event.Type)
	chain.RecentEvents = appendRecent(chain.RecentEvents, publicEvent(event, ChainRoleSignificant), config.RecentEventsLimitPerChain)
	applyEventMetadata(chain, event)
	if evaluation != nil {
		evaluation.Index = len(chain.Evaluations) + 1
		chain.Evaluations = append(chain.Evaluations, *evaluation)
		if len(chain.Evaluations) > config.EvaluationsLimitPerChain {
			chain.Evaluations = chain.Evaluations[len(chain.Evaluations)-config.EvaluationsLimitPerChain:]
		}
		chain.CurrentState = evaluation.State
		chain.DangerLevel = normalizeDangerLevel(evaluation.DangerLevel, evaluation.DangerScore)
		chain.DangerScore = evaluation.DangerScore
		if chain.DangerScore > chain.MaxDangerScore {
			chain.MaxDangerScore = chain.DangerScore
		}
		if dangerRank(chain.DangerLevel) > dangerRank(chain.MaxDangerLevel) {
			chain.MaxDangerLevel = chain.DangerLevel
		}
		chain.DangerReasons = appendUnique(chain.DangerReasons, evaluation.Reasons...)
		chain.Summary = summaryFromEvaluation(chain, evaluation)
	}
	compactChain(chain, config)
	// The chain remains a bounded, queryable state projection while the
	// manager retains its lock for the event transaction.
	//
	// syncChainLocked is intentionally after compaction so StateStore never
	// observes an unbounded recent-events list.
}

func (m *ChainManager) attachContextualLocked(chain *contract.EventChain, event *contract.Event, at time.Time) bool {
	if chain == nil || !m.config.MotionExtendsChain && event.Type == contract.EventVisionMotion && !chain.LastSignificantEventAt.IsZero() &&
		at.Sub(chain.LastSignificantEventAt) > m.config.SignificantInactivityTimeout {
		return false
	}
	chain.LastEventAt = at
	chain.UpdatedAt = at
	chain.EventsCount++
	chain.ContextualEventsCount++
	if event.Type == contract.EventVisionMotion {
		chain.MotionCount++
	}
	coalesced := false
	if len(chain.RecentEvents) > 0 {
		last := chain.RecentEvents[len(chain.RecentEvents)-1]
		coalesced = last.Contextual && last.Type == event.Type && near(at, last.Timestamp, m.config.ContextualCoalesceWindow)
	}
	if !coalesced {
		chain.RecentEvents = appendRecent(chain.RecentEvents, publicEvent(event, ChainRoleContextual), m.config.RecentEventsLimitPerChain)
	}
	compactChain(chain, m.config)
	m.syncChainLocked(chain)
	lastPublished := m.lastContextualPublish[chain.ID]
	if !lastPublished.IsZero() && near(at, lastPublished, m.config.ContextualCoalesceWindow) {
		return false
	}
	m.lastContextualPublish[chain.ID] = at
	return true
}

func applyEventMetadata(chain *contract.EventChain, event *contract.Event) {
	if chain == nil || event == nil {
		return
	}
	if event.Identity != "" {
		chain.IdentityID = event.Identity
		chain.ResidentID = event.Identity
	}
	if event.TrackID != "" {
		chain.TrackIDs = appendUnique(chain.TrackIDs, event.TrackID)
	}
	if event.ClipID != "" {
		chain.ClipIDs = appendUnique(chain.ClipIDs, event.ClipID)
	}
	metadata, _ := event.Payload["metadata"].(map[string]any)
	if metadata != nil {
		chain.Simulated = chain.Simulated || boolValue(metadata["simulated"])
		if chain.TestRunID == "" {
			chain.TestRunID = stringValue(metadata["test_run_id"])
		}
		if chain.ScenarioID == "" {
			chain.ScenarioID = stringValue(metadata["scenario_id"])
		}
	}
}

func publicEvent(event *contract.Event, role ChainRole) contract.PublicEvent {
	metadata, _ := event.Payload["metadata"].(map[string]any)
	return contract.PublicEvent{
		ID: event.ID, Type: event.Type, Timestamp: event.Timestamp.UTC(), DeviceID: event.DeviceID,
		NodeID: event.NodeID, ActivationID: event.ActivationID, SequenceKey: event.SequenceKey,
		ClipID: event.ClipID, ClipIndex: event.ClipIndex, TrackID: event.TrackID,
		Severity: severity(event.Priority), Significant: role == ChainRoleSignificant,
		Contextual: role == ChainRoleContextual, Simulated: boolValue(metadataValue(metadata, "simulated")),
		TestRunID: stringValue(metadataValue(metadata, "test_run_id")), Payload: redactMap(event.Payload),
	}
}

func compactChain(chain *contract.EventChain, config ChainConfig) {
	if chain == nil {
		return
	}
	if len(chain.RecentEvents) > config.RecentEventsLimitPerChain {
		removed := len(chain.RecentEvents) - config.RecentEventsLimitPerChain
		chain.RecentEvents = chain.RecentEvents[removed:]
	}
	retained := len(chain.RecentEvents)
	chain.RollingSummary = fmt.Sprintf("%d événements, dont %d significatifs et %d contextuels", chain.EventsCount, chain.SignificantEventsCount, chain.ContextualEventsCount)
	chain.Compaction = &contract.ChainCompaction{
		TotalEventsCount: chain.EventsCount, RetainedEventsCount: retained,
		CompactedContextualCount: chain.ContextualEventsCount - countContextual(chain.RecentEvents),
		RollingSummary:           chain.RollingSummary,
	}
}

func countContextual(events []contract.PublicEvent) int {
	count := 0
	for _, event := range events {
		if event.Contextual {
			count++
		}
	}
	return count
}

func isCriticalChain(chain *contract.EventChain, event *contract.Event, evaluation *contract.ChainEvaluation) bool {
	if chain != nil && chain.Critical {
		return true
	}
	if event != nil {
		switch event.Type {
		case contract.EventVisionWeapon, contract.EventVisionFall, contract.EventVisionFight, contract.EventVisionTamper:
			return true
		}
	}
	if evaluation == nil {
		return false
	}
	return dangerRank(normalizeDangerLevel(evaluation.DangerLevel, evaluation.DangerScore)) >= dangerRank(contract.DangerHigh) ||
		evaluation.State == "intrusion" || evaluation.State == "break-in"
}

func (m *ChainManager) upsertCriticalMemoryLocked(chain *contract.EventChain, at time.Time) {
	if chain == nil || m.critical[chain.ID] {
		return
	}
	m.critical[chain.ID] = true
	templateID := criticalTemplate(chain)
	memory := m.memories[templateID]
	if memory == nil {
		memory = &contract.CriticalChainMemory{
			ID: idgen.New("critical-chain"), TemplateID: templateID, FirstSeen: at,
			RepresentativeChainID: chain.ID, SignificantEventTypes: append([]string(nil), chain.SignificantEventTypes...),
			MaxDangerLevel: string(chain.MaxDangerLevel), MaxDangerScore: chain.MaxDangerScore,
			Summary: chain.Summary, LearnedReason: "chaîne critique observée",
		}
		if chain.Simulated {
			memory.SimulatedOccurrences = 1
		} else {
			memory.RealOccurrences = 1
		}
		m.memories[templateID] = memory
	} else if chain.Simulated {
		memory.SimulatedOccurrences++
	} else {
		memory.RealOccurrences++
	}
	memory.LastSeen = at
	memory.Occurrences++
	if memory.SimulatedOccurrences > 0 && memory.RealOccurrences > 0 {
		memory.Source = "mixed"
	} else if memory.SimulatedOccurrences > 0 {
		memory.Source = "simulation"
	} else {
		memory.Source = "real"
	}
	memory.Simulated = memory.Source == "simulation"
	if chain.MaxDangerScore > memory.MaxDangerScore {
		memory.MaxDangerScore = chain.MaxDangerScore
	}
	if dangerRank(contract.DangerLevel(memory.MaxDangerLevel)) < dangerRank(chain.MaxDangerLevel) {
		memory.MaxDangerLevel = string(chain.MaxDangerLevel)
	}
	memory.RecentChainIDs = appendUnique(memory.RecentChainIDs, chain.ID)
	if len(memory.RecentChainIDs) > 20 {
		memory.RecentChainIDs = memory.RecentChainIDs[len(memory.RecentChainIDs)-20:]
	}
	memory.Confidence = minFloat(0.99, 0.35+float64(memory.Occurrences)*0.1)
	m.syncMemoryLocked(memory)
}

func criticalTemplate(chain *contract.EventChain) string {
	return strings.Join(chain.SignificantEventTypes, ">") + "|" + chain.PrimaryNodeID + "|" + string(chain.MaxDangerLevel)
}

func chainTitle(event *contract.Event) string {
	if event == nil {
		return "Chaîne d’événements"
	}
	return fmt.Sprintf("%s à %s", event.Type, firstNonEmpty(event.NodeID, event.DeviceID, "emplacement inconnu"))
}

func summaryFromEvaluation(chain *contract.EventChain, evaluation *contract.ChainEvaluation) string {
	if len(evaluation.Reasons) > 0 {
		return strings.Join(evaluation.Reasons, "; ")
	}
	if chain.Title != "" {
		return chain.Title
	}
	return "Évaluation CGE mise à jour"
}

func severity(priority int) string {
	switch {
	case priority >= contract.PriorityCritical:
		return "critical"
	case priority >= contract.PriorityHigh:
		return "high"
	case priority >= contract.PriorityNormal:
		return "medium"
	default:
		return "low"
	}
}

func normalizeDangerLevel(value string, score float64) contract.DangerLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return contract.DangerCritical
	case "high":
		return contract.DangerHigh
	case "medium", "medium_high":
		return contract.DangerMedium
	case "low":
		return contract.DangerLow
	case "none":
		return contract.DangerNone
	}
	switch {
	case score >= 0.9:
		return contract.DangerCritical
	case score >= 0.75:
		return contract.DangerHigh
	case score >= 0.5:
		return contract.DangerMedium
	case score > 0:
		return contract.DangerLow
	default:
		return contract.DangerNone
	}
}

func dangerRank(level contract.DangerLevel) int {
	switch level {
	case contract.DangerCritical:
		return 4
	case contract.DangerHigh:
		return 3
	case contract.DangerMedium:
		return 2
	case contract.DangerLow:
		return 1
	default:
		return 0
	}
}

func appendRecent(events []contract.PublicEvent, value contract.PublicEvent, limit int) []contract.PublicEvent {
	events = append(events, value)
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events
}

func appendUnique(values []string, additions ...string) []string {
	for _, value := range additions {
		value = strings.TrimSpace(value)
		if value == "" || contains(values, value) {
			continue
		}
		values = append(values, value)
	}
	return values
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func stringValue(value any) string {
	if value, ok := value.(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
func boolValue(value any) bool { typed, ok := value.(bool); return ok && typed }
func metadataValue(metadata map[string]any, key string) any {
	if metadata == nil {
		return nil
	}
	return metadata[key]
}
func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func redactMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "path" || lower == "clip_path" || lower == "token" || lower == "secret" || strings.Contains(lower, "password") || strings.Contains(lower, "credential") || strings.HasSuffix(lower, "_token") || strings.HasSuffix(lower, "_secret") {
			continue
		}
		switch nested := value.(type) {
		case map[string]any:
			output[key] = redactMap(nested)
		case []any:
			items := make([]any, 0, len(nested))
			for _, item := range nested {
				if mapped, ok := item.(map[string]any); ok {
					items = append(items, redactMap(mapped))
				} else {
					items = append(items, item)
				}
			}
			output[key] = items
		default:
			output[key] = value
		}
	}
	return output
}

func cloneChain(value *contract.EventChain) *contract.EventChain {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.TrackIDs = append([]string(nil), value.TrackIDs...)
	cloned.ClipIDs = append([]string(nil), value.ClipIDs...)
	cloned.DangerReasons = append([]string(nil), value.DangerReasons...)
	cloned.SignificantEventTypes = append([]string(nil), value.SignificantEventTypes...)
	cloned.RecentEvents = append([]contract.PublicEvent(nil), value.RecentEvents...)
	cloned.Evaluations = append([]contract.ChainEvaluation(nil), value.Evaluations...)
	for i := range cloned.RecentEvents {
		cloned.RecentEvents[i].Payload = redactMap(cloned.RecentEvents[i].Payload)
	}
	for i := range cloned.Evaluations {
		cloned.Evaluations[i].Reasons = append([]string(nil), value.Evaluations[i].Reasons...)
		cloned.Evaluations[i].Hypotheses = append([]string(nil), value.Evaluations[i].Hypotheses...)
		cloned.Evaluations[i].RecommendedActions = append([]string(nil), value.Evaluations[i].RecommendedActions...)
	}
	if value.Compaction != nil {
		compaction := *value.Compaction
		cloned.Compaction = &compaction
	}
	return &cloned
}

func cloneMemory(value *contract.CriticalChainMemory) *contract.CriticalChainMemory {
	if value == nil {
		return nil
	}
	cloned := contract.NormalizeCriticalChainMemory(*value)
	cloned.RecentChainIDs = append([]string{}, cloned.RecentChainIDs...)
	cloned.SignificantEventTypes = append([]string{}, cloned.SignificantEventTypes...)
	cloned.NodePattern = append([]string{}, cloned.NodePattern...)
	cloned.DeviceTypes = append([]string{}, cloned.DeviceTypes...)
	cloned.IdentityPattern = append([]string{}, cloned.IdentityPattern...)
	cloned.TypicalStatePath = append([]string{}, cloned.TypicalStatePath...)
	cloned.TypicalDangerPath = append([]string{}, cloned.TypicalDangerPath...)
	cloned.RecommendedActions = append([]string{}, cloned.RecommendedActions...)
	cloned.ActionsTaken = append([]string{}, cloned.ActionsTaken...)
	cloned.Outcomes = append([]string{}, cloned.Outcomes...)
	return &cloned
}

func (m *ChainManager) List(filter ChainFilter) []*contract.EventChain {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]*contract.EventChain, 0, len(m.chains))
	for _, value := range m.chains {
		if value == nil {
			continue
		}
		if filter.Status != "" && filter.Status != "all" && string(value.Status) != filter.Status {
			continue
		}
		if !filter.Since.IsZero() && value.UpdatedAt.Before(filter.Since) {
			continue
		}
		if filter.Severity != "" && string(value.DangerLevel) != filter.Severity {
			continue
		}
		if filter.Simulated != nil && value.Simulated != *filter.Simulated {
			continue
		}
		items = append(items, cloneChain(value))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items
}

func (m *ChainManager) Get(id string) (*contract.EventChain, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.chains[strings.TrimSpace(id)]
	return cloneChain(value), ok && value != nil
}

func (m *ChainManager) CriticalMemories(limit int) []*contract.CriticalChainMemory {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]*contract.CriticalChainMemory, 0, len(m.memories))
	for _, value := range m.memories {
		items = append(items, cloneMemory(value))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].LastSeen.After(items[j].LastSeen) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (m *ChainManager) CriticalMemory(id string) (*contract.CriticalChainMemory, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, value := range m.memories {
		if value.ID == id {
			return cloneMemory(value), true
		}
	}
	return nil, false
}

func (m *ChainManager) Summary() map[string]any {
	items := m.List(ChainFilter{Status: "all"})
	open, realOpen, simulatedOpen, critical, closed := 0, 0, 0, 0, 0
	highest := contract.DangerNone
	highestReal := contract.DangerNone
	for _, item := range items {
		if item.Status == contract.EventChainOpen {
			open++
			if item.Simulated {
				simulatedOpen++
			} else {
				realOpen++
			}
			if item.Critical {
				critical++
			}
		} else {
			closed++
		}
		if dangerRank(item.DangerLevel) > dangerRank(highest) {
			highest = item.DangerLevel
		}
		if !item.Simulated && dangerRank(item.DangerLevel) > dangerRank(highestReal) {
			highestReal = item.DangerLevel
		}
	}
	return map[string]any{
		"open_count": open, "real_open_count": realOpen, "simulated_open_count": simulatedOpen,
		"critical_open_count": critical, "recent_closed_count": closed,
		"highest_danger_level": highest, "highest_real_danger_level": highestReal,
	}
}
