package contract

import "time"

type Action struct {
	Type      string   `json:"type,omitempty" yaml:"type,omitempty"`
	Device    string   `json:"device,omitempty" yaml:"device,omitempty"`
	Command   string   `json:"command,omitempty" yaml:"command,omitempty"`
	Value     any      `json:"value,omitempty" yaml:"value,omitempty"`
	Channel   string   `json:"channel,omitempty" yaml:"channel,omitempty"`
	Residents []string `json:"residents,omitempty" yaml:"residents,omitempty"`
}

type ActionRequest struct {
	ID             string    `json:"id,omitempty"`
	Version        string    `json:"version,omitempty"`
	RequestID      string    `json:"request_id,omitempty"`
	CorrelationID  string    `json:"correlation_id,omitempty"`
	Source         string    `json:"source,omitempty"`
	Target         string    `json:"target,omitempty"`
	Timestamp      time.Time `json:"timestamp,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Action         Action    `json:"action"`
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
	Details       map[string]any `json:"details,omitempty"`
}
