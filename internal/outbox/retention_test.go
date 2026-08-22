package outbox

import (
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestCompactTerminalBeforePreservesAtLeastOnceRecords(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store, err := Open(filepath.Join(t.TempDir(), "outbox.json"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	old := testMessage("old", 1)
	fresh := testMessage("fresh", 2)
	pending := testMessage("pending", 3)
	for _, message := range []contract.Message{old, fresh, pending} {
		if err := store.Enqueue(message); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.MarkInFlight(old.DeliveryIdentity()); err != nil {
		t.Fatal(err)
	}
	if err := store.Ack(contract.DeliveryAck{Identity: old.DeliveryIdentity(), State: contract.DeliveryAcknowledged}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkInFlight(fresh.DeliveryIdentity()); err != nil {
		t.Fatal(err)
	}
	if err := store.Ack(contract.DeliveryAck{Identity: fresh.DeliveryIdentity(), State: contract.DeliveryAcknowledged}); err != nil {
		t.Fatal(err)
	}
	// Make the old terminal record eligible without changing the pending one.
	store.mu.Lock()
	oldRecord := store.records[old.DeliveryIdentity().Key()]
	oldRecord.UpdatedAt = now.Add(-2 * time.Hour)
	store.records[old.DeliveryIdentity().Key()] = oldRecord
	store.mu.Unlock()
	if err := store.CompactTerminalBefore(now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(old.DeliveryIdentity()); err != ErrNotFound {
		t.Fatalf("old terminal record still present: %v", err)
	}
	if _, err := store.Get(fresh.DeliveryIdentity()); err != nil {
		t.Fatalf("fresh terminal record removed: %v", err)
	}
	if _, err := store.Get(pending.DeliveryIdentity()); err != nil {
		t.Fatalf("pending delivery removed: %v", err)
	}
}
