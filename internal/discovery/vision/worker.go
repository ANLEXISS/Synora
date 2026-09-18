package vision

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	if err := publishClipLifecycle(publisher, contract.EventClipProcessing, job, "", job.ID+":processing"); err != nil {
		return err
	}

	result, err := processor.Process(job)

	if err != nil {
		if publishFailure {
			_ = publishClipLifecycle(publisher, contract.EventClipFailed, job, "vision_processing_failed", job.ID+":failed")
		}
		return err
	}
	if result == nil {
		if publishFailure {
			_ = publishClipLifecycle(publisher, contract.EventClipFailed, job, "vision_empty_result", job.ID+":failed")
		}
		return errors.New("vision worker returned no result")
	}
	if job.Pipeline == "clip-v1" && len(result.Events) == 0 {
		err := errors.New("vision contract invalid: no clip-v1 summary")
		_ = publishClipLifecycle(publisher, contract.EventClipFailed, job, "vision_contract_invalid", job.ID+":failed")
		return err
	}
	prepared := make([]map[string]any, len(result.Events))
	for index, evt := range result.Events {
		payloadMap, prepareErr := prepareVisionEvent(evt, job, index)
		if prepareErr != nil {
			_ = publishClipLifecycle(publisher, contract.EventClipFailed, job, "vision_contract_invalid", job.ID+":failed")
			return prepareErr
		}
		prepared[index] = payloadMap
	}

	for index, evt := range result.Events {
		payloadMap := prepared[index]
		stableEventID := fmt.Sprintf("%s:event:%d:%s", job.ID, index, evt.Type)

		payload, err := json.Marshal(
			payloadMap,
		)

		if err != nil {

			log.Printf(
				"event marshal failed type=%s err=%v",
				evt.Type,
				err,
			)

			_ = publishClipLifecycle(publisher, contract.EventClipFailed, job, "vision_event_marshal_failed", job.ID+":failed")
			return err
		}

		err = publisher.Send(
			contract.Message{
				ID: stableEventID,

				Type: evt.Type,

				Kind: contract.KindEvent,

				Source: "discovery",

				Target: "core",

				Timestamp: time.Now().UTC(),

				Payload: payload,
			},
		)

		if err != nil {

			log.Printf(
				"failed to publish event=%s err=%v",
				evt.Type,
				err,
			)

			_ = publishClipLifecycle(publisher, contract.EventClipFailed, job, "vision_event_publish_failed", job.ID+":failed")
			return err
		}

		log.Printf(
			"event published type=%s clip=%s",
			evt.Type,
			job.ID,
		)
	}
	if err := publishClipLifecycle(publisher, contract.EventClipProcessed, job, "", job.ID+":processed"); err != nil {
		return err
	}
	if job.ActivationID != "" {
		if err := publishVisionEnd(publisher, job); err != nil {
			return err
		}
	}
	return nil
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
	if evt.Type != contract.EventVisionClipSummaryV1 && evt.Type != contract.EventVisionPreliminaryAlertV1 {
		return nil, fmt.Errorf("vision contract invalid: event %q is not admitted for clip-v1", evt.Type)
	}
	if job.EpisodeID == "" || job.NodeID == "" || job.Zone == "" || job.TriggerReason == "" || job.StartedAt.IsZero() {
		return nil, errors.New("vision contract invalid: incomplete authoritative clip metadata")
	}
	payloadMap["episode_id"] = job.EpisodeID
	payloadMap["topology"] = map[string]any{"node_id": job.NodeID, "zone": job.Zone}
	payloadMap["trigger"] = map[string]any{"reason": job.TriggerReason, "started_at": job.StartedAt}
	if evt.TrackID == nil {
		return nil, errors.New("vision contract invalid: track id is required")
	}
	if evt.Type == contract.EventVisionClipSummaryV1 {
		summary, err := contract.DecodeVisionClipSummary(mustJSON(payloadMap))
		if err != nil {
			return nil, fmt.Errorf("vision contract invalid: %w", err)
		}
		if summary.EpisodeID != job.EpisodeID || summary.ClipID != job.ID || summary.CameraID != job.CameraID || summary.Topology.NodeID != job.NodeID || summary.Topology.Zone != job.Zone || summary.Trigger.Reason != job.TriggerReason || summary.Track.ID != fmt.Sprint(evt.TrackID) {
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

func publishVisionEnd(publisher Publisher, job *ClipJob) error {
	payload, err := json.Marshal(map[string]any{
		"event_id":      job.ID + ":end",
		"activation_id": job.ActivationID,
		"sequence_key":  job.SequenceKey,
		"clip_id":       job.ID,
		"clip_index":    job.ClipIndex,
		"camera_id":     job.CameraID,
		"device_id":     job.CameraID,
		"node_id":       job.NodeID,
		"track_id":      job.TrackID,
	})
	if err != nil {
		return err
	}
	return publisher.Send(contract.Message{
		ID: job.ID + ":end", Type: contract.EventVisionEnd, Kind: contract.KindEvent,
		Source: "discovery", Target: "core", Timestamp: time.Now().UTC(), Payload: payload,
	})
}

func publishClipLifecycle(publisher Publisher, eventType string, job *ClipJob, failureCode, id string) error {
	if publisher == nil {
		return nil
	}
	payload, err := json.Marshal(contract.ClipLifecyclePayload{
		Clip:   contract.Clip{ID: job.ID, CameraID: job.CameraID, ActivationID: job.ActivationID, ClipIndex: job.ClipIndex, SequenceKey: job.SequenceKey, TrackID: job.TrackID, NodeID: job.NodeID},
		ClipID: job.ID, CameraID: job.CameraID, FailureCode: failureCode,
	})
	if err != nil {
		return err
	}
	return publisher.Send(contract.Message{ID: id, Type: eventType, Kind: contract.KindEvent, Source: "discovery", Target: "core", Timestamp: time.Now().UTC(), Payload: payload})
}

// PublishClipFailure lets the queue report a terminal timeout or delivery
// failure when the normal worker callback could not produce the lifecycle
// event itself.
func PublishClipFailure(publisher Publisher, job *ClipJob, failureCode string) error {
	if job == nil {
		return errors.New("invalid clip job")
	}
	if err := publishClipLifecycle(publisher, contract.EventClipFailed, job, failureCode, job.ID+":failed"); err != nil {
		return err
	}
	if job.ActivationID != "" {
		return publishVisionEnd(publisher, job)
	}
	return nil
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
