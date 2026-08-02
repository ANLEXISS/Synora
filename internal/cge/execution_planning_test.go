package cge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func planningDecision(now time.Time, target DecisionTarget, intents ...string) DecisionEnvelope {
	return DecisionEnvelope{
		SchemaVersion: DecisionEnvelopeSchemaVersion, DecisionID: "decision-plan-1", SituationID: "situation-plan-1", DecisionType: DecisionTypeNotify,
		Target: target, Confidence: .9, Priority: 40, EvidenceRefs: []string{"evidence-plan-1"},
		CriticalChainRef: &ChainReference{ChainID: "critical-plan", Version: 1, Class: ChainClassCritical, RevisionHash: "sha256:critical-plan"},
		Constraints:      DecisionConstraints{ProposedActions: intents, RequiresAuthorization: true}, CreatedAt: now, ValidUntil: now.Add(time.Hour), IdempotencyKey: "decision-idempotency-1",
	}
}

func planningSnapshot(now time.Time, target DecisionTarget) OperationalSnapshot {
	grant := ConfiguredExecutionCapabilityGrant{SchemaVersion: ConfiguredExecutionCapabilityGrantSchemaVersion, GrantID: "configured-record-1", Capability: "record_clip", ActionType: "record.clip", Target: target, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), Revision: 3, Enabled: true}
	grant.Fingerprint = ConfiguredExecutionCapabilityGrantFingerprint(grant)
	grantSnapshot := GrantSnapshot{SchemaVersion: GrantSnapshotSchemaVersion, Revision: 3, CapturedAt: now, FreshUntil: now.Add(time.Hour), Grants: []ConfiguredExecutionCapabilityGrant{grant}}
	grantSnapshot.Fingerprint = GrantSnapshotFingerprint(grantSnapshot)
	return OperationalSnapshot{CapturedAt: now, FreshUntil: now.Add(10 * time.Minute), Revision: 7, PolicyRevision: 11, GrantSnapshot: grantSnapshot, AuthorityMode: AuthorityModeAdvisory, Targets: []OperationalTarget{{Target: target, Exists: true, CurrentRevision: 7, Capabilities: []string{"record_clip"}, Authorization: OperationalAuthorization{Known: true, Authorized: true, PolicyID: "alarm-policy-1", Revision: 11}, PhysicalLimits: OperationalPhysicalLimits{Known: true, MaxValue: 100}}}}
}

func TestCGEExecutionModeDefaultsAndRejectsUnknown(t *testing.T) {
	config, err := LoadShadowConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if config.ExecutionMode != CGEExecutionDisabled {
		t.Fatalf("mode=%q", config.ExecutionMode)
	}
	_, err = LoadShadowConfig(func(key string) string {
		if key == CGEExecutionModeEnv {
			return "unsafe"
		}
		return ""
	})
	if err == nil || !errors.Is(err, ErrInvalidShadowConfig) {
		t.Fatalf("unknown mode accepted: %v", err)
	}
}

func TestGovernedExecutionPlanIsDeterministicAndResolvesKnownCamera(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	target := DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"}
	decision := planningDecision(now, target, "record_clip")
	snapshot := planningSnapshot(now, target)
	planner := DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}
	first, err := planner.BuildPlan(context.Background(), decision, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := planner.BuildPlan(context.Background(), decision, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanID != second.PlanID || first.IdempotencyKey != second.IdempotencyKey || first.Actions[0].RequestFingerprint != second.Actions[0].RequestFingerprint {
		t.Fatalf("plan is not deterministic: %#v %#v", first, second)
	}
	if first.Actions[0].ActionType != "record.clip" || first.Actions[0].Target.ID != "camera-1" {
		t.Fatalf("unexpected action: %#v", first.Actions[0])
	}
	if !first.Atomic || len(first.AppliedGrants) != len(first.Actions) || first.AppliedGrants[0].ConfiguredGrantID != "configured-record-1" || first.AppliedGrants[0].PlannedActionID != first.Actions[0].PlannedActionID || first.AppliedGrants[0].RequestFingerprint != first.Actions[0].RequestFingerprint {
		t.Fatalf("plan is not atomically granted: %#v", first)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("valid atomic plan rejected: %v", err)
	}
}

func TestGovernedExecutionPlanRejectsPartialResolution(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	target := DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"}
	decision := planningDecision(now, target, "record_clip", "unknown_intent")
	plan, err := (DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}).BuildPlan(context.Background(), decision, planningSnapshot(now, target))
	if !errors.Is(err, ErrUnsupportedIntent) {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Actions) != 0 || len(plan.AppliedGrants) != 0 || plan.Status != ExecutionPlanUnsupported {
		t.Fatalf("partial plan escaped refusal: %#v", plan)
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("atomic refusal is not persistable: %v", err)
	}
}

func TestGovernedExecutionPlanResolvesMultipleOperationalCapabilities(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	decision := planningDecision(now, DecisionTarget{Kind: DecisionTargetZone, ID: "zone-a"}, "turn_on_relevant_lights")
	snapshot := planningSnapshot(now, decision.Target)
	snapshot.Targets = []OperationalTarget{
		{Target: DecisionTarget{Kind: DecisionTargetDevice, ID: "light-b"}, Exists: true, NodeID: "room-b", ZoneID: "zone-a", Capabilities: []string{"light"}, Authorization: OperationalAuthorization{Known: true, Authorized: true}, PhysicalLimits: OperationalPhysicalLimits{Known: true, MaxValue: 100}},
		{Target: DecisionTarget{Kind: DecisionTargetDevice, ID: "light-a"}, Exists: true, NodeID: "room-a", ZoneID: "zone-a", Capabilities: []string{"light_on"}, Authorization: OperationalAuthorization{Known: true, Authorized: true}, PhysicalLimits: OperationalPhysicalLimits{Known: true, MaxValue: 100}},
		{Target: decision.Target, Exists: true},
	}
	lightGrants := []ConfiguredExecutionCapabilityGrant{
		{SchemaVersion: ConfiguredExecutionCapabilityGrantSchemaVersion, GrantID: "configured-light-a", Capability: "turn_on_relevant_lights", ActionType: "light.on", Target: decision.Target, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), Revision: 3, Enabled: true},
		{SchemaVersion: ConfiguredExecutionCapabilityGrantSchemaVersion, GrantID: "configured-light-b", Capability: "turn_on_relevant_lights", ActionType: "light.on", Target: decision.Target, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), Revision: 3, Enabled: true},
	}
	for i := range lightGrants {
		lightGrants[i].Fingerprint = ConfiguredExecutionCapabilityGrantFingerprint(lightGrants[i])
	}
	snapshot.GrantSnapshot.Grants = lightGrants
	snapshot.GrantSnapshot.Fingerprint = GrantSnapshotFingerprint(snapshot.GrantSnapshot)
	decision.Constraints.RequiresAuthorization = false
	plan, err := (DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}).BuildPlan(context.Background(), decision, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 2 || plan.Actions[0].Target.ID != "light-a" || plan.Actions[1].Target.ID != "light-b" {
		t.Fatalf("multi-target plan=%#v", plan.Actions)
	}
	if plan.Actions[0].RequestFingerprint == plan.Actions[1].RequestFingerprint {
		t.Fatal("multi-target actions share a fingerprint")
	}
	replay, err := (DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}).BuildPlan(context.Background(), decision, snapshot)
	if err != nil || plan.PlanID != replay.PlanID || plan.IdempotencyKey != replay.IdempotencyKey {
		t.Fatalf("multi-target plan is not deterministic: %#v %#v err=%v", plan, replay, err)
	}
}

func TestGovernedExecutionPlanRejectsMissingOperationalCapability(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	target := DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"}
	snapshot := planningSnapshot(now, target)
	snapshot.Targets[0].Capabilities = nil
	_, err := (DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}).BuildPlan(context.Background(), planningDecision(now, target, "record_clip"), snapshot)
	if !errors.Is(err, ErrOperationalCapabilityUnavailable) {
		t.Fatalf("missing capability accepted: %v", err)
	}
}

func TestGovernedExecutionPlanRequiresConfiguredGrant(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	target := DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"}
	snapshot := planningSnapshot(now, target)
	snapshot.GrantSnapshot.Grants = nil
	snapshot.GrantSnapshot.Fingerprint = GrantSnapshotFingerprint(snapshot.GrantSnapshot)
	plan, err := (DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}).BuildPlan(context.Background(), planningDecision(now, target, "record_clip"), snapshot)
	if !errors.Is(err, ErrExecutionGrantUnavailable) {
		t.Fatalf("configured grant absence was not refused: %v", err)
	}
	if plan.Status != ExecutionPlanDenied || len(plan.Actions) != 0 || len(plan.AppliedGrants) != 0 {
		t.Fatalf("grant refusal was not atomic: %#v", plan)
	}
}

func TestConfiguredGrantSnapshotLoadsAndAppliedEvidenceIsNotAuthorization(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "grants.yaml")
	content := "schema_version: " + ConfiguredExecutionCapabilityGrantSchemaVersion + "\nrevision: 4\ngrants:\n  - grant_id: configured-camera\n    capability: record_clip\n    action_type: record.clip\n    target:\n      kind: device\n      id: camera-1\n    valid_from: 2026-07-30T11:00:00Z\n    valid_until: 2026-07-30T13:00:00Z\n    revision: 4\n    enabled: true\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := LoadConfiguredExecutionGrantSnapshot(path, now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 4 || len(snapshot.Grants) != 1 || !snapshot.Grants[0].Enabled {
		t.Fatalf("unexpected configured snapshot: %#v", snapshot)
	}
	planning := planningSnapshot(now, DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"})
	planning.GrantSnapshot = snapshot
	plan, err := (DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}).BuildPlan(context.Background(), planningDecision(now, planning.Targets[0].Target, "record_clip"), planning)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.AppliedGrants) != 1 || plan.AppliedGrants[0].ConfiguredGrantID != "configured-camera" || plan.AppliedGrants[0].GrantSnapshotRevision != 4 {
		t.Fatalf("configured grant was not applied as evidence: %#v", plan.AppliedGrants)
	}
}

func TestGovernedExecutionPlanRefusalsAreClosed(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	planner := DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}
	cases := []struct {
		name   string
		target DecisionTarget
		intent string
		mutate func(*OperationalSnapshot)
		want   error
	}{
		{"camera capability unavailable", DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, "record_clip", nil, ErrOperationalCapabilityUnavailable},
		{"light capability unavailable", DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, "turn_on_relevant_lights", nil, ErrOperationalCapabilityUnavailable},
		{"alarm without policy", DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, "trigger_approved_alarm_policy", func(s *OperationalSnapshot) { s.Targets[0].Authorization.PolicyID = "" }, ErrPolicyUnavailable},
		{"unknown intent", DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, "invented_action", nil, ErrUnsupportedIntent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := planningDecision(now, tc.target, tc.intent)
			snapshot := planningSnapshot(now, tc.target)
			if tc.mutate != nil {
				tc.mutate(&snapshot)
			}
			plan, err := planner.BuildPlan(context.Background(), decision, snapshot)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if plan.Status == ExecutionPlanPlanned || len(plan.FailureCodes) != 1 {
				t.Fatalf("refusal not closed: %#v", plan)
			}
		})
	}
}

func TestExecutionPlanSafetyKernelRejectsStaleAndUnknownAuthorization(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	target := DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"}
	decision := planningDecision(now, target, "record_clip")
	snapshot := planningSnapshot(now, target)
	planner := DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}
	plan, err := planner.BuildPlan(context.Background(), decision, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	kernel := DefaultExecutionPlanSafetyKernel{Now: func() time.Time { return now }}
	if verdict := kernel.ValidatePlan(context.Background(), decision, plan, snapshot); !verdict.Allowed {
		t.Fatalf("valid plan denied: %#v", verdict)
	}
	stale := snapshot
	stale.Revision++
	if verdict := kernel.ValidatePlan(context.Background(), decision, plan, stale); verdict.Allowed {
		t.Fatal("stale plan accepted")
	}
	unknown := snapshot
	unknown.Targets[0].Authorization.Known = false
	if verdict := kernel.ValidatePlan(context.Background(), decision, plan, unknown); verdict.Allowed {
		t.Fatal("unknown authorization accepted")
	}
	grantStale := plan
	grantStale.AppliedGrants = append([]AppliedExecutionGrant(nil), plan.AppliedGrants...)
	grantStale.AppliedGrants[0].PolicyRevision++
	grantStale.AppliedGrants[0].Fingerprint = AppliedExecutionGrantFingerprint(grantStale.AppliedGrants[0])
	if verdict := kernel.ValidatePlan(context.Background(), decision, grantStale, snapshot); verdict.Allowed {
		t.Fatal("stale capability grant accepted")
	}
}

func TestAuthoritativeDryRunHasNoLeaseAndRejectsFeedback(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	target := DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"}
	decision := planningDecision(now, target, "record_clip")
	snapshot := planningSnapshot(now, target)
	snapshot.AuthorityMode = AuthorityModeAuthoritative
	store := &MemoryDecisionStore{}
	plans := &MemoryExecutionPlanStore{}
	decisionKernel, err := NewSafetyKernel(AuthorityModeAuthoritative, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewGovernedDecisionAuthority(AuthorityModeAuthoritative, CGEExecutionDryRun, decisionKernel, DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}, DefaultExecutionPlanSafetyKernel{Now: func() time.Time { return now }}, store, plans, false)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := authority.PublishDecision(context.Background(), decision, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if publication.Status != DecisionPublishedAuthoritativeDryRun || publication.ExecutionPlan == nil {
		t.Fatalf("publication=%#v", publication)
	}
	records, err := authority.Decisions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if records[0].ExecutionLease != nil || records[0].ExecutionRequest != nil {
		t.Fatalf("dry-run created execution state: %#v", records[0])
	}
	result := ActionResult{SchemaVersion: DecisionActionResultSchema, DecisionID: decision.DecisionID, ActionRequestID: "not-dispatched", Status: ActionResultSucceeded, Duration: time.Second, BeforeStateFingerprint: "sha256:before", AfterStateFingerprint: "sha256:after", Timestamp: now}
	if err := authority.RecordActionResult(context.Background(), result); !errors.Is(err, ErrActionResultUnauthorized) {
		t.Fatalf("feedback accepted: %v", err)
	}
}

func TestAdvisoryDryRunPersistsPlanAndShadowRequiresDiagnostics(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	target := DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"}
	decision := planningDecision(now, target, "record_clip")
	snapshot := planningSnapshot(now, target)
	decisionKernel, err := NewSafetyKernel(AuthorityModeAdvisory, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	planner := DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}
	planKernel := DefaultExecutionPlanSafetyKernel{Now: func() time.Time { return now }}
	plans := &MemoryExecutionPlanStore{}
	authority, err := NewGovernedDecisionAuthority(AuthorityModeAdvisory, CGEExecutionDryRun, decisionKernel, planner, planKernel, &MemoryDecisionStore{}, plans, false)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := authority.PublishDecision(context.Background(), decision, snapshot)
	if err != nil || publication.Status != DecisionPublishedAdvisoryDryRun || publication.ExecutionPlan == nil {
		t.Fatalf("advisory publication=%#v err=%v", publication, err)
	}
	stored, err := plans.ExecutionPlans(context.Background())
	if err != nil || len(stored) != 1 {
		t.Fatalf("advisory plan persistence=%#v err=%v", stored, err)
	}

	shadowKernel, err := NewSafetyKernel(AuthorityModeShadow, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	shadowSnapshot := snapshot
	shadowSnapshot.AuthorityMode = AuthorityModeShadow
	shadow, err := NewGovernedDecisionAuthority(AuthorityModeShadow, CGEExecutionDryRun, shadowKernel, planner, planKernel, &MemoryDecisionStore{}, &MemoryExecutionPlanStore{}, false)
	if err != nil {
		t.Fatal(err)
	}
	shadowPublication, err := shadow.PublishDecision(context.Background(), decision, shadowSnapshot)
	if err != nil || shadowPublication.Status != DecisionPublishedShadow {
		t.Fatalf("shadow publication without diagnostics=%#v err=%v", shadowPublication, err)
	}
}

func TestExecutionModesFailClosedForAuthoritativeAuthority(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	target := DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"}
	decision := planningDecision(now, target, "record_clip")
	snapshot := planningSnapshot(now, target)
	snapshot.AuthorityMode = AuthorityModeAuthoritative
	kernel, err := NewSafetyKernel(AuthorityModeAuthoritative, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		mode CGEExecutionMode
		code string
	}{
		{name: "disabled", mode: CGEExecutionDisabled, code: ErrExecutionDisabled.Error()},
		{name: "live", mode: CGEExecutionLive, code: ErrLiveExecutionUnavailable.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authority, err := NewGovernedDecisionAuthority(AuthorityModeAuthoritative, tc.mode, kernel, nil, nil, &MemoryDecisionStore{}, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			publication, err := authority.PublishDecision(context.Background(), decision, snapshot)
			if err != nil || publication.Status != DecisionPublicationDenied || publication.Verdict.Status != SafetyDenied || len(publication.Verdict.Violations) != 1 || publication.Verdict.Violations[0].Code != tc.code {
				t.Fatalf("publication=%#v err=%v", publication, err)
			}
		})
	}
}

func TestExecutionPlanComparisonIgnoresOrderAndFileStoreFailsClosed(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	target := DecisionTarget{Kind: DecisionTargetDevice, ID: "camera-1"}
	planner := DefaultGovernedExecutionPlanner{Now: func() time.Time { return now }}
	plan, err := planner.BuildPlan(context.Background(), planningDecision(now, target, "record_clip"), planningSnapshot(now, target))
	if err != nil {
		t.Fatal(err)
	}
	historical := NormalizedHistoricalRequests(plan)
	for i, j := 0, len(historical)-1; i < j; i, j = i+1, j-1 {
		historical[i], historical[j] = historical[j], historical[i]
	}
	comparison, err := CompareExecutionPlan("event-1", plan, historical, now)
	if err != nil || !comparison.ExactMatch {
		t.Fatalf("comparison=%#v err=%v", comparison, err)
	}
	root := t.TempDir()
	store, err := NewFileExecutionPlanStore(filepath.Join(root, "cge"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PersistExecutionPlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if plans, err := store.ExecutionPlans(context.Background()); err != nil || len(plans) != 1 {
		t.Fatalf("recovery plans=%#v err=%v", plans, err)
	}
	if err := os.WriteFile(filepath.Join(root, "cge", "execution-plans.ndjson"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecutionPlans(context.Background()); !errors.Is(err, ErrExecutionPlanStore) {
		t.Fatalf("corruption not fail-closed: %v", err)
	}
}
