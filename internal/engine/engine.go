package engine

import (
	"fmt"
	"strings"
	"time"

	"synora/internal/device"
	"synora/internal/engine/adapter"
	"synora/internal/engine/cognitive"
	cgecontracts "synora/internal/engine/contracts"
	"synora/internal/engine/danger"
	"synora/internal/engine/graph"
	"synora/internal/engine/situation"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

type Engine struct {
	Topology *topology.Topology
	device   *device.Registry

	graphMemory *graph.GraphMemory
	cognitive   *cognitive.Engine
}

func NewEngine(
	topo *topology.Topology,
	registry *device.Registry,
	_ map[string]*topology.Resident,
) *Engine {
	memory := graph.NewGraphMemory()
	return &Engine{
		Topology:    topo,
		device:      registry,
		graphMemory: memory,
		cognitive:   cognitive.NewEngine(memory),
	}
}

func (e *Engine) Analyze(
	event *contract.Event,
	store *state.Store,
) *Result {
	if event == nil {
		return nil
	}
	if store == nil {
		store = state.NewStore()
	}

	now := adapter.NormalizeEvent(event, e.device)
	cgeEvent := adapter.ToCGEEvent(event, store, now)

	e.graphMemory.LearnEvent(cgeEvent)
	decisionResult := e.cognitive.ProcessEvent(cgeEvent)
	assessment := danger.AssessEvent(event, e.dangerContext(event, store, decisionResult, now))
	decisionResult = applyDangerAssessment(decisionResult, assessment)
	decisionResult.Situations = situation.Analyze(cgeEvent, decisionResult, now)

	return adapter.BuildResult(event, store, decisionResult, now, &assessment)
}

func (e *Engine) Process(
	event *contract.Event,
	stores ...*state.Store,
) *contract.Decision {
	var store *state.Store
	if len(stores) > 0 {
		store = stores[0]
	}
	result := e.Analyze(event, store)
	if result == nil {
		return nil
	}
	return result.Decision
}

func (e *Engine) StartDecayLoop() {}

func (e *Engine) CGEInspection() map[string]any {
	if e == nil || e.graphMemory == nil {
		return map[string]any{
			"stats":             map[string]any{},
			"sequences":         []any{},
			"transitions":       []any{},
			"learned_behaviors": []any{},
		}
	}
	return e.graphMemory.CompactInspection()
}

func (e *Engine) CGEDetailInspection() map[string]any {
	if e == nil || e.graphMemory == nil {
		return map[string]any{
			"stats":             map[string]any{},
			"sequences":         []any{},
			"transitions":       []any{},
			"learned_behaviors": []any{},
		}
	}
	return e.graphMemory.Inspection()
}

func (e *Engine) ObserveActionResult(event *contract.Event) {
	if e == nil || e.graphMemory == nil || event == nil || event.Payload == nil {
		return
	}
	metadata, _ := event.Payload["metadata"].(map[string]any)
	testRunID := metadataString(metadata["test_run_id"])
	if testRunID == "" {
		return
	}
	actionID := firstNonEmpty(metadataString(event.Payload["action_id"]), metadataString(event.Payload["id"]))
	status := metadataString(event.Payload["status"])
	evidence := fmt.Sprintf("action_result action=%s status=%s", actionID, status)
	e.graphMemory.ObserveActionEvidence(testRunID, evidence, metadataBool(metadata["simulated"]), event.Timestamp)
}

func (e *Engine) ResetIntrusion(stores ...*state.Store) {
	if len(stores) == 0 || stores[0] == nil {
		return
	}
	current := stores[0].SystemState()
	current.LastState = "idle"
	current.LastStateTime = time.Now().UTC()
	current.IntrusionActive = false
	current.IntrusionTime = time.Time{}
	current.EmergencyActive = false
	current.EmergencyTime = time.Time{}
	stores[0].SetSystemState(current)
}

func (e *Engine) dangerContext(
	event *contract.Event,
	store *state.Store,
	decision cgecontracts.DecisionResult,
	now time.Time,
) danger.Context {
	context := danger.Context{
		NodeID:            event.NodeID,
		DeviceID:          event.DeviceID,
		TimeBucket:        danger.TimeBucket(now),
		HomeMode:          houseMode(store),
		ResidentsPresent:  residentsPresent(store),
		RepetitionCount:   repetitionCount(event, store, now),
		SequenceNovelty:   decision.ValidationReason == "rapid_novel_transition" || containsString(decision.Reasons, "rapid_novel_transition"),
		SequenceSignature: decision.SequenceKey,
		Simulated:         payloadMetadataBool(event.Payload, "simulated"),
		DryRun:            payloadMetadataBool(event.Payload, "dry_run"),
		DecisionReasons:   append([]string(nil), decision.Reasons...),
		DecisionEvidence:  append([]string(nil), decision.Evidence...),
		Now:               now,
	}
	if e != nil && e.device != nil && event.DeviceID != "" {
		if device, ok := e.device.Get(event.DeviceID); ok && device != nil {
			context.DeviceRole = device.Role
			if context.NodeID == "" {
				context.NodeID = device.NodeID
			}
		}
	}
	return context
}

func applyDangerAssessment(
	decision cgecontracts.DecisionResult,
	assessment contract.DangerAssessment,
) cgecontracts.DecisionResult {
	decision.DecisionScore = assessment.Score
	decision.Level = severityFromDangerLevel(assessment.Level)
	decision.ValidationRequired = assessment.ValidationRequired
	decision.ValidationReason = assessment.ValidationReason
	decision.Reasons = mergeStrings(decision.Reasons, assessment.Reasons)
	decision.Evidence = mergeStrings(decision.Evidence, assessment.Evidence)
	return decision
}

func severityFromDangerLevel(level int) cgecontracts.Severity {
	switch {
	case level >= 5:
		return cgecontracts.SeverityCritical
	case level >= 3:
		return cgecontracts.SeverityHigh
	case level == 2:
		return cgecontracts.SeverityMedium
	case level == 1:
		return cgecontracts.SeverityLow
	default:
		return cgecontracts.SeverityInfo
	}
}

func repetitionCount(event *contract.Event, store *state.Store, now time.Time) int {
	if event == nil || store == nil {
		return 1
	}
	count := 0
	for _, recent := range store.RecentEventsList() {
		if recent == nil {
			continue
		}
		if contract.NormalizeEventType(recent.Type) != contract.NormalizeEventType(event.Type) {
			continue
		}
		if event.NodeID != "" && recent.NodeID != "" && recent.NodeID != event.NodeID {
			continue
		}
		if !recent.Timestamp.IsZero() && !now.IsZero() && now.Sub(recent.Timestamp) > 2*time.Minute {
			continue
		}
		count++
	}
	if count == 0 {
		return 1
	}
	return count
}

func residentsPresent(store *state.Store) int {
	if store == nil {
		return 0
	}
	count := 0
	for _, item := range store.Snapshot("presence") {
		if presence, ok := item.(state.PresenceState); ok && presence.State == adapter.StatePresent {
			count++
		}
	}
	return count
}

func houseMode(store *state.Store) string {
	if store == nil {
		return ""
	}
	return store.SystemState().LastState
}

func payloadMetadataBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	if metadataBool(payload[key]) {
		return true
	}
	metadata, _ := payload["metadata"].(map[string]any)
	return metadataBool(metadata[key])
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func mergeStrings(left []string, right []string) []string {
	out := append([]string(nil), left...)
	for _, value := range right {
		value = strings.TrimSpace(value)
		if value == "" || containsString(out, value) {
			continue
		}
		out = append(out, value)
	}
	return out
}

func metadataString(value any) string {
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
}

func metadataBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
