package vision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"synora/pkg/contract"
)

const (
	WorkerTimeout = 2 * time.Minute
)

type Processor interface {
	Process(job *ClipJob) (*WorkerResponse, error)
}

type Publisher interface {
	Send(msg contract.Message) error
}

func RunClipWorker(
	processor Processor,
	publisher Publisher,
	job *ClipJob,
) error {
	return runClipWorker(processor, publisher, job, true)
}

// RunClipWorkerAttempt executes one retryable attempt. It does not publish a
// terminal clip.failed event when processing fails; the pool publishes that
// event only after all attempts are exhausted.
func RunClipWorkerAttempt(
	processor Processor,
	publisher Publisher,
	job *ClipJob,
) error {
	return runClipWorker(processor, publisher, job, false)
}

func runClipWorker(
	processor Processor,
	publisher Publisher,
	job *ClipJob,
	publishFailure bool,
) error {
	if job == nil || job.ID == "" || job.CameraID == "" {
		return errors.New("invalid clip job")
	}
	if publisher == nil {
		return errors.New("clip publisher unavailable")
	}
	result, err := processor.Process(job)

	if err != nil {
		if publishFailure {
			return publishUnavailableEvidence(publisher, job, "worker_unavailable")
		}
		return err
	}
	if result == nil {
		return publishUnavailableEvidence(publisher, job, "worker_empty_result")
	}
	if result.VisionEvidence != nil {
		evidence := *result.VisionEvidence
		if err := evidence.Validate(); err != nil {
			return publishUnavailableEvidence(publisher, job, "worker_evidence_invalid")
		}
		if evidence.SimulatedCamera != job.SimulatedCamera {
			return publishUnavailableEvidence(publisher, job, "worker_provenance_mismatch")
		}
		return publishEvidenceToDiscovery(publisher, evidence)
	}
	// Historical worker outputs are never republished. Until the worker emits
	// a complete Evidence V1 aggregate, retain only a redacted unavailable
	// result and account for the legacy payload as quarantined.
	return publishUnavailableEvidence(publisher, job, "legacy_worker_output_quarantined")
}

func publishEvidenceToDiscovery(publisher Publisher, evidence contract.VisionEvidenceV1) error {
	if err := evidence.Validate(); err != nil {
		return err
	}
	body, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	return publisher.Send(contract.Message{ID: evidence.EventID, Type: contract.EventVisionEvidenceV1, Kind: contract.KindEvent, Source: "discovery", Target: "discovery", Timestamp: evidence.WindowEnd.UTC(), Payload: body})
}

func publishUnavailableEvidence(publisher Publisher, job *ClipJob, code string) error {
	if publisher == nil || job == nil {
		return errors.New("Vision Evidence publisher unavailable")
	}
	start := job.StartedAt.UTC()
	if start.IsZero() {
		start = time.Now().UTC().Add(-time.Second)
	}
	end := time.Now().UTC()
	if !end.After(start) {
		end = start.Add(time.Second)
	}
	if end.Sub(start) > 24*time.Hour {
		start = end.Add(-time.Second)
	}
	digest := sha256.Sum256([]byte(job.ID + "\x00" + code))
	availability := contract.VisionUnavailable
	unknownMeasure := func() contract.VisionMeasureV1 {
		return contract.VisionMeasureV1{Availability: availability, State: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}}
	}
	unknownSemantic := func() contract.VisionSemanticResultV1 {
		return contract.VisionSemanticResultV1{Availability: availability, Result: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}}
	}
	provenance, simulated := "real", false
	if job.SimulatedCamera {
		provenance, simulated = "simulated_test", true
	}
	topology := authoritativeTopologyClass(job)
	if !contract.ValidVisionTopologyClass(topology) {
		topology = contract.VisionTopologyUnknown
	}
	evidence := contract.VisionEvidenceV1{SchemaVersion: contract.EventVisionEvidenceV1, EventID: "ev_" + hex.EncodeToString(digest[:12]), EpisodeID: "ep_" + hex.EncodeToString(digest[12:24]), WindowStart: start, WindowEnd: end, WindowSeconds: end.Sub(start).Seconds(), Topology: topology, Provenance: provenance, SimulatedCamera: simulated,
		CameraHealth: contract.VisionMeasureV1{Availability: availability, State: "unavailable", Support: contract.VisionSupportV1{Continuity: "unknown"}}, Trigger: unknownMeasure(), Presence: contract.VisionPresenceV1{Human: unknownMeasure(), Vehicle: unknownMeasure(), Animal: unknownMeasure()}, Activity: unknownMeasure(),
		Pose: contract.VisionPoseV1{Availability: availability, Posture: contract.VisionPostureUnknown, Support: contract.VisionSupportV1{Continuity: "unknown"}}, Face: unknownSemantic(), Plate: unknownSemantic(),
		Sensitive: contract.VisionSensitiveV1{Availability: availability, Category: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}}, Media: contract.VisionMediaContinuityV1{Availability: availability, EpisodeState: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}},
		ProducerHealth: "unavailable", Processing: "unavailable", ErrorCode: code}
	return publishEvidenceToDiscovery(publisher, evidence)
}

func visionEventPriority(eventType string, payload map[string]any) int {
	priority := contract.EventPriority(eventType)
	if eventType != contract.EventVisionClipObservationV1 && eventType != contract.EventVisionClipSummaryV1 {
		return priority
	}
	if hint, ok := payload["priority_hint"].(string); ok && hint == contract.VisionPriorityP1 {
		return contract.PriorityHigh
	}
	return priority
}

func prepareVisionEvent(evt Event, job *ClipJob, index int) (map[string]any, error) {
	payloadMap := clonePayload(evt.Payload)
	stableEventID := fmt.Sprintf("%s:event:%d:%s", job.ID, index, evt.Type)
	if job.Pipeline == "clip-v1" {
		if err := rejectAuthoritativeSpoof(payloadMap, job); err != nil {
			return nil, err
		}
	}
	// Camera and clip identity come from the accepted job, never the model.
	payloadMap["device_id"] = job.CameraID
	payloadMap["camera_id"] = job.CameraID
	payloadMap["clip_id"] = job.ID
	payloadMap["event_id"] = stableEventID
	if job.ActivationID != "" {
		payloadMap["activation_id"] = job.ActivationID
	} else {
		delete(payloadMap, "activation_id")
	}
	if job.SequenceKey != "" {
		payloadMap["sequence_key"] = job.SequenceKey
	} else {
		delete(payloadMap, "sequence_key")
	}
	payloadMap["clip_index"] = job.ClipIndex
	if evt.TrackID != nil {
		payloadMap["track_id"] = evt.TrackID
	} else if job.TrackID != "" {
		payloadMap["track_id"] = job.TrackID
	}
	if job.NodeID != "" {
		payloadMap["node_id"] = job.NodeID
	}
	if job.Pipeline != "clip-v1" {
		return payloadMap, nil
	}
	if evt.Type != contract.EventVisionClipSummaryV1 && evt.Type != contract.EventVisionPreliminaryAlertV1 && evt.Type != contract.EventVisionClipObservationV1 {
		return nil, fmt.Errorf("vision contract invalid: event %q is not admitted for clip-v1", evt.Type)
	}
	if job.EpisodeID == "" || job.NodeID == "" || job.Zone == "" || job.TriggerReason == "" || job.StartedAt.IsZero() {
		return nil, errors.New("vision contract invalid: incomplete authoritative clip metadata")
	}
	topologyClass := authoritativeTopologyClass(job)
	if !contract.ValidVisionTopologyClass(topologyClass) {
		return nil, fmt.Errorf("vision contract invalid: unknown topology class %q", topologyClass)
	}
	payloadMap["episode_id"] = job.EpisodeID
	payloadMap["topology_class"] = topologyClass
	payloadMap["topology"] = map[string]any{"node_id": job.NodeID, "zone": job.Zone}
	payloadMap["trigger"] = map[string]any{"reason": job.TriggerReason, "started_at": job.StartedAt}
	if evt.Type == contract.EventVisionClipObservationV1 {
		payloadMap["zone"] = job.Zone
		payloadMap["trigger"] = job.TriggerReason
		delete(payloadMap, "topology")
		observation, err := contract.DecodeVisionClipObservation(mustJSON(payloadMap))
		if err != nil {
			return nil, fmt.Errorf("vision contract invalid: %w", err)
		}
		if observation.EpisodeID != job.EpisodeID || observation.ClipID != job.ID || observation.CameraID != job.CameraID || observation.NodeID != job.NodeID || observation.Zone != job.Zone || observation.TopologyClass != topologyClass || observation.Trigger != job.TriggerReason {
			return nil, errors.New("vision contract invalid: authoritative observation metadata mismatch")
		}
		return payloadMap, nil
	}
	if evt.TrackID == nil {
		return nil, errors.New("vision contract invalid: track id is required")
	}
	if evt.Type == contract.EventVisionClipSummaryV1 {
		summary, err := contract.DecodeVisionClipSummary(mustJSON(payloadMap))
		if err != nil {
			return nil, fmt.Errorf("vision contract invalid: %w", err)
		}
		if summary.EpisodeID != job.EpisodeID || summary.ClipID != job.ID || summary.CameraID != job.CameraID || summary.Topology.NodeID != job.NodeID || summary.Topology.Zone != job.Zone || summary.TopologyClass != topologyClass || summary.Trigger.Reason != job.TriggerReason || summary.Track.ID != fmt.Sprint(evt.TrackID) {
			return nil, errors.New("vision contract invalid: authoritative metadata mismatch")
		}
		return payloadMap, nil
	}
	alert, err := contract.DecodeVisionPreliminaryAlert(mustJSON(payloadMap))
	if err != nil {
		return nil, fmt.Errorf("vision contract invalid: %w", err)
	}
	if alert.EpisodeID != job.EpisodeID || alert.ClipID != job.ID || alert.CameraID != job.CameraID || alert.Topology.NodeID != job.NodeID || alert.Topology.Zone != job.Zone || alert.Track.ID != fmt.Sprint(evt.TrackID) {
		return nil, errors.New("vision contract invalid: authoritative metadata mismatch")
	}
	return payloadMap, nil
}

func rejectAuthoritativeSpoof(payload map[string]any, job *ClipJob) error {
	for key, expected := range map[string]string{"clip_id": job.ID, "camera_id": job.CameraID, "episode_id": job.EpisodeID, "node_id": job.NodeID} {
		if value, ok := payload[key]; ok && !sameAuthoritativeScalar(value, expected) {
			return fmt.Errorf("vision contract invalid: worker spoofed %s", key)
		}
	}
	if value, exists := payload["topology_class"]; exists && !sameAuthoritativeScalar(value, authoritativeTopologyClass(job)) {
		return fmt.Errorf("vision contract invalid: worker spoofed topology_class")
	}
	if rawTopology, exists := payload["topology"]; exists {
		topology, ok := rawTopology.(map[string]any)
		if !ok {
			return errors.New("vision contract invalid: worker spoofed topology")
		}
		if value, exists := topology["node_id"]; exists && !sameAuthoritativeScalar(value, job.NodeID) {
			return errors.New("vision contract invalid: worker spoofed topology.node_id")
		}
		if value, exists := topology["zone"]; exists && !sameAuthoritativeScalar(value, job.Zone) {
			return errors.New("vision contract invalid: worker spoofed topology.zone")
		}
	}
	if rawTrigger, exists := payload["trigger"]; exists {
		if text, scalar := rawTrigger.(string); scalar {
			if text != job.TriggerReason {
				return errors.New("vision contract invalid: worker spoofed trigger.reason")
			}
			return nil
		}
		trigger, ok := rawTrigger.(map[string]any)
		if !ok {
			return errors.New("vision contract invalid: worker spoofed trigger")
		}
		if value, exists := trigger["reason"]; exists && !sameAuthoritativeScalar(value, job.TriggerReason) {
			return errors.New("vision contract invalid: worker spoofed trigger.reason")
		}
		if value, exists := trigger["started_at"]; exists && !sameAuthoritativeTime(value, job.StartedAt) {
			return errors.New("vision contract invalid: worker spoofed trigger.started_at")
		}
	}
	return nil
}

func sameAuthoritativeScalar(value any, expected string) bool {
	text, ok := value.(string)
	return ok && text == expected
}

func sameAuthoritativeTime(value any, expected time.Time) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	return err == nil && parsed.Equal(expected)
}

func mustJSON(value map[string]any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

// PublishClipFailure lets the queue report a terminal timeout or delivery
// failure as a redacted Evidence V1 result. Clip transport remains private.
func PublishClipFailure(publisher Publisher, job *ClipJob, failureCode string) error {
	if job == nil {
		return errors.New("invalid clip job")
	}
	return publishUnavailableEvidence(publisher, job, safeEvidenceErrorCode(failureCode))
}

func safeEvidenceErrorCode(value string) string {
	for _, code := range []string{"worker_unavailable", "worker_timeout", "worker_empty_result", "worker_evidence_invalid", "worker_provenance_mismatch", "legacy_worker_output_quarantined"} {
		if value == code {
			return code
		}
	}
	return "worker_unavailable"
}

func clonePayload(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
