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
		return actions.ExecutionResult{}, fmt.Errorf("mqtt topic not configured")
	}

	payload, err := json.Marshal(request.Action.Value)
	if err != nil {
		return actions.ExecutionResult{}, err
	}

	if err := a.Publisher.Publish(topic, payload); err != nil {
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
