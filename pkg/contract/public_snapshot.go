package contract

import (
	"sort"
	"strings"
	"time"
	"unicode"
)

type PublicSnapshot struct {
	System      map[string]any   `json:"system"`
	Devices     []map[string]any `json:"devices"`
	Residents   []map[string]any `json:"residents"`
	Nodes       []map[string]any `json:"nodes"`
	Events      []map[string]any `json:"events"`
	Automations []map[string]any `json:"automations"`
	Cameras     []map[string]any `json:"cameras"`
	Tracks      []map[string]any `json:"tracks"`
	Clusters    []map[string]any `json:"clusters"`
	Clips       []map[string]any `json:"clips"`
	Presence    []map[string]any `json:"presence"`
	Identities  []map[string]any `json:"identities"`
	Metrics     map[string]any   `json:"metrics"`
}

func PublicSnapshotFromCoreState(state map[string]any) PublicSnapshot {
	store := mapValue(state["state_store"])

	return PublicSnapshot{
		System:      mapOrEmpty(normalizeMap(mapValue(state["system"]))),
		Devices:     collectionFrom(state, store, "devices"),
		Residents:   collectionFrom(state, store, "residents"),
		Nodes:       collectionFrom(state, store, "nodes"),
		Events:      collectionFrom(state, store, "events"),
		Automations: automationCollection(state["automations"]),
		Cameras:     collectionFrom(state, store, "cameras"),
		Tracks:      collectionFrom(state, store, "tracks"),
		Clusters:    collectionFrom(state, store, "clusters"),
		Clips:       collectionFrom(state, store, "clips"),
		Presence:    collectionFrom(state, store, "presence"),
		Identities:  collectionFrom(state, store, "identities"),
		Metrics:     publicMetrics(state["metrics"]),
	}
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
		return nil
	}
}

func normalizeMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[toSnakeKey(key)] = normalizeValue(value)
	}
	return out
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
		return value
	}
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
		"ID":              "id",
		"NodeID":          "node_id",
		"DeviceID":        "device_id",
		"CameraID":        "camera_id",
		"ResidentID":      "resident_id",
		"LastEventID":     "last_event_id",
		"LastClipID":      "last_clip_id",
		"LastNodeID":      "last_node_id",
		"LastDeviceID":    "last_device_id",
		"EventIDs":        "event_ids",
		"LastSeen":        "last_seen",
		"CreatedAt":       "created_at",
		"UpdatedAt":       "updated_at",
		"ExpiresAt":       "expires_at",
		"LastState":       "last_state",
		"LastStateTime":   "last_state_time",
		"IntrusionActive": "intrusion_active",
		"IntrusionTime":   "intrusion_time",
		"ActivityCount":   "activity_count",
		"MinScore":        "min_score",
		"ScoreMultiplier": "score_multiplier",
		"ScoreOffset":     "score_offset",
		"EventType":       "event_type",
		"GroupKey":        "group_key",
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
