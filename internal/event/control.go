package event

import (
	"strings"
	"sync"
	"time"

	"synora/pkg/contract"
)

type RateController struct {
	mu             sync.Mutex
	dedupeWindow   time.Duration
	throttleWindow time.Duration
	fingerprints   map[string]time.Time
	groups         map[string]*groupState
}

type groupState struct {
	lastAccepted time.Time
	suppressed   int
}

func NewRateController(
	dedupeWindow,
	throttleWindow time.Duration,
) *RateController {

	return &RateController{
		dedupeWindow:   dedupeWindow,
		throttleWindow: throttleWindow,
		fingerprints:   make(map[string]time.Time),
		groups:         make(map[string]*groupState),
	}
}

func (c *RateController) Accept(event *contract.Event) bool {

	if c == nil || event == nil {
		return true
	}

	// bypass pour événements critiques
	if event.Priority >= contract.PriorityHigh {
		return true
	}

	now := event.Timestamp

	if now.IsZero() {
		now = time.Now().UTC()
	}

	// sécurité payload
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}

	payload := event.Payload

	c.mu.Lock()
	defer c.mu.Unlock()

	// ------------------------------------------------
	// DEDUPE
	// ------------------------------------------------

	fingerprint :=
		event.Type + "|" +
			event.Source + "|" +
			event.NodeID + "|" +
			event.Identity
	if suffix := simulatedFingerprintSuffix(event.Payload); suffix != "" {
		fingerprint += "|" + suffix
	}

	if lastSeen, ok := c.fingerprints[fingerprint]; ok {

		if now.Sub(lastSeen) <= c.dedupeWindow {
			return false
		}
	}

	c.fingerprints[fingerprint] = now

	// ------------------------------------------------
	// THROTTLE GROUP
	// ------------------------------------------------

	groupKey := event.GroupKey

	if groupKey == "" {
		groupKey =
			event.Type + "|" +
				event.Source + "|" +
				event.DeviceID
	}

	group := c.groups[groupKey]

	if group == nil {
		group = &groupState{}
		c.groups[groupKey] = group
	}

	// throttle actif
	if !group.lastAccepted.IsZero() &&
		now.Sub(group.lastAccepted) <= c.throttleWindow {

		group.suppressed++
		return false
	}

	// injecte nombre supprimé
	if group.suppressed > 0 {
		payload["grouped_count"] = group.suppressed
		group.suppressed = 0
	}

	group.lastAccepted = now

	return true
}

func simulatedFingerprintSuffix(payload map[string]any) string {
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok || !metadataBool(metadata["simulated"]) {
		return ""
	}
	if eventInstanceID := metadataString(metadata["event_instance_id"]); eventInstanceID != "" {
		return eventInstanceID
	}
	testRunID := metadataString(metadata["test_run_id"])
	stepID := metadataString(metadata["scenario_step_id"])
	if testRunID != "" && stepID != "" {
		return testRunID + "|" + stepID
	}
	if testRunID != "" {
		return testRunID
	}
	return stepID
}

func metadataString(value any) string {
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
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
