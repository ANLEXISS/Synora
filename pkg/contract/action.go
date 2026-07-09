package contract

import "time"

type Action struct {
	Type      string         `json:"type,omitempty" yaml:"type,omitempty"`
	Device    string         `json:"device,omitempty" yaml:"device,omitempty"`
	Command   string         `json:"command,omitempty" yaml:"command,omitempty"`
	Value     any            `json:"value,omitempty" yaml:"value,omitempty"`
	Channel   string         `json:"channel,omitempty" yaml:"channel,omitempty"`
	Residents []string       `json:"residents,omitempty" yaml:"residents,omitempty"`
	Data      map[string]any `json:"data,omitempty" yaml:"data,omitempty"`
	TimeoutMs int            `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	Retry     int            `json:"retry,omitempty" yaml:"retry,omitempty"`
}

type ActionRequest struct {
	ID             string         `json:"id,omitempty"`
	Type           string         `json:"type,omitempty"`
	Version        string         `json:"version,omitempty"`
	RequestID      string         `json:"request_id,omitempty"`
	CorrelationID  string         `json:"correlation_id,omitempty"`
	Source         string         `json:"source,omitempty"`
	Target         string         `json:"target,omitempty"`
	Timestamp      time.Time      `json:"timestamp,omitempty"`
	CreatedAt      time.Time      `json:"created_at,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
	SourceEventID  string         `json:"source_event_id,omitempty"`
	DecisionID     string         `json:"decision_id,omitempty"`
	TimeoutMs      int            `json:"timeout_ms,omitempty"`
	Retry          int            `json:"retry,omitempty"`
	Action         Action         `json:"action"`
}

type ActionResult struct {
	ID            string         `json:"id,omitempty"`
	Version       string         `json:"version,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	ActionID      string         `json:"action_id,omitempty"`
	Source        string         `json:"source,omitempty"`
	Timestamp     time.Time      `json:"timestamp,omitempty"`
	Status        string         `json:"status"`
	Error         string         `json:"error,omitempty"`
	StartedAt     time.Time      `json:"started_at,omitempty"`
	FinishedAt    time.Time      `json:"finished_at,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
}
