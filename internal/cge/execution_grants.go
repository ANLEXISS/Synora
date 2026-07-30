package cge

import (
	"encoding/json"
	"fmt"
	"time"
)

const ExecutionCapabilityGrantSchemaVersion = "synora.cge.execution-capability-grant.v1"

type ExecutionCapabilityGrantStatus string

const (
	ExecutionCapabilityGrantIssued  ExecutionCapabilityGrantStatus = "issued"
	ExecutionCapabilityGrantRevoked ExecutionCapabilityGrantStatus = "revoked"
	ExecutionCapabilityGrantExpired ExecutionCapabilityGrantStatus = "expired"
)

// ExecutionCapabilityGrant is a bounded planning authorization. It records
// which operational capability was observed for one planned action; it is not
// a token, an invocation credential, or a dispatcher permission.
type ExecutionCapabilityGrant struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	GrantID            string `json:"grant_id" yaml:"grant_id"`
	PlanID             string `json:"plan_id" yaml:"plan_id"`
	DecisionID         string `json:"decision_id" yaml:"decision_id"`
	PlannedActionID    string `json:"planned_action_id" yaml:"planned_action_id"`
	RequestFingerprint string `json:"request_fingerprint" yaml:"request_fingerprint"`

	Capability string         `json:"capability" yaml:"capability"`
	ActionType string         `json:"action_type" yaml:"action_type"`
	Target     DecisionTarget `json:"target" yaml:"target"`

	StateRevision         uint64 `json:"state_revision" yaml:"state_revision"`
	PolicyRevision        uint64 `json:"policy_revision" yaml:"policy_revision"`
	AuthorizationRevision uint64 `json:"authorization_revision,omitempty" yaml:"authorization_revision,omitempty"`
	AuthorizationPolicyID string `json:"authorization_policy_id,omitempty" yaml:"authorization_policy_id,omitempty"`

	IssuedAt   time.Time                      `json:"issued_at" yaml:"issued_at"`
	ValidUntil time.Time                      `json:"valid_until" yaml:"valid_until"`
	Status     ExecutionCapabilityGrantStatus `json:"status" yaml:"status"`

	DryRun           bool   `json:"dry_run" yaml:"dry_run"`
	DispatchEligible bool   `json:"dispatch_eligible" yaml:"dispatch_eligible"`
	Fingerprint      string `json:"fingerprint" yaml:"fingerprint"`
}

func (s ExecutionCapabilityGrantStatus) Validate() error {
	switch s {
	case ExecutionCapabilityGrantIssued, ExecutionCapabilityGrantRevoked, ExecutionCapabilityGrantExpired:
		return nil
	default:
		return fmt.Errorf("invalid execution capability grant status %q", s)
	}
}

func (g ExecutionCapabilityGrant) Validate() error {
	if g.SchemaVersion != ExecutionCapabilityGrantSchemaVersion {
		return fmt.Errorf("invalid execution capability grant schema version %q", g.SchemaVersion)
	}
	for name, value := range map[string]string{
		"grant id": g.GrantID, "plan id": g.PlanID, "decision id": g.DecisionID,
		"planned action id": g.PlannedActionID, "request fingerprint": g.RequestFingerprint, "capability": g.Capability,
		"action type": g.ActionType, "fingerprint": g.Fingerprint,
	} {
		if err := validateAuthorityText(value, name, 256, true); err != nil {
			return err
		}
	}
	if err := g.Target.Validate(); err != nil {
		return err
	}
	if !validExecutionCapability(g.Capability) {
		return fmt.Errorf("execution capability is not allowlisted: %s", g.Capability)
	}
	if !validExecutionActionType(g.ActionType) {
		return fmt.Errorf("execution action type is not allowlisted: %s", g.ActionType)
	}
	if err := validateAuthorityText(g.AuthorizationPolicyID, "authorization policy id", 256, false); err != nil {
		return err
	}
	if g.StateRevision == 0 || g.PolicyRevision == 0 || g.IssuedAt.IsZero() || !g.ValidUntil.After(g.IssuedAt) {
		return fmt.Errorf("invalid execution capability grant binding")
	}
	if err := g.Status.Validate(); err != nil {
		return err
	}
	if !g.DryRun || g.DispatchEligible {
		return fmt.Errorf("execution capability grant is not fail-closed dry-run")
	}
	if ExecutionCapabilityGrantFingerprint(g) != g.Fingerprint {
		return fmt.Errorf("execution capability grant fingerprint mismatch")
	}
	return nil
}

// ExecutionCapabilityGrantFingerprint hashes only the closed grant fields.
// Dynamic payloads and credentials are intentionally not part of this model.
func ExecutionCapabilityGrantFingerprint(grant ExecutionCapabilityGrant) string {
	copy := grant
	copy.Fingerprint = ""
	payload, _ := json.Marshal(copy)
	return digest("execution-capability-grant", string(payload))
}

func validExecutionCapability(value string) bool {
	switch normalizeIntent(value) {
	case "record_clip", "increase_tracking_frequency", "notify_user", "notify_user_high_priority", "notify_user_critical", "turn_on_relevant_lights", "turn_on_security_lights", "trigger_approved_alarm_policy", "mark_security_degraded":
		return true
	default:
		return false
	}
}

func validExecutionActionType(value string) bool {
	switch value {
	case "record.clip", "device.command", "notify", "light.on", "siren", "mark_security_degraded":
		return true
	default:
		return false
	}
}
