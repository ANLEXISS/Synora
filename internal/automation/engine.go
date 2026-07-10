package automation

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

type Engine struct {
	store      *Store
	filePath   string
	Now        func() time.Time
	cooldowns  map[string]time.Time
	mutationMu sync.Mutex
}

func NewEngine(path string, _ ...interface{}) *Engine {
	return &Engine{
		store:     &Store{rules: make(map[string]Rule)},
		filePath:  path,
		cooldowns: make(map[string]time.Time),
	}
}

func (e *Engine) Add(rule Rule) error {
	_, err := e.create(rule, false)
	return err
}

func (e *Engine) Remove(id string) error {
	e.mutationMu.Lock()
	defer e.mutationMu.Unlock()
	id = strings.TrimSpace(id)
	if _, ok := e.store.Get(id); !ok {
		return contract.NewAPIError(contract.ErrorNotFound, "automation %q not found", id)
	}
	staged := e.store.List()
	filtered := make([]Rule, 0, len(staged)-1)
	for _, rule := range staged {
		if rule.ID != id {
			filtered = append(filtered, rule)
		}
	}
	if err := SaveToFile(e.filePath, filtered); err != nil {
		return err
	}
	e.store.Replace(filtered)
	return nil
}

func (e *Engine) List() []Rule {
	return e.store.List()
}

func (e *Engine) Get(id string) (Rule, bool) {
	return e.store.Get(strings.TrimSpace(id))
}

func (e *Engine) Create(rule Rule) (Rule, error) {
	return e.create(rule, true)
}

func (e *Engine) create(rule Rule, strictID bool) (Rule, error) {
	e.mutationMu.Lock()
	defer e.mutationMu.Unlock()
	rule.ID = strings.TrimSpace(rule.ID)
	if _, exists := e.store.Get(rule.ID); exists {
		return Rule{}, contract.NewAPIError(contract.ErrorDuplicateID, "automation %q already exists", rule.ID)
	}
	rule = normalizeRule(rule)
	now := e.now().UTC()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	if strictID && containsSensitiveAction(rule.Actions) {
		rule.RequiresValidation = true
	}
	validator := validateStoredRule
	if strictID {
		validator = ValidateRule
	}
	if err := validator(rule); err != nil {
		return Rule{}, err
	}
	staged := append(e.store.List(), cloneRule(rule))
	if err := SaveToFile(e.filePath, staged); err != nil {
		return Rule{}, err
	}
	e.store.Replace(staged)
	return cloneRule(rule), nil
}

func (e *Engine) Patch(id string, patch AutomationPatch) (Rule, error) {
	e.mutationMu.Lock()
	defer e.mutationMu.Unlock()
	id = strings.TrimSpace(id)
	current, ok := e.store.Get(id)
	if !ok {
		return Rule{}, contract.NewAPIError(contract.ErrorNotFound, "automation %q not found", id)
	}
	updated := cloneRule(current)
	applyAutomationPatch(&updated, patch, e.now())
	if containsSensitiveAction(updated.Actions) {
		updated.RequiresValidation = true
	}
	if err := ValidateRule(updated); err != nil {
		return Rule{}, err
	}
	staged := e.store.List()
	for i := range staged {
		if staged[i].ID == id {
			staged[i] = cloneRule(updated)
		}
	}
	if err := SaveToFile(e.filePath, staged); err != nil {
		return Rule{}, err
	}
	e.store.Replace(staged)
	return cloneRule(updated), nil
}

func (e *Engine) SoftDelete(id string) (Rule, error) {
	e.mutationMu.Lock()
	defer e.mutationMu.Unlock()
	id = strings.TrimSpace(id)
	current, ok := e.store.Get(id)
	if !ok {
		return Rule{}, contract.NewAPIError(contract.ErrorNotFound, "automation %q not found", id)
	}
	if current.DeletedAt != nil {
		return current, nil
	}
	updated := cloneRule(current)
	now := e.now().UTC()
	updated.Enabled = false
	updated.enabledSet = true
	updated.Status = contract.AutomationStatusDisabled
	updated.DeletedAt = &now
	updated.UpdatedAt = now
	staged := e.store.List()
	for i := range staged {
		if staged[i].ID == id {
			staged[i] = cloneRule(updated)
		}
	}
	if err := SaveToFile(e.filePath, staged); err != nil {
		return Rule{}, err
	}
	e.store.Replace(staged)
	return cloneRule(updated), nil
}

func (e *Engine) DisableMissingTopologyNodes(valid map[string]bool) ([]Rule, error) {
	e.mutationMu.Lock()
	defer e.mutationMu.Unlock()
	staged := e.store.List()
	changed := false
	now := e.now().UTC()
	for i := range staged {
		nodeID := strings.TrimSpace(staged[i].Trigger.NodeID)
		if nodeID == "" {
			nodeID = strings.TrimSpace(staged[i].Node)
		}
		if nodeID == "" || nodeID == "unlocated" || valid[nodeID] {
			continue
		}
		staged[i].Enabled = false
		staged[i].enabledSet = true
		staged[i].ConfigError = topologyNodeMissing
		staged[i].UpdatedAt = now
		changed = true
	}
	if changed {
		if err := SaveToFile(e.filePath, staged); err != nil {
			return nil, err
		}
		e.store.Replace(staged)
	}
	return staged, nil
}

func (e *Engine) Save() error {
	e.mutationMu.Lock()
	defer e.mutationMu.Unlock()
	return SaveToFile(e.filePath, e.store.List())
}

func (e *Engine) Load() error {
	e.mutationMu.Lock()
	defer e.mutationMu.Unlock()
	rules, err := LoadFromFile(e.filePath)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(rules))
	for i := range rules {
		rules[i] = normalizeRule(rules[i])
		if _, duplicate := seen[rules[i].ID]; duplicate {
			return contract.NewAPIError(contract.ErrorDuplicateID, "duplicate automation id %q", rules[i].ID)
		}
		seen[rules[i].ID] = struct{}{}
		if err := validateStoredRule(rules[i]); err != nil {
			return err
		}
	}
	e.store.Replace(rules)
	return nil
}

func (e *Engine) Evaluate(event *contract.Event, decision *contract.Decision) []contract.Action {
	requests := e.EvaluateRequests(event, decision)
	out := make([]contract.Action, 0, len(requests))
	for _, request := range requests {
		out = append(out, request.Action)
	}
	return out
}

func (e *Engine) EvaluateRequests(event *contract.Event, decision *contract.Decision) []contract.ActionRequest {
	if event == nil || decision == nil {
		return nil
	}

	now := e.now()
	matched := make([]contract.ActionRequest, 0)
	for _, rule := range e.store.List() {
		rule = normalizeRule(rule)
		if !rule.Enabled || rule.DeletedAt != nil || rule.ConfigError != "" ||
			strings.EqualFold(rule.Status, contract.AutomationStatusDisabled) ||
			strings.EqualFold(rule.Status, contract.AutomationStatusRejected) {
			continue
		}
		if rule.RequiresValidation && !strings.EqualFold(rule.Status, contract.AutomationStatusApproved) {
			continue
		}
		if !matchesTrigger(rule, event, decision) {
			continue
		}
		if rule.Node != "" && rule.Node != decision.NodeID {
			continue
		}
		if rule.EventType != "" && rule.EventType != event.Type {
			continue
		}
		if decision.EffectiveScore < rule.MinScore {
			continue
		}
		if rule.Schedule != nil && !isWithinSchedule(rule.Schedule, now) {
			continue
		}
		if len(rule.Conditions) > 0 && !evaluateConditions(rule.Conditions, rule.ConditionLogic, *event, decision, now) {
			continue
		}
		ruleCooldownKey := "automation:" + rule.ID
		if rule.CooldownMs > 0 && e.cooldownActive(ruleCooldownKey, now) {
			continue
		}
		actions := append([]AutomationAction(nil), rule.Actions...)
		sort.SliceStable(actions, func(i, j int) bool {
			return actions[i].Order < actions[j].Order
		})
		generated := 0
		for _, action := range actions {
			if !action.Enabled {
				continue
			}
			actionCooldownKey := action.CooldownKey
			if actionCooldownKey != "" && e.cooldownActive(actionCooldownKey, now) {
				continue
			}
			if actionCooldownKey != "" && rule.CooldownMs > 0 {
				e.cooldowns[actionCooldownKey] = now.Add(time.Duration(rule.CooldownMs) * time.Millisecond)
			}
			matched = append(matched, actionRequest(rule, action, event, decision, now))
			generated++
		}
		if rule.CooldownMs > 0 && generated > 0 {
			e.cooldowns[ruleCooldownKey] = now.Add(time.Duration(rule.CooldownMs) * time.Millisecond)
		}
	}
	return matched
}

func (e *Engine) Process(event *contract.Event, decision *contract.Decision) []contract.Action {
	return e.Evaluate(event, decision)
}

func (e *Engine) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func matchesTrigger(rule Rule, event *contract.Event, decision *contract.Decision) bool {
	if rule.Trigger.EventType != "" && rule.Trigger.EventType != event.Type {
		return false
	}
	if rule.Trigger.State != "" && rule.Trigger.State != decision.State {
		return false
	}
	if rule.Trigger.SituationType != "" && rule.Trigger.SituationType != decision.Type {
		return false
	}
	if rule.Trigger.DeviceID != "" && rule.Trigger.DeviceID != event.DeviceID {
		return false
	}
	nodeID := firstNonEmpty(event.NodeID, decision.NodeID)
	if rule.Trigger.NodeID != "" && rule.Trigger.NodeID != nodeID {
		return false
	}
	if rule.Trigger.ResidentID != "" && rule.Trigger.ResidentID != event.Identity {
		return false
	}
	if decision.EffectiveScore < rule.Trigger.MinScore {
		return false
	}
	return true
}

func (e *Engine) cooldownActive(key string, now time.Time) bool {
	if key == "" {
		return false
	}
	until, ok := e.cooldowns[key]
	return ok && now.Before(until)
}

func actionRequest(rule Rule, action AutomationAction, event *contract.Event, decision *contract.Decision, now time.Time) contract.ActionRequest {
	legacy := legacyAction(action)
	actionID := action.ID
	if actionID == "" {
		actionID = idgen.New("act")
	}
	requestID := idgen.New("areq")
	clipID := firstNonEmpty(decision.ClipID, event.ClipID)
	nodeID := firstNonEmpty(event.NodeID, decision.NodeID, rule.Node)
	request := contract.ActionRequest{
		ID:             requestID,
		AutomationID:   rule.ID,
		ActionID:       actionID,
		Type:           firstNonEmpty(action.Type, legacy.Type),
		Target:         firstNonEmpty(action.Target, actionTarget(legacy)),
		Data:           actionData(legacy),
		SourceEventID:  event.ID,
		DecisionID:     decision.ID,
		SituationID:    decision.Type,
		ClipID:         clipID,
		NodeID:         nodeID,
		DeviceID:       event.DeviceID,
		CreatedAt:      now.UTC(),
		TimeoutMs:      action.TimeoutMs,
		RetryCount:     action.RetryCount,
		CooldownKey:    action.CooldownKey,
		Metadata:       simulationMetadata(event),
		Version:        "v1",
		RequestID:      requestID,
		CorrelationID:  firstNonEmpty(decision.ID, event.ID, requestID),
		Source:         "core",
		Timestamp:      now.UTC(),
		IdempotencyKey: fmt.Sprintf("%s:%s:%s", rule.ID, actionID, event.ID),
		Retry:          action.RetryCount,
		Action:         legacy,
	}
	if action.TimeoutMs == 0 {
		request.TimeoutMs = rule.TimeoutMs
	}
	if action.RetryCount == 0 {
		request.RetryCount = rule.RetryCount
		request.Retry = rule.RetryCount
	}
	if len(action.Data) > 0 {
		request.Data = cloneMap(action.Data)
	}
	if request.Data == nil {
		request.Data = map[string]any{}
	}
	if legacy.Command != "" {
		request.Data["command"] = legacy.Command
	}
	if legacy.Value != nil {
		request.Data["value"] = legacy.Value
	}
	if len(legacy.Residents) > 0 {
		request.Data["residents"] = append([]string(nil), legacy.Residents...)
	}
	if len(request.Data) == 0 {
		request.Data = nil
	}
	if rule.DryRun {
		if request.Metadata == nil {
			request.Metadata = map[string]any{}
		}
		request.Metadata["dry_run"] = true
	}
	return request
}

func simulationMetadata(event *contract.Event) map[string]any {
	if event == nil || event.Payload == nil {
		return nil
	}
	raw, ok := event.Payload["metadata"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := cloneMap(raw)
	if simulated, ok := out["simulated"].(bool); ok && simulated {
		return out
	}
	return out
}
