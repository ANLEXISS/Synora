package ingest

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"synora/internal/device"
	"synora/internal/event"
	"synora/internal/idgen"
	"synora/pkg/contract"
)

type Parser struct {
	Devices *device.Registry
	Now     func() time.Time
}

func (p Parser) Parse(msg contract.Message) (*contract.Event, error) {
	payload := map[string]any{}
	if len(msg.Payload) > 0 {
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return nil, err
		}
	}
	if msg.SourceType != "" {
		payload["source_type"] = msg.SourceType
	}

	parsed := &contract.Event{
		ID:        idgen.New("evt"),
		Type:      contract.NormalizeEventType(msg.Type),
		Source:    strings.TrimSpace(msg.Source),
		Timestamp: p.resolveTimestamp(msg.Timestamp, payload["timestamp"]),
		Payload:   payload,
		Priority:  msg.Priority,
	}
	if eventID, ok := payload["event_id"].(string); ok && strings.TrimSpace(eventID) != "" {
		parsed.ID = strings.TrimSpace(eventID)
	}
	if parsed.Source == "" {
		return nil, errors.New("source is required")
	}

	parsed.DeviceID = resolveDeviceIDFromPayload(parsed.Source, payload)
	parsed.NodeID = strings.TrimSpace(resolveString(payload, "node_id", "node"))
	if parsed.NodeID == "" && p.Devices != nil {
		if dev, ok := p.Devices.Get(parsed.DeviceID); ok && dev != nil {
			parsed.NodeID = dev.NodeID
		}
	}
	if parsed.NodeID != "" && strings.TrimSpace(resolveString(payload, "node_id", "node")) == "" {
		payload["node_id"] = parsed.NodeID
	}
	parsed.Identity = strings.TrimSpace(resolveString(payload, "identity", "resident_id"))
	parsed.Confidence = resolveFloat(payload, "confidence")
	parsed.TrackID = resolveString(payload, "track_id")
	parsed.ClipID = resolveString(payload, "clip_id")
	parsed.ActivationID = resolveString(payload, "activation_id", "activation", "session_id")
	parsed.SequenceKey = resolveString(payload, "sequence_key", "sequence")
	parsed.ClipIndex = resolveInt(payload, "clip_index", "index")
	if parsed.Priority == 0 {
		parsed.Priority = contract.EventPriority(parsed.Type)
	}
	parsed.GroupKey = strings.Join([]string{
		parsed.Type,
		parsed.Source,
		parsed.DeviceID,
		parsed.NodeID,
		parsed.Identity,
	}, "|")
	if suffix := controlledGroupKeySuffix(payload); suffix != "" {
		parsed.GroupKey += "|" + suffix
	}

	return parsed, nil
}

func controlledGroupKeySuffix(payload map[string]any) string {
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok {
		return ""
	}
	if metadataBool(metadata["validation"]) {
		return strings.TrimSpace(resolveString(metadata, "event_id"))
	}
	if !metadataBool(metadata["simulated"]) {
		return ""
	}
	if eventInstanceID := strings.TrimSpace(resolveString(metadata, "event_instance_id")); eventInstanceID != "" {
		return eventInstanceID
	}
	testRunID := strings.TrimSpace(resolveString(metadata, "test_run_id"))
	stepID := strings.TrimSpace(resolveString(metadata, "scenario_step_id"))
	if testRunID != "" && stepID != "" {
		return testRunID + "|" + stepID
	}
	if testRunID != "" {
		return testRunID
	}
	return stepID
}

func metadataBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func (p Parser) resolveTimestamp(messageTS time.Time, raw any) time.Time {
	if !messageTS.IsZero() {
		return messageTS.UTC()
	}
	switch value := raw.(type) {
	case float64:
		if value > 1e12 {
			return time.UnixMilli(int64(value)).UTC()
		}
		seconds, frac := math.Modf(value)
		return time.Unix(int64(seconds), int64(frac*float64(time.Second))).UTC()
	case int64:
		return time.UnixMilli(value).UTC()
	case int:
		return time.UnixMilli(int64(value)).UTC()
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			break
		}
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed.UTC()
		}
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			if parsed > 1e12 {
				return time.UnixMilli(parsed).UTC()
			}
			return time.Unix(parsed, 0).UTC()
		}
	}
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

type Queue struct {
	Parser Parser
	Rate   *event.RateController
	High   chan<- *contract.Event
	Normal chan<- *contract.Event
}

func (q *Queue) Ingest(msg contract.Message) (*contract.Event, bool) {
	parsed, err := q.Parser.Parse(msg)
	if err != nil {
		log.Println("core: invalid event payload", err)
		return nil, false
	}

	log.Printf(
		"core: parsed event id=%s type=%s device=%s node=%s",
		parsed.ID,
		parsed.Type,
		parsed.DeviceID,
		parsed.NodeID,
	)
	log.Printf(
		"core: enqueue event id=%s type=%s priority=%d",
		parsed.ID,
		parsed.Type,
		parsed.Priority,
	)

	if q.Rate != nil && !q.Rate.Accept(parsed) {
		return parsed, false
	}

	if parsed.Priority >= contract.PriorityHigh {
		select {
		case q.High <- parsed:
			return parsed, true
		default:
			log.Println("core: high priority queue full, dropping event", parsed.Type)
			return parsed, false
		}
	}

	select {
	case q.Normal <- parsed:
		return parsed, true
	default:
		log.Println("core: event queue full, dropping event", parsed.Type)
		return parsed, false
	}
}

func resolveDeviceIDFromPayload(source string, payload map[string]any) string {
	for _, key := range []string{"device", "device_id", "camera", "camera_id"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(source)
}

func resolveString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func resolveFloat(payload map[string]any, key string) float64 {
	value, ok := payload[key]
	if !ok {
		return 0
	}
	switch current := value.(type) {
	case float64:
		return current
	case float32:
		return float64(current)
	case int:
		return float64(current)
	case int64:
		return float64(current)
	default:
		return 0
	}
}

func resolveInt(payload map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch current := value.(type) {
		case int:
			return current
		case int64:
			return int(current)
		case float64:
			return int(current)
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(current))
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}
