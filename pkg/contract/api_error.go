package contract

import (
	"errors"
	"fmt"
	"strings"
)

const (
	ErrorInvalidJSON      = "invalid_json"
	ErrorInvalidRequest   = "invalid_request"
	ErrorNotFound         = "not_found"
	ErrorDuplicateID      = "duplicate_id"
	ErrorValidationFailed = "validation_failed"
	ErrorForbiddenAction  = "forbidden_action"
	ErrorTopologyRequired = "topology_required"
	ErrorUnsafeAutomation = "unsafe_automation"
	ErrorInternal         = "internal_error"
)

// APIError is created by Core and transported through RPC. The HTTP layer only
// maps its stable code to a status; it does not contain domain validation logic.
type APIError struct {
	Code    string `json:"error"`
	Message string `json:"message,omitempty"`
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) == "" {
		return e.Code
	}
	return e.Message
}

func NewAPIError(code string, format string, args ...any) error {
	code = strings.TrimSpace(code)
	if code == "" {
		code = ErrorInternal
	}
	return &APIError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func APIErrorCode(err error) string {
	var typed *APIError
	if errors.As(err, &typed) && strings.TrimSpace(typed.Code) != "" {
		return typed.Code
	}
	return ErrorInternal
}
