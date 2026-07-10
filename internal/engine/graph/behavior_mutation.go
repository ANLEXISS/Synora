package graph

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"synora/internal/engine/contracts"
)

var (
	ErrLearnedBehaviorNotFound = errors.New("learned behavior not found")
	ErrForbiddenAction         = errors.New("forbidden action")
)

func (g *GraphMemory) LearnedBehaviors() []contracts.LearnedBehavior {
	g.mu.RLock()
	defer g.mu.RUnlock()
	items := sortedBehaviors(g.learnedBehaviors)
	for i := range items {
		items[i] = cloneLearnedBehavior(items[i])
	}
	return items
}

func (g *GraphMemory) LearnedBehavior(id string) (contracts.LearnedBehavior, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	behavior := g.learnedBehaviors[strings.TrimSpace(id)]
	if behavior == nil {
		return contracts.LearnedBehavior{}, false
	}
	return cloneLearnedBehavior(*behavior), true
}

func (g *GraphMemory) PatchLearnedBehavior(id string, patch contracts.LearnedBehaviorPatch) (contracts.LearnedBehavior, error) {
	id = strings.TrimSpace(id)
	g.mu.Lock()
	defer g.mu.Unlock()
	behavior := g.learnedBehaviors[id]
	if behavior == nil {
		return contracts.LearnedBehavior{}, ErrLearnedBehaviorNotFound
	}

	override := cloneLearnedBehaviorOverride(g.behaviorOverrides[id])
	override.BehaviorID = id
	applyPatchToOverride(&override, patch)
	if override.UpdatedAt.IsZero() {
		override.UpdatedAt = time.Now().UTC()
	} else {
		override.UpdatedAt = time.Now().UTC()
	}
	if err := validateLearnedBehaviorOverride(override); err != nil {
		return contracts.LearnedBehavior{}, err
	}

	candidate := cloneLearnedBehavior(*behavior)
	applyOverride(&candidate, override)
	normalizeBehaviorLifecycle(&candidate)
	if candidate.Status == contracts.LearnedBehaviorApproved {
		if err := validateBehaviorForAutomaticUse(candidate); err != nil {
			return contracts.LearnedBehavior{}, err
		}
	}

	g.behaviorOverrides[id] = override
	g.applyBehaviorOverrideLocked(behavior)
	behavior.UpdatedAt = override.UpdatedAt
	return cloneLearnedBehavior(*behavior), nil
}

func (g *GraphMemory) ApproveLearnedBehavior(id string, requiresValidation *bool) (contracts.LearnedBehavior, error) {
	status := contracts.LearnedBehaviorApproved
	patch := contracts.LearnedBehaviorPatch{Status: &status}
	if requiresValidation != nil {
		patch.RequiresValidation = requiresValidation
	} else {
		value := g.behaviorRequiresValidation(id)
		patch.RequiresValidation = &value
	}
	enabled := true
	patch.Enabled = &enabled
	return g.PatchLearnedBehavior(id, patch)
}

func (g *GraphMemory) RejectLearnedBehavior(id string) (contracts.LearnedBehavior, error) {
	status := contracts.LearnedBehaviorRejected
	enabled := false
	return g.PatchLearnedBehavior(id, contracts.LearnedBehaviorPatch{Status: &status, Enabled: &enabled})
}

func (g *GraphMemory) DisableLearnedBehavior(id string) (contracts.LearnedBehavior, error) {
	status := contracts.LearnedBehaviorDisabled
	enabled := false
	return g.PatchLearnedBehavior(id, contracts.LearnedBehaviorPatch{Status: &status, Enabled: &enabled})
}

func (g *GraphMemory) ResetLearnedBehavior(id string) (contracts.LearnedBehavior, error) {
	id = strings.TrimSpace(id)
	g.mu.Lock()
	defer g.mu.Unlock()
	behavior := g.learnedBehaviors[id]
	if behavior == nil {
		return contracts.LearnedBehavior{}, ErrLearnedBehaviorNotFound
	}
	delete(g.behaviorOverrides, id)
	g.restoreBehaviorBaseLocked(behavior)
	return cloneLearnedBehavior(*behavior), nil
}

func (g *GraphMemory) ForgetLearnedBehavior(id string) (contracts.LearnedBehavior, error) {
	id = strings.TrimSpace(id)
	g.mu.Lock()
	defer g.mu.Unlock()
	behavior := g.learnedBehaviors[id]
	if behavior == nil {
		return contracts.LearnedBehavior{}, ErrLearnedBehaviorNotFound
	}
	override := cloneLearnedBehaviorOverride(g.behaviorOverrides[id])
	override.BehaviorID = id
	status := contracts.LearnedBehaviorDisabled
	enabled := false
	override.Status = &status
	override.Enabled = &enabled
	override.Forgotten = true
	override.UpdatedAt = time.Now().UTC()
	g.behaviorOverrides[id] = override
	g.applyBehaviorOverrideLocked(behavior)
	return cloneLearnedBehavior(*behavior), nil
}

func (g *GraphMemory) ApplyLearnedBehaviorFeedback(id string, feedbackType string) (contracts.LearnedBehavior, error) {
	return g.applyLearnedBehaviorFeedback(id, feedbackType, "")
}

func (g *GraphMemory) ApplyLearnedBehaviorFeedbackFromValidation(id string, feedbackType string, validationID string) (contracts.LearnedBehavior, error) {
	return g.applyLearnedBehaviorFeedback(id, feedbackType, strings.TrimSpace(validationID))
}

func (g *GraphMemory) applyLearnedBehaviorFeedback(id string, feedbackType string, validationID string) (contracts.LearnedBehavior, error) {
	id = strings.TrimSpace(id)
	feedbackType = strings.ToLower(strings.TrimSpace(feedbackType))
	g.mu.Lock()
	defer g.mu.Unlock()
	behavior := g.learnedBehaviors[id]
	if behavior == nil {
		return contracts.LearnedBehavior{}, ErrLearnedBehaviorNotFound
	}
	override := cloneLearnedBehaviorOverride(g.behaviorOverrides[id])
	override.BehaviorID = id
	if validationID != "" && containsString(override.UserFeedback.ValidationIDs, validationID) {
		return cloneLearnedBehavior(*behavior), nil
	}
	switch feedbackType {
	case contracts.FeedbackPositive:
		override.UserFeedback.PositiveCount++
	case contracts.FeedbackNegative:
		override.UserFeedback.NegativeCount++
	case contracts.FeedbackFalsePositive:
		override.UserFeedback.FalsePositiveCount++
		confidence := effectiveBehaviorConfidence(*behavior)
		confidence = math.Max(0, confidence-0.10)
		override.ConfidenceOverride = &confidence
		requiresValidation := true
		override.RequiresValidation = &requiresValidation
	case contracts.FeedbackFalseNegative:
		override.UserFeedback.FalseNegativeCount++
		risk := behavior.DangerScore
		if behavior.RiskOverride != nil {
			risk = *behavior.RiskOverride
		}
		risk = math.Min(1, math.Max(risk, behavior.Confidence)+0.10)
		override.RiskOverride = &risk
	default:
		return contracts.LearnedBehavior{}, fmt.Errorf("unsupported learned behavior feedback %q", feedbackType)
	}
	now := time.Now().UTC()
	override.UserFeedback.LastType = feedbackType
	override.UserFeedback.UpdatedAt = now
	if validationID != "" {
		override.UserFeedback.ValidationIDs = appendLimited(override.UserFeedback.ValidationIDs, validationID, 100)
	}
	override.UpdatedAt = now
	g.behaviorOverrides[id] = override
	g.applyBehaviorOverrideLocked(behavior)
	return cloneLearnedBehavior(*behavior), nil
}

func (g *GraphMemory) ExportLearnedBehaviorOverrides() []contracts.LearnedBehaviorOverride {
	g.mu.RLock()
	defer g.mu.RUnlock()
	ids := make([]string, 0, len(g.behaviorOverrides))
	for id := range g.behaviorOverrides {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]contracts.LearnedBehaviorOverride, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneLearnedBehaviorOverride(g.behaviorOverrides[id]))
	}
	return out
}

func (g *GraphMemory) ApplyLearnedBehaviorOverrides(overrides []contracts.LearnedBehaviorOverride) error {
	normalized := make(map[string]contracts.LearnedBehaviorOverride, len(overrides))
	for _, override := range overrides {
		override.BehaviorID = strings.TrimSpace(override.BehaviorID)
		if err := validateLearnedBehaviorOverride(override); err != nil {
			return err
		}
		if _, exists := normalized[override.BehaviorID]; exists {
			return fmt.Errorf("duplicate learned behavior override %q", override.BehaviorID)
		}
		normalized[override.BehaviorID] = cloneLearnedBehaviorOverride(override)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	for id, override := range normalized {
		if behavior := g.learnedBehaviors[id]; behavior != nil {
			candidate := cloneLearnedBehavior(*behavior)
			applyOverride(&candidate, override)
			normalizeBehaviorLifecycle(&candidate)
			if candidate.Status == contracts.LearnedBehaviorApproved {
				if err := validateBehaviorForAutomaticUse(candidate); err != nil {
					return err
				}
			}
		}
	}
	g.behaviorOverrides = normalized
	for _, behavior := range g.learnedBehaviors {
		if behavior == nil {
			continue
		}
		g.restoreBehaviorBaseLocked(behavior)
		g.applyBehaviorOverrideLocked(behavior)
	}
	return nil
}

// MatchApprovedBehavior returns only behavior guidance explicitly approved by
// the user. Rejected, disabled, forgotten, unsafe, or unapproved behaviors are
// never candidates for automatic decision guidance.
func (g *GraphMemory) MatchApprovedBehavior(event *contracts.Event) *contracts.LearnedBehavior {
	if event == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	events := g.behaviorMatchEventsLocked(event)
	if len(events) == 0 {
		return nil
	}

	var best *contracts.LearnedBehavior
	bestLength := 0
	for _, behavior := range g.learnedBehaviors {
		if behavior == nil || behavior.Status != contracts.LearnedBehaviorApproved || !behavior.Enabled || behavior.Forgotten {
			continue
		}
		if err := validateBehaviorForAutomaticUse(*behavior); err != nil {
			continue
		}
		length, matches := behaviorSignatureMatches(behavior.TriggerSequenceSignature, events)
		if !matches {
			continue
		}
		if best == nil || length > bestLength || (length == bestLength && effectiveBehaviorConfidence(*behavior) > effectiveBehaviorConfidence(*best)) {
			best = behavior
			bestLength = length
		}
	}
	if best == nil {
		return nil
	}
	now := event.Timestamp
	if now.IsZero() {
		now = time.Now().UTC()
	}
	best.LastTriggeredAt = &now
	result := cloneLearnedBehavior(*best)
	return &result
}

func (g *GraphMemory) behaviorRequiresValidation(id string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	behavior := g.learnedBehaviors[strings.TrimSpace(id)]
	if behavior == nil {
		return true
	}
	if behavior.RequiresValidation || hasSensitiveAction(behavior.ProposedActions) {
		return true
	}
	return false
}

func (g *GraphMemory) applyBehaviorOverrideLocked(behavior *contracts.LearnedBehavior) {
	if behavior == nil {
		return
	}
	if override, ok := g.behaviorOverrides[behavior.ID]; ok {
		applyOverride(behavior, override)
	}
	normalizeBehaviorLifecycle(behavior)
	if behavior.CriticalSeedID != "" {
		seed, ok := g.criticalSeeds[behavior.CriticalSeedID]
		if !ok || !seed.Enabled || seed.DeletedAt != nil {
			behavior.Enabled = false
			behavior.Status = contracts.LearnedBehaviorDisabled
		}
	}
	if behavior.Status == contracts.LearnedBehaviorApproved {
		if err := validateBehaviorForAutomaticUse(*behavior); err != nil {
			behavior.Enabled = false
			behavior.Status = contracts.LearnedBehaviorDisabled
			if behavior.Context == nil {
				behavior.Context = map[string]any{}
			}
			behavior.Context["config_error"] = "forbidden_action"
		} else if behavior.Context != nil {
			delete(behavior.Context, "config_error")
		}
	}
}

func (g *GraphMemory) restoreBehaviorBaseLocked(behavior *contracts.LearnedBehavior) {
	if behavior == nil {
		return
	}
	if behavior.CriticalSeedID != "" {
		seed, ok := g.criticalSeeds[behavior.CriticalSeedID]
		if ok {
			g.upsertCriticalSeedLocked(seed)
			if !seed.Enabled || seed.DeletedAt != nil {
				g.deactivateCriticalSeedLocked(seed.ID, false)
			}
			return
		}
	}
	behavior.Status = contracts.LearnedBehaviorObserving
	behavior.RequiresValidation = true
	behavior.ProposedActions = []map[string]any{}
	behavior.ForbiddenActions = nil
	behavior.Enabled = true
	behavior.Forgotten = false
	behavior.UserNotes = ""
	behavior.ConfidenceOverride = nil
	behavior.RiskOverride = nil
	behavior.UserFeedback = contracts.UserFeedback{}
	behavior.UpdatedAt = time.Now().UTC()
}

func applyPatchToOverride(override *contracts.LearnedBehaviorOverride, patch contracts.LearnedBehaviorPatch) {
	if patch.Status != nil {
		value := strings.ToLower(strings.TrimSpace(*patch.Status))
		override.Status = &value
	}
	if patch.RequiresValidation != nil {
		value := *patch.RequiresValidation
		override.RequiresValidation = &value
	}
	if patch.ProposedActions != nil {
		value := cloneActionMaps(*patch.ProposedActions)
		override.ProposedActions = &value
	}
	if patch.ForbiddenActions != nil {
		value := uniqueCanonicalActions(*patch.ForbiddenActions)
		override.ForbiddenActions = &value
	}
	if patch.UserNotes != nil {
		value := strings.TrimSpace(*patch.UserNotes)
		override.UserNotes = &value
	}
	if patch.ConfidenceOverride != nil {
		value := *patch.ConfidenceOverride
		override.ConfidenceOverride = &value
	}
	if patch.RiskOverride != nil {
		value := *patch.RiskOverride
		override.RiskOverride = &value
	}
	if patch.Enabled != nil {
		value := *patch.Enabled
		override.Enabled = &value
	}
}

func applyOverride(behavior *contracts.LearnedBehavior, override contracts.LearnedBehaviorOverride) {
	if behavior == nil {
		return
	}
	if override.Status != nil {
		behavior.Status = strings.ToLower(strings.TrimSpace(*override.Status))
	}
	if override.RequiresValidation != nil {
		behavior.RequiresValidation = *override.RequiresValidation
	}
	if override.ProposedActions != nil {
		behavior.ProposedActions = cloneActionMaps(*override.ProposedActions)
	}
	if override.ForbiddenActions != nil {
		behavior.ForbiddenActions = append([]string(nil), (*override.ForbiddenActions)...)
	}
	if override.UserNotes != nil {
		behavior.UserNotes = *override.UserNotes
	}
	if override.ConfidenceOverride != nil {
		value := *override.ConfidenceOverride
		behavior.ConfidenceOverride = &value
	}
	if override.RiskOverride != nil {
		value := *override.RiskOverride
		behavior.RiskOverride = &value
	}
	if override.Enabled != nil {
		behavior.Enabled = *override.Enabled
	}
	behavior.Forgotten = override.Forgotten
	behavior.UserFeedback = override.UserFeedback
	behavior.UserFeedback.ValidationIDs = append([]string(nil), override.UserFeedback.ValidationIDs...)
	if !override.UpdatedAt.IsZero() {
		behavior.UpdatedAt = override.UpdatedAt
	}
}

func normalizeBehaviorLifecycle(behavior *contracts.LearnedBehavior) {
	if behavior == nil {
		return
	}
	behavior.Status = strings.ToLower(strings.TrimSpace(behavior.Status))
	if behavior.Status == "" {
		behavior.Status = contracts.LearnedBehaviorObserving
	}
	if behavior.Forgotten || behavior.Status == contracts.LearnedBehaviorRejected || behavior.Status == contracts.LearnedBehaviorDisabled {
		behavior.Enabled = false
	}
	if behavior.Forgotten {
		behavior.Status = contracts.LearnedBehaviorDisabled
	}
}

func validateLearnedBehaviorOverride(override contracts.LearnedBehaviorOverride) error {
	if strings.TrimSpace(override.BehaviorID) == "" {
		return errors.New("learned behavior override behavior_id is required")
	}
	if override.Status != nil && !validBehaviorStatus(*override.Status) {
		return fmt.Errorf("invalid learned behavior status %q", *override.Status)
	}
	for name, value := range map[string]*float64{
		"confidence_override": override.ConfidenceOverride,
		"risk_override":       override.RiskOverride,
	} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 1) {
			return fmt.Errorf("%s must be between 0 and 1", name)
		}
	}
	feedback := override.UserFeedback
	if feedback.PositiveCount < 0 || feedback.NegativeCount < 0 || feedback.FalsePositiveCount < 0 || feedback.FalseNegativeCount < 0 {
		return errors.New("learned behavior feedback counters cannot be negative")
	}
	return nil
}

func validBehaviorStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case contracts.LearnedBehaviorObserving,
		contracts.LearnedBehaviorSuggested,
		contracts.LearnedBehaviorApproved,
		contracts.LearnedBehaviorRejected,
		contracts.LearnedBehaviorDisabled:
		return true
	default:
		return false
	}
}

func validateBehaviorForAutomaticUse(behavior contracts.LearnedBehavior) error {
	forbidden := make(map[string]bool, len(behavior.ForbiddenActions))
	for _, action := range behavior.ForbiddenActions {
		forbidden[canonicalActionName(action)] = true
	}
	for _, action := range behavior.ProposedActions {
		canonical := canonicalActionFromMap(action)
		if canonical == "" {
			continue
		}
		if forbidden[canonical] {
			return fmt.Errorf("%w: proposed action %q is forbidden by behavior", ErrForbiddenAction, canonical)
		}
		if isAlwaysForbiddenAutomaticAction(canonical) {
			return fmt.Errorf("%w: automatic action %q is forbidden", ErrForbiddenAction, canonical)
		}
		if canonical == "siren.turn_on" && !behavior.RequiresValidation {
			return fmt.Errorf("%w: siren.turn_on requires validation", ErrForbiddenAction)
		}
	}
	return nil
}

func hasSensitiveAction(actions []map[string]any) bool {
	for _, action := range actions {
		switch canonicalActionFromMap(action) {
		case "door.unlock", "siren.turn_on", "emergency_call", "disable_camera":
			return true
		}
	}
	return false
}

func isAlwaysForbiddenAutomaticAction(action string) bool {
	switch canonicalActionName(action) {
	case "door.unlock", "emergency_call", "disable_camera":
		return true
	default:
		return false
	}
}

func canonicalActionFromMap(action map[string]any) string {
	if action == nil {
		return ""
	}
	for _, key := range []string{"action", "type"} {
		if value, ok := action[key].(string); ok {
			if canonical := canonicalActionName(value); canonical != "" && canonical != "device" && canonical != "device.command" {
				return canonical
			}
		}
	}
	command, _ := action["command"].(string)
	target := ""
	for _, key := range []string{"device_id", "device", "target"} {
		if value, ok := action[key].(string); ok && strings.TrimSpace(value) != "" {
			target = value
			break
		}
	}
	combined := strings.ToLower(target + "." + command)
	switch {
	case strings.Contains(combined, "door") && strings.EqualFold(strings.TrimSpace(command), "unlock"):
		return "door.unlock"
	case strings.Contains(combined, "siren") && (strings.EqualFold(strings.TrimSpace(command), "on") || strings.EqualFold(strings.TrimSpace(command), "turn_on")):
		return "siren.turn_on"
	case strings.Contains(combined, "camera") && (strings.EqualFold(strings.TrimSpace(command), "disable") || strings.EqualFold(strings.TrimSpace(command), "off")):
		return "disable_camera"
	}
	return canonicalActionName(command)
}

func canonicalActionName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	switch value {
	case "door_unlock", "unlock_door":
		return "door.unlock"
	case "siren_turn_on", "turn_on_siren":
		return "siren.turn_on"
	case "emergency.call", "call_emergency", "emergency_call":
		return "emergency_call"
	case "camera.disable", "disable.camera", "camera_off", "disable_camera":
		return "disable_camera"
	default:
		return value
	}
}

func uniqueCanonicalActions(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		canonical := canonicalActionName(value)
		if canonical == "" || seen[canonical] {
			continue
		}
		seen[canonical] = true
		out = append(out, canonical)
	}
	return out
}

func (g *GraphMemory) behaviorMatchEventsLocked(event *contracts.Event) []learnedEvent {
	current := learnedEventFromCGE(event)
	scope := SequenceKey(event)
	if current.Simulated {
		runKey := current.TestRunID
		if runKey == "" {
			runKey = scope
		}
		return append([]learnedEvent(nil), g.runEvents[runKey]...)
	}
	return append([]learnedEvent(nil), g.recentEvents[scope]...)
}

func behaviorSignatureMatches(signature string, events []learnedEvent) (int, bool) {
	parts := strings.Split(strings.TrimSpace(signature), " > ")
	if len(parts) == 0 || len(parts) > len(events) {
		return 0, false
	}
	start := len(events) - len(parts)
	for i, part := range parts {
		if strings.TrimSpace(part) != events[start+i].EventType {
			return 0, false
		}
	}
	return len(parts), true
}

func effectiveBehaviorConfidence(behavior contracts.LearnedBehavior) float64 {
	if behavior.ConfidenceOverride != nil {
		return *behavior.ConfidenceOverride
	}
	return behavior.Confidence
}

func cloneLearnedBehavior(value contracts.LearnedBehavior) contracts.LearnedBehavior {
	value.Context = cloneAnyMap(value.Context)
	value.ProposedActions = cloneActionMaps(value.ProposedActions)
	value.ForbiddenActions = append([]string(nil), value.ForbiddenActions...)
	value.Evidence = append([]string(nil), value.Evidence...)
	if value.LastTriggeredAt != nil {
		lastTriggeredAt := *value.LastTriggeredAt
		value.LastTriggeredAt = &lastTriggeredAt
	}
	if value.ConfidenceOverride != nil {
		confidence := *value.ConfidenceOverride
		value.ConfidenceOverride = &confidence
	}
	if value.RiskOverride != nil {
		risk := *value.RiskOverride
		value.RiskOverride = &risk
	}
	value.UserFeedback.ValidationIDs = append([]string(nil), value.UserFeedback.ValidationIDs...)
	return value
}

func cloneLearnedBehaviorOverride(value contracts.LearnedBehaviorOverride) contracts.LearnedBehaviorOverride {
	if value.Status != nil {
		status := *value.Status
		value.Status = &status
	}
	if value.RequiresValidation != nil {
		requiresValidation := *value.RequiresValidation
		value.RequiresValidation = &requiresValidation
	}
	if value.ProposedActions != nil {
		actions := cloneActionMaps(*value.ProposedActions)
		value.ProposedActions = &actions
	}
	if value.ForbiddenActions != nil {
		actions := append([]string(nil), (*value.ForbiddenActions)...)
		value.ForbiddenActions = &actions
	}
	if value.UserNotes != nil {
		notes := *value.UserNotes
		value.UserNotes = &notes
	}
	if value.ConfidenceOverride != nil {
		confidence := *value.ConfidenceOverride
		value.ConfidenceOverride = &confidence
	}
	if value.RiskOverride != nil {
		risk := *value.RiskOverride
		value.RiskOverride = &risk
	}
	if value.Enabled != nil {
		enabled := *value.Enabled
		value.Enabled = &enabled
	}
	value.UserFeedback.ValidationIDs = append([]string(nil), value.UserFeedback.ValidationIDs...)
	return value
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func cloneActionMaps(values []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		out = append(out, cloneAnyMap(value))
	}
	return out
}

func cloneAnyMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		switch typed := item.(type) {
		case map[string]any:
			out[key] = cloneAnyMap(typed)
		case []map[string]any:
			out[key] = cloneActionMaps(typed)
		case []string:
			out[key] = append([]string(nil), typed...)
		case []any:
			cloned := make([]any, len(typed))
			copy(cloned, typed)
			out[key] = cloned
		default:
			out[key] = item
		}
	}
	return out
}
