package event

import (
	"strings"
	"sync"
	"time"

	"synora/internal/resourcebudget"
	"synora/pkg/contract"
)

type RateController struct {
	mu             sync.Mutex
	dedupeWindow   time.Duration
	throttleWindow time.Duration
	maxEntries     int
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
	return NewRateControllerWithLimit(dedupeWindow, throttleWindow, resourcebudget.MaxRateControllerEntries)
}

func NewRateControllerWithLimit(
	dedupeWindow,
	throttleWindow time.Duration,
	maxEntries int,
) *RateController {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &RateController{
		dedupeWindow:   dedupeWindow,
		throttleWindow: throttleWindow,
		maxEntries:     maxEntries,
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
	c.pruneLocked(now)

	// ------------------------------------------------
	// DEDUPE
	// ------------------------------------------------

	fingerprint :=
		event.Type + "|" +
			event.Source + "|" +
			event.NodeID + "|" +
			event.Identity
	if suffix := controlledFingerprintSuffix(event.Payload); suffix != "" {
		fingerprint += "|" + suffix
	}

	// An explicit transport ID is Core's idempotence key. Let it reach Core so
	// a retry can be distinguished from an ID collision with another payload.
	// The rate controller still deduplicates legacy messages that have no ID.
	if event.ID == "" {
		if lastSeen, ok := c.fingerprints[fingerprint]; ok {
			if now.Sub(lastSeen) <= c.dedupeWindow {
				return false
			}
		}
		if len(c.fingerprints) >= c.maxEntries {
			deleteOldestFingerprint(c.fingerprints)
		}
		c.fingerprints[fingerprint] = now
	}

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
		if len(c.groups) >= c.maxEntries {
			deleteOldestGroup(c.groups)
		}
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

func (c *RateController) pruneLocked(now time.Time) {
	if c == nil {
		return
	}
	for key, seen := range c.fingerprints {
		if c.dedupeWindow <= 0 || now.Sub(seen) > c.dedupeWindow {
			delete(c.fingerprints, key)
		}
	}
	for key, group := range c.groups {
		if group == nil || c.throttleWindow <= 0 || now.Sub(group.lastAccepted) > c.throttleWindow {
			delete(c.groups, key)
		}
	}
}

func deleteOldestFingerprint(values map[string]time.Time) {
	oldestKey := ""
	var oldest time.Time
	for key, seen := range values {
		if oldestKey == "" || seen.Before(oldest) || seen.Equal(oldest) && key < oldestKey {
			oldestKey, oldest = key, seen
		}
	}
	if oldestKey != "" {
		delete(values, oldestKey)
	}
}

func deleteOldestGroup(values map[string]*groupState) {
	oldestKey := ""
	var oldest time.Time
	for key, group := range values {
		seen := time.Time{}
		if group != nil {
			seen = group.lastAccepted
		}
		if oldestKey == "" || seen.Before(oldest) || seen.Equal(oldest) && key < oldestKey {
			oldestKey, oldest = key, seen
		}
	}
	if oldestKey != "" {
		delete(values, oldestKey)
	}
}

func controlledFingerprintSuffix(payload map[string]any) string {
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok {
		return ""
	}
	if metadataBool(metadata["validation"]) {
		return metadataString(metadata["event_id"])
	}
	if !metadataBool(metadata["simulated"]) {
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
