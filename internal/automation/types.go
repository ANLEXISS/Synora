package automation

import (
	"encoding/json"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"synora/pkg/contract"
)

type Condition = contract.Condition
type AutomationAction = contract.AutomationAction
type Trigger = contract.AutomationTrigger
type AutomationPatch = contract.AutomationPatch

type Rule struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Priority    int    `json:"priority,omitempty" yaml:"priority,omitempty"`

	Trigger        Trigger `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	ConditionLogic string  `json:"condition_logic,omitempty" yaml:"condition_logic,omitempty"`

	EventType string `json:"event,omitempty" yaml:"event,omitempty"`
	State     string `json:"state,omitempty" yaml:"state,omitempty"`
	Node      string `json:"node,omitempty" yaml:"node,omitempty"`

	MinScore        float64 `json:"min_score" yaml:"min_score"`
	ScoreMultiplier float64 `json:"score_multiplier" yaml:"score_multiplier"`
	ScoreOffset     float64 `json:"score_offset" yaml:"score_offset"`

	Conditions         []Condition        `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	Actions            []AutomationAction `json:"actions,omitempty" yaml:"actions,omitempty"`
	CooldownMs         int                `json:"cooldown_ms,omitempty" yaml:"cooldown_ms,omitempty"`
	TimeoutMs          int                `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	RetryCount         int                `json:"retry_count,omitempty" yaml:"retry_count,omitempty"`
	DryRun             bool               `json:"dry_run,omitempty" yaml:"dry_run,omitempty"`
	RequiresValidation bool               `json:"requires_validation,omitempty" yaml:"requires_validation,omitempty"`
	Status             string             `json:"status,omitempty" yaml:"status,omitempty"`
	ConfigError        string             `json:"config_error,omitempty" yaml:"config_error,omitempty"`
	Schedule           *Schedule          `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	CreatedAt          time.Time          `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt          time.Time          `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	DeletedAt          *time.Time         `json:"deleted_at,omitempty" yaml:"deleted_at,omitempty"`

	enabledSet bool
}

type Schedule struct {
	Always bool   `json:"always" yaml:"always"`
	Start  string `json:"start" yaml:"start"`
	End    string `json:"end" yaml:"end"`
}

type Store struct {
	rules map[string]Rule
	mu    sync.RWMutex
}

func (r *Rule) UnmarshalYAML(value *yaml.Node) error {
	type alias Rule
	raw := alias{Enabled: true, ConditionLogic: "all"}
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
		raw.Enabled = !containsSensitiveAction(raw.Actions)
	}
	if raw.ConditionLogic == "" {
		raw.ConditionLogic = "all"
	}
	*r = Rule(raw)
	r.enabledSet = hasEnabled
	return nil
}

func (r *Rule) UnmarshalJSON(data []byte) error {
	type alias Rule
	raw := alias{Enabled: true, ConditionLogic: "all"}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	hasEnabled := false
	if err := json.Unmarshal(data, &fields); err == nil {
		_, hasEnabled = fields["enabled"]
		if !hasEnabled {
			raw.Enabled = !containsSensitiveAction(raw.Actions)
		}
	}
	if raw.ConditionLogic == "" {
		raw.ConditionLogic = "all"
	}
	*r = Rule(raw)
	r.enabledSet = hasEnabled
	return nil
}
