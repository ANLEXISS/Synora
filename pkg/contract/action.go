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
	ID            string         `json:"id,omitempty"`
	AutomationID  string         `json:"automation_id,omitempty"`
	ActionID      string         `json:"action_id,omitempty"`
	Type          string         `json:"type,omitempty"`
	Target        string         `json:"target,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
	SourceEventID string         `json:"source_event_id,omitempty"`
	DecisionID    string         `json:"decision_id,omitempty"`
	SituationID   string         `json:"situation_id,omitempty"`
	ClipID        string         `json:"clip_id,omitempty"`
	NodeID        string         `json:"node_id,omitempty"`
	DeviceID      string         `json:"device_id,omitempty"`
	CreatedAt     time.Time      `json:"created_at,omitempty"`
	TimeoutMs     int            `json:"timeout_ms,omitempty"`
	RetryCount    int            `json:"retry_count,omitempty"`
	CooldownKey   string         `json:"cooldown_key,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`

	// Legacy transport/action fields kept while older callers and adapters are migrated.
	Version        string    `json:"version,omitempty"`
	RequestID      string    `json:"request_id,omitempty"`
	CorrelationID  string    `json:"correlation_id,omitempty"`
	Source         string    `json:"source,omitempty"`
	Timestamp      time.Time `json:"timestamp,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Retry          int       `json:"retry,omitempty"`
	Action         Action    `json:"action,omitempty"`
}

type ActionResult struct {
	ID           string         `json:"id,omitempty"`
	RequestID    string         `json:"request_id,omitempty"`
	AutomationID string         `json:"automation_id,omitempty"`
	ActionID     string         `json:"action_id,omitempty"`
	Type         string         `json:"type,omitempty"`
	Target       string         `json:"target,omitempty"`
	Status       string         `json:"status"`
	Error        string         `json:"error,omitempty"`
	StartedAt    time.Time      `json:"started_at,omitempty"`
	FinishedAt   time.Time      `json:"finished_at,omitempty"`
	DurationMs   int64          `json:"duration_ms,omitempty"`
	Attempts     int            `json:"attempts,omitempty"`
	Data         map[string]any `json:"data,omitempty"`

	// Legacy metadata fields retained for older snapshot/API consumers.
	Version       string         `json:"version,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Source        string         `json:"source,omitempty"`
	Timestamp     time.Time      `json:"timestamp,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
}

const (
	ActionStatusSuccess          = "success"
	ActionStatusError            = "error"
	ActionStatusTimeout          = "timeout"
	ActionStatusSkipped          = "skipped"
	ActionStatusUnknownAction    = "unknown_action"
	ActionStatusSimulatedSuccess = "simulated_success"
)
