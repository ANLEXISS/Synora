package recorder

import (
	"context"
	"fmt"
	"strings"

	"synora/internal/actions"
	"synora/pkg/contract"
)

type Recorder interface {
	Record(ctx context.Context, channel string, residents []string, value any) (map[string]any, error)
}

type Adapter struct {
	Recorder Recorder
}

func (a Adapter) Execute(ctx context.Context, request contract.ActionRequest) (actions.ExecutionResult, error) {
	channel := strings.TrimSpace(request.Action.Channel)
	if channel == "" {
		channel = strings.TrimSpace(request.Action.Device)
	}
	if channel == "" {
		return actions.ExecutionResult{}, fmt.Errorf("recorder channel required")
	}

	if a.Recorder == nil {
		return actions.ExecutionResult{
			Status: actions.StatusAccepted,
			Details: map[string]any{
				"adapter": "recorder",
				"dry_run": true,
				"channel": channel,
			},
		}, nil
	}

	details, err := a.Recorder.Record(ctx, channel, request.Action.Residents, request.Action.Value)
	if err != nil {
		return actions.ExecutionResult{}, err
	}
	if details == nil {
		details = map[string]any{}
	}
	details["adapter"] = "recorder"
	details["channel"] = channel

	return actions.ExecutionResult{
		Status:  actions.StatusAccepted,
		Details: details,
	}, nil
}
