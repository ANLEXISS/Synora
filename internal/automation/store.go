package automation

import "sort"

func (s *Store) Add(rule Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rule = normalizeRule(rule)
	s.rules[rule.ID] = cloneRule(rule)
	return nil
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rules, id)
	return nil
}

func (s *Store) Replace(rules []Rule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]Rule, len(rules))
	for _, rule := range rules {
		rule = normalizeRule(rule)
		next[rule.ID] = cloneRule(rule)
	}
	s.rules = next
}

func (s *Store) Get(id string) (Rule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rule, ok := s.rules[id]
	if !ok {
		return Rule{}, false
	}
	return cloneRule(rule), true
}

func normalizeRule(rule Rule) Rule {
	if rule.ConditionLogic == "" {
		rule.ConditionLogic = "all"
	}
	if rule.Trigger.EventType == "" {
		rule.Trigger.EventType = rule.EventType
	}
	if rule.Trigger.State == "" {
		rule.Trigger.State = rule.State
	}
	if rule.Trigger.NodeID == "" {
		rule.Trigger.NodeID = rule.Node
	}
	if rule.Trigger.MinScore == 0 {
		rule.Trigger.MinScore = rule.MinScore
	}
	if rule.EventType == "" {
		rule.EventType = rule.Trigger.EventType
	}
	if rule.State == "" {
		rule.State = rule.Trigger.State
	}
	if rule.Node == "" {
		rule.Node = rule.Trigger.NodeID
	}
	if rule.MinScore == 0 {
		rule.MinScore = rule.Trigger.MinScore
	}
	for i := range rule.Actions {
		legacy := legacyAction(rule.Actions[i])
		if rule.Actions[i].Type == "" {
			rule.Actions[i].Type = legacy.Type
		}
		if rule.Actions[i].Target == "" {
			rule.Actions[i].Target = actionTarget(legacy)
		}
		if rule.Actions[i].RetryCount == 0 {
			rule.Actions[i].RetryCount = rule.Actions[i].Retry
		}
		if rule.Actions[i].TimeoutMs == 0 {
			rule.Actions[i].TimeoutMs = rule.TimeoutMs
		}
		if rule.Actions[i].RetryCount == 0 {
			rule.Actions[i].RetryCount = rule.RetryCount
		}
	}
	return rule
}

func (s *Store) List() []Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.rules))
	for id := range s.rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Rule, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneRule(s.rules[id]))
	}
	return out
}

func cloneRule(rule Rule) Rule {
	rule.Conditions = append([]Condition(nil), rule.Conditions...)
	for i := range rule.Conditions {
		// Values are configuration JSON/YAML values. Marshal-free shallow copying
		// is sufficient for scalars; maps and slices receive their own top level.
		switch value := rule.Conditions[i].Value.(type) {
		case map[string]any:
			rule.Conditions[i].Value = cloneMap(value)
		case []any:
			rule.Conditions[i].Value = append([]any(nil), value...)
		case []string:
			rule.Conditions[i].Value = append([]string(nil), value...)
		}
	}
	rule.Actions = append([]AutomationAction(nil), rule.Actions...)
	for i := range rule.Actions {
		rule.Actions[i].Data = cloneMap(rule.Actions[i].Data)
		rule.Actions[i].Residents = append([]string(nil), rule.Actions[i].Residents...)
	}
	if rule.Schedule != nil {
		schedule := *rule.Schedule
		rule.Schedule = &schedule
	}
	if rule.DeletedAt != nil {
		deletedAt := *rule.DeletedAt
		rule.DeletedAt = &deletedAt
	}
	return rule
}
