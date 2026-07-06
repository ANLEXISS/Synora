package actions

import "sync"

type Deduper struct {
	mu sync.Mutex

	seen map[string]struct{}
}

func NewDeduper() *Deduper {
	return &Deduper{
		seen: map[string]struct{}{},
	}
}

func (d *Deduper) SeenOrAdd(key string) bool {
	if key == "" {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.seen[key]; ok {
		return true
	}

	d.seen[key] = struct{}{}
	return false
}
