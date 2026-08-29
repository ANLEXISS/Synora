package actions

import (
	"context"
	"errors"
	"fmt"

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

// ErrorClass is deliberately small and stable on the ActionResult wire
// contract. Unknown adapter errors remain transient for compatibility, while
// adapters may explicitly stop retries for permanent or already-confirmed
// effects.
type ErrorClass string

const (
	ErrorClassNone      ErrorClass = ""
	ErrorClassTransient ErrorClass = "transient"
	ErrorClassPermanent ErrorClass = "permanent"
	ErrorClassTimeout   ErrorClass = "timeout"
	ErrorClassConfirmed ErrorClass = "confirmed"
	ErrorClassDuplicate ErrorClass = "duplicate"
)

// ClassifiedError lets an injected adapter describe whether retrying is safe.
// EffectConfirmed is used for a response that arrived after the adapter may
// already have applied a non-idempotent effect.
type ClassifiedError struct {
	Err             error
	Class           ErrorClass
	EffectConfirmed bool
}

func (e ClassifiedError) Error() string {
	if e.Err == nil {
		return string(e.Class)
	}
	return e.Err.Error()
}

func (e ClassifiedError) Unwrap() error { return e.Err }

func TransientError(err error) error {
	return ClassifiedError{Err: err, Class: ErrorClassTransient}
}

func PermanentError(err error) error {
	return ClassifiedError{Err: err, Class: ErrorClassPermanent}
}

func ConfirmedError(err error) error {
	return ClassifiedError{Err: err, Class: ErrorClassConfirmed, EffectConfirmed: true}
}

func ErrorClassOf(err error) ErrorClass {
	var classified ClassifiedError
	if errors.As(err, &classified) && classified.Class != "" {
		return classified.Class
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorClassTimeout
	}
	if err == nil {
		return ErrorClassNone
	}
	return ErrorClassTransient
}

func IsEffectConfirmed(err error) bool {
	var classified ClassifiedError
	return errors.As(err, &classified) && classified.EffectConfirmed
}

func ValidateErrorClass(class ErrorClass) error {
	switch class {
	case ErrorClassNone, ErrorClassTransient, ErrorClassPermanent, ErrorClassTimeout, ErrorClassConfirmed, ErrorClassDuplicate:
		return nil
	default:
		return fmt.Errorf("unknown action error class %q", class)
	}
}

type Executor interface {
	Execute(ctx context.Context, request contract.ActionRequest) (ExecutionResult, error)
}

type ExecutionResult struct {
	Status          string
	Details         map[string]any
	ErrorClass      ErrorClass
	EffectConfirmed bool
}

type Publisher interface {
	Send(contract.Message) error
}
