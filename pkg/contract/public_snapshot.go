package contract

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode"
)

type PublicSnapshot struct {
	System        map[string]any   `json:"system"`
	Devices       []map[string]any `json:"devices"`
	Residents     []map[string]any `json:"residents"`
	Nodes         []map[string]any `json:"nodes"`
	Events        []map[string]any `json:"events"`
	Automations   []map[string]any `json:"automations"`
	Cameras       []map[string]any `json:"cameras"`
	Tracks        []map[string]any `json:"tracks"`
	Clusters      []map[string]any `json:"clusters"`
	Clips         []map[string]any `json:"clips"`
	Presence      []map[string]any `json:"presence"`
	Identities    []map[string]any `json:"identities"`
	Validations   []map[string]any `json:"validations"`
	ActionResults []map[string]any `json:"action_results"`
	Metrics       map[string]any   `json:"metrics"`
	CGE           map[string]any   `json:"cge"`
	EventChains   map[string]any   `json:"event_chains,omitempty"`
}

func PublicSnapshotFromCoreState(state map[string]any) PublicSnapshot {
	store := mapValue(state["state_store"])
	system := publicSystemState(mapValue(state["system"]))

	return PublicSnapshot{
		System:        system,
		Devices:       collectionFrom(state, store, "devices"),
		Residents:     collectionFrom(state, store, "residents"),
		Nodes:         collectionFrom(state, store, "nodes"),
		Events:        collectionFrom(state, store, "events"),
		Automations:   automationCollection(state["automations"]),
		Cameras:       collectionFrom(state, store, "cameras"),
		Tracks:        collectionFrom(state, store, "tracks"),
		Clusters:      collectionFrom(state, store, "clusters"),
		Clips:         collectionFrom(state, store, "clips"),
		Presence:      collectionFrom(state, store, "presence"),
		Identities:    collectionFrom(state, store, "identities"),
		Validations:   collectionFrom(state, store, "validations"),
		ActionResults: collectionFrom(state, store, "action_results"),
		Metrics:       publicMetrics(state["metrics"]),
		CGE:           mapOrEmpty(normalizeMap(mapValue(state["cge"]))),
		EventChains:   mapOrEmpty(normalizeMap(mapValue(state["event_chains"]))),
	}
}

func publicSystemState(raw map[string]any) map[string]any {
	system := mapOrEmpty(normalizeMap(raw))
	securityState := DefaultSecurityModeState(time.Now().UTC())
	if security := mapValue(raw["security"]); security != nil {
		if mode, ok := security["mode"].(string); ok {
			securityState.Mode = SecurityMode(mode)
		}
		if armed, ok := security["armed"].(bool); ok {
			securityState.Armed = armed
		}
		if occupancy, ok := security["expected_occupancy"].(string); ok {
			securityState.ExpectedOccupancy = ExpectedOccupancy(occupancy)
		}
	}
	securityState = NormalizeSecurityModeState(securityState, time.Now().UTC())
	security := map[string]any{
		"mode":               string(securityState.Mode),
		"armed":              securityState.Armed,
		"expected_occupancy": string(securityState.ExpectedOccupancy),
	}
	system["security"] = security
	system["security_mode"] = security["mode"]
	system["security_armed"] = security["armed"]
	system["expected_occupancy"] = security["expected_occupancy"]
	return system
}

func publicMetrics(value any) map[string]any {
	metrics := mapOrEmpty(normalizeMap(mapValue(value)))
	if stateStoreSize, ok := metrics["state_store_size"]; ok {
		metrics["state_size"] = stateStoreSize
		delete(metrics, "state_store_size")
	}
	return metrics
}

func collectionFrom(state map[string]any, store map[string]any, key string) []map[string]any {
	if value, ok := state[key]; ok && value != nil {
		return collection(value)
	}
	if value, ok := store[key]; ok && value != nil {
		return collection(value)
	}
	return []map[string]any{}
}

func automationCollection(value any) []map[string]any {
	items := collection(value)
	for _, item := range items {
		if eventType, ok := item["event"]; ok {
			item["event_type"] = eventType
			delete(item, "event")
		}
		normalizeAutomationActions(item)
	}
	return items
}

func normalizeAutomationActions(item map[string]any) {
	actions, ok := item["actions"].([]any)
	if !ok {
		return
	}
	for _, action := range actions {
		mapped, ok := action.(map[string]any)
		if !ok {
			continue
		}
		if deviceID, ok := mapped["device"]; ok {
			mapped["device_id"] = deviceID
			delete(mapped, "device")
		}
	}
}

func collection(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, objectValue(item))
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeMap(item))
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		out := make([]map[string]any, 0, len(keys))
		for _, key := range keys {
			item := objectValue(typed[key])
			if _, ok := item["id"]; !ok {
				item["id"] = key
			}
			out = append(out, item)
		}
		return out
	default:
		if converted := jsonValue(value); converted != nil {
			return collection(converted)
		}
		return []map[string]any{}
	}
}

func objectValue(value any) map[string]any {
	if mapped := mapValue(value); mapped != nil {
		return normalizeMap(mapped)
	}
	return map[string]any{"value": normalizeValue(value)}
}

func mapValue(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		if converted, ok := jsonValue(value).(map[string]any); ok {
			return converted
		}
		return nil
	}
}

func normalizeMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		publicKey := toSnakeKey(key)
		if isSensitivePublicKey(publicKey) {
			continue
		}
		out[publicKey] = normalizeValue(value)
	}
	return out
}

func isSensitivePublicKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "api_token", "api_token_hash", "token", "access_token", "refresh_token",
		"secret", "secret_hash", "password", "passphrase", "authorization",
		"credential", "credentials", "private_key", "security", "security_yaml",
		"path", "clip_path", "file_path", "filesystem_path":
		return true
	}
	for _, suffix := range []string{"_secret", "_password", "_token", "_private_key"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return nil
		}
		return typed
	case string:
		if typed == "0001-01-01T00:00:00Z" {
			return nil
		}
		return typed
	case map[string]any:
		return normalizeMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeValue(item))
		}
		return out
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeMap(item))
		}
		return out
	default:
		if converted := jsonValue(value); converted != nil {
			return normalizeValue(converted)
		}
		return value
	}
}

func jsonValue(value any) any {
	if value == nil {
		return nil
	}
	switch value.(type) {
	case string, bool,
		float32, float64,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		map[string]any, []any, []map[string]any:
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil || string(data) == "null" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil
	}
	return decoded
}

func mapOrEmpty(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func toSnakeKey(key string) string {
	if key == "" || strings.Contains(key, "_") {
		return key
	}

	known := map[string]string{
		"ID":               "id",
		"NodeID":           "node_id",
		"DeviceID":         "device_id",
		"CameraID":         "camera_id",
		"ResidentID":       "resident_id",
		"LastEventID":      "last_event_id",
		"LastClipID":       "last_clip_id",
		"LastNodeID":       "last_node_id",
		"LastDeviceID":     "last_device_id",
		"EventIDs":         "event_ids",
		"DecisionID":       "decision_id",
		"EventID":          "event_id",
		"SituationID":      "situation_id",
		"ProposedIdentity": "proposed_identity",
		"ResolvedAt":       "resolved_at",
		"ActionID":         "action_id",
		"StartedAt":        "started_at",
		"FinishedAt":       "finished_at",
		"LastSeen":         "last_seen",
		"CreatedAt":        "created_at",
		"UpdatedAt":        "updated_at",
		"ExpiresAt":        "expires_at",
		"LastState":        "last_state",
		"LastStateTime":    "last_state_time",
		"IntrusionActive":  "intrusion_active",
		"IntrusionTime":    "intrusion_time",
		"ActivityCount":    "activity_count",
		"MinScore":         "min_score",
		"ScoreMultiplier":  "score_multiplier",
		"ScoreOffset":      "score_offset",
		"EventType":        "event_type",
		"GroupKey":         "group_key",
	}
	if value, ok := known[key]; ok {
		return value
	}

	var out []rune
	runes := []rune(key)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				out = append(out, '_')
			}
			r = unicode.ToLower(r)
		}
		out = append(out, r)
	}
	return string(out)
}
