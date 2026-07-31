package cge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfiguredGrant(at time.Time, id string) ConfiguredExecutionCapabilityGrant {
	grant := ConfiguredExecutionCapabilityGrant{
		SchemaVersion: ConfiguredExecutionCapabilityGrantSchemaVersion,
		GrantID:       id,
		Capability:    "record_clip",
		ActionType:    "record.clip",
		Target:        DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"},
		ValidFrom:     at.Add(-time.Minute),
		ValidUntil:    at.Add(time.Hour),
		Revision:      4,
		Enabled:       true,
	}
	grant.Fingerprint = ConfiguredExecutionCapabilityGrantFingerprint(grant)
	return grant
}

func testGrantSnapshot(at time.Time, grants ...ConfiguredExecutionCapabilityGrant) GrantSnapshot {
	snapshot := GrantSnapshot{SchemaVersion: GrantSnapshotSchemaVersion, Revision: 7, CapturedAt: at, FreshUntil: at.Add(time.Hour), Grants: grants}
	snapshot.Fingerprint = GrantSnapshotFingerprint(snapshot)
	return snapshot
}

func TestConfiguredGrantValidationIsStrictAndFailClosed(t *testing.T) {
	at := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	grant := testConfiguredGrant(at, "grant-1")
	cases := []struct {
		name   string
		mutate func(*ConfiguredExecutionCapabilityGrant)
	}{
		{"missing schema", func(g *ConfiguredExecutionCapabilityGrant) { g.SchemaVersion = "" }},
		{"missing action type", func(g *ConfiguredExecutionCapabilityGrant) { g.ActionType = "" }},
		{"noncanonical capability", func(g *ConfiguredExecutionCapabilityGrant) { g.Capability = " RECORD-CLIP " }},
		{"missing fingerprint", func(g *ConfiguredExecutionCapabilityGrant) { g.Fingerprint = "" }},
		{"changed content", func(g *ConfiguredExecutionCapabilityGrant) { g.Enabled = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := grant
			tc.mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatal("invalid configured grant was accepted")
			}
		})
	}
}

func TestGrantSnapshotFingerprintIsCanonicalAndRestartStable(t *testing.T) {
	at := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	first := testConfiguredGrant(at, "grant-a")
	second := testConfiguredGrant(at, "grant-b")
	left := testGrantSnapshot(at, first, second)
	right := testGrantSnapshot(at.Add(12*time.Hour), second, first)
	right.FreshUntil = right.CapturedAt.Add(24 * time.Hour)
	right.Fingerprint = GrantSnapshotFingerprint(right)
	if left.Fingerprint != right.Fingerprint {
		t.Fatalf("runtime metadata or order changed snapshot identity: %q != %q", left.Fingerprint, right.Fingerprint)
	}
	if err := left.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := right.Validate(); err != nil {
		t.Fatal(err)
	}

	changed := first
	changed.Target.ID = "camera-2"
	changed.Fingerprint = ConfiguredExecutionCapabilityGrantFingerprint(changed)
	modified := testGrantSnapshot(at, changed, second)
	if modified.Fingerprint == left.Fingerprint {
		t.Fatal("changed grant content retained the same snapshot identity")
	}
}

func TestGrantSnapshotCloneIsDetached(t *testing.T) {
	at := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	snapshot := testGrantSnapshot(at, testConfiguredGrant(at, "grant-1"))
	clone := snapshot.Clone()
	clone.Grants[0].Target.ID = "camera-mutated"
	clone.Grants[0].Fingerprint = "mutated"
	if snapshot.Grants[0].Target.ID != "camera-1" || snapshot.Grants[0].Fingerprint == "mutated" {
		t.Fatal("snapshot clone aliases configured grant content")
	}
}

func TestLoadConfiguredGrantSnapshotDoesNotInventSecurityFields(t *testing.T) {
	at := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	grant := testConfiguredGrant(at, "grant-1")
	valid := "schema_version: " + ConfiguredExecutionCapabilityGrantSchemaVersion + "\nrevision: 4\ngrants:\n  - schema_version: " + ConfiguredExecutionCapabilityGrantSchemaVersion + "\n    grant_id: grant-1\n    capability: record_clip\n    action_type: record.clip\n    target:\n      kind: device\n      id: camera-1\n    valid_from: 2026-07-30T11:59:00Z\n    valid_until: 2026-07-30T13:00:00Z\n    revision: 4\n    enabled: true\n    fingerprint: " + grant.Fingerprint + "\n"
	cases := []struct {
		name string
		data string
	}{
		{"missing schema", strings.Replace(valid, "schema_version: "+ConfiguredExecutionCapabilityGrantSchemaVersion, "", 1)},
		{"missing revision", strings.Replace(valid, "revision: 4\n", "", 1)},
		{"missing grants", strings.Replace(valid[strings.Index(valid, "grants:"):], "grants:", "", 1)},
		{"missing grant fingerprint", strings.Replace(valid, "    fingerprint: "+grant.Fingerprint+"\n", "", 1)},
		{"unknown field", valid + "unexpected: true\n"},
		{"multiple documents", valid + "---\n" + valid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "grants.yaml")
			if err := os.WriteFile(path, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfiguredExecutionGrantSnapshot(path, at); err == nil {
				t.Fatal("malformed configured grant file was accepted")
			}
		})
	}

	path := filepath.Join(t.TempDir(), "grants.yaml")
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfiguredExecutionGrantSnapshot(path, at)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Fingerprint != GrantSnapshotFingerprint(loaded) {
		t.Fatal("valid configured grant snapshot did not validate")
	}
	if err := loaded.Validate(); err != nil {
		t.Fatal(err)
	}
}
