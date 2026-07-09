package simulation

import (
	"encoding/json"
	"strings"
	"time"

	"synora/pkg/contract"
)

const (
	defaultDevice     = "cam_01"
	defaultIdentity   = "alexis"
	defaultConfidence = 0.92
)

func BuildEventsForScenario(run SimulationRun, scenario Scenario, overrides EventBuildOptions) ([]contract.Message, error) {
	out := make([]contract.Message, 0, len(scenario.Steps))
	for _, step := range scenario.Steps {
		msg, err := BuildMessage(EventBuildOptions{
			Type:        step.EventType,
			DeviceID:    firstNonEmpty(overrides.DeviceID, step.DeviceID),
			CameraID:    firstNonEmpty(overrides.CameraID, step.CameraID),
			NodeID:      firstNonEmpty(overrides.NodeID, step.NodeID),
			Identity:    firstNonEmpty(step.Identity, overrides.Identity),
			Confidence:  nonZeroFloat(step.Confidence, overrides.Confidence),
			Run:         &run,
			ScenarioID:  scenario.ID,
			StepID:      step.ID,
			DryRun:      overrides.DryRun || run.Mode == ModeDryRun,
			GeneratedBy: firstNonEmpty(overrides.GeneratedBy, run.CreatedBy),
			Data:        step.Data,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func BuildMessage(opts EventBuildOptions) (contract.Message, error) {
	eventType := contract.NormalizeEventType(opts.Type)
	payload := BuildPayload(opts)
	body, err := json.Marshal(payload)
	if err != nil {
		return contract.Message{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	deviceID := firstNonEmpty(opts.DeviceID, opts.CameraID, defaultDevice)
	return contract.Message{
		Type:       eventType,
		Kind:       contract.KindEvent,
		Source:     deviceID,
		Target:     "core",
		SourceType: contract.SourceDevice,
		Timestamp:  now,
		Priority:   contract.EventPriority(eventType),
		Payload:    body,
	}, nil
}

func BuildPayload(opts EventBuildOptions) map[string]any {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	eventType := contract.NormalizeEventType(opts.Type)
	deviceID := firstNonEmpty(opts.DeviceID, opts.CameraID, defaultDevice)
	cameraID := firstNonEmpty(opts.CameraID, deviceID)
	trackID := opts.TrackID
	if trackID == "" {
		trackID = "track-" + cameraID
	}
	clipID := opts.ClipID
	if clipID == "" {
		clipID = "clip-" + cameraID
	}
	payload := cloneMap(opts.Data)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["device_id"] = deviceID
	payload["camera_id"] = cameraID
	payload["node_id"] = opts.NodeID
	payload["track_id"] = trackID
	payload["clip_id"] = clipID
	payload["timestamp"] = now.Format(time.RFC3339Nano)
	if opts.ClipPath != "" {
		payload["clip_path"] = opts.ClipPath
	}
	switch eventType {
	case contract.EventVisionIdentity:
		payload["identity"] = firstNonEmpty(opts.Identity, defaultIdentity)
		payload["confidence"] = nonZeroFloat(opts.Confidence, defaultConfidence)
	case contract.EventVisionUnknown:
		payload["identity"] = "unknown"
		payload["confidence"] = nonZeroFloat(opts.Confidence, 0.70)
	case contract.EventVisionUncertain:
		payload["identity"] = "uncertain"
		if opts.Identity != "" {
			payload["best_match"] = opts.Identity
		}
		payload["confidence"] = nonZeroFloat(opts.Confidence, 0.62)
	case contract.EventVisionMotion:
		payload["motion"] = true
		payload["confidence"] = nonZeroFloat(opts.Confidence, 0.80)
	case contract.EventVisionWeapon:
		payload["weapon"] = true
		payload["weapon_type"] = "unknown"
		payload["confidence"] = nonZeroFloat(opts.Confidence, 0.88)
	case contract.EventVisionFall:
		payload["fall"] = true
		payload["confidence"] = nonZeroFloat(opts.Confidence, 0.82)
	case contract.EventVisionTamper:
		payload["tamper"] = true
		payload["confidence"] = nonZeroFloat(opts.Confidence, 0.82)
	case contract.EventDeviceOffline, contract.EventDiscoveryCameraOffline:
		payload["online"] = false
	case contract.EventDiscoveryCameraOnline:
		payload["online"] = true
	}
	payload["metadata"] = EventMetadata(opts)
	return payload
}

func EventMetadata(opts EventBuildOptions) map[string]any {
	generatedBy := firstNonEmpty(opts.GeneratedBy, GeneratedBySimulationEngine)
	testRunID := ""
	scenarioID := opts.ScenarioID
	if opts.Run != nil {
		testRunID = opts.Run.ID
		if scenarioID == "" {
			scenarioID = opts.Run.ScenarioID
		}
		if generatedBy == GeneratedBySimulationEngine && opts.Run.CreatedBy != "" {
			generatedBy = opts.Run.CreatedBy
		}
	}
	return map[string]any{
		"simulated":        true,
		"test_run_id":      testRunID,
		"scenario_id":      scenarioID,
		"scenario_step_id": opts.StepID,
		"dry_run":          opts.DryRun,
		"generated_by":     generatedBy,
	}
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonZeroFloat(value float64, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}
