package actions

import (
	"context"

	"synora/pkg/contract"
)

const (
	StatusSuccess          = contract.ActionStatusSuccess
	StatusError            = contract.ActionStatusError
	StatusTimeout          = contract.ActionStatusTimeout
	StatusSkipped          = contract.ActionStatusSkipped
	StatusUnknownAction    = contract.ActionStatusUnknownAction
	StatusSimulatedSuccess = contract.ActionStatusSimulatedSuccess

	StatusAccepted  = StatusSuccess
	StatusDuplicate = StatusSkipped
	StatusFailed    = StatusError
	StatusIgnored   = StatusSkipped
)

type Executor interface {
	Execute(ctx context.Context, request contract.ActionRequest) (ExecutionResult, error)
}

type ExecutionResult struct {
	Status  string
	Details map[string]any
}

type Publisher interface {
	Send(contract.Message) error
}
