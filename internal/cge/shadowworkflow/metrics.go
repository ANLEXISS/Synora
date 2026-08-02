package shadowworkflow

import "sync"

type metricCounter struct {
	mu     sync.Mutex
	values map[string]uint64
}

func newMetricCounter() *metricCounter { return &metricCounter{values: map[string]uint64{}} }
func (m *metricCounter) add(code string) {
	m.addN(code, 1)
}
func (m *metricCounter) addN(code string, value uint64) {
	if m == nil || code == "" {
		return
	}
	m.mu.Lock()
	m.values[code] += value
	m.mu.Unlock()
}
func (m *metricCounter) set(code string, value uint64) {
	if m == nil || code == "" {
		return
	}
	m.mu.Lock()
	m.values[code] = value
	m.mu.Unlock()
}
func (m *metricCounter) snapshot() map[string]uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]uint64, len(m.values))
	for k, v := range m.values {
		out[k] = v
	}
	return out
}
