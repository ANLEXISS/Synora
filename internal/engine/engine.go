package engine

import (
	"fmt"
	"os"
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

	criticalSeedMatch := e.graphMemory.LearnEvent(cgeEvent)
	decisionResult := e.cognitive.ProcessEvent(cgeEvent)
	assessment := danger.AssessEvent(event, e.dangerContext(event, store, decisionResult, now))
	if criticalSeedMatch != nil {
		applyCriticalSeedMatch(criticalSeedMatch, &assessment, &decisionResult)
	}
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

func (e *Engine) ApplyCriticalSeeds(seeds []cgecontracts.CriticalSeed) {
	if e == nil || e.graphMemory == nil {
		return
	}
	e.graphMemory.ApplyCriticalSeeds(seeds)
}

func (e *Engine) LoadCriticalSeeds(path string) error {
	if e == nil || e.graphMemory == nil {
		return nil
	}
	seeds, err := graph.LoadCriticalSeeds(path)
	if err != nil {
		return err
	}
	e.graphMemory.ApplyCriticalSeeds(seeds)
	return nil
}

func (e *Engine) LoadCriticalSeedsFirstExisting(paths ...string) (string, error) {
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		if err := e.LoadCriticalSeeds(path); err != nil {
			return path, err
		}
		return path, nil
	}
	return "", nil
}

func (e *Engine) CriticalSeeds() []cgecontracts.CriticalSeed {
	if e == nil || e.graphMemory == nil {
		return nil
	}
	return e.graphMemory.CriticalSeeds()
}

func (e *Engine) CriticalSeed(id string) (cgecontracts.CriticalSeed, bool) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.CriticalSeed{}, false
	}
	return e.graphMemory.CriticalSeed(id)
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

func applyCriticalSeedMatch(
	match *cgecontracts.CriticalSeedMatch,
	assessment *contract.DangerAssessment,
	decision *cgecontracts.DecisionResult,
) {
	if match == nil || assessment == nil || decision == nil {
		return
	}
	assessment.Score = maxFloat(assessment.Score, match.ExpectedDangerScore)
	assessment.Level = maxInt(assessment.Level, levelFromCriticalSeed(match.RiskLevel, match.ExpectedState))
	assessment.SequenceSignature = match.Signature
	assessment.ValidationRequired = true
	if assessment.ValidationReason == "" {
		assessment.ValidationReason = "critical_seed_match"
	}
	assessment.Reasons = mergeStrings(assessment.Reasons, []string{
		"critical_seed_match",
		"critical_seed_id=" + match.CriticalSeedID,
		"expected_state=" + match.ExpectedState,
	})
	assessment.Evidence = mergeStrings(assessment.Evidence, []string{
		"critical_seed=" + match.CriticalSeedID,
		"critical_seed_signature=" + match.Signature,
	})
	decision.DecisionScore = maxFloat(decision.DecisionScore, match.ExpectedDangerScore)
	decision.ValidationRequired = true
	if decision.ValidationReason == "" {
		decision.ValidationReason = "critical_seed_match"
	}
	decision.Reasons = mergeStrings(decision.Reasons, []string{
		"critical_seed_match",
		"critical_seed_id=" + match.CriticalSeedID,
	})
	decision.Evidence = mergeStrings(decision.Evidence, []string{"critical_seed=" + match.CriticalSeedID})
	decision.SequenceKey = match.Signature
	decision.GraphUsed = true
}

func levelFromCriticalSeed(riskLevel string, expectedState string) int {
	expectedState = strings.ToLower(strings.TrimSpace(expectedState))
	switch expectedState {
	case "emergency", "break_in", "intrusion":
		return 5
	}
	switch strings.ToLower(strings.TrimSpace(riskLevel)) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium_high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
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

func maxFloat(left float64, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
