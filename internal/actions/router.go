package actions

import (
	"context"
	"fmt"
	"strings"

	"synora/pkg/contract"
)

type Router struct {
	MQTT      Executor
	DeviceCmd Executor
	Recorder  Executor
	Fallback  Executor
}

func (r Router) Execute(ctx context.Context, request contract.ActionRequest) (ExecutionResult, error) {
	executor := r.executorFor(request.Action)
	if executor == nil {
		return ExecutionResult{
			Status: StatusIgnored,
			Details: map[string]any{
				"reason": "no executor configured",
			},
		}, nil
	}

	return executor.Execute(ctx, request)
}

func (r Router) executorFor(action contract.Action) Executor {
	switch {
	case action.Device != "":
		return r.DeviceCmd
	case isRecorderAction(action):
		return r.Recorder
	case isMQTTAction(action):
		return r.MQTT
	default:
		return r.Fallback
	}
}

func isRecorderAction(action contract.Action) bool {
	actionType := strings.ToLower(strings.TrimSpace(action.Type))
	return actionType == "record" ||
		actionType == "record.clip" ||
		actionType == "recorder" ||
		strings.HasPrefix(actionType, "recorder.")
}

func isMQTTAction(action contract.Action) bool {
	actionType := strings.ToLower(strings.TrimSpace(action.Type))
	return actionType == "mqtt" ||
		strings.HasPrefix(actionType, "mqtt.") ||
		action.Channel != ""
}

type DryRunExecutor struct {
	Adapter string
}

func (e DryRunExecutor) Execute(_ context.Context, request contract.ActionRequest) (ExecutionResult, error) {
	adapter := e.Adapter
	if adapter == "" {
		adapter = "dry_run"
	}

	return ExecutionResult{
		Status: StatusAccepted,
		Details: map[string]any{
			"adapter": adapter,
			"dry_run": true,
			"action":  fmt.Sprintf("%+v", request.Action),
		},
	}, nil
}
