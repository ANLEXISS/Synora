package contract

import "time"

const (
	ValidationStatusPending   = "pending"
	ValidationStatusAccepted  = "accepted"
	ValidationStatusApproved  = "approved"
	ValidationStatusRejected  = "rejected"
	ValidationStatusCorrected = "corrected"
	ValidationStatusIgnored   = "ignored"
)

const (
	ValidationTypeIdentity            = "identity"
	ValidationTypeEventClassification = "event_classification"
	ValidationTypeBehaviorApproval    = "behavior_approval"
	ValidationTypeActionFeedback      = "action_feedback"
	ValidationTypeFalsePositive       = "false_positive"
	ValidationTypeFalseNegative       = "false_negative"
)

const (
	ValidationActionAccept         = "accept"
	ValidationActionReject         = "reject"
	ValidationActionIgnore         = "ignore"
	ValidationActionAssignIdentity = "assign_identity"
)

type ValidationRequest struct {
	ID string `json:"id"`

	DecisionID string `json:"decision_id,omitempty"`
	EventID    string `json:"event_id,omitempty"`
	BehaviorID string `json:"behavior_id,omitempty"`
	ResidentID string `json:"resident_id,omitempty"`
	Type       string `json:"type,omitempty"`

	SituationID string `json:"situation_id,omitempty"`

	Reason   string   `json:"reason,omitempty"`
	Evidence []string `json:"evidence,omitempty"`

	ProposedIdentity string `json:"proposed_identity,omitempty"`

	NodeID string `json:"node_id,omitempty"`
	ClipID string `json:"clip_id,omitempty"`

	Status     string         `json:"status"`
	Correction map[string]any `json:"correction,omitempty"`
	Notes      string         `json:"notes,omitempty"`
	Enabled    bool           `json:"enabled"`
	DeletedAt  *time.Time     `json:"deleted_at,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

type ValidationResolveRequest struct {
	ID string `json:"id"`

	Action string `json:"action"`

	ProposedIdentity string `json:"proposed_identity,omitempty"`
}
