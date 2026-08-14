package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"synora/pkg/contract"
)

const maxProcessedEventKeys = 4096

// acceptEvent is the single Core idempotence/order gate. It runs before
// Engine, StateStore mutation, incident creation, or realtime publication.
func (a *coreApp) acceptEvent(event *contract.Event) bool {
	if a == nil || event == nil {
		return false
	}
	key := canonicalEventKey(event)
	a.eventMu.Lock()
	defer a.eventMu.Unlock()
	if a.inputEpoch == "" && a.state != nil {
		a.inputEpoch, a.inputSequence = a.state.InputCursor()
	}
	if a.processedEvents == nil {
		a.processedEvents = make(map[string]struct{})
	}
	if _, exists := a.processedEvents[key]; exists || a.persistedEventSeen(event) {
		return false
	}
	if event.Epoch != "" {
		if a.inputEpoch == "" {
			a.inputEpoch, a.inputSequence = event.Epoch, 0
		} else if event.Epoch == a.inputEpoch {
			if event.Sequence > 0 && event.Sequence <= a.inputSequence {
				return false
			}
		} else {
			// A new worker epoch is accepted only at its first sequence and
			// cannot arrive with an older receive timestamp. Older epochs are
			// therefore unable to overwrite the current Core projection.
			if event.Sequence > 1 || (!a.inputReceivedAt.IsZero() && !event.ReceivedAt.IsZero() && event.ReceivedAt.Before(a.inputReceivedAt)) {
				return false
			}
			a.inputEpoch, a.inputSequence = event.Epoch, 0
		}
		if event.Sequence > a.inputSequence {
			a.inputSequence = event.Sequence
		}
		if !event.ReceivedAt.IsZero() {
			a.inputReceivedAt = event.ReceivedAt
		}
	}
	a.processedEvents[key] = struct{}{}
	if event.Epoch != "" && a.state != nil {
		a.state.SetInputCursor(a.inputEpoch, a.inputSequence)
	}
	if len(a.processedEvents) > maxProcessedEventKeys {
		// The durable recent-event journal remains the restart boundary; this
		// in-memory map is only a bounded hot-path cache.
		for candidate := range a.processedEvents {
			delete(a.processedEvents, candidate)
			break
		}
	}
	return true
}

func (a *coreApp) persistedEventSeen(event *contract.Event) bool {
	if a == nil || a.state == nil || event == nil {
		return false
	}
	for _, recent := range a.state.RecentEventsList() {
		if recent == nil {
			continue
		}
		if event.ID != "" && recent.ID == event.ID {
			return true
		}
		if canonicalEventKey(recent) == canonicalEventKey(event) {
			return true
		}
	}
	return false
}

func canonicalEventKey(event *contract.Event) string {
	if event == nil {
		return ""
	}
	if id := strings.TrimSpace(event.ID); id != "" {
		return "event:" + id
	}
	// The fallback is deliberately made only from capture-stable fields. In
	// particular it must not contain receive time, processing order, a
	// StateStore revision, or a realtime sequence generated after admission.
	// Length-prefixing avoids collisions caused by separators in producer IDs.
	entityID := ""
	if event.Payload != nil {
		for _, key := range []string{"entity_id", "entity"} {
			if value, ok := event.Payload[key].(string); ok && strings.TrimSpace(value) != "" {
				entityID = strings.TrimSpace(value)
				break
			}
		}
	}
	parts := []string{
		contract.NormalizeEventType(event.Type), event.ActivationID,
		event.ClipID, strconv.Itoa(event.ClipIndex), event.TrackID, entityID,
		event.ResidentID, event.DeviceID, event.NodeID, event.SequenceKey,
		event.Epoch, event.Timestamp.UTC().Format(time.RFC3339Nano),
	}
	var material strings.Builder
	for _, part := range parts {
		material.WriteString(strconv.Itoa(len(part)))
		material.WriteByte(':')
		material.WriteString(part)
		material.WriteByte('|')
	}
	digest := sha256.Sum256([]byte(material.String()))
	return "event-hash:" + hex.EncodeToString(digest[:])
}
