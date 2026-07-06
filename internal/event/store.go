package event

import (
	"sync"
	"synora/pkg/contract"
)

type Store struct {
	mu     sync.RWMutex
	events []*contract.Event
	limit  int
	index  int
	full   bool
}

func NewStore(limit int) *Store {
	return &Store{
		events: make([]*contract.Event, limit),
		limit:  limit,
	}
}

func (s *Store) Add(e *contract.Event) {

	s.mu.Lock()
	defer s.mu.Unlock()

	s.events[s.index] = e
	s.index++

	if s.index >= s.limit {
		s.index = 0
		s.full = true
	}
}

func (s *Store) List() []*contract.Event {

	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []*contract.Event

	if !s.full {
		out = make([]*contract.Event, s.index)
		copy(out, s.events[:s.index])
		return out
	}

	// ordre chronologique
	out = make([]*contract.Event, s.limit)

	n := copy(out, s.events[s.index:])
	copy(out[n:], s.events[:s.index])

	return out
}
