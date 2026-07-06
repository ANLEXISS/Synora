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

		payload, err := json.Marshal(
			evt.Payload,
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
