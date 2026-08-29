package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"synora/internal/idgen"
	"synora/internal/resourcebudget"
	"synora/pkg/contract"
)

const (
	wsClientQueueSize  = resourcebudget.WebSocketQueue
	wsReplayBufferSize = resourcebudget.WebSocketReplay
	wsPingInterval     = 30 * time.Second
	wsPongWait         = 60 * time.Second
	wsWriteWait        = 10 * time.Second
	wsReadLimit        = 1 << 20
)

type wsEnvelope = contract.RealtimeEnvelope

type websocketHub struct {
	core        stateProvider
	now         func() time.Time
	allowOrigin func(*http.Request) bool

	// publishMu serializes publication with the initial Core snapshot. This
	// closes the snapshot/message race without holding a StateStore lock.
	publishMu sync.Mutex
	mu        sync.RWMutex
	clients   map[*websocketClient]struct{}
	closed    bool

	epoch        string
	sequence     uint64
	journal      []contract.RealtimeEnvelope
	pingInterval time.Duration
	pongWait     time.Duration
	writeWait    time.Duration

	sourceEpoch    string
	sourceSequence uint64
	sourceGap      bool
	sourceGapCause string
}

type websocketClient struct {
	hub          *websocketHub
	conn         *websocket.Conn
	send         chan []byte
	done         chan struct{}
	once         sync.Once
	sessionValid func() bool
}

type wsSessionValidatorContextKey struct{}

type websocketBus interface {
	SubscribeChannel(string) <-chan contract.Message
}

type wsCursor struct {
	epoch    string
	sequence uint64
	present  bool
}

func newWebSocketHub(core stateProvider) *websocketHub {
	return &websocketHub{
		core:         core,
		now:          func() time.Time { return time.Now().UTC() },
		epoch:        idgen.New("api-ws"),
		sequence:     1,
		pingInterval: wsPingInterval,
		pongWait:     wsPongWait,
		writeWait:    wsWriteWait,
		journal:      make([]contract.RealtimeEnvelope, 0, wsReplayBufferSize),
		clients:      make(map[*websocketClient]struct{}),
		allowOrigin: func(r *http.Request) bool {
			return r == nil || strings.TrimSpace(r.Header.Get("Origin")) == ""
		},
	}
}

func newWebSocketHubWithOrigin(core stateProvider, allowOrigin func(*http.Request) bool) *websocketHub {
	hub := newWebSocketHub(core)
	if allowOrigin != nil {
		hub.allowOrigin = allowOrigin
	}
	return hub
}

func (h *websocketHub) observeBus(bus websocketBus) {
	if h == nil || bus == nil {
		return
	}
	for msg := range bus.SubscribeChannel("api") {
		h.handleBusMessage(msg)
	}
	log.Printf("websocket bus observer stopped")
}

func (h *websocketHub) handleBusMessage(msg contract.Message) {
	if h == nil || (msg.Source != "core" && msg.Source != "synora-core") {
		return
	}
	h.publishMu.Lock()
	defer h.publishMu.Unlock()
	if h.observeSourceLocked(msg) {
		return
	}

	switch msg.Type {
	case "incident.created":
		var incident contract.Incident
		if err := json.Unmarshal(msg.Payload, &incident); err != nil {
			log.Printf("websocket realtime decode error type=%s err=%v", msg.Type, err)
			return
		}
		h.publishPayloadLocked(contract.RealtimeIncidentCreated, msg.Source, msg.Revision,
			contract.RealtimeIncidentCreatedPayload{Incident: incident})
	case "incident.updated":
		var update contract.RealtimeIncidentUpdatedPayload
		if err := json.Unmarshal(msg.Payload, &update); err != nil {
			log.Printf("websocket realtime decode error type=%s err=%v", msg.Type, err)
			return
		}
		h.publishPayloadLocked(contract.RealtimeIncidentUpdated, msg.Source, msg.Revision, update)
	case "clip.available":
		var available contract.RealtimeClipAvailablePayload
		if err := json.Unmarshal(msg.Payload, &available); err != nil {
			log.Printf("websocket realtime decode error type=%s err=%v", msg.Type, err)
			return
		}
		h.publishPayloadLocked(contract.RealtimeClipAvailable, msg.Source, msg.Revision, available)
	case contract.EventSystemStateChanged:
		var state map[string]any
		if err := json.Unmarshal(msg.Payload, &state); err != nil {
			log.Printf("websocket realtime decode error type=%s err=%v", msg.Type, err)
			return
		}
		if nested, ok := state["state"].(map[string]any); ok {
			state = nested
		}
		h.publishPayloadLocked(contract.RealtimeSecurityStateChanged, msg.Source, msg.Revision,
			contract.RealtimeSecurityStateChangedPayload{State: state})
	case "state.snapshot":
		h.publishSnapshotLocked(msg.Payload, "bus_snapshot", msg.Source, msg.Revision)
	case contract.EventActionResult:
		h.publishSnapshotFromCoreLocked("action_result_created", msg.Source, msg.Revision)
	case contract.EventSystemPresence:
		h.publishSnapshotFromCoreLocked("system_presence", msg.Source, msg.Revision)
	case "event.chain.created", "event.chain.updated", "event.chain.closed", "engine.evaluation.updated":
		var data any
		if err := json.Unmarshal(msg.Payload, &data); err != nil {
			return
		}
		h.publishLegacyPayloadLocked(msg.Type, msg.Source, msg.Revision, data)
	default:
		if contract.IsVisionEvent(msg.Type) || contract.IsDeviceEvent(msg.Type) {
			h.publishSnapshotFromCoreLocked("event_created", msg.Source, msg.Revision)
		}
	}
}

func (h *websocketHub) publishSnapshotFromCoreLocked(reason, source string, revision uint64) {
	if h == nil || h.core == nil {
		return
	}
	snapshot, err := h.core.State()
	if err != nil {
		log.Printf("websocket snapshot fetch error reason=%s err=%v", reason, err)
		return
	}
	if revision == 0 {
		revision = snapshot.Revision
	}
	h.publishPayloadLocked(contract.RealtimeSnapshot, source, revision,
		contract.RealtimeSnapshotPayload{Snapshot: *snapshot, Reason: reason})
}

func (h *websocketHub) publishSnapshotLocked(raw []byte, reason, source string, revision uint64) {
	if len(raw) > 0 {
		var state map[string]any
		if json.Unmarshal(raw, &state) == nil {
			snapshot := contract.PublicSnapshotFromCoreState(state)
			if revision == 0 {
				revision = snapshot.Revision
			}
			h.publishPayloadLocked(contract.RealtimeSnapshot, source, revision,
				contract.RealtimeSnapshotPayload{Snapshot: snapshot, Reason: reason})
			return
		}
	}
	h.publishSnapshotFromCoreLocked(reason, source, revision)
}

func (h *websocketHub) Publish(messageType string, data any) {
	if h == nil {
		return
	}
	h.publishMu.Lock()
	defer h.publishMu.Unlock()
	h.publishLegacyPayloadLocked(messageType, "api", 0, data)
}

func (h *websocketHub) publishLegacyPayloadLocked(messageType, source string, revision uint64, data any) {
	h.publishPayloadLocked(contract.RealtimeMessageType(messageType), source, revision, data)
}

func (h *websocketHub) publishPayloadLocked(messageType contract.RealtimeMessageType, source string, revision uint64, data any) {
	if h == nil || h.closed {
		return
	}
	if h.sourceGap {
		h.appendAndBroadcastLocked(h.newEnvelopeLocked(contract.RealtimeResyncRequired, "api", 0,
			contract.RealtimeResyncRequiredPayload{Reason: h.sourceGapCause}))
		h.sourceGap = false
		h.sourceGapCause = ""
	}
	payload, err := json.Marshal(data)
	if err != nil {
		log.Printf("websocket realtime marshal error type=%s err=%v", messageType, err)
		return
	}
	h.appendAndBroadcastLocked(h.newEnvelopeLocked(messageType, source, revision, json.RawMessage(payload)))
}

func (h *websocketHub) newEnvelopeLocked(messageType contract.RealtimeMessageType, source string, revision uint64, payload any) contract.RealtimeEnvelope {
	var body json.RawMessage
	switch value := payload.(type) {
	case json.RawMessage:
		body = append(json.RawMessage(nil), value...)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			body = json.RawMessage(`{"error":"serialization_failed"}`)
		} else {
			body = encoded
		}
	}
	h.sequence++
	return contract.RealtimeEnvelope{
		SchemaVersion: contract.RealtimeSchemaVersion,
		Type:          messageType,
		MessageID:     idgen.New("ws"),
		OccurredAt:    h.timestamp(),
		Source:        source,
		Epoch:         h.epoch,
		Sequence:      h.sequence,
		Revision:      revision,
		Payload:       body,
	}
}

func (h *websocketHub) appendAndBroadcastLocked(envelope contract.RealtimeEnvelope) {
	h.mu.Lock()
	h.journal = append(h.journal, cloneRealtimeEnvelope(envelope))
	if len(h.journal) > wsReplayBufferSize {
		h.journal = h.journal[len(h.journal)-wsReplayBufferSize:]
	}
	message, err := json.Marshal(envelope)
	if err != nil {
		log.Printf("websocket realtime envelope marshal error: %v", err)
		return
	}
	var slow []*websocketClient
	for client := range h.clients {
		select {
		case client.send <- message:
		default:
			slow = append(slow, client)
		}
	}
	for _, client := range slow {
		delete(h.clients, client)
	}
	h.mu.Unlock()
	for _, client := range slow {
		log.Printf("websocket slow client closed")
		client.close()
	}
}

func (h *websocketHub) observeSourceLocked(msg contract.Message) bool {
	if msg.Epoch == "" || msg.Sequence == 0 {
		return false
	}
	if h.sourceEpoch == "" {
		h.sourceEpoch, h.sourceSequence = msg.Epoch, msg.Sequence
		return false
	}
	if msg.Epoch != h.sourceEpoch {
		h.sourceEpoch, h.sourceSequence = msg.Epoch, msg.Sequence
		h.sourceGap = true
		h.sourceGapCause = "source_epoch_changed"
		return false
	}
	if msg.Sequence <= h.sourceSequence {
		return true
	}
	if msg.Sequence > h.sourceSequence+1 {
		h.sourceGap = true
		h.sourceGapCause = "source_sequence_gap"
	}
	h.sourceSequence = msg.Sequence
	return false
}

func (h *websocketHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.core == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "websocket unavailable"})
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if h.allowOrigin != nil && !h.allowOrigin(r) {
		log.Printf("websocket origin refused")
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "origin_not_allowed"})
		return
	}
	if !h.hasClientCapacity() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "websocket capacity exhausted"})
		return
	}
	cursor, err := parseWSCursor(r)
	if err != nil {
		writeError(w, err)
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  4 << 10,
		WriteBufferSize: 4 << 10,
		CheckOrigin: func(request *http.Request) bool {
			return h.allowOrigin == nil || h.allowOrigin(request)
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	validator, _ := r.Context().Value(wsSessionValidatorContextKey{}).(func() bool)
	client := &websocketClient{hub: h, conn: conn, send: make(chan []byte, wsClientQueueSize), done: make(chan struct{}), sessionValid: validator}

	// Serializing this short handshake with publication gives the client a
	// coherent snapshot cursor without taking any StateStore lock.
	h.publishMu.Lock()
	if h.closed {
		h.publishMu.Unlock()
		client.close()
		return
	}
	h.mu.RLock()
	resume := cursor.present && cursor.epoch == h.epoch && h.canReplayLocked(cursor.sequence)
	h.mu.RUnlock()
	needSnapshot := !resume
	var snapshot *contract.PublicSnapshot
	if needSnapshot {
		snapshot, err = h.core.State()
		if err != nil {
			h.publishMu.Unlock()
			client.close()
			log.Printf("websocket initial snapshot failed err=%v", err)
			return
		}
	}
	h.mu.Lock()
	if len(h.clients) >= resourcebudget.MaxWebSocketClients {
		h.mu.Unlock()
		h.publishMu.Unlock()
		client.close()
		return
	}
	h.clients[client] = struct{}{}
	ready := h.readyEnvelopeLocked()
	if err := h.enqueueLocked(client, ready); err != nil {
		delete(h.clients, client)
		h.mu.Unlock()
		h.publishMu.Unlock()
		client.close()
		return
	}
	if needSnapshot {
		if cursor.present {
			reason := "epoch_changed"
			if cursor.epoch == h.epoch {
				reason = "cursor_too_old"
			}
			resync := h.controlEnvelopeLocked(contract.RealtimeResyncRequired, "api", 0,
				contract.RealtimeResyncRequiredPayload{Reason: reason, RequestedEpoch: cursor.epoch, RequestedSequence: cursor.sequence})
			_ = h.enqueueLocked(client, resync)
		}
		snapshotEnvelope := h.controlEnvelopeLocked(contract.RealtimeSnapshot, "api", snapshot.Revision,
			contract.RealtimeSnapshotPayload{Snapshot: *snapshot, Reason: "initial"})
		_ = h.enqueueLocked(client, snapshotEnvelope)
	} else {
		for _, message := range h.replayLocked(cursor.sequence) {
			_ = h.enqueueLocked(client, message)
		}
	}
	h.mu.Unlock()
	h.publishMu.Unlock()

	go client.writePump()
	go client.readPump()
}

func (h *websocketHub) hasClientCapacity() bool {
	if h == nil {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return !h.closed && len(h.clients) < resourcebudget.MaxWebSocketClients
}

func (h *websocketHub) readyEnvelopeLocked() contract.RealtimeEnvelope {
	return h.controlEnvelopeLocked(contract.RealtimeConnectionReady, "api", 0,
		contract.RealtimeConnectionReadyPayload{ServerTime: h.timestamp(), Epoch: h.epoch, Sequence: h.currentSequenceLocked()})
}

// Control frames describe the current cursor but are not journaled mutations.
// In particular, the initial ready/snapshot handshake must not consume public
// event sequence numbers or manufacture a replay gap.
func (h *websocketHub) controlEnvelopeLocked(messageType contract.RealtimeMessageType, source string, revision uint64, payload any) contract.RealtimeEnvelope {
	return contract.RealtimeEnvelope{
		SchemaVersion: contract.RealtimeSchemaVersion,
		Type:          messageType,
		MessageID:     idgen.New("ws-control"),
		OccurredAt:    h.timestamp(),
		Source:        source,
		Epoch:         h.epoch,
		Sequence:      h.currentSequenceLocked(),
		Revision:      revision,
		Payload:       mustJSON(payload),
	}
}

func (h *websocketHub) currentSequenceLocked() uint64 {
	if h.sequence == 0 {
		return 1
	}
	return h.sequence
}

func (h *websocketHub) enqueueLocked(client *websocketClient, envelope contract.RealtimeEnvelope) error {
	if err := envelope.Validate(); err != nil {
		return err
	}
	message, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	select {
	case client.send <- message:
		return nil
	default:
		return errors.New("websocket client queue full")
	}
}

func (h *websocketHub) canReplayLocked(sequence uint64) bool {
	if sequence > h.sequence {
		return false
	}
	if len(h.journal) == 0 {
		return sequence == h.sequence
	}
	return sequence >= h.journal[0].Sequence-1
}

func (h *websocketHub) replayLocked(sequence uint64) []contract.RealtimeEnvelope {
	result := make([]contract.RealtimeEnvelope, 0, len(h.journal))
	for _, message := range h.journal {
		if message.Sequence > sequence {
			result = append(result, cloneRealtimeEnvelope(message))
		}
	}
	return result
}

func (h *websocketHub) register(client *websocketClient) {
	if h == nil || client == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		client.close()
		return
	}
	h.clients[client] = struct{}{}
}

func (h *websocketHub) unregister(client *websocketClient) {
	if h == nil || client == nil {
		return
	}
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
}

func (h *websocketHub) Close() {
	if h == nil {
		return
	}
	h.publishMu.Lock()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		h.publishMu.Unlock()
		return
	}
	h.closed = true
	clients := make([]*websocketClient, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
		delete(h.clients, client)
	}
	h.mu.Unlock()
	h.publishMu.Unlock()
	for _, client := range clients {
		client.close()
	}
}

func (h *websocketHub) timestamp() time.Time {
	if h != nil && h.now != nil {
		return h.now().UTC()
	}
	return time.Now().UTC()
}

func (c *websocketClient) readPump() {
	defer func() {
		c.hub.unregister(c)
		c.close()
	}()
	c.conn.SetReadLimit(wsReadLimit)
	pongWait := c.hub.pongWait
	if pongWait <= 0 {
		pongWait = wsPongWait
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *websocketClient) writePump() {
	interval := c.hub.pingInterval
	if interval <= 0 {
		interval = wsPingInterval
	}
	writeWait := c.hub.writeWait
	if writeWait <= 0 {
		writeWait = wsWriteWait
	}
	ticker := time.NewTicker(interval)
	defer func() {
		ticker.Stop()
		c.hub.unregister(c)
		c.close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if c.sessionValid != nil && !c.sessionValid() {
				_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session revoked"), time.Now().Add(writeWait))
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *websocketClient) close() {
	c.once.Do(func() {
		close(c.done)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

func parseWSCursor(r *http.Request) (wsCursor, error) {
	if r == nil {
		return wsCursor{}, nil
	}
	epoch := strings.TrimSpace(r.URL.Query().Get("epoch"))
	rawSequence := strings.TrimSpace(r.URL.Query().Get("sequence"))
	if epoch == "" && rawSequence == "" {
		return wsCursor{}, nil
	}
	if epoch == "" || rawSequence == "" {
		return wsCursor{}, contract.NewAPIError(contract.ErrorInvalidRequest, "websocket cursor requires epoch and sequence")
	}
	sequence, err := strconv.ParseUint(rawSequence, 10, 64)
	if err != nil || sequence == 0 {
		return wsCursor{}, contract.NewAPIError(contract.ErrorInvalidRequest, "websocket sequence is invalid")
	}
	return wsCursor{epoch: epoch, sequence: sequence, present: true}, nil
}

func cloneRealtimeEnvelope(value contract.RealtimeEnvelope) contract.RealtimeEnvelope {
	value.Payload = append(json.RawMessage(nil), value.Payload...)
	return value
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{"error":"serialization_failed"}`)
	}
	return data
}
