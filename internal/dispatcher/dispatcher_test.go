package dispatcher

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"synora/internal/outbox"
	"synora/pkg/contract"
)

type fakeSender struct {
	mu       sync.Mutex
	messages []contract.Message
	failures int
	wake     chan struct{}
}

func (s *fakeSender) Send(message contract.Message) error {
	s.mu.Lock()
	s.messages = append(s.messages, message)
	if s.wake != nil {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	if s.failures > 0 {
		s.failures--
		s.mu.Unlock()
		return errors.New("transport unavailable")
	}
	s.mu.Unlock()
	return nil
}

func (s *fakeSender) Messages() []contract.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]contract.Message(nil), s.messages...)
}

func TestDispatcherLeavesSuccessfulAttemptInFlightUntilAck(t *testing.T) {
	store := openTestOutbox(t)
	message := testMessage("msg-1")
	if err := store.Enqueue(message); err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{wake: make(chan struct{}, 4)}
	d, err := New(store, sender, Config{Interval: time.Millisecond, AckTimeout: time.Second, BatchSize: 1, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxAttempts: 3, Jitter: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(sender.Messages()) == 1 })
	record, err := store.Get(message.DeliveryIdentity())
	if err != nil || record.State != contract.DeliveryInFlight || record.Attempts != 1 {
		t.Fatalf("record after send=%#v err=%v", record, err)
	}
	if err := d.HandleAck(contract.DeliveryAck{Identity: message.DeliveryIdentity(), State: contract.DeliveryAcknowledged}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		record, _ := store.Get(message.DeliveryIdentity())
		return record.State == contract.DeliveryAcknowledged
	})
	if err := d.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(sender.Messages()) != 1 {
		t.Fatalf("ACKed message was sent more than once: %#v", sender.Messages())
	}
}

func TestDispatcherRetriesTransportFailureAndUsesSameIdentity(t *testing.T) {
	store := openTestOutbox(t)
	message := testMessage("msg-1")
	if err := store.Enqueue(message); err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{failures: 1, wake: make(chan struct{}, 8)}
	d, err := New(store, sender, Config{Interval: time.Millisecond, AckTimeout: time.Second, BatchSize: 1, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxAttempts: 3, Jitter: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(sender.Messages()) >= 2 })
	messages := sender.Messages()
	if messages[0].ID != messages[1].ID || messages[0].Sequence != messages[1].Sequence {
		t.Fatalf("retry changed delivery identity: %#v", messages)
	}
	if err := d.HandleAck(contract.DeliveryAck{Identity: message.DeliveryIdentity(), State: contract.DeliveryAcknowledged}); err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(); err != nil {
		t.Fatal(err)
	}
	record, err := store.Get(message.DeliveryIdentity())
	if err != nil || record.State != contract.DeliveryAcknowledged || record.Attempts != 2 {
		t.Fatalf("record after retry=%#v err=%v", record, err)
	}
}

func TestDispatcherReplaysAfterLostAckWithoutChangingMessageID(t *testing.T) {
	store := openTestOutbox(t)
	message := testMessage("msg-1")
	if err := store.Enqueue(message); err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{wake: make(chan struct{}, 8)}
	d, err := New(store, sender, Config{Interval: time.Millisecond, AckTimeout: 5 * time.Millisecond, BatchSize: 1, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxAttempts: 3, Jitter: 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(sender.Messages()) >= 2 })
	messages := sender.Messages()
	if messages[0].ID != messages[1].ID {
		t.Fatalf("replay changed message ID: %#v", messages)
	}
	if err := d.HandleAck(contract.DeliveryAck{Identity: message.DeliveryIdentity(), State: contract.DeliveryAcknowledged}); err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestDispatcherStopsContextAwareTransport(t *testing.T) {
	store := openTestOutbox(t)
	message := testMessage("msg-1")
	if err := store.Enqueue(message); err != nil {
		t.Fatal(err)
	}
	sender := &blockingSender{started: make(chan struct{})}
	d, err := New(store, sender, Config{Interval: time.Millisecond, AckTimeout: time.Second, BatchSize: 1, MaxAttempts: 2, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		select {
		case <-sender.started:
			return true
		default:
			return false
		}
	})
	if err := d.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestBackoffIsExponentialAndBounded(t *testing.T) {
	base, maximum := 10*time.Millisecond, 50*time.Millisecond
	if got := Backoff(1, base, maximum, 0, nil); got != base {
		t.Fatalf("first backoff=%s", got)
	}
	if got := Backoff(3, base, maximum, 0, nil); got != 40*time.Millisecond {
		t.Fatalf("third backoff=%s", got)
	}
	if got := Backoff(9, base, maximum, 0, nil); got != maximum {
		t.Fatalf("bounded backoff=%s", got)
	}
	if got := Backoff(1, base, maximum, 0.2, func() float64 { return 1 }); got > maximum {
		t.Fatalf("jitter escaped maximum: %s", got)
	}
}

func TestDispatcherRejectsInvalidAck(t *testing.T) {
	store := openTestOutbox(t)
	sender := &fakeSender{}
	d, err := New(store, sender, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.HandleAck(contract.DeliveryAck{State: contract.DeliveryAcknowledged}); err == nil {
		t.Fatal("invalid ACK was accepted")
	}
}

type blockingSender struct {
	started chan struct{}
	once    sync.Once
}

func (s *blockingSender) Send(contract.Message) error { return errors.New("unused") }

func (s *blockingSender) SendContext(ctx context.Context, _ contract.Message) error {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return ctx.Err()
}

func openTestOutbox(t *testing.T) *outbox.Store {
	t.Helper()
	store, err := outbox.Open(filepath.Join(t.TempDir(), "outbox.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testMessage(id string) contract.Message {
	return contract.Message{ID: id, Epoch: "epoch-1", Sequence: 1, Revision: 1, Type: contract.EventVisionMotion, Kind: contract.KindEvent, Source: "core"}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
