package discovery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestSnapshotCacheIsBoundedAndPublishesCanonicalUpdates(t *testing.T) {
	cache := NewSnapshotCache()
	updates := cache.Subscribe(context.Background())
	for i := 0; i < snapshotHistoryLimit+4; i++ {
		body, _ := json.Marshal(map[string]any{"snapshot": map[string]any{"revision": i, "topology": "unknown"}})
		if err := cache.Apply(contract.Message{ID: "snapshot", Type: "core.snapshot", Source: "core", Timestamp: time.Unix(int64(i), 0).UTC(), Revision: uint64(i), Payload: body}); err != nil {
			t.Fatal(err)
		}
	}
	history, err := cache.HistoryJSON()
	if err != nil {
		t.Fatal(err)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(history, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != snapshotHistoryLimit {
		t.Fatalf("history length=%d, want %d", len(entries), snapshotHistoryLimit)
	}
	select {
	case event := <-updates:
		if event.Type != "core.snapshot" || IsForbiddenValue(event.Payload) {
			t.Fatalf("unexpected update: %#v", event)
		}
	default:
		t.Fatal("snapshot update was not published")
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := cache.Subscribe(ctx)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			select {
			case _, ok = <-ch:
				if ok {
					t.Fatal("subscription remained open")
				}
			default:
			}
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not close")
	}
}
