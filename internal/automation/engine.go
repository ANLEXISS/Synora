package automation

import (
	"fmt"
	"sort"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

type Engine struct {
	store     *Store
	filePath  string
	Now       func() time.Time
	cooldowns map[string]time.Time
}

func NewEngine(path string, _ ...interface{}) *Engine {
	return &Engine{
		store:     &Store{rules: make(map[string]Rule)},
		filePath:  path,
		cooldowns: make(map[string]time.Time),
	}
}

func (e *Engine) Add(rule Rule) error {
	if err := e.store.Add(rule); err != nil {
		return err
	}
	return e.Save()
}

func (e *Engine) Remove(id string) error {
	if err := e.store.Remove(id); err != nil {
		return err
	}
	return e.Save()
}

func (e *Engine) List() []Rule {
	return e.store.List()
}

func (e *Engine) Save() error {
	return SaveToFile(e.filePath, e.store.List())
}

func (e *Engine) Load() error {
	rules, err := LoadFromFile(e.filePath)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		_ = e.store.Add(rule)
	}
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
		if !rule.Enabled {
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
