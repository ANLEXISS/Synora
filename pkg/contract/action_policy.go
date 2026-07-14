package contract

// ActionPolicy is the readable baseline reaction profile selected by danger
// level. Automations remain the contextual execution layer.
type ActionPolicy struct {
	Levels map[DangerLevel]ActionPolicyLevel `json:"levels" yaml:"levels"`
}

type ActionPolicyLevel struct {
	DangerLevel DangerLevel         `json:"danger_level" yaml:"danger_level"`
	Enabled     bool                `json:"enabled" yaml:"enabled"`
	Actions     []ActionPolicyEntry `json:"actions" yaml:"actions"`
}

type ActionPolicyEntry struct {
	ID              string      `json:"id" yaml:"id"`
	Command         string      `json:"command" yaml:"command"`
	Target          string      `json:"target,omitempty" yaml:"target,omitempty"`
	Enabled         bool        `json:"enabled" yaml:"enabled"`
	Priority        int         `json:"priority" yaml:"priority"`
	CooldownSeconds int         `json:"cooldown_seconds,omitempty" yaml:"cooldown_seconds,omitempty"`
	Conditions      []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
	Template        string      `json:"template,omitempty" yaml:"template,omitempty"`
	Message         string      `json:"message,omitempty" yaml:"message,omitempty"`
}

type PolicyActionDecision struct {
	ID              string `json:"id"`
	Command         string `json:"command"`
	Target          string `json:"target,omitempty"`
	Source          string `json:"source"`
	Priority        int    `json:"priority"`
	CooldownSeconds int    `json:"cooldown_seconds,omitempty"`
	Reason          string `json:"reason"`
	Enabled         bool   `json:"enabled"`
	Blocked         bool   `json:"blocked"`
	BlockedReason   string `json:"blocked_reason,omitempty"`
	Template        string `json:"template,omitempty"`
	Message         string `json:"message,omitempty"`
}

type ActionPlanItem struct {
	ID       string `json:"id,omitempty"`
	Command  string `json:"command"`
	Target   string `json:"target,omitempty"`
	Source   string `json:"source"`
	Priority int    `json:"priority"`
	Reason   string `json:"reason,omitempty"`
}

type ActionCatalogEntry struct {
	ID          string `json:"id"`
	Command     string `json:"command"`
	Description string `json:"description"`
	Dangerous   bool   `json:"dangerous"`
	Available   bool   `json:"available"`
}
