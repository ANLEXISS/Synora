package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"synora/internal/security"
	"synora/pkg/contract"
)

const (
	wsClientQueueSize      = 64
	wsPingInterval         = 30 * time.Second
	wsPongWait             = 60 * time.Second
	wsWriteWait            = 10 * time.Second
	wsReadLimit            = 1 << 20
	maxIntelligenceTraces  = 24
	maxIntelligenceEvents  = 64
	maxActiveNodesPerLayer = 5
	maxActivePaths         = 64
)

type wsEnvelope struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

type websocketHub struct {
	mu      sync.RWMutex
	clients map[*websocketClient]struct{}
	closed  bool

	security *security.Config
	auth     *apiAuth

	intelligenceMu       sync.RWMutex
	intelligenceTopology map[string]any
	intelligenceTraces   []map[string]any
	intelligenceEvents   []map[string]any
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

func newWebSocketHub(cfg *security.Config, auth ...*apiAuth) *websocketHub {
	hub := &websocketHub{clients: make(map[*websocketClient]struct{}), security: cfg}
	if len(auth) > 0 {
		hub.auth = auth[0]
	}
	return hub
}

func (h *websocketHub) observeBus(bus websocketBus) {
	if h == nil || bus == nil {
		return
	}
	for msg := range bus.SubscribeChannel("api") {
		if msg.Type == "core.decision" {
			h.handleIntelligenceDecisionAt(msg.Payload, msg.Timestamp)
		}
	}
}

func (h *websocketHub) handleIntelligenceDecision(payload []byte) {
	h.handleIntelligenceDecisionAt(payload, time.Now().UTC())
}

func (h *websocketHub) handleIntelligenceDecisionAt(payload []byte, timestamp time.Time) {
	var envelope struct {
		Decision map[string]any `json:"decision"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return
	}
	raw, ok := envelope.Decision["trace"].(map[string]any)
	if !ok {
		return
	}
	trace := sanitizeIntelligenceTraceRuntime(raw, stringValue(envelope.Decision["mode"]), stringValue(envelope.Decision["status"]))
	if trace == nil {
		return
	}
	event := intelligenceEvent(trace, timestamp)
	topology, _ := trace["topology"].(map[string]any)
	h.intelligenceMu.Lock()
	h.intelligenceTopology = topology
	h.intelligenceTraces = append(h.intelligenceTraces, trace)
	if len(h.intelligenceTraces) > maxIntelligenceTraces {
		h.intelligenceTraces = h.intelligenceTraces[len(h.intelligenceTraces)-maxIntelligenceTraces:]
	}
	h.intelligenceEvents = append(h.intelligenceEvents, event)
	if len(h.intelligenceEvents) > maxIntelligenceEvents {
		h.intelligenceEvents = h.intelligenceEvents[len(h.intelligenceEvents)-maxIntelligenceEvents:]
	}
	h.intelligenceMu.Unlock()
	h.Publish("intelligence.inference", map[string]any{"trace": trace, "event": event})
}

func (h *websocketHub) intelligenceSnapshot() (map[string]any, []map[string]any) {
	h.intelligenceMu.RLock()
	defer h.intelligenceMu.RUnlock()
	topology := cloneMap(h.intelligenceTopology)
	if topology == nil {
		topology = emptyIntelligenceTopology()
	}
	traces := make([]map[string]any, 0, len(h.intelligenceTraces))
	for _, trace := range h.intelligenceTraces {
		traces = append(traces, cloneMap(trace))
	}
	return topology, traces
}

func (h *websocketHub) recentEventsSnapshot() []map[string]any {
	h.intelligenceMu.RLock()
	defer h.intelligenceMu.RUnlock()
	events := make([]map[string]any, 0, len(h.intelligenceEvents))
	for _, event := range h.intelligenceEvents {
		events = append(events, cloneMap(event))
	}
	return events
}

func intelligenceEvent(trace map[string]any, timestamp time.Time) map[string]any {
	event := map[string]any{
		"schema_version": "synora.recent-event/v1",
		"event_type":     "inference",
		"timestamp":      timestamp.UTC().Format(time.RFC3339Nano),
		"inference_id":   trace["inference_id"],
		"model_version":  trace["model_version"],
		"live":           trace["live"],
	}
	for _, key := range []string{"runtime_mode", "inference_status", "proposed_output", "provenance", "test"} {
		if value, ok := trace[key]; ok {
			event[key] = value
		}
	}
	return event
}

func emptyIntelligenceTopology() map[string]any {
	return map[string]any{"schema_version": "synora.mlp-trace/v1", "model_version": "", "heads": []any{}}
}

func sanitizeIntelligenceTrace(raw map[string]any) map[string]any {
	return sanitizeIntelligenceTraceRuntime(raw, "", "")
}

func sanitizeIntelligenceTraceRuntime(raw map[string]any, mode, status string) map[string]any {
	if raw == nil || stringValue(raw["schema_version"]) != "synora.mlp-trace/v1" || raw["redacted"] != true {
		return nil
	}
	inferenceID := safeToken(stringValue(raw["inference_id"]))
	modelVersion := safeToken(stringValue(raw["model_version"]))
	if inferenceID == "" || modelVersion == "" {
		return nil
	}
	trace := map[string]any{
		"schema_version": "synora.mlp-trace/v1",
		"inference_id":   inferenceID,
		"model_version":  modelVersion,
		"redacted":       true,
	}
	if proposed := safeToken(stringValue(raw["proposed_output"])); proposed != "" {
		trace["proposed_output"] = proposed
	}
	if duration, ok := finiteNumber(raw["duration_ms"]); ok {
		trace["duration_ms"] = duration
	}
	if mode = safeToken(mode); mode != "" {
		trace["runtime_mode"] = mode
	}
	if status = safeToken(status); status != "" {
		trace["inference_status"] = status
	}
	if stringValue(raw["provenance"]) == "test-harness" && raw["test"] == true {
		trace["provenance"] = "test-harness"
		trace["test"] = true
	}
	trace["live"] = mode != "active_dry_run" && status == "available"
	trace["topology"] = sanitizeIntelligenceTopology(raw["topology"])
	trace["activations"] = sanitizeActivations(raw["activations"])
	trace["active_paths"] = sanitizePaths(raw["active_paths"])
	return trace
}

func sanitizeIntelligenceTopology(value any) map[string]any {
	raw, ok := value.(map[string]any)
	if !ok {
		return emptyIntelligenceTopology()
	}
	result := map[string]any{
		"schema_version": "synora.mlp-trace/v1",
		"model_version":  safeToken(stringValue(raw["model_version"])),
		"heads":          []any{},
	}
	headValues, _ := raw["heads"].([]any)
	for _, value := range headValues {
		head, ok := value.(map[string]any)
		if !ok || len(result["heads"].([]any)) >= 4 {
			break
		}
		clean := map[string]any{"name": safeToken(stringValue(head["name"])), "layers": []any{}}
		layers, _ := head["layers"].([]any)
		for _, value := range layers {
			layer, ok := value.(map[string]any)
			if !ok || len(clean["layers"].([]any)) >= 16 {
				break
			}
			cleanLayer := map[string]any{
				"id":          safeToken(stringValue(layer["id"])),
				"input_size":  boundedInt(layer["input_size"], 0, 4096),
				"output_size": boundedInt(layer["output_size"], 0, 4096),
				"activation":  safeToken(stringValue(layer["activation"])),
			}
			clean["layers"] = append(clean["layers"].([]any), cleanLayer)
		}
		result["heads"] = append(result["heads"].([]any), clean)
	}
	return result
}

func sanitizeActivations(value any) []any {
	values, _ := value.([]any)
	clean := make([]any, 0, min(len(values), 64))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok || len(clean) >= 64 {
			break
		}
		activation := map[string]any{"layer_id": safeToken(stringValue(item["layer_id"]))}
		for _, key := range []string{"count", "minimum", "maximum", "mean"} {
			if number, ok := finiteNumber(item[key]); ok {
				activation[key] = number
			}
		}
		nodes := make([]any, 0, maxActiveNodesPerLayer)
		if rawNodes, ok := item["active_nodes"].([]any); ok {
			for _, rawNode := range rawNodes {
				if len(nodes) >= maxActiveNodesPerLayer {
					break
				}
				node, ok := rawNode.(map[string]any)
				if !ok {
					continue
				}
				entry := map[string]any{"node_id": safeToken(stringValue(node["node_id"]))}
				if number, ok := finiteNumber(node["activation"]); ok {
					entry["activation"] = number
				}
				nodes = append(nodes, entry)
			}
		}
		activation["active_nodes"] = nodes
		clean = append(clean, activation)
	}
	return clean
}

func sanitizePaths(value any) []any {
	values, _ := value.([]any)
	clean := make([]any, 0, min(len(values), maxActivePaths))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok || len(clean) >= maxActivePaths {
			break
		}
		entry := map[string]any{"from": safeToken(stringValue(item["from"])), "to": safeToken(stringValue(item["to"]))}
		if strength, ok := finiteNumber(item["strength"]); ok {
			entry["strength"] = strength
		}
		clean = append(clean, entry)
	}
	return clean
}

func safeToken(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 128 || strings.ContainsAny(value, "\\/\r\n\x00") {
		return ""
	}
	return value
}

func stringValue(value any) string {
	current, _ := value.(string)
	return current
}

func finiteNumber(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok && number == number && number < 1e9 && number > -1e9
}

func boundedInt(value any, minValue, maxValue int) int {
	number, ok := finiteNumber(value)
	if !ok {
		return 0
	}
	result := int(number)
	if result < minValue {
		return minValue
	}
	if result > maxValue {
		return maxValue
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var cloned map[string]any
	if json.Unmarshal(data, &cloned) != nil {
		return nil
	}
	return cloned
}

func (h *websocketHub) Publish(messageType string, data any) {
	if h == nil {
		return
	}
	payload, err := json.Marshal(wsEnvelope{Type: messageType, Timestamp: time.Now().UTC(), Data: data})
	if err != nil {
		return
	}
	h.mu.RLock()
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
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.security == nil || !h.security.AllowsOrigin(r.Header.Get("Origin")) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	if h.auth != nil {
		claims, ok := h.auth.authenticate(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !security.RoleAllows(claims.Role, "guest") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(request *http.Request) bool {
		return h.security != nil && h.security.AllowsOrigin(request.Header.Get("Origin"))
	}}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &websocketClient{hub: h, conn: conn, send: make(chan []byte, wsClientQueueSize), done: make(chan struct{})}
	h.register(client)
	topology, traces := h.intelligenceSnapshot()
	events := h.recentEventsSnapshot()
	initial, _ := json.Marshal(wsEnvelope{Type: "snapshot.initial", Timestamp: time.Now().UTC(), Data: map[string]any{"topology": topology, "traces": traces, "events": events}})
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

func (c *websocketClient) readPump() {
	defer func() { c.hub.unregister(c); c.close() }()
	c.conn.SetReadLimit(wsReadLimit)
	_ = c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(wsPongWait)) })
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}
func (c *websocketClient) writePump() {
	ticker := time.NewTicker(wsPingInterval)
	defer func() { ticker.Stop(); c.hub.unregister(c); c.close() }()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				return
			}
			if c.conn.WriteMessage(websocket.TextMessage, message) != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if c.conn.WriteMessage(websocket.PingMessage, nil) != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}
func (c *websocketClient) close() { c.once.Do(func() { close(c.done); _ = c.conn.Close() }) }
