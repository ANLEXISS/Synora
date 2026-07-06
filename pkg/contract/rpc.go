package contract

import (
	"encoding/json"
	"time"
)

type RPCRequest struct {
	ID            string          `json:"id,omitempty"`
	Version       string          `json:"version,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Method        string          `json:"method"`
	Source        string          `json:"source,omitempty"`
	Target        string          `json:"target,omitempty"`
	Timestamp     time.Time       `json:"timestamp,omitempty"`
	Params        json.RawMessage `json:"params,omitempty"`
}

type RPCResponse struct {
	ID            string          `json:"id,omitempty"`
	Version       string          `json:"version,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Method        string          `json:"method,omitempty"`
	Source        string          `json:"source,omitempty"`
	Target        string          `json:"target,omitempty"`
	Timestamp     time.Time       `json:"timestamp,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Error         *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}
