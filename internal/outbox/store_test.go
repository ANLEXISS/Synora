package outbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestStorePersistsPendingAndRestoresAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delivery", "outbox.json")
	now := time.Date(2026, 8, 22, 18, 0, 0, 0, time.UTC)
	message := testMessage("msg-1", 1)
	store, err := Open(path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("open outbox: %v", err)
	}
	if err := store.Enqueue(message); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := store.MarkInFlight(message.DeliveryIdentity()); err != nil {
		t.Fatalf("mark in flight: %v", err)
	}
	if err := store.MarkRetry(message.DeliveryIdentity(), "bus unavailable", now.Add(time.Minute)); err != nil {
		t.Fatalf("mark retry: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	restored, err := Open(path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("reopen outbox: %v", err)
	}
	if got := restored.Ready(now, 10); len(got) != 0 {
		t.Fatalf("retry before deadline returned %d records", len(got))
	}
	ready := restored.Ready(now.Add(time.Minute), 10)
	if len(ready) != 1 || ready[0].State != contract.DeliveryRetryWait || ready[0].Attempts != 1 {
		t.Fatalf("restored record=%#v", ready)
	}
}

func TestStoreAcknowledgementIsIdempotentAndCompactionIsDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.json")
	store, err := Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	message := testMessage("msg-1", 1)
	if err := store.Enqueue(message); err != nil {
		t.Fatal(err)
	}
	identity := message.DeliveryIdentity()
	if err := store.MarkInFlight(identity); err != nil {
		t.Fatal(err)
	}
	ack := contract.DeliveryAck{Identity: identity, State: contract.DeliveryAcknowledged, Code: "accepted"}
	if err := store.Ack(ack); err != nil {
		t.Fatal(err)
	}
	if err := store.Ack(ack); err != nil {
		t.Fatalf("duplicate ACK should be idempotent: %v", err)
	}
	if err := store.CompactAcknowledged(); err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot(); len(got) != 0 {
		t.Fatalf("acknowledged record not compacted: %#v", got)
	}
	restored, err := Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.Snapshot(); len(got) != 0 {
		t.Fatalf("compaction was not durable: %#v", got)
	}
}

func TestStoreReturnsBoundedCopies(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "outbox.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	message := testMessage("msg-1", 1)
	message.Payload = []byte(`{"safe":true}`)
	if err := store.Enqueue(message); err != nil {
		t.Fatal(err)
	}
	got := store.Ready(time.Now(), 1)
	if len(got) != 1 {
		t.Fatalf("ready=%#v", got)
	}
	got[0].Message.Payload[0] = 'X'
	got[0].Message.ID = "mutated"
	snapshot := store.Snapshot()
	if string(snapshot[0].Message.Payload) != `{"safe":true}` || snapshot[0].Message.ID != "msg-1" {
		t.Fatalf("caller mutated durable record: %#v", snapshot[0])
	}
	if got := store.Ready(time.Now(), 0); len(got) != 0 {
		t.Fatal("zero limit must be bounded to an empty result")
	}
}

func TestStoreFailsClosedOnCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.json")
	if err := os.WriteFile(path, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, time.Now); err == nil {
		t.Fatal("corrupt outbox must fail closed")
	}
}

func TestStoreRollsBackMemoryWhenAtomicWriteFails(t *testing.T) {
	root := t.TempDir()
	store := &Store{path: root, records: make(map[string]contract.DeliveryRecord), now: time.Now}
	message := testMessage("msg-1", 1)
	if err := store.Enqueue(message); err == nil {
		t.Fatal("write through a file parent must fail")
	}
	if !errors.Is(storeErr(store, message.DeliveryIdentity()), ErrNotFound) {
		t.Fatal("failed write left an in-memory record")
	}
}

func storeErr(store *Store, identity contract.DeliveryIdentity) error {
	_, err := store.Get(identity)
	return err
}

func testMessage(id string, sequence uint64) contract.Message {
	return contract.Message{ID: id, Epoch: "epoch-1", Sequence: sequence, Revision: sequence, Type: contract.EventVisionMotion, Kind: contract.KindEvent, Source: "core"}
}
