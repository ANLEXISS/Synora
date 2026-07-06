package automation

func (s *Store) Add(rule Rule) error {

	s.mu.Lock()
	defer s.mu.Unlock()

	s.rules[rule.ID] = rule

	return nil
}

func (s *Store) Remove(id string) error {

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.rules, id)

	return nil
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
