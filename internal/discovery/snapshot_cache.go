package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"synora/pkg/contract"
)

const snapshotHistoryLimit = 256

// SnapshotCache is a bounded read model fed by Core commit notifications.
// Discovery may serve it, but it is never a Store writer.
type SnapshotCache struct {
	mu          sync.RWMutex
	snapshot    []byte
	history     [][]byte
	subscribers map[chan contract.Event]struct{}
}

func NewSnapshotCache() *SnapshotCache {
	return &SnapshotCache{subscribers: make(map[chan contract.Event]struct{})}
}

func (c *SnapshotCache) Apply(message contract.Message) error {
	if c == nil {
		return errors.New("nil snapshot cache")
	}
	var payload struct {
		Snapshot json.RawMessage `json:"snapshot"`
	}
	if err := json.Unmarshal(message.Payload, &payload); err != nil {
		return err
	}
	if len(payload.Snapshot) == 0 {
		return errors.New("core snapshot notification has no snapshot")
	}
	var canonical any
	if err := json.Unmarshal(payload.Snapshot, &canonical); err != nil {
		return err
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return err
	}
	event := contract.Event{ID: message.ID, Type: message.Type, Source: message.Source, Timestamp: message.Timestamp, Payload: map[string]any{"snapshot": canonical}}
	c.mu.Lock()
	c.snapshot = append(c.snapshot[:0], encoded...)
	entry, _ := json.Marshal(map[string]any{"revision": message.Revision, "snapshot": canonical, "timestamp": message.Timestamp})
	c.history = append(c.history, entry)
	if len(c.history) > snapshotHistoryLimit {
		c.history = c.history[len(c.history)-snapshotHistoryLimit:]
	}
	for subscriber := range c.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
	c.mu.Unlock()
	return nil
}

func (c *SnapshotCache) SnapshotJSON() ([]byte, error) {
	if c == nil {
		return nil, errors.New("snapshot cache unavailable")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.snapshot) == 0 {
		return []byte(`{"schema_version":"core-snapshot/v1","status":"unavailable"}`), nil
	}
	return append([]byte(nil), c.snapshot...), nil
}

func (c *SnapshotCache) HistoryJSON() ([]byte, error) {
	if c == nil {
		return nil, errors.New("snapshot cache unavailable")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	items := make([]json.RawMessage, len(c.history))
	for i := range c.history {
		items[i] = append(json.RawMessage(nil), c.history[i]...)
	}
	return json.Marshal(items)
}

// Clear removes the bounded read model after a successful data reset. It does
// not alter Core state or configuration files.
func (c *SnapshotCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.snapshot = nil
	c.history = nil
	c.mu.Unlock()
}

func (c *SnapshotCache) Subscribe(ctx context.Context) <-chan contract.Event {
	channel := make(chan contract.Event, 1)
	if c == nil {
		close(channel)
		return channel
	}
	c.mu.Lock()
	c.subscribers[channel] = struct{}{}
	c.mu.Unlock()
	go func() {
		<-ctx.Done()
		c.mu.Lock()
		delete(c.subscribers, channel)
		close(channel)
		c.mu.Unlock()
	}()
	return channel
}
