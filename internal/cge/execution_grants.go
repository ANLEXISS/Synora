package cge

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ConfiguredExecutionCapabilityGrantSchemaVersion = "synora.cge.configured-execution-capability-grant.v1"
	GrantSnapshotSchemaVersion                      = "synora.cge.grant-snapshot.v1"
	AppliedExecutionGrantSchemaVersion              = "synora.cge.applied-execution-grant.v1"
	maxConfiguredExecutionGrants                    = 256
)

// ConfiguredExecutionCapabilityGrant is Core-owned authorization
// configuration. It is a standing, bounded permission and is intentionally
// distinct from the evidence copied into an individual ExecutionPlan.
type ConfiguredExecutionCapabilityGrant struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	GrantID    string         `json:"grant_id" yaml:"grant_id"`
	Capability string         `json:"capability" yaml:"capability"`
	ActionType string         `json:"action_type,omitempty" yaml:"action_type,omitempty"`
	Target     DecisionTarget `json:"target" yaml:"target"`

	AuthorizationPolicyID string `json:"authorization_policy_id,omitempty" yaml:"authorization_policy_id,omitempty"`
	MaxPriority           int    `json:"max_priority,omitempty" yaml:"max_priority,omitempty"`

	ValidFrom  time.Time `json:"valid_from" yaml:"valid_from"`
	ValidUntil time.Time `json:"valid_until" yaml:"valid_until"`
	Revision   uint64    `json:"revision" yaml:"revision"`
	Enabled    bool      `json:"enabled" yaml:"enabled"`

	Fingerprint string `json:"fingerprint" yaml:"fingerprint"`
}

func (g ConfiguredExecutionCapabilityGrant) Validate() error {
	if g.SchemaVersion != ConfiguredExecutionCapabilityGrantSchemaVersion {
		return fmt.Errorf("invalid configured execution capability grant schema version %q", g.SchemaVersion)
	}
	for name, value := range map[string]string{
		"configured grant id":    g.GrantID,
		"configured capability":  g.Capability,
		"configured fingerprint": g.Fingerprint,
	} {
		if err := validateAuthorityText(value, name, 256, true); err != nil {
			return err
		}
	}
	if err := g.Target.Validate(); err != nil {
		return err
	}
	if !validExecutionCapability(g.Capability) {
		return fmt.Errorf("configured execution capability is not allowlisted: %s", g.Capability)
	}
	if g.ActionType != "" && !validExecutionActionType(g.ActionType) {
		return fmt.Errorf("configured execution action type is not allowlisted: %s", g.ActionType)
	}
	if err := validateAuthorityText(g.AuthorizationPolicyID, "configured authorization policy id", 256, false); err != nil {
		return err
	}
	if g.MaxPriority < 0 || g.MaxPriority > 100 || g.ValidFrom.IsZero() || g.ValidUntil.IsZero() || !g.ValidUntil.After(g.ValidFrom) || g.Revision == 0 {
		return fmt.Errorf("invalid configured execution capability grant binding")
	}
	if ConfiguredExecutionCapabilityGrantFingerprint(g) != g.Fingerprint {
		return fmt.Errorf("configured execution capability grant fingerprint mismatch")
	}
	return nil
}

func ConfiguredExecutionCapabilityGrantFingerprint(grant ConfiguredExecutionCapabilityGrant) string {
	copy := grant
	copy.Fingerprint = ""
	payload, _ := json.Marshal(copy)
	return digest("configured-execution-capability-grant", string(payload))
}

func (g ConfiguredExecutionCapabilityGrant) Clone() ConfiguredExecutionCapabilityGrant {
	return g
}

// GrantSnapshot is the immutable Core configuration view used by the
// planner. It contains permissions, never plan-specific application data.
type GrantSnapshot struct {
	SchemaVersion string                               `json:"schema_version" yaml:"schema_version"`
	Revision      uint64                               `json:"revision" yaml:"revision"`
	CapturedAt    time.Time                            `json:"captured_at" yaml:"captured_at"`
	FreshUntil    time.Time                            `json:"fresh_until" yaml:"fresh_until"`
	Grants        []ConfiguredExecutionCapabilityGrant `json:"grants" yaml:"grants"`
	Fingerprint   string                               `json:"fingerprint" yaml:"fingerprint"`
}

func (s GrantSnapshot) Validate() error {
	if s.SchemaVersion != GrantSnapshotSchemaVersion || s.Revision == 0 || s.CapturedAt.IsZero() || !s.FreshUntil.After(s.CapturedAt) || s.Fingerprint == "" {
		return fmt.Errorf("invalid grant snapshot")
	}
	if len(s.Grants) > maxConfiguredExecutionGrants || GrantSnapshotFingerprint(s) != s.Fingerprint {
		return fmt.Errorf("invalid grant snapshot fingerprint or bound")
	}
	seen := make(map[string]struct{}, len(s.Grants))
	for _, grant := range s.Grants {
		if _, ok := seen[grant.GrantID]; ok {
			return fmt.Errorf("duplicate configured execution grant")
		}
		seen[grant.GrantID] = struct{}{}
		if err := grant.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func GrantSnapshotFingerprint(snapshot GrantSnapshot) string {
	copy := snapshot
	copy.Fingerprint = ""
	copy.Grants = append([]ConfiguredExecutionCapabilityGrant(nil), snapshot.Grants...)
	sort.Slice(copy.Grants, func(i, j int) bool { return copy.Grants[i].GrantID < copy.Grants[j].GrantID })
	payload, _ := json.Marshal(copy)
	return digest("grant-snapshot", string(payload))
}

func (s GrantSnapshot) Clone() GrantSnapshot {
	out := s
	out.Grants = append([]ConfiguredExecutionCapabilityGrant(nil), s.Grants...)
	return out
}

func EmptyGrantSnapshot(at time.Time) GrantSnapshot {
	at = at.UTC()
	snapshot := GrantSnapshot{SchemaVersion: GrantSnapshotSchemaVersion, Revision: 1, CapturedAt: at, FreshUntil: at.Add(24 * time.Hour), Grants: []ConfiguredExecutionCapabilityGrant{}}
	snapshot.Fingerprint = GrantSnapshotFingerprint(snapshot)
	return snapshot
}

type configuredExecutionGrantFile struct {
	SchemaVersion string                               `yaml:"schema_version"`
	Revision      uint64                               `yaml:"revision"`
	Grants        []ConfiguredExecutionCapabilityGrant `yaml:"grants"`
}

// LoadConfiguredExecutionGrantSnapshot is a Core configuration boundary. It
// reads only the closed grant list; it never creates applied plan evidence.
func LoadConfiguredExecutionGrantSnapshot(path string, capturedAt time.Time) (GrantSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GrantSnapshot{}, err
	}
	var file configuredExecutionGrantFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return GrantSnapshot{}, fmt.Errorf("configured execution grants: %w", err)
	}
	if file.SchemaVersion == "" {
		file.SchemaVersion = ConfiguredExecutionCapabilityGrantSchemaVersion
	}
	if file.Revision == 0 {
		file.Revision = 1
	}
	for i := range file.Grants {
		if file.Grants[i].SchemaVersion == "" {
			file.Grants[i].SchemaVersion = ConfiguredExecutionCapabilityGrantSchemaVersion
		}
		if file.Grants[i].Fingerprint == "" {
			file.Grants[i].Fingerprint = ConfiguredExecutionCapabilityGrantFingerprint(file.Grants[i])
		}
	}
	at := capturedAt.UTC()
	snapshot := GrantSnapshot{SchemaVersion: GrantSnapshotSchemaVersion, Revision: file.Revision, CapturedAt: at, FreshUntil: at.Add(24 * time.Hour), Grants: file.Grants}
	snapshot.Fingerprint = GrantSnapshotFingerprint(snapshot)
	if file.SchemaVersion != ConfiguredExecutionCapabilityGrantSchemaVersion {
		return GrantSnapshot{}, fmt.Errorf("invalid configured execution grant file schema version %q", file.SchemaVersion)
	}
	if err := snapshot.Validate(); err != nil {
		return GrantSnapshot{}, err
	}
	return snapshot, nil
}

// AppliedExecutionGrant is plan-local evidence that a configured grant was
// selected. It is not a new authorization and cannot be used for dispatch.
type AppliedExecutionGrant struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	AppliedGrantID     string `json:"applied_grant_id" yaml:"applied_grant_id"`
	ConfiguredGrantID  string `json:"configured_grant_id" yaml:"configured_grant_id"`
	PlanID             string `json:"plan_id" yaml:"plan_id"`
	DecisionID         string `json:"decision_id" yaml:"decision_id"`
	PlannedActionID    string `json:"planned_action_id" yaml:"planned_action_id"`
	RequestFingerprint string `json:"request_fingerprint" yaml:"request_fingerprint"`

	Capability string         `json:"capability" yaml:"capability"`
	ActionType string         `json:"action_type" yaml:"action_type"`
	Target     DecisionTarget `json:"target" yaml:"target"`

	StateRevision         uint64 `json:"state_revision" yaml:"state_revision"`
	PolicyRevision        uint64 `json:"policy_revision" yaml:"policy_revision"`
	GrantSnapshotRevision uint64 `json:"grant_snapshot_revision" yaml:"grant_snapshot_revision"`

	AppliedAt  time.Time `json:"applied_at" yaml:"applied_at"`
	ValidUntil time.Time `json:"valid_until" yaml:"valid_until"`

	DryRun           bool   `json:"dry_run" yaml:"dry_run"`
	DispatchEligible bool   `json:"dispatch_eligible" yaml:"dispatch_eligible"`
	Fingerprint      string `json:"fingerprint" yaml:"fingerprint"`
}

func (g AppliedExecutionGrant) Validate() error {
	if g.SchemaVersion != AppliedExecutionGrantSchemaVersion {
		return fmt.Errorf("invalid applied execution grant schema version %q", g.SchemaVersion)
	}
	for name, value := range map[string]string{
		"applied grant id": g.AppliedGrantID, "configured grant id": g.ConfiguredGrantID,
		"plan id": g.PlanID, "decision id": g.DecisionID, "planned action id": g.PlannedActionID,
		"request fingerprint": g.RequestFingerprint, "capability": g.Capability,
		"action type": g.ActionType, "fingerprint": g.Fingerprint,
	} {
		if err := validateAuthorityText(value, name, 256, true); err != nil {
			return err
		}
	}
	if err := g.Target.Validate(); err != nil {
		return err
	}
	if !validExecutionCapability(g.Capability) || !validExecutionActionType(g.ActionType) || g.StateRevision == 0 || g.PolicyRevision == 0 || g.GrantSnapshotRevision == 0 || g.AppliedAt.IsZero() || !g.ValidUntil.After(g.AppliedAt) {
		return fmt.Errorf("invalid applied execution grant binding")
	}
	if !g.DryRun || g.DispatchEligible {
		return fmt.Errorf("applied execution grant is not fail-closed dry-run")
	}
	if AppliedExecutionGrantFingerprint(g) != g.Fingerprint {
		return fmt.Errorf("applied execution grant fingerprint mismatch")
	}
	return nil
}

func AppliedExecutionGrantFingerprint(grant AppliedExecutionGrant) string {
	copy := grant
	copy.Fingerprint = ""
	payload, _ := json.Marshal(copy)
	return digest("applied-execution-grant", string(payload))
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

func normalizedConfiguredCapability(value string) string {
	return normalizeIntent(strings.TrimSpace(value))
}
