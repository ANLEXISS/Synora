package vision

import (
	"encoding/json"
	"log"
	"time"

	"synora/internal/idgen"
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

	result, err := processor.Process(job)

	if err != nil {

		return err
	}

	for _, evt := range result.Events {
		payloadMap := clonePayload(evt.Payload)
		if _, ok := payloadMap["device_id"]; !ok && job != nil {
			payloadMap["device_id"] = job.CameraID
		}
		if _, ok := payloadMap["camera_id"]; !ok && job != nil {
			payloadMap["camera_id"] = job.CameraID
		}
		if _, ok := payloadMap["clip_id"]; !ok && job != nil {
			payloadMap["clip_id"] = job.ID
		}
		if evt.TrackID != nil {
			if _, ok := payloadMap["track_id"]; !ok {
				payloadMap["track_id"] = evt.TrackID
			}
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

			continue
		}

		err = publisher.Send(
			contract.Message{
				ID: idgen.New("msg"),

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

			continue
		}

		log.Printf(
			"event published type=%s clip=%s",
			evt.Type,
			job.ID,
		)
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
