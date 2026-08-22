package main

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"synora/internal/clipstore"
	"synora/internal/retention"
	"synora/internal/state"
	"synora/pkg/contract"
)

const defaultClipRoot = "/var/lib/synora/clips"

func clipStorageRoot() string {
	if value := strings.TrimSpace(os.Getenv("SYNORA_CLIP_DIR")); value != "" {
		return value
	}
	return defaultClipRoot
}

func (a *coreApp) processClipLifecycle(event *contract.Event) bool {
	if a == nil || a.state == nil || event == nil {
		return false
	}
	switch event.Type {
	case contract.EventClipReady:
		var lifecycle contract.ClipLifecyclePayload
		if err := json.Unmarshal(mustPayloadJSON(event.Payload), &lifecycle); err != nil {
			log.Printf("core: invalid clip.ready payload err=%v", err)
			return true
		}
		clip := lifecycle.Clip
		if clip.ID == "" {
			clip.ID = firstNonEmptyClip(lifecycle.ClipID, event.ClipID)
		}
		if clip.CameraID == "" {
			clip.CameraID = firstNonEmptyClip(lifecycle.CameraID, event.DeviceID)
		}
		path, err := clipstore.FinalPath(clipStorageRoot(), clip.CameraID, clip.ID)
		if err != nil {
			log.Printf("core: invalid clip.ready path clip=%s err=%v", clip.ID, err)
			return true
		}
		clip.Path = path
		available, verifyErr := clipstore.VerifyRegularFile(path, clip.SizeBytes, clip.Checksum)
		if verifyErr != nil || !available {
			clip.Status = contract.ClipStatusMissing
			clip.FailureCode = "file_missing_or_unsafe"
		} else {
			clip.Status = contract.ClipStatusReady
			if clip.ReadyAt.IsZero() {
				clip.ReadyAt = event.Timestamp.UTC()
			}
		}
		if clip.CreatedAt.IsZero() {
			clip.CreatedAt = event.Timestamp.UTC()
		}
		if clip.ReceivedAt.IsZero() {
			clip.ReceivedAt = clip.CreatedAt
		}
		if clip.UpdatedAt.IsZero() {
			clip.UpdatedAt = clip.CreatedAt
		}
		registered, created, err := a.state.RegisterClip((*state.ClipState)(&clip))
		if err != nil {
			log.Printf("core: clip registration failed clip=%s err=%v", clip.ID, err)
			return true
		}
		if !created && clip.Status == contract.ClipStatusReady {
			switch registered.Status {
			case contract.ClipStatusReceiving, contract.ClipStatusFailed, contract.ClipStatusMissing:
				_, _, _ = a.state.TransitionClip(clip.ID, contract.ClipStatusReady, "")
			}
		}
		a.publishClipAvailableIfReady(clip.ID)
		return true
	case contract.EventClipProcessing:
		a.transitionClipFromEvent(event, contract.ClipStatusProcessing, "")
		return true
	case contract.EventClipProcessed:
		a.transitionClipFromEvent(event, contract.ClipStatusProcessed, "")
		a.publishClipAvailableIfReady(firstNonEmptyClip(event.ClipID, resolveClipPayloadString(event.Payload, "clip_id")))
		return true
	case contract.EventClipFailed:
		failureCode := strings.TrimSpace(resolveClipPayloadString(event.Payload, "failure_code"))
		if failureCode == "" {
			failureCode = "vision_processing_failed"
		}
		a.transitionClipFromEvent(event, contract.ClipStatusFailed, failureCode)
		return true
	default:
		return false
	}
}

func (a *coreApp) publishClipAvailableIfReady(clipID string) {
	if a == nil || a.state == nil || strings.TrimSpace(clipID) == "" {
		return
	}
	clip, ok := a.state.Clip(strings.TrimSpace(clipID))
	if !ok || clip == nil || (clip.Status != contract.ClipStatusReady && clip.Status != contract.ClipStatusProcessed) {
		return
	}
	if a.clipAvailable == nil {
		a.clipAvailable = make(map[string]struct{})
	}
	if _, seen := a.clipAvailable[clip.ID]; seen {
		return
	}
	a.clipAvailable[clip.ID] = struct{}{}
	a.publishEvent("clip.available", contract.RealtimeClipAvailablePayload{
		ClipID: clip.ID, CameraID: clip.CameraID, NodeID: clip.NodeID,
		Status: clip.Status, Revision: clip.Revision, IncidentIDs: append([]string(nil), clip.IncidentIDs...),
	}, contract.PriorityNormal)
}

func (a *coreApp) transitionClipFromEvent(event *contract.Event, target contract.ClipStatus, failureCode string) {
	id := firstNonEmptyClip(event.ClipID, resolveClipPayloadString(event.Payload, "clip_id"))
	if id == "" {
		return
	}
	if _, _, err := a.state.TransitionClip(id, target, failureCode); err != nil {
		log.Printf("core: clip transition failed clip=%s status=%s err=%v", id, target, err)
	}
}

func (a *coreApp) reconcileClips() {
	if a == nil || a.state == nil {
		return
	}
	now := time.Now().UTC()
	if changed := a.state.ReconcileClipFiles(now); changed > 0 {
		log.Printf("core: reconciled clips changed=%d", changed)
	}
	retention := clipRetentionConfig()
	if removed, err := clipstore.ReconcileOrphans(clipStorageRoot(), a.state.ClipStorageReferences(), now, retention.MaxAge); err != nil {
		log.Printf("core: orphan clip reconciliation warning err=%v", err)
	} else if removed > 0 {
		log.Printf("core: orphan clips removed count=%d", removed)
	}
	removed, err := a.state.PurgeClips(now, retention)
	if err != nil {
		log.Printf("core: clip retention warning err=%v", err)
	} else if len(removed) > 0 {
		log.Printf("core: clips expired count=%d", len(removed))
	}
}

func (a *coreApp) applyRetention(now time.Time) {
	if a == nil || a.state == nil {
		return
	}
	policy := retention.DefaultPolicy()
	if removed := a.state.PurgeIncidents(now, policy.Incidents.MaxAge); len(removed) > 0 {
		log.Printf("core: incidents expired count=%d", len(removed))
	}
	if removed := a.state.PurgeRecentEvents(now, policy.Events.MaxAge); removed > 0 {
		log.Printf("core: events expired count=%d", removed)
	}
	if a.eventStore != nil {
		if removed := a.eventStore.Prune(now, policy.Events.MaxAge); removed > 0 {
			log.Printf("core: in-memory events expired count=%d", removed)
		}
	}
}

func clipRetentionConfig() state.ClipRetentionConfig {
	config := state.DefaultClipRetentionConfig()
	config.MinFreeBytes = retention.DefaultPolicy().MinFreeBytes
	if value := os.Getenv("SYNORA_CLIP_MAX_COUNT"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			config.MaxCount = parsed
		}
	}
	if value := os.Getenv("SYNORA_CLIP_MAX_BYTES"); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			config.MaxBytes = parsed
		}
	}
	if value := os.Getenv("SYNORA_CLIP_MAX_AGE"); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			config.MaxAge = parsed
		}
	}
	if value := os.Getenv("SYNORA_CLIP_ACK_MIN_AGE"); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			config.AcknowledgedMinAge = parsed
		}
	}
	if value := os.Getenv("SYNORA_MIN_FREE_BYTES"); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			config.MinFreeBytes = parsed
		}
	}
	return config
}

func mustPayloadJSON(payload map[string]any) []byte {
	body, _ := json.Marshal(payload)
	return body
}

func resolveClipPayloadString(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func firstNonEmptyClip(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
