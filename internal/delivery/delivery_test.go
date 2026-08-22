package delivery

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"synora/internal/bus"
	"synora/internal/dispatcher"
	"synora/internal/outbox"
	"synora/pkg/contract"
)

func TestDeliveryACKCodecCarriesOnlyDeliveryMetadata(t *testing.T) {
	original := deliveryMessage("msg-1", "epoch-1", 1, "incident.created")
	ack, err := NewAckMessage("vision", original, contract.DeliveryAck{Identity: original.DeliveryIdentity(), State: contract.DeliveryAcknowledged, Code: "stored"})
	if err != nil {
		t.Fatal(err)
	}
	if ack.Target != original.Source || ack.CorrelationID != original.ID || ack.Type != AckMessageType {
		t.Fatalf("ACK routing metadata=%#v", ack)
	}
	if string(ack.Payload) == string(original.Payload) {
		t.Fatal("ACK unexpectedly copied business payload")
	}
	decoded, err := DecodeAck(ack)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Identity != original.DeliveryIdentity() || decoded.State != contract.DeliveryAcknowledged {
		t.Fatalf("decoded ACK=%#v", decoded)
	}
}

func TestUnixBusOutboxDispatcherPreservesIncidentBeforeClipAndACKs(t *testing.T) {
	h := newDeliveryBusHarness(t)
	defer h.close()

	config := dispatcher.Config{Interval: time.Millisecond, AckTimeout: time.Second, BatchSize: 8, MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond, Jitter: 0}
	publisher, err := New(filepath.Join(h.root, "outbox.json"), h.core, config, h.core)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	receiverDone := h.startReceiver(true)
	incident := deliveryMessage("msg-incident", "epoch-1", 1, "incident.created")
	clip := deliveryMessage("msg-clip", "epoch-1", 2, "clip.available")
	if err := publisher.Publish(incident); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(clip); err != nil {
		t.Fatal(err)
	}
	waitForDelivery(t, func() bool {
		first, second := h.received()
		return first == incident.ID && second == clip.ID && acknowledged(publisher.Store, incident) && acknowledged(publisher.Store, clip)
	})
	if err := publisher.Stop(); err != nil {
		t.Fatal(err)
	}
	h.stopReceiver()
	<-receiverDone
	if got := publisher.Store.Snapshot(); len(got) != 2 {
		t.Fatalf("unexpected outbox snapshot: %#v", got)
	}
}

func TestDeliveryReopensPendingAfterDispatcherCrashAndReplaysSameIdentity(t *testing.T) {
	h := newDeliveryBusHarness(t)
	defer h.close()
	path := filepath.Join(h.root, "outbox.json")
	message := deliveryMessage("msg-replay", "epoch-old", 1, "incident.created")
	config := dispatcher.Config{Interval: time.Millisecond, AckTimeout: 5 * time.Millisecond, BatchSize: 1, MaxAttempts: 4, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond, Jitter: 0}
	first, err := New(path, h.core, config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	receiverDone := h.startReceiver(false)
	if err := first.Publish(message); err != nil {
		t.Fatal(err)
	}
	waitForDelivery(t, func() bool { return h.receivedCount() == 1 })
	if err := first.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := first.Store.Close(); err != nil {
		t.Fatal(err)
	}
	h.stopReceiver()
	<-receiverDone

	restarted, err := New(path, h.core, config, h.core)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	receiverDone = h.startReceiver(true)
	waitForDelivery(t, func() bool {
		return h.receivedCount() >= 2 && acknowledged(restarted.Store, message)
	})
	if err := restarted.Stop(); err != nil {
		t.Fatal(err)
	}
	h.stopReceiver()
	<-receiverDone
	received := h.receivedIDs()
	if len(received) < 2 || received[0] != message.ID || received[1] != message.ID {
		t.Fatalf("replay identities=%v", received)
	}
}

func TestDeliveryRejectsOldOrDuplicateACKWithoutCreatingRecords(t *testing.T) {
	store, err := outbox.Open(filepath.Join(t.TempDir(), "outbox.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	d, err := dispatcher.New(store, &noopSender{}, dispatcher.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	old := deliveryMessage("old", "epoch-1", 1, "incident.created")
	if err := store.Enqueue(old); err != nil {
		t.Fatal(err)
	}
	if err := d.HandleAck(contract.DeliveryAck{Identity: deliveryMessage("missing", "epoch-0", 1, "x").DeliveryIdentity(), State: contract.DeliveryAcknowledged}); err == nil {
		t.Fatal("ACK from an old/unknown epoch was accepted")
	}
	if got := store.Snapshot(); len(got) != 1 || got[0].State != contract.DeliveryPending {
		t.Fatalf("unknown ACK changed outbox: %#v", got)
	}
	if err := store.Enqueue(old); err != nil {
		t.Fatal("duplicate enqueue should be idempotent: ", err)
	}
	if got := store.Snapshot(); len(got) != 1 {
		t.Fatalf("duplicate enqueue created a second record: %#v", got)
	}
}

type noopSender struct{}

func (noopSender) Send(contract.Message) error { return nil }

type deliveryBusHarness struct {
	t      *testing.T
	root   string
	socket string
	server *bus.Server
	core   *bus.Client
	vision *bus.Client

	mu               sync.Mutex
	receivedMessages []contract.Message
	receiverCancel   context.CancelFunc
	receiverDone     <-chan struct{}
}

func newDeliveryBusHarness(t *testing.T) *deliveryBusHarness {
	t.Helper()
	root := t.TempDir()
	socket := filepath.Join(root, "bus.sock")
	server := bus.NewServer(socket)
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Start() }()
	waitForDelivery(t, func() bool {
		_, err := os.Stat(socket)
		return err == nil
	})
	select {
	case err := <-serverErr:
		t.Fatalf("bus stopped: %v", err)
	default:
	}
	core, err := bus.NewClient(socket, "core")
	if err != nil {
		t.Fatal(err)
	}
	vision, err := bus.NewClient(socket, "vision")
	if err != nil {
		_ = core.Close()
		_ = server.Close()
		t.Fatal(err)
	}
	return &deliveryBusHarness{t: t, root: root, socket: socket, server: server, core: core, vision: vision}
}

func (h *deliveryBusHarness) close() {
	h.stopReceiver()
	if h.core != nil {
		_ = h.core.Close()
	}
	if h.vision != nil {
		_ = h.vision.Close()
	}
	if h.server != nil {
		_ = h.server.Close()
	}
}

func (h *deliveryBusHarness) startReceiver(ack bool) <-chan struct{} {
	h.stopReceiver()
	ctx, cancel := context.WithCancel(context.Background())
	h.receiverCancel = cancel
	done := make(chan struct{})
	h.receiverDone = done
	go func() {
		defer close(done)
		channel := h.vision.SubscribeChannel("")
		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-channel:
				if !ok {
					return
				}
				h.mu.Lock()
				h.receivedMessages = append(h.receivedMessages, message)
				h.mu.Unlock()
				if ack {
					ackMessage, err := NewAckMessage("vision", message, contract.DeliveryAck{Identity: message.DeliveryIdentity(), State: contract.DeliveryAcknowledged, Code: "stored"})
					if err == nil {
						_ = h.vision.Send(ackMessage)
					}
				}
			}
		}
	}()
	return done
}

func (h *deliveryBusHarness) stopReceiver() {
	if h.receiverCancel != nil {
		cancel, done := h.receiverCancel, h.receiverDone
		h.receiverCancel = nil
		h.receiverDone = nil
		cancel()
		if done != nil {
			<-done
		}
	}
}

func (h *deliveryBusHarness) received() (string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.receivedMessages) < 2 {
		return "", ""
	}
	return h.receivedMessages[0].ID, h.receivedMessages[1].ID
}

func (h *deliveryBusHarness) receivedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.receivedMessages)
}

func (h *deliveryBusHarness) receivedIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]string, 0, len(h.receivedMessages))
	for _, message := range h.receivedMessages {
		ids = append(ids, message.ID)
	}
	return ids
}

func acknowledged(store *outbox.Store, message contract.Message) bool {
	record, err := store.Get(message.DeliveryIdentity())
	return err == nil && record.State == contract.DeliveryAcknowledged
}

func deliveryMessage(id, epoch string, sequence uint64, eventType string) contract.Message {
	return contract.Message{ID: id, Epoch: epoch, Sequence: sequence, Revision: sequence, Type: eventType, Kind: contract.KindEvent, Source: "core", Target: "vision", Payload: []byte(`{"incident_id":"bounded"}`)}
}

func waitForDelivery(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("delivery condition did not become true")
}
