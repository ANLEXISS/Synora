package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"synora/internal/bus"
	"synora/pkg/contract"
)

type realtimeTestBus struct {
	messages chan contract.Message
}

func newRealtimeTestBus() *realtimeTestBus {
	return &realtimeTestBus{messages: make(chan contract.Message, 32)}
}

func (b *realtimeTestBus) SubscribeChannel(string) <-chan contract.Message { return b.messages }

func (b *realtimeTestBus) publish(message contract.Message) { b.messages <- message }

func TestWebSocketRealtimeUsesUnixBusFromCoreToAPI(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "bus.sock")
	busServer := bus.NewServer(socketPath)
	serverDone := make(chan error, 1)
	go func() { serverDone <- busServer.Start() }()
	waitFor(t, time.Second, func() bool {
		_, err := os.Stat(socketPath)
		return err == nil
	})
	defer busServer.Close()

	coreClient, err := bus.NewClient(socketPath, "core")
	if err != nil {
		t.Fatalf("core bus client: %v", err)
	}
	defer coreClient.Close()
	apiClient, err := bus.NewClient(socketPath, "api")
	if err != nil {
		t.Fatalf("api bus client: %v", err)
	}
	defer apiClient.Close()

	hub := newWebSocketHub(&dynamicStateCore{state: emptyPublicSnapshot()})
	defer hub.Close()
	go hub.observeBus(apiClient)
	server := newIPv4TestServer(t, hub)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close()
	discardInitial(t, conn)

	incident := contract.Incident{ID: "incident-unix-bus", Status: contract.IncidentStatusNew}
	if err := coreClient.Send(contract.Message{
		Type: "incident.created", Kind: contract.KindEvent, Source: "core",
		Epoch: "core-unix-epoch", Sequence: 1, Revision: 9,
		Payload: mustJSON(incident),
	}); err != nil {
		t.Fatalf("publish core event on Unix bus: %v", err)
	}
	var envelope wsEnvelope
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatalf("read Unix bus event: %v", err)
	}
	if envelope.Type != contract.RealtimeIncidentCreated || envelope.Source != "core" || envelope.Revision != 9 {
		t.Fatalf("unexpected Unix bus realtime event: %#v", envelope)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("bus server stopped unexpectedly: %v", err)
		}
	default:
	}
}

func TestWebSocketRealtimeBusPublishesTypedCoreMutations(t *testing.T) {
	core := &dynamicStateCore{state: emptyPublicSnapshot()}
	hub := newWebSocketHub(core)
	defer hub.Close()
	bus := newRealtimeTestBus()
	go hub.observeBus(bus)
	server := newIPv4TestServer(t, hub)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close()
	readInitialSnapshot(t, conn)

	incident := contract.Incident{ID: "incident-realtime", Status: contract.IncidentStatusNew, Revision: 4}
	body, _ := json.Marshal(incident)
	bus.publish(contract.Message{
		Type: "incident.created", Kind: contract.KindEvent, Source: "core",
		Epoch: "core-epoch-1", Sequence: 1, Revision: 4, Payload: body,
	})

	var envelope wsEnvelope
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatalf("read incident.created: %v", err)
	}
	if envelope.Type != contract.RealtimeIncidentCreated || envelope.Source != "core" || envelope.Revision != 4 {
		t.Fatalf("unexpected incident envelope: %#v", envelope)
	}
	var created contract.RealtimeIncidentCreatedPayload
	if err := json.Unmarshal(envelope.Payload, &created); err != nil {
		t.Fatalf("decode incident.created: %v", err)
	}
	if created.Incident.ID != incident.ID {
		t.Fatalf("unexpected incident payload: %#v", created)
	}

	bus.publish(contract.Message{
		Type: contract.EventSystemStateChanged, Kind: contract.KindEvent, Source: "core",
		Epoch: "core-epoch-1", Sequence: 2, Revision: 5,
		Payload: mustJSON(map[string]any{"state": map[string]any{"last_state": "intrusion"}}),
	})
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatalf("read security state: %v", err)
	}
	if envelope.Type != contract.RealtimeSecurityStateChanged || envelope.Revision != 5 {
		t.Fatalf("unexpected security envelope: %#v", envelope)
	}
	var state contract.RealtimeSecurityStateChangedPayload
	if err := json.Unmarshal(envelope.Payload, &state); err != nil || state.State["last_state"] != "intrusion" {
		t.Fatalf("unexpected security payload: %#v err=%v", state, err)
	}

	bus.publish(contract.Message{
		Type: "incident.updated", Kind: contract.KindEvent, Source: "core",
		Epoch: "core-epoch-1", Sequence: 3, Revision: 6,
		Payload: mustJSON(contract.RealtimeIncidentUpdatedPayload{
			IncidentID: incident.ID, Revision: 6, Status: contract.IncidentStatusViewed,
			Reason: "viewed", Incident: incident,
		}),
	})
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatalf("read incident.updated: %v", err)
	}
	if envelope.Type != contract.RealtimeIncidentUpdated || envelope.Revision != 6 {
		t.Fatalf("unexpected incident update envelope: %#v", envelope)
	}
	var updated contract.RealtimeIncidentUpdatedPayload
	if err := json.Unmarshal(envelope.Payload, &updated); err != nil || updated.Reason != "viewed" || updated.IncidentID != incident.ID {
		t.Fatalf("unexpected incident update payload: %#v err=%v", updated, err)
	}
}

func TestWebSocketReconnectReplaysMessagesInOrder(t *testing.T) {
	hub := newWebSocketHub(&dynamicStateCore{state: emptyPublicSnapshot()})
	defer hub.Close()
	server := newIPv4TestServer(t, hub)
	defer server.Close()

	first, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", nil)
	if err != nil {
		t.Fatalf("first websocket dial: %v", err)
	}
	readInitialSnapshot(t, first)
	hub.Publish("test.one", map[string]any{"n": 1})
	var firstMessage wsEnvelope
	if err := first.ReadJSON(&firstMessage); err != nil {
		t.Fatalf("read first published message: %v", err)
	}
	first.Close()

	hub.Publish("test.two", map[string]any{"n": 2})
	hub.Publish("test.three", map[string]any{"n": 3})
	query := "?epoch=" + firstMessage.Epoch + "&sequence=" + formatUint(firstMessage.Sequence)
	second, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws"+query, nil)
	if err != nil {
		t.Fatalf("reconnect websocket dial: %v", err)
	}
	defer second.Close()

	var ready wsEnvelope
	if err := second.ReadJSON(&ready); err != nil {
		t.Fatalf("read reconnect ready: %v", err)
	}
	if ready.Type != contract.RealtimeConnectionReady || ready.Epoch != firstMessage.Epoch {
		t.Fatalf("unexpected reconnect ready: %#v", ready)
	}
	var previous uint64 = firstMessage.Sequence
	for index, expected := range []string{"test.two", "test.three"} {
		var replay wsEnvelope
		if err := second.ReadJSON(&replay); err != nil {
			t.Fatalf("read replay %d: %v", index, err)
		}
		if replay.Type != contract.RealtimeMessageType(expected) || replay.Sequence <= previous {
			t.Fatalf("unexpected replay %d: %#v", index, replay)
		}
		previous = replay.Sequence
	}
}

func TestWebSocketOldCursorAndEpochChangeRequireSnapshot(t *testing.T) {
	hub := newWebSocketHub(&dynamicStateCore{state: emptyPublicSnapshot()})
	defer hub.Close()
	server := newIPv4TestServer(t, hub)
	defer server.Close()

	for index := 0; index < wsReplayBufferSize+8; index++ {
		hub.Publish("test.fill", map[string]any{"n": index})
	}
	oldEpoch := hub.epoch
	oldURL := wsURL(server.URL) + "/api/ws?epoch=" + oldEpoch + "&sequence=1"
	conn, _, err := websocket.DefaultDialer.Dial(oldURL, nil)
	if err != nil {
		t.Fatalf("old cursor dial: %v", err)
	}
	var ready, resync, snapshot wsEnvelope
	for _, target := range []*wsEnvelope{&ready, &resync, &snapshot} {
		if err := conn.ReadJSON(target); err != nil {
			t.Fatalf("read old cursor message: %v", err)
		}
	}
	conn.Close()
	if ready.Type != contract.RealtimeConnectionReady || resync.Type != contract.RealtimeResyncRequired || snapshot.Type != contract.RealtimeSnapshot {
		t.Fatalf("old cursor should receive ready, resync, snapshot: %#v %#v %#v", ready, resync, snapshot)
	}

	otherURL := wsURL(server.URL) + "/api/ws?epoch=other-epoch&sequence=1"
	conn, _, err = websocket.DefaultDialer.Dial(otherURL, nil)
	if err != nil {
		t.Fatalf("epoch change dial: %v", err)
	}
	defer conn.Close()
	if err := conn.ReadJSON(&ready); err != nil {
		t.Fatalf("read epoch ready: %v", err)
	}
	if err := conn.ReadJSON(&resync); err != nil {
		t.Fatalf("read epoch resync: %v", err)
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatalf("read epoch snapshot: %v", err)
	}
	var reason contract.RealtimeResyncRequiredPayload
	if resync.Type != contract.RealtimeResyncRequired || json.Unmarshal(resync.Payload, &reason) != nil || reason.Reason != "epoch_changed" {
		t.Fatalf("unexpected epoch resync: %#v payload=%s", resync, resync.Payload)
	}
}

func TestWebSocketRejectsDisallowedOrigin(t *testing.T) {
	hub := newWebSocketHubWithOrigin(&dynamicStateCore{state: emptyPublicSnapshot()}, func(r *http.Request) bool {
		return r != nil && r.Header.Get("Origin") == "http://allowed.example"
	})
	defer hub.Close()
	server := newIPv4TestServer(t, hub)
	defer server.Close()

	badHeader := http.Header{"Origin": []string{"http://evil.example"}}
	_, response, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", badHeader)
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("disallowed origin should be rejected: response=%#v err=%v", response, err)
	}

	goodHeader := http.Header{"Origin": []string{"http://allowed.example"}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", goodHeader)
	if err != nil {
		t.Fatalf("allowed origin should connect: %v", err)
	}
	defer conn.Close()
	discardInitial(t, conn)
}

func TestWebSocketPingKeepsConnectionAndSlowClientIsBounded(t *testing.T) {
	hub := newWebSocketHub(&dynamicStateCore{state: emptyPublicSnapshot()})
	hub.pingInterval = 10 * time.Millisecond
	hub.pongWait = 100 * time.Millisecond
	hub.writeWait = 100 * time.Millisecond
	defer hub.Close()
	server := newIPv4TestServer(t, hub)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"/api/ws", nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close()
	discardInitial(t, conn)
	time.Sleep(30 * time.Millisecond)
	hub.Publish("test.after_ping", map[string]any{"ok": true})
	var message wsEnvelope
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatalf("connection should survive ping/pong: %v", err)
	}
	if message.Type != "test.after_ping" {
		t.Fatalf("unexpected post-ping message: %#v", message)
	}
}

func formatUint(value uint64) string {
	return strconv.FormatUint(value, 10)
}
