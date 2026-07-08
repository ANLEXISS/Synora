package automation

import (
	"time"

	"synora/pkg/contract"
)

type Engine struct {
	store    *Store
	filePath string
	Now      func() time.Time
}

func NewEngine(path string, _ ...interface{}) *Engine {
	return &Engine{
		store:    &Store{rules: make(map[string]Rule)},
		filePath: path,
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
	if event == nil || decision == nil {
		return nil
	}

	now := e.now()
	matched := make([]contract.Action, 0)
	for _, rule := range e.store.List() {
		if rule.State != "" && rule.State != decision.State {
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
		if len(rule.Conditions) > 0 && !evaluateConditions(rule.Conditions, *event, decision, now) {
			continue
		}
		matched = append(matched, rule.Actions...)
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
