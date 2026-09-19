package contract

// PolicyActionDecision is the deterministic, non-executing action assessment
// attached to a cognitive decision.
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

// ActionPlanItem is the compact action plan representation persisted in V1
// decisions and snapshots.
type ActionPlanItem struct {
	ID       string `json:"id,omitempty"`
	Command  string `json:"command"`
	Target   string `json:"target,omitempty"`
	Source   string `json:"source"`
	Priority int    `json:"priority"`
	Reason   string `json:"reason,omitempty"`
}
