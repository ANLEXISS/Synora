package cge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
// distinct from evidence copied into an individual execution plan.
type ConfiguredExecutionCapabilityGrant struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	GrantID    string         `json:"grant_id" yaml:"grant_id"`
	Capability string         `json:"capability" yaml:"capability"`
	ActionType string         `json:"action_type" yaml:"action_type"`
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
		"configured action type": g.ActionType,
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
		return fmt.Errorf("configured execution capability is not allowlisted or canonical: %s", g.Capability)
	}
	if !validExecutionActionType(g.ActionType) {
		return fmt.Errorf("configured execution action type is not allowlisted: %s", g.ActionType)
	}
	if !actionTypeMatchesCapability(g.Capability, g.ActionType) {
		return fmt.Errorf("configured execution action type does not match capability")
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

// ConfiguredExecutionCapabilityGrantFingerprint hashes the canonical grant
// content. Runtime capture metadata and the supplied fingerprint are not part
// of the grant identity.
func ConfiguredExecutionCapabilityGrantFingerprint(grant ConfiguredExecutionCapabilityGrant) string {
	copy := canonicalConfiguredGrant(grant)
	copy.Fingerprint = ""
	return grantDigest("configured-execution-capability-grant", copy)
}

func (g ConfiguredExecutionCapabilityGrant) Clone() ConfiguredExecutionCapabilityGrant {
	return g
}

// GrantSnapshot is the immutable Core configuration view used at the CGE
// boundary. Its fingerprint covers only configured content; CapturedAt and
// FreshUntil are runtime freshness metadata and therefore do not change the
// identity across process restarts.
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

type grantSnapshotFingerprintMaterial struct {
	SchemaVersion string                               `json:"schema_version"`
	Revision      uint64                               `json:"revision"`
	Grants        []ConfiguredExecutionCapabilityGrant `json:"grants"`
}

func GrantSnapshotFingerprint(snapshot GrantSnapshot) string {
	grants := make([]ConfiguredExecutionCapabilityGrant, len(snapshot.Grants))
	for i, grant := range snapshot.Grants {
		grants[i] = canonicalConfiguredGrant(grant)
		grants[i].Fingerprint = ""
	}
	sort.Slice(grants, func(i, j int) bool {
		if grants[i].GrantID != grants[j].GrantID {
			return grants[i].GrantID < grants[j].GrantID
		}
		return grants[i].Fingerprint < grants[j].Fingerprint
	})
	return grantDigest("grant-snapshot", grantSnapshotFingerprintMaterial{
		SchemaVersion: snapshot.SchemaVersion,
		Revision:      snapshot.Revision,
		Grants:        grants,
	})
}

func (s GrantSnapshot) Clone() GrantSnapshot {
	out := s
	out.Grants = make([]ConfiguredExecutionCapabilityGrant, len(s.Grants))
	for i, grant := range s.Grants {
		out.Grants[i] = grant.Clone()
	}
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

// LoadConfiguredExecutionGrantSnapshot is a strict Core configuration
// boundary. It never supplies security-relevant defaults and never creates
// applied plan evidence.
func LoadConfiguredExecutionGrantSnapshot(path string, capturedAt time.Time) (GrantSnapshot, error) {
	if strings.TrimSpace(path) == "" || capturedAt.IsZero() {
		return GrantSnapshot{}, fmt.Errorf("invalid configured execution grant load arguments")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return GrantSnapshot{}, err
	}
	var file configuredExecutionGrantFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return GrantSnapshot{}, fmt.Errorf("configured execution grants: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return GrantSnapshot{}, fmt.Errorf("invalid configured execution grant file: multiple YAML documents")
		}
		return GrantSnapshot{}, fmt.Errorf("configured execution grants: %w", err)
	}
	if file.SchemaVersion != ConfiguredExecutionCapabilityGrantSchemaVersion || file.Revision == 0 || file.Grants == nil {
		return GrantSnapshot{}, fmt.Errorf("invalid configured execution grant file")
	}
	at := capturedAt.UTC()
	snapshot := GrantSnapshot{SchemaVersion: GrantSnapshotSchemaVersion, Revision: file.Revision, CapturedAt: at, FreshUntil: at.Add(24 * time.Hour), Grants: file.Grants}
	snapshot.Fingerprint = GrantSnapshotFingerprint(snapshot)
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
	return grantDigest("applied-execution-grant", copy)
}

func validExecutionCapability(value string) bool {
	switch value {
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

func actionTypeMatchesCapability(capability, actionType string) bool {
	switch capability {
	case "record_clip":
		return actionType == "record.clip"
	case "increase_tracking_frequency":
		return actionType == "device.command"
	case "notify_user", "notify_user_high_priority", "notify_user_critical":
		return actionType == "notify"
	case "turn_on_relevant_lights", "turn_on_security_lights":
		return actionType == "light.on"
	case "trigger_approved_alarm_policy":
		return actionType == "siren"
	case "mark_security_degraded":
		return actionType == "mark_security_degraded"
	default:
		return false
	}
}

// normalizedConfiguredCapability is retained as a compatibility helper for
// callers that compare a configured capability with an intent. The boundary
// itself still rejects non-canonical configured values in Validate.
func normalizedConfiguredCapability(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, ".", "_")
	return value
}

func canonicalConfiguredGrant(grant ConfiguredExecutionCapabilityGrant) ConfiguredExecutionCapabilityGrant {
	grant.ValidFrom = grant.ValidFrom.UTC()
	grant.ValidUntil = grant.ValidUntil.UTC()
	return grant
}

func grantDigest(prefix string, value any) string {
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return prefix + ":sha256:" + hex.EncodeToString(digest[:])
}
