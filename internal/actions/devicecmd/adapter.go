package devicecmd

import (
	"context"
	"fmt"
	"strings"

	"synora/internal/actions"
	"synora/pkg/contract"
)

type Commander interface {
	Command(ctx context.Context, deviceID string, command string, value any) error
}

type IdempotentCommander interface {
	CommandWithIdempotency(ctx context.Context, deviceID string, command string, value any, commandID string, idempotencyKey string) error
}

type Adapter struct {
	Commander Commander
}

func (a Adapter) Execute(ctx context.Context, request contract.ActionRequest) (actions.ExecutionResult, error) {
	deviceID := strings.TrimSpace(request.Action.Device)
	if deviceID == "" {
		return actions.ExecutionResult{}, actions.PermanentError(fmt.Errorf("device id required"))
	}

	command := strings.TrimSpace(request.Action.Command)
	if command == "" {
		return actions.ExecutionResult{}, actions.PermanentError(fmt.Errorf("device command required"))
	}

	if a.Commander == nil {
		return actions.ExecutionResult{
			Status: actions.StatusAccepted,
			Details: map[string]any{
				"adapter": "devicecmd",
				"dry_run": true,
				"device":  deviceID,
				"command": command,
			},
		}, nil
	}

	var err error
	if commander, ok := a.Commander.(IdempotentCommander); ok {
		err = commander.CommandWithIdempotency(ctx, deviceID, command, request.Action.Value, request.CommandID, request.IdempotencyKey)
	} else {
		err = a.Commander.Command(ctx, deviceID, command, request.Action.Value)
	}
	if err != nil {
		return actions.ExecutionResult{}, err
	}

	return actions.ExecutionResult{
		Status: actions.StatusAccepted,
		Details: map[string]any{
			"adapter": "devicecmd",
			"device":  deviceID,
			"command": command,
		},
	}, nil
}
