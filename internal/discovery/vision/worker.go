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

	for index, evt := range result.Events {
		payloadMap := clonePayload(evt.Payload)
		stableEventID := fmt.Sprintf("%s:event:%d:%s", job.ID, index, evt.Type)
		// Camera identity comes from the accepted upload, not model output.
		payloadMap["device_id"] = job.CameraID
		payloadMap["camera_id"] = job.CameraID
		// The physical job is authoritative for clip identity. A model payload
		// cannot redirect an event to another clip or manufacture a reference.
		payloadMap["clip_id"] = job.ID
		if evt.TrackID != nil {
			if _, ok := payloadMap["track_id"]; !ok {
				payloadMap["track_id"] = evt.TrackID
			}
		}
		payloadMap["event_id"] = stableEventID
		payloadMap["activation_id"] = firstNonEmpty(payloadMap["activation_id"], job.ActivationID)
		payloadMap["sequence_key"] = firstNonEmpty(payloadMap["sequence_key"], job.SequenceKey)
		payloadMap["clip_index"] = firstInt(payloadMap["clip_index"], job.ClipIndex)
		if _, ok := payloadMap["node_id"]; !ok && job.NodeID != "" {
			payloadMap["node_id"] = job.NodeID
		}
		if _, ok := payloadMap["track_id"]; !ok && job.TrackID != "" {
			payloadMap["track_id"] = job.TrackID
		}

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

func firstNonEmpty(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func firstInt(value any, fallback int) int {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	}
	return fallback
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
