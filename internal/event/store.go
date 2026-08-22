package event

import (
	"sync"
	"time"

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

func (s *Store) Load(events []*contract.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = make([]*contract.Event, s.limit)
	s.index = 0
	s.full = false
	for _, event := range events {
		if event == nil {
			continue
		}
		cloned := *event
		if event.Payload != nil {
			cloned.Payload = cloneMap(event.Payload)
		}
		s.events[s.index] = &cloned
		s.index++
		if s.index >= s.limit {
			s.index = 0
			s.full = true
		}
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

// Prune removes events older than maxAge in a deterministic pass. Event store
// ownership remains in Core; this method only bounds the in-memory projection.
func (s *Store) Prune(now time.Time, maxAge time.Duration) int {
	if s == nil || maxAge <= 0 {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	current := s.List()
	kept := make([]*contract.Event, 0, len(current))
	removed := 0
	for _, event := range current {
		if event != nil && !event.Timestamp.IsZero() && now.Sub(event.Timestamp.UTC()) >= maxAge {
			removed++
			continue
		}
		kept = append(kept, event)
	}
	if removed > 0 {
		s.Load(kept)
	}
	return removed
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneMap(nested)
			continue
		}
		out[key] = value
	}
	return out
}
