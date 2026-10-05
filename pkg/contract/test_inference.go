package contract

// TestInferenceCase is the public, bounded catalog used by the authenticated
// Intelligence test bench. Payloads contain only aggregate contract facts;
// they never contain media, identities, hardware identifiers or commands.
type TestInferenceCase struct {
	ID                string         `json:"case_id"`
	Label             string         `json:"label"`
	Category          string         `json:"category"`
	EventType         string         `json:"event_type"`
	TriggersInference bool           `json:"triggers_inference"`
	Payload           map[string]any `json:"-"`
}

// TestInferenceCatalog is deliberately built from the existing V1 event
// constants and the same aggregate fields consumed by Core.composeSnapshot.
// A fresh catalog is returned on every call so request handling cannot mutate
// the definitions shared by another request.
func TestInferenceCatalog() []TestInferenceCase {
	return []TestInferenceCase{
		{ID: "vision-private-perimeter", Label: "Présence au périmètre", Category: "Vision", EventType: EventVisionSegmentReadyV1, TriggersInference: true, Payload: map[string]any{
			"topology": VisionTopologyPrivatePerimeter, "human_present": true, "track_count": 1, "priority": VisionPriorityP2, "episode_phase": "candidate", "real_detection": true, "observation_count": 1, "confidence": 0.82,
		}},
		{ID: "vision-restricted-threshold", Label: "Présence au seuil", Category: "Vision", EventType: EventVisionSegmentReadyV1, TriggersInference: true, Payload: map[string]any{
			"topology": VisionTopologyRestrictedThreshold, "human_present": true, "track_count": 1, "priority": VisionPriorityP1, "episode_phase": "candidate", "real_detection": true, "observation_count": 1, "confidence": 0.88,
		}},
		{ID: "vision-protected-interior", Label: "Présence dans l’intérieur protégé", Category: "Vision", EventType: EventVisionSegmentReadyV1, TriggersInference: true, Payload: map[string]any{
			"topology": VisionTopologyProtectedInterior, "human_present": true, "track_count": 1, "track_confirmed": true, "priority": VisionPriorityP1, "episode_phase": "candidate", "real_detection": true, "observation_count": 1, "confidence": 0.91,
		}},
		{ID: "vision-confirmed-presence", Label: "Présence confirmée", Category: "Vision", EventType: EventVisionSegmentReadyV1, TriggersInference: true, Payload: map[string]any{
			"topology": VisionTopologyProtectedInterior, "human_present": true, "track_count": 1, "track_confirmed": true, "priority": VisionPriorityP1, "episode_phase": "confirmed", "segment_count": 2, "real_detection": true, "observation_count": 2, "confidence": 0.94,
		}},
		{ID: "vision-confirmed-episode", Label: "Épisode confirmé", Category: "Vision", EventType: EventVisionSegmentReadyV1, TriggersInference: true, Payload: map[string]any{
			"topology": VisionTopologyProtectedInterior, "human_present": true, "track_count": 1, "track_confirmed": true, "priority": VisionPriorityP1, "episode_phase": "confirmed", "segment_count": 3, "seconds_since_first": 4, "real_detection": true, "observation_count": 3, "confidence": 0.96,
		}},
		{ID: "vision-episode-end", Label: "Fin d’épisode", Category: "Vision", EventType: EventVisionEnd, TriggersInference: true, Payload: map[string]any{
			"topology": VisionTopologyProtectedInterior, "human_present": false, "track_count": 0, "priority": VisionPriorityP4, "episode_phase": "final", "is_final": true, "real_detection": true, "observation_count": 1, "confidence": 0.9,
		}},
		{ID: "sensor-normal", Label: "Capteurs stables", Category: "Capteurs", EventType: EventSensorNormal, TriggersInference: true, Payload: map[string]any{
			"movement": false, "access_state": "closed", "alarm_state": "armed", "sensor_evidence": true, "confidence": 0.99,
		}},
		{ID: "sensor-anomaly", Label: "Anomalie capteur faible", Category: "Capteurs", EventType: EventSensorAnomaly, TriggersInference: true, Payload: map[string]any{
			"movement": true, "access_state": "forced", "alarm_state": "triggered", "sensor_evidence": true, "confidence": 0.74,
		}},
		{ID: "runtime-degraded", Label: "Runtime dégradé", Category: "État système", EventType: EventDiscoveryRuntimeStatus, TriggersInference: true, Payload: map[string]any{
			"component": "runtime", "status": "degraded", "degraded": true,
		}},
		{ID: "equipment-unavailable", Label: "Équipement indisponible", Category: "État système", EventType: EventDeviceOffline, TriggersInference: true, Payload: map[string]any{
			"component": "camera", "status": "offline", "degraded": true,
		}},
		{ID: "web-review-simulated", Label: "Revue web simulée", Category: "Commande web", EventType: EventWebCommand, TriggersInference: false, Payload: map[string]any{
			"command": "review", "arguments": map[string]any{"topology": VisionTopologyUnknown},
		}},
	}
}
