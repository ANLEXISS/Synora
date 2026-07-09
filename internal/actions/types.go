package actions

import (
	"context"

	"synora/pkg/contract"
)

const (
	StatusSuccess = "success"
	StatusError   = "error"
	StatusTimeout = "timeout"
	StatusSkipped = "skipped"

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
