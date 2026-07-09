package automation

func (s *Store) Add(rule Rule) error {

	s.mu.Lock()
	defer s.mu.Unlock()

	rule = normalizeRule(rule)
	s.rules[rule.ID] = rule

	return nil
}

func (s *Store) Remove(id string) error {

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.rules, id)

	return nil
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
	if rule.EventType == "" {
		rule.EventType = rule.Trigger.EventType
	}
	if rule.State == "" {
		rule.State = rule.Trigger.State
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
	}
	return rule
}

func (s *Store) List() []Rule {

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Rule, 0, len(s.rules))

	for _, r := range s.rules {
		out = append(out, r)
	}

	return out
}
