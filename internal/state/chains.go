package state

import (
	"encoding/json"

	"synora/pkg/contract"
)

func (s *Store) SetEventChain(value *contract.EventChain) {
	if s == nil || value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	s.EventChains[value.ID] = cloneEventChain(value)
	s.mu.Unlock()
}

func (s *Store) EventChain(id string) (*contract.EventChain, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.EventChains[id]
	return cloneEventChain(value), ok && value != nil
}

func (s *Store) EventChainsList() []*contract.EventChain {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*contract.EventChain, 0, len(s.EventChains))
	for _, value := range s.EventChains {
		if value != nil {
			items = append(items, cloneEventChain(value))
		}
	}
	return items
}

func (s *Store) DeleteEventChain(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.EventChains, id)
	s.mu.Unlock()
}

func (s *Store) SetCriticalChainMemory(value *contract.CriticalChainMemory) {
	if s == nil || value == nil || value.ID == "" {
		return
	}
	s.mu.Lock()
	s.CriticalChains[value.ID] = cloneCriticalChainMemory(value)
	s.mu.Unlock()
}

func (s *Store) CriticalChainMemory(id string) (*contract.CriticalChainMemory, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.CriticalChains[id]
	return cloneCriticalChainMemory(value), ok && value != nil
}

func (s *Store) CriticalChainMemoriesList() []*contract.CriticalChainMemory {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*contract.CriticalChainMemory, 0, len(s.CriticalChains))
	for _, value := range s.CriticalChains {
		if value != nil {
			items = append(items, cloneCriticalChainMemory(value))
		}
	}
	return items
}

func (s *Store) DeleteCriticalChainMemory(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.CriticalChains, id)
	s.mu.Unlock()
}

func cloneEventChain(value *contract.EventChain) *contract.EventChain {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var cloned contract.EventChain
	if json.Unmarshal(data, &cloned) != nil {
		return nil
	}
	return &cloned
}

func cloneCriticalChainMemory(value *contract.CriticalChainMemory) *contract.CriticalChainMemory {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var cloned contract.CriticalChainMemory
	if json.Unmarshal(data, &cloned) != nil {
		return nil
	}
	cloned = contract.NormalizeCriticalChainMemory(cloned)
	return &cloned
}
