package contract

import "time"

const (
	ValidationStatusPending  = "pending"
	ValidationStatusAccepted = "accepted"
	ValidationStatusRejected = "rejected"
	ValidationStatusIgnored  = "ignored"
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

	SituationID string `json:"situation_id,omitempty"`

	Reason   string   `json:"reason,omitempty"`
	Evidence []string `json:"evidence,omitempty"`

	ProposedIdentity string `json:"proposed_identity,omitempty"`

	NodeID string `json:"node_id,omitempty"`
	ClipID string `json:"clip_id,omitempty"`

	Status string `json:"status"`

	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

type ValidationResolveRequest struct {
	ID string `json:"id"`

	Action string `json:"action"`

	ProposedIdentity string `json:"proposed_identity,omitempty"`
}
