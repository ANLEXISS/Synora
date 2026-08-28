package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"synora/pkg/contract"
)

func (a *coreApp) validateIngressEvent(event *contract.Event) error {
	if event == nil {
		return fmt.Errorf("event is required")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	switch contract.NormalizeEventType(event.Type) {
	case contract.EventActionResult:
		var result contract.ActionResult
		if err := json.Unmarshal(mustPayloadJSON(event.Payload), &result); err != nil {
			return fmt.Errorf("action result payload: %w", err)
		}
		if err := result.Validate(); err != nil {
			return fmt.Errorf("action result contract: %w", err)
		}
	case contract.EventClipReady:
		var lifecycle contract.ClipLifecyclePayload
		if err := json.Unmarshal(mustPayloadJSON(event.Payload), &lifecycle); err != nil {
			return fmt.Errorf("clip.ready payload: %w", err)
		}
		if err := lifecycle.Clip.Validate(); err != nil {
			return fmt.Errorf("clip.ready contract: %w", err)
		}
	case contract.EventClipProcessing, contract.EventClipProcessed, contract.EventClipFailed:
		clipID := strings.TrimSpace(event.ClipID)
		if clipID == "" {
			clipID = resolveClipPayloadString(event.Payload, "clip_id")
		}
		if clipID == "" {
			return fmt.Errorf("clip reference is required")
		}
		if a.state == nil {
			return fmt.Errorf("state store is unavailable")
		}
		if _, ok := a.state.Clip(clipID); !ok {
			return fmt.Errorf("clip %q does not exist", clipID)
		}
	}
	return nil
}
