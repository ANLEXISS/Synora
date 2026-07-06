package actions

import (
	"context"

	"synora/pkg/contract"
)

const (
	StatusAccepted  = "accepted"
	StatusDuplicate = "duplicate"
	StatusFailed    = "failed"
	StatusIgnored   = "ignored"
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
