package contract

import (
	"encoding/json"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	AutomationStatusObserving = "observing"
	AutomationStatusSuggested = "suggested"
	AutomationStatusApproved  = "approved"
	AutomationStatusRejected  = "rejected"
	AutomationStatusDisabled  = "disabled"
)

type Automation struct {
	ID                 string             `json:"id" yaml:"id"`
	Name               string             `json:"name,omitempty" yaml:"name,omitempty"`
	Enabled            bool               `json:"enabled" yaml:"enabled"`
	Description        string             `json:"description,omitempty" yaml:"description,omitempty"`
	Priority           int                `json:"priority,omitempty" yaml:"priority,omitempty"`
	Trigger            AutomationTrigger  `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	Conditions         []Condition        `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	ConditionLogic     string             `json:"condition_logic,omitempty" yaml:"condition_logic,omitempty"`
	Actions            []AutomationAction `json:"actions,omitempty" yaml:"actions,omitempty"`
	CooldownMs         int                `json:"cooldown_ms,omitempty" yaml:"cooldown_ms,omitempty"`
	TimeoutMs          int                `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	RetryCount         int                `json:"retry_count,omitempty" yaml:"retry_count,omitempty"`
	DryRun             bool               `json:"dry_run,omitempty" yaml:"dry_run,omitempty"`
	RequiresValidation bool               `json:"requires_validation,omitempty" yaml:"requires_validation,omitempty"`
	Status             string             `json:"status,omitempty" yaml:"status,omitempty"`
	ConfigError        string             `json:"config_error,omitempty" yaml:"config_error,omitempty"`
	Schedule           any                `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	CreatedAt          time.Time          `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt          time.Time          `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	DeletedAt          *time.Time         `json:"deleted_at,omitempty" yaml:"deleted_at,omitempty"`
}

type AutomationView struct {
	ID                 string             `json:"id" yaml:"id"`
	Name               string             `json:"name,omitempty" yaml:"name,omitempty"`
	Enabled            bool               `json:"enabled" yaml:"enabled"`
	Description        string             `json:"description,omitempty" yaml:"description,omitempty"`
	Priority           int                `json:"priority,omitempty" yaml:"priority,omitempty"`
	Trigger            AutomationTrigger  `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	Conditions         []Condition        `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	ConditionLogic     string             `json:"condition_logic,omitempty" yaml:"condition_logic,omitempty"`
	Actions            []AutomationAction `json:"actions,omitempty" yaml:"actions,omitempty"`
	CooldownMs         int                `json:"cooldown_ms,omitempty" yaml:"cooldown_ms,omitempty"`
	TimeoutMs          int                `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	RetryCount         int                `json:"retry_count,omitempty" yaml:"retry_count,omitempty"`
	DryRun             bool               `json:"dry_run,omitempty" yaml:"dry_run,omitempty"`
	RequiresValidation bool               `json:"requires_validation,omitempty" yaml:"requires_validation,omitempty"`
	Status             string             `json:"status,omitempty" yaml:"status,omitempty"`
	ConfigError        string             `json:"config_error,omitempty" yaml:"config_error,omitempty"`
	Schedule           any                `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	CreatedAt          time.Time          `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt          time.Time          `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	DeletedAt          *time.Time         `json:"deleted_at,omitempty" yaml:"deleted_at,omitempty"`
}

type AutomationTrigger struct {
	EventType     string  `json:"event_type,omitempty" yaml:"event_type,omitempty"`
	DeviceID      string  `json:"device_id,omitempty" yaml:"device_id,omitempty"`
	NodeID        string  `json:"node_id,omitempty" yaml:"node_id,omitempty"`
	ResidentID    string  `json:"resident_id,omitempty" yaml:"resident_id,omitempty"`
	MinScore      float64 `json:"min_score,omitempty" yaml:"min_score,omitempty"`
	State         string  `json:"state,omitempty" yaml:"state,omitempty"`
	SituationType string  `json:"situation_type,omitempty" yaml:"situation_type,omitempty"`
}

type AutomationPatch struct {
	Name               *string             `json:"name,omitempty"`
	Enabled            *bool               `json:"enabled,omitempty"`
	Trigger            *AutomationTrigger  `json:"trigger,omitempty"`
	Conditions         *[]Condition        `json:"conditions,omitempty"`
	ConditionLogic     *string             `json:"condition_logic,omitempty"`
	Actions            *[]AutomationAction `json:"actions,omitempty"`
	CooldownMs         *int                `json:"cooldown_ms,omitempty"`
	TimeoutMs          *int                `json:"timeout_ms,omitempty"`
	RetryCount         *int                `json:"retry_count,omitempty"`
	DryRun             *bool               `json:"dry_run,omitempty"`
	RequiresValidation *bool               `json:"requires_validation,omitempty"`
	Status             *string             `json:"status,omitempty"`
}

type Condition struct {
	ID        string `json:"id,omitempty" yaml:"id,omitempty"`
	Field     string `json:"field" yaml:"field"`
	Op        string `json:"op" yaml:"op"`
	Value     any    `json:"value,omitempty" yaml:"value,omitempty"`
	ValueType string `json:"value_type,omitempty" yaml:"value_type,omitempty"`
	Negate    bool   `json:"negate,omitempty" yaml:"negate,omitempty"`
}

type AutomationAction struct {
	ID          string         `json:"id,omitempty" yaml:"id,omitempty"`
	Type        string         `json:"type,omitempty" yaml:"type,omitempty"`
	Target      string         `json:"target,omitempty" yaml:"target,omitempty"`
	Data        map[string]any `json:"data,omitempty" yaml:"data,omitempty"`
	TimeoutMs   int            `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	RetryCount  int            `json:"retry_count,omitempty" yaml:"retry_count,omitempty"`
	Enabled     bool           `json:"enabled" yaml:"enabled"`
	Order       int            `json:"order,omitempty" yaml:"order,omitempty"`
	CooldownKey string         `json:"cooldown_key,omitempty" yaml:"cooldown_key,omitempty"`

	// Legacy readable YAML fields accepted during migration.
	Device    string   `json:"device,omitempty" yaml:"device,omitempty"`
	Command   string   `json:"command,omitempty" yaml:"command,omitempty"`
	Value     any      `json:"value,omitempty" yaml:"value,omitempty"`
	Channel   string   `json:"channel,omitempty" yaml:"channel,omitempty"`
	Residents []string `json:"residents,omitempty" yaml:"residents,omitempty"`
	Retry     int      `json:"retry,omitempty" yaml:"retry,omitempty"`
}

func (a *AutomationAction) UnmarshalJSON(data []byte) error {
	type alias AutomationAction
	raw := alias{Enabled: true}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err == nil {
		if _, ok := fields["enabled"]; !ok {
			raw.Enabled = true
		}
	}
	*a = AutomationAction(raw)
	return nil
}

func (a *AutomationAction) UnmarshalYAML(value *yaml.Node) error {
	type alias AutomationAction
	raw := alias{Enabled: true}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	hasEnabled := false
	for i := 0; i+1 < len(value.Content); i += 2 {
		if value.Content[i].Value == "enabled" {
			hasEnabled = true
			break
		}
	}
	if !hasEnabled {
		raw.Enabled = true
	}
	*a = AutomationAction(raw)
	return nil
}
