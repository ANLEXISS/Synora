package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"synora/internal/actions"
	"synora/pkg/contract"
)

type Publisher interface {
	Publish(topic string, payload []byte) error
}

type IdempotentPublisher interface {
	PublishWithIdempotency(topic string, payload []byte, commandID string, idempotencyKey string) error
}

type Adapter struct {
	Publisher Publisher
	Topic     string
}

func (a Adapter) Execute(_ context.Context, request contract.ActionRequest) (actions.ExecutionResult, error) {
	if a.Publisher == nil {
		return actions.ExecutionResult{
			Status: actions.StatusAccepted,
			Details: map[string]any{
				"adapter": "mqtt",
				"dry_run": true,
			},
		}, nil
	}

	topic := strings.TrimSpace(a.Topic)
	if topic == "" {
		topic = strings.TrimSpace(request.Action.Channel)
	}
	if topic == "" {
		return actions.ExecutionResult{}, actions.PermanentError(fmt.Errorf("mqtt topic not configured"))
	}

	payload, err := json.Marshal(request.Action.Value)
	if err != nil {
		return actions.ExecutionResult{}, actions.PermanentError(err)
	}

	if publisher, ok := a.Publisher.(IdempotentPublisher); ok {
		err = publisher.PublishWithIdempotency(topic, payload, request.CommandID, request.IdempotencyKey)
	} else {
		err = a.Publisher.Publish(topic, payload)
	}
	if err != nil {
		return actions.ExecutionResult{}, err
	}

	return actions.ExecutionResult{
		Status: actions.StatusAccepted,
		Details: map[string]any{
			"adapter": "mqtt",
			"topic":   topic,
		},
	}, nil
}
