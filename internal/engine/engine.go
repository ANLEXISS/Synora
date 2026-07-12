package engine

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"synora/internal/device"
	"synora/internal/engine/adapter"
	"synora/internal/engine/cognitive"
	cgecontracts "synora/internal/engine/contracts"
	"synora/internal/engine/danger"
	"synora/internal/engine/graph"
	"synora/internal/engine/situation"
	"synora/internal/idgen"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

type Engine struct {
	Topology *topology.Topology
	device   *device.Registry

	graphMemory *graph.GraphMemory
	cognitive   *cognitive.Engine

	cgeConfigMu       sync.Mutex
	criticalSeedPath  string
	securityProfileMu sync.RWMutex
	securityProfile   *contract.CgeSecurityProfile
	feedbackMu        sync.RWMutex
	feedbackHints     []feedbackHint
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
	approvedBehavior := e.graphMemory.MatchApprovedBehavior(cgeEvent)
	if approvedBehavior != nil {
		decisionResult = applyLearnedBehaviorGuidance(decisionResult, approvedBehavior)
	}
	assessment := danger.AssessEvent(event, e.dangerContext(event, store, decisionResult, now))
	if criticalSeedMatch != nil {
		applyCriticalSeedMatch(criticalSeedMatch, &assessment, &decisionResult)
	}
	decisionResult = applyDangerAssessment(decisionResult, assessment)
	if approvedBehavior != nil {
		decisionResult = applyLearnedBehaviorGuidance(decisionResult, approvedBehavior)
	}
	decisionResult.Situations = situation.Analyze(cgeEvent, decisionResult, now)

	result := adapter.BuildResult(event, store, decisionResult, now, &assessment)
	e.applyFeedbackHint(event, result)
	return result
}

// ObserveContext records contextual events for legacy graph continuity without
// running danger assessment, state transition, validation, or action planning.
func (e *Engine) ObserveContext(event *contract.Event, store *state.Store) *Result {
	if e == nil || event == nil {
		return nil
	}
	if store == nil {
		store = state.NewStore()
	}
	now := adapter.NormalizeEvent(event, e.device)
	e.graphMemory.LearnEvent(adapter.ToCGEEvent(event, store, now))
	return &Result{Decision: &contract.Decision{
		ID: idgen.New("dec"), Type: "engine.decision", Source: "core", Timestamp: now,
		Priority: contract.EventPriority(event.Type), EventID: event.ID, State: "activity",
		NodeID: event.NodeID, ClipID: event.ClipID, TrackID: event.TrackID,
		GroupKey: event.GroupKey, Reason: "contextual event observed",
	}}
}

func ShouldPersistDangerAssessment(assessment *contract.DangerAssessment) bool {
	return contract.IsPersistableDangerAssessment(assessment)
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
	_ = e.graphMemory.ReplaceCriticalSeeds(seeds, false)
}

func (e *Engine) LoadCriticalSeeds(path string) error {
	if e == nil || e.graphMemory == nil {
		return nil
	}
	path = strings.TrimSpace(path)
	seeds, err := graph.LoadCriticalSeeds(path)
	if err != nil {
		return err
	}
	if err := e.graphMemory.ReplaceCriticalSeeds(seeds, false); err != nil {
		return err
	}
	e.cgeConfigMu.Lock()
	e.criticalSeedPath = path
	e.cgeConfigMu.Unlock()
	return nil
}

func (e *Engine) SetCriticalSeedPath(path string) {
	if e == nil {
		return
	}
	e.cgeConfigMu.Lock()
	e.criticalSeedPath = strings.TrimSpace(path)
	e.cgeConfigMu.Unlock()
}

func (e *Engine) LoadCriticalSeedsFirstExisting(paths ...string) (string, error) {
	for _, candidate := range paths {
		if path := strings.TrimSpace(candidate); path != "" {
			e.SetCriticalSeedPath(path)
			break
		}
	}
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

func (e *Engine) CreateCriticalSeed(seed cgecontracts.CriticalSeed, allowLowScore bool) (cgecontracts.CriticalSeed, error) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.CriticalSeed{}, errors.New("cge unavailable")
	}
	e.cgeConfigMu.Lock()
	defer e.cgeConfigMu.Unlock()

	seed.ID = strings.TrimSpace(seed.ID)
	for _, current := range e.graphMemory.CriticalSeeds() {
		if current.ID == seed.ID {
			return cgecontracts.CriticalSeed{}, fmt.Errorf("duplicate critical seed id %q", seed.ID)
		}
	}
	now := time.Now().UTC()
	seed.Version = graph.CriticalSeedVersion
	seed.AllowLowScore = allowLowScore
	seed.CreatedAt = now
	seed.UpdatedAt = now
	seed.DeletedAt = nil
	normalized, err := graph.NormalizeCriticalSeeds([]cgecontracts.CriticalSeed{seed}, allowLowScore)
	if err != nil {
		return cgecontracts.CriticalSeed{}, err
	}
	seed = normalized[0]
	candidate := append(e.graphMemory.CriticalSeeds(), seed)
	if err := e.persistAndReplaceCriticalSeedsLocked(candidate); err != nil {
		return cgecontracts.CriticalSeed{}, err
	}
	return e.mustCriticalSeed(seed.ID)
}

func (e *Engine) PatchCriticalSeed(id string, patch cgecontracts.CriticalSeedPatch) (cgecontracts.CriticalSeed, error) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.CriticalSeed{}, errors.New("cge unavailable")
	}
	e.cgeConfigMu.Lock()
	defer e.cgeConfigMu.Unlock()

	id = strings.TrimSpace(id)
	candidate := e.graphMemory.CriticalSeeds()
	index := -1
	for i := range candidate {
		if candidate[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return cgecontracts.CriticalSeed{}, errors.New("critical seed not found")
	}
	seed := candidate[index]
	if patch.Name != nil {
		seed.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Enabled != nil {
		seed.Enabled = *patch.Enabled
		if seed.Enabled {
			seed.DeletedAt = nil
		}
	}
	if patch.DangerScore != nil {
		seed.DangerScore = *patch.DangerScore
	}
	if patch.AllowLowScore {
		seed.AllowLowScore = true
	}
	if patch.RiskLevel != nil {
		seed.RiskLevel = strings.TrimSpace(*patch.RiskLevel)
	}
	if patch.ExpectedState != nil {
		seed.ExpectedState = strings.TrimSpace(*patch.ExpectedState)
	}
	if patch.ProposedActions != nil {
		seed.ProposedActions = append([]string(nil), (*patch.ProposedActions)...)
	}
	if patch.ForbiddenActions != nil {
		seed.ForbiddenActions = append([]string(nil), (*patch.ForbiddenActions)...)
	}
	if patch.RequiresValidation != nil {
		seed.RequiresValidation = *patch.RequiresValidation
	}
	seed.Version++
	if seed.Version <= graph.CriticalSeedVersion {
		seed.Version = graph.CriticalSeedVersion + 1
	}
	seed.UpdatedAt = time.Now().UTC()
	normalized, err := graph.NormalizeCriticalSeeds([]cgecontracts.CriticalSeed{seed}, patch.AllowLowScore)
	if err != nil {
		return cgecontracts.CriticalSeed{}, err
	}
	candidate[index] = normalized[0]
	if err := e.persistAndReplaceCriticalSeedsLocked(candidate); err != nil {
		return cgecontracts.CriticalSeed{}, err
	}
	return e.mustCriticalSeed(id)
}

func (e *Engine) DeleteCriticalSeed(id string) (cgecontracts.CriticalSeed, error) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.CriticalSeed{}, errors.New("cge unavailable")
	}
	e.cgeConfigMu.Lock()
	defer e.cgeConfigMu.Unlock()

	id = strings.TrimSpace(id)
	candidate := e.graphMemory.CriticalSeeds()
	index := -1
	for i := range candidate {
		if candidate[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return cgecontracts.CriticalSeed{}, errors.New("critical seed not found")
	}
	now := time.Now().UTC()
	candidate[index].Enabled = false
	candidate[index].DeletedAt = &now
	candidate[index].UpdatedAt = now
	candidate[index].Version++
	if err := e.persistAndReplaceCriticalSeedsLocked(candidate); err != nil {
		return cgecontracts.CriticalSeed{}, err
	}
	return e.mustCriticalSeed(id)
}

func (e *Engine) persistAndReplaceCriticalSeedsLocked(seeds []cgecontracts.CriticalSeed) error {
	if strings.TrimSpace(e.criticalSeedPath) == "" {
		return errors.New("critical seed persistence path is not configured")
	}
	normalized, err := graph.NormalizeCriticalSeeds(seeds, true)
	if err != nil {
		return err
	}
	// Memory is intentionally left untouched until the durable write commits.
	// Therefore any write/backup error is an automatic in-memory rollback.
	if err := graph.SaveCriticalSeeds(e.criticalSeedPath, normalized); err != nil {
		return err
	}
	return e.graphMemory.ReplaceCriticalSeeds(normalized, true)
}

func (e *Engine) mustCriticalSeed(id string) (cgecontracts.CriticalSeed, error) {
	seed, ok := e.graphMemory.CriticalSeed(id)
	if !ok {
		return cgecontracts.CriticalSeed{}, errors.New("critical seed not found after update")
	}
	return seed, nil
}

func (e *Engine) LearnedBehaviors() []cgecontracts.LearnedBehavior {
	if e == nil || e.graphMemory == nil {
		return nil
	}
	return e.graphMemory.LearnedBehaviors()
}

func (e *Engine) LearnedBehavior(id string) (cgecontracts.LearnedBehavior, bool) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.LearnedBehavior{}, false
	}
	return e.graphMemory.LearnedBehavior(id)
}

func (e *Engine) PatchLearnedBehavior(id string, patch cgecontracts.LearnedBehaviorPatch) (cgecontracts.LearnedBehavior, error) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.LearnedBehavior{}, errors.New("cge unavailable")
	}
	return e.graphMemory.PatchLearnedBehavior(id, patch)
}

func (e *Engine) ApproveLearnedBehavior(id string, requiresValidation *bool) (cgecontracts.LearnedBehavior, error) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.LearnedBehavior{}, errors.New("cge unavailable")
	}
	return e.graphMemory.ApproveLearnedBehavior(id, requiresValidation)
}

func (e *Engine) RejectLearnedBehavior(id string) (cgecontracts.LearnedBehavior, error) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.LearnedBehavior{}, errors.New("cge unavailable")
	}
	return e.graphMemory.RejectLearnedBehavior(id)
}

func (e *Engine) DisableLearnedBehavior(id string) (cgecontracts.LearnedBehavior, error) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.LearnedBehavior{}, errors.New("cge unavailable")
	}
	return e.graphMemory.DisableLearnedBehavior(id)
}

func (e *Engine) ResetLearnedBehavior(id string) (cgecontracts.LearnedBehavior, error) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.LearnedBehavior{}, errors.New("cge unavailable")
	}
	return e.graphMemory.ResetLearnedBehavior(id)
}

func (e *Engine) ForgetLearnedBehavior(id string) (cgecontracts.LearnedBehavior, error) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.LearnedBehavior{}, errors.New("cge unavailable")
	}
	return e.graphMemory.ForgetLearnedBehavior(id)
}

func (e *Engine) ApplyLearnedBehaviorFeedback(id string, feedbackType string) (cgecontracts.LearnedBehavior, error) {
	if e == nil || e.graphMemory == nil {
		return cgecontracts.LearnedBehavior{}, errors.New("cge unavailable")
	}
	return e.graphMemory.ApplyLearnedBehaviorFeedback(id, feedbackType)
}

func (e *Engine) ExportLearnedBehaviorOverrides() []cgecontracts.LearnedBehaviorOverride {
	if e == nil || e.graphMemory == nil {
		return nil
	}
	return e.graphMemory.ExportLearnedBehaviorOverrides()
}

func (e *Engine) ApplyLearnedBehaviorOverrides(overrides []cgecontracts.LearnedBehaviorOverride) error {
	if e == nil || e.graphMemory == nil {
		return errors.New("cge unavailable")
	}
	return e.graphMemory.ApplyLearnedBehaviorOverrides(overrides)
}

// ApplyUserValidation translates durable user feedback into the separate CGE
// override layer. It never rewrites raw learned counters or source events.
func (e *Engine) ApplyUserValidation(validation contract.ValidationRequest) error {
	if e == nil || e.graphMemory == nil {
		return errors.New("cge unavailable")
	}
	if validation.DeletedAt != nil || !validation.Enabled {
		return nil
	}
	status := strings.ToLower(strings.TrimSpace(validation.Status))
	if status == contract.ValidationStatusPending || status == contract.ValidationStatusIgnored || status == "" {
		return nil
	}
	behaviorID := strings.TrimSpace(validation.BehaviorID)
	switch strings.ToLower(strings.TrimSpace(validation.Type)) {
	case contract.ValidationTypeBehaviorApproval:
		if behaviorID == "" {
			return errors.New("behavior_id is required for behavior approval")
		}
		switch status {
		case contract.ValidationStatusAccepted, contract.ValidationStatusApproved, contract.ValidationStatusCorrected:
			_, err := e.ApproveLearnedBehavior(behaviorID, validationRequiresValidation(validation.Correction))
			return err
		case contract.ValidationStatusRejected:
			_, err := e.RejectLearnedBehavior(behaviorID)
			return err
		}
	case contract.ValidationTypeFalsePositive:
		if behaviorID == "" {
			return nil
		}
		if !affirmativeValidationStatus(status) {
			return nil
		}
		_, err := e.graphMemory.ApplyLearnedBehaviorFeedbackFromValidation(behaviorID, cgecontracts.FeedbackFalsePositive, validation.ID)
		return err
	case contract.ValidationTypeFalseNegative:
		if behaviorID == "" {
			return nil
		}
		if !affirmativeValidationStatus(status) {
			return nil
		}
		_, err := e.graphMemory.ApplyLearnedBehaviorFeedbackFromValidation(behaviorID, cgecontracts.FeedbackFalseNegative, validation.ID)
		return err
	case contract.ValidationTypeActionFeedback:
		if behaviorID == "" {
			return errors.New("behavior_id is required for action feedback")
		}
		forbidden := validationForbiddenActions(validation.Correction)
		if len(forbidden) == 0 {
			return nil
		}
		behavior, ok := e.LearnedBehavior(behaviorID)
		if !ok {
			return graph.ErrLearnedBehaviorNotFound
		}
		forbidden = mergeStrings(behavior.ForbiddenActions, forbidden)
		patch := cgecontracts.LearnedBehaviorPatch{ForbiddenActions: &forbidden}
		if behavior.Status == cgecontracts.LearnedBehaviorApproved {
			// A newly forbidden action must take effect immediately. Disable the
			// behavior in the same patch so an approved conflict is never active.
			status := cgecontracts.LearnedBehaviorDisabled
			enabled := false
			patch.Status = &status
			patch.Enabled = &enabled
		}
		_, err := e.PatchLearnedBehavior(behaviorID, patch)
		return err
	}
	return nil
}

func validationRequiresValidation(correction map[string]any) *bool {
	if correction == nil {
		return nil
	}
	value, ok := correction["requires_validation"].(bool)
	if !ok {
		return nil
	}
	return &value
}

func affirmativeValidationStatus(status string) bool {
	switch status {
	case contract.ValidationStatusAccepted, contract.ValidationStatusApproved, contract.ValidationStatusCorrected:
		return true
	default:
		return false
	}
}

func validationForbiddenActions(correction map[string]any) []string {
	if correction == nil {
		return nil
	}
	out := []string{}
	if value, ok := correction["forbidden_action"].(string); ok {
		out = append(out, value)
	}
	switch values := correction["forbidden_actions"].(type) {
	case []string:
		out = append(out, values...)
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
	}
	if forbidden, ok := correction["forbidden"].(bool); ok && forbidden {
		if value, ok := correction["action"].(string); ok {
			out = append(out, value)
		}
	}
	return mergeStrings(nil, out)
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
	repetitionWindow := 2 * time.Minute
	if profile := e.SecurityProfile(); profile != nil && profile.UnknownPersistenceSeconds > 0 {
		repetitionWindow = time.Duration(profile.UnknownPersistenceSeconds) * time.Second
	}
	context := danger.Context{
		NodeID:            event.NodeID,
		DeviceID:          event.DeviceID,
		TimeBucket:        danger.TimeBucket(now),
		HomeMode:          houseMode(store),
		ResidentsPresent:  residentsPresent(store),
		RepetitionCount:   repetitionCount(event, store, now, repetitionWindow),
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
	e.dangerProfileContext(event, &context)
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

func applyLearnedBehaviorGuidance(
	decision cgecontracts.DecisionResult,
	behavior *cgecontracts.LearnedBehavior,
) cgecontracts.DecisionResult {
	if behavior == nil || behavior.Status != cgecontracts.LearnedBehaviorApproved || !behavior.Enabled || behavior.Forgotten {
		return decision
	}
	decision.GraphUsed = true
	decision.LearnedBehaviorID = behavior.ID
	decision.ProposedActions = cloneActionGuidance(behavior.ProposedActions)
	decision.ForbiddenActions = append([]string(nil), behavior.ForbiddenActions...)
	decision.Reasons = mergeStrings(decision.Reasons, []string{"approved_learned_behavior"})
	decision.Evidence = mergeStrings(decision.Evidence, []string{"learned_behavior=" + behavior.ID})
	if behavior.ConfidenceOverride != nil {
		decision.Evidence = mergeStrings(decision.Evidence, []string{fmt.Sprintf("behavior_confidence_override=%.2f", *behavior.ConfidenceOverride)})
	}
	if behavior.RiskOverride != nil {
		// A user override may raise the safety floor, but never lower risk already
		// established by deterministic danger rules.
		decision.DecisionScore = maxFloat(decision.DecisionScore, *behavior.RiskOverride)
		decision.Level = maxSeverity(decision.Level, severityFromDangerScore(*behavior.RiskOverride))
	}
	if behavior.RequiresValidation {
		decision.ValidationRequired = true
		if decision.ValidationReason == "" {
			decision.ValidationReason = "approved_behavior_requires_validation"
		}
	}
	return decision
}

func cloneActionGuidance(actions []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(actions))
	for _, action := range actions {
		cloned := make(map[string]any, len(action))
		for key, value := range action {
			cloned[key] = value
		}
		out = append(out, cloned)
	}
	return out
}

func severityFromDangerScore(score float64) cgecontracts.Severity {
	switch {
	case score >= 0.80:
		return cgecontracts.SeverityCritical
	case score >= 0.60:
		return cgecontracts.SeverityHigh
	case score >= 0.40:
		return cgecontracts.SeverityMedium
	case score >= 0.20:
		return cgecontracts.SeverityLow
	default:
		return cgecontracts.SeverityInfo
	}
}

func maxSeverity(left cgecontracts.Severity, right cgecontracts.Severity) cgecontracts.Severity {
	order := map[cgecontracts.Severity]int{
		cgecontracts.SeverityInfo:     0,
		cgecontracts.SeverityLow:      1,
		cgecontracts.SeverityMedium:   2,
		cgecontracts.SeverityHigh:     3,
		cgecontracts.SeverityCritical: 4,
	}
	if order[right] > order[left] {
		return right
	}
	return left
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
	assessment.MatchedSeedID = match.CriticalSeedID
	assessment.RiskLevel = match.RiskLevel
	assessment.ExpectedState = match.ExpectedState
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

func repetitionCount(event *contract.Event, store *state.Store, now time.Time, window time.Duration) int {
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
		if !recent.Timestamp.IsZero() && !now.IsZero() && now.Sub(recent.Timestamp) > window {
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
