package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"synora/pkg/contract"
)

const (
	wsClientQueueSize = 64
	wsPingInterval    = 30 * time.Second
	wsPongWait        = 60 * time.Second
	wsWriteWait       = 10 * time.Second
	wsReadLimit       = 1 << 20
)

type wsEnvelope struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

type websocketHub struct {
	core stateProvider
	now  func() time.Time

	mu      sync.RWMutex
	clients map[*websocketClient]struct{}
	closed  bool
}

type websocketClient struct {
	hub  *websocketHub
	conn *websocket.Conn
	send chan []byte
	done chan struct{}
	once sync.Once
}

type websocketBus interface {
	SubscribeChannel(string) <-chan contract.Message
}

func newWebSocketHub(core stateProvider) *websocketHub {
	return &websocketHub{
		core:    core,
		clients: make(map[*websocketClient]struct{}),
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (h *websocketHub) observeBus(bus websocketBus) {
	if h == nil || bus == nil {
		return
	}
	for msg := range bus.SubscribeChannel("api") {
		h.handleBusMessage(msg)
	}
}

func (h *websocketHub) handleBusMessage(msg contract.Message) {
	switch msg.Type {
	case "state.snapshot":
		h.broadcastSnapshot("snapshot.updated")
	case contract.EventActionResult:
		h.broadcastSnapshot("action_result.created")
	case contract.EventSystemStateChanged, contract.EventSystemPresence:
		h.broadcastSnapshot("system.updated")
	default:
		if contract.IsVisionEvent(msg.Type) || contract.IsDeviceEvent(msg.Type) {
			h.broadcastSnapshot("event.created")
		}
	}
}

func (h *websocketHub) broadcastSnapshot(messageType string) {
	if h == nil || h.core == nil {
		return
	}
	snapshot, err := h.core.State()
	if err != nil {
		log.Printf("websocket snapshot fetch error: %v", err)
		return
	}
	h.Publish(messageType, snapshot)
}

func (h *websocketHub) Publish(messageType string, data any) {
	if h == nil {
		return
	}
	payload, err := json.Marshal(wsEnvelope{
		Type:      messageType,
		Timestamp: h.timestamp(),
		Data:      data,
	})
	if err != nil {
		log.Printf("websocket marshal error: %v", err)
		return
	}

	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return
	}
	clients := make([]*websocketClient, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		select {
		case client.send <- payload:
		default:
			h.unregister(client)
			client.close()
		}
	}
}

func (h *websocketHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.core == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "websocket unavailable"})
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	snapshot, err := h.core.State()
	if err != nil {
		writeError(w, err)
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := &websocketClient{
		hub:  h,
		conn: conn,
		send: make(chan []byte, wsClientQueueSize),
		done: make(chan struct{}),
	}
	h.register(client)

	initial, err := json.Marshal(wsEnvelope{
		Type:      "snapshot.initial",
		Timestamp: h.timestamp(),
		Data:      snapshot,
	})
	if err != nil {
		h.unregister(client)
		client.close()
		return
	}
	client.send <- initial

	go client.writePump()
	go client.readPump()
}

func (h *websocketHub) register(client *websocketClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		client.close()
		return
	}
	h.clients[client] = struct{}{}
}

func (h *websocketHub) unregister(client *websocketClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, client)
}

func (h *websocketHub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	clients := make([]*websocketClient, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
		delete(h.clients, client)
	}
	h.mu.Unlock()

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
	_ = c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *websocketClient) writePump() {
	ticker := time.NewTicker(wsPingInterval)
	defer func() {
		ticker.Stop()
		c.hub.unregister(c)
		c.close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
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
