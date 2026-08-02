package cge

import (
	"context"
	"errors"
	"testing"
	"time"
)

func authorityTestDecision(now time.Time) DecisionEnvelope {
	return DecisionEnvelope{
		SchemaVersion: DecisionEnvelopeSchemaVersion,
		DecisionID:    "decision-1", SituationID: "situation-1", DecisionType: DecisionTypeNotify,
		Target:     DecisionTarget{Kind: DecisionTargetDevice, ID: "device-1", Scope: "home"},
		Confidence: .9, Priority: 20, EvidenceRefs: []string{"evidence-1"},
		CriticalChainRef: &ChainReference{ChainID: "critical-1", Version: 1, Class: ChainClassCritical, RevisionHash: "sha256:critical"},
		Constraints:      DecisionConstraints{RequiresAuthorization: true, RequiredInvariantRefs: []string{"safety.contract_valid", "safety.target_exists"}},
		CreatedAt:        now, ValidUntil: now.Add(time.Hour), IdempotencyKey: "idem-1",
	}
}

func authorityTestSnapshot(now time.Time, mode AuthorityMode) OperationalSnapshot {
	return OperationalSnapshot{
		CapturedAt: now, FreshUntil: now.Add(10 * time.Minute), Revision: 7, AuthorityMode: mode,
		Targets: []OperationalTarget{{Target: DecisionTarget{Kind: DecisionTargetDevice, ID: "device-1", Scope: "home"}, Exists: true, Authorized: true, Authorization: OperationalAuthorization{Known: true, Authorized: true}, PhysicalLimit: 100, CurrentRevision: 7}},
	}
}

func TestAuthorityModeDefaultsToShadow(t *testing.T) {
	config, err := LoadShadowConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if config.AuthorityMode != AuthorityModeShadow {
		t.Fatalf("mode=%q", config.AuthorityMode)
	}
}

func TestAuthorityModeRejectsUnknownValue(t *testing.T) {
	_, err := LoadShadowConfig(func(key string) string {
		if key == AuthorityModeEnv {
			return "unsafe"
		}
		return ""
	})
	if err == nil || !errors.Is(err, ErrInvalidShadowConfig) {
		t.Fatalf("unknown authority mode accepted: %v", err)
	}
}

func TestDecisionEnvelopeValidation(t *testing.T) {
	now := time.Now().UTC()
	decision := authorityTestDecision(now)
	if err := decision.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := decision.ValidateAt(now.Add(2 * time.Hour)); !errors.Is(err, ErrDecisionExpired) {
		t.Fatalf("expired decision error=%v", err)
	}
	decision.IdempotencyKey = ""
	if err := decision.Validate(); err == nil {
		t.Fatal("missing idempotency key accepted")
	}
}

func TestSafetyKernelRejectsUnsafeOperationalStates(t *testing.T) {
	now := time.Now().UTC()
	kernel, err := NewSafetyKernel(AuthorityModeShadow, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	decision := authorityTestDecision(now)
	verdict := kernel.ValidateDecision(context.Background(), decision, authorityTestSnapshot(now, AuthorityModeShadow))
	if verdict.Status != SafetyAllowed {
		t.Fatalf("valid verdict=%+v", verdict)
	}
	verdict = kernel.ValidateDecision(context.Background(), decision, OperationalSnapshot{CapturedAt: now, FreshUntil: now.Add(-time.Second), AuthorityMode: AuthorityModeShadow})
	if verdict.Status != SafetyStaleContext {
		t.Fatalf("stale verdict=%+v", verdict)
	}
	missing := authorityTestSnapshot(now, AuthorityModeShadow)
	missing.Targets[0].Exists = false
	if verdict = kernel.ValidateDecision(context.Background(), decision, missing); verdict.Status != SafetyInvalidTarget {
		t.Fatalf("missing target verdict=%+v", verdict)
	}
	unauthorized := authorityTestSnapshot(now, AuthorityModeShadow)
	unauthorized.Targets[0].Authorized = false
	unauthorized.Targets[0].Authorization.Authorized = false
	if verdict = kernel.ValidateDecision(context.Background(), decision, unauthorized); verdict.Status != SafetyInsufficientAuthorization {
		t.Fatalf("authorization verdict=%+v", verdict)
	}
	expired := authorityTestDecision(now.Add(-2 * time.Hour))
	expired.ValidUntil = now.Add(-time.Hour)
	if verdict = kernel.ValidateDecision(context.Background(), expired, authorityTestSnapshot(now, AuthorityModeShadow)); verdict.Status != SafetyExpired {
		t.Fatalf("expired verdict=%+v", verdict)
	}
}

type authorityTestPlanner struct{ calls int }

func (p *authorityTestPlanner) PlanExecution(context.Context, DecisionEnvelope, OperationalSnapshot) (ExecutionRequest, error) {
	p.calls++
	createdAt := time.Now().UTC()
	return ExecutionRequest{SchemaVersion: DecisionExecutionSchema, DecisionID: "decision-1", ActionRequestID: "request-1", ExecutionType: "notify", Target: DecisionTarget{Kind: DecisionTargetDevice, ID: "device-1"}, IdempotencyKey: "idem-1", CreatedAt: createdAt, ValidUntil: createdAt.Add(time.Hour)}, nil
}

type decisionTargetPlanner struct{}

func (decisionTargetPlanner) PlanExecution(_ context.Context, decision DecisionEnvelope, _ OperationalSnapshot) (ExecutionRequest, error) {
	return ExecutionRequest{SchemaVersion: DecisionExecutionSchema, DecisionID: decision.DecisionID, ActionRequestID: "request-" + decision.DecisionID, ExecutionType: string(decision.DecisionType), Target: decision.Target, IdempotencyKey: decision.IdempotencyKey, CreatedAt: decision.CreatedAt, ValidUntil: decision.ValidUntil}, nil
}

func conflictDecision(now time.Time, id string, kind DecisionType, desired string, actions ...string) DecisionEnvelope {
	decision := authorityTestDecision(now)
	decision.DecisionID = id
	decision.IdempotencyKey = "idem-" + id
	decision.DecisionType = kind
	decision.DesiredState = desired
	decision.Constraints.ProposedActions = actions
	return decision
}

func TestExecutionLeaseConflictUsesIntentAndLifecycle(t *testing.T) {
	now := time.Now().UTC()
	store := &MemoryDecisionStore{}
	authority, err := NewDecisionAuthority(AuthorityModeAuthoritative, nil, decisionTargetPlanner{}, store)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := authorityTestSnapshot(now, AuthorityModeAuthoritative)
	first := conflictDecision(now, "first", DecisionTypeNotify, "suspicious", "notify")
	if publication, err := authority.PublishDecision(context.Background(), first, snapshot); err != nil || publication.Status != DecisionPublishedAuthoritative {
		t.Fatalf("first publication=%+v err=%v", publication, err)
	}
	compatible := conflictDecision(now, "compatible", DecisionTypeNotify, "suspicious", "notify")
	if publication, err := authority.PublishDecision(context.Background(), compatible, snapshot); err != nil || publication.Status != DecisionPublishedAuthoritative {
		t.Fatalf("compatible publication=%+v err=%v", publication, err)
	}
	incompatible := conflictDecision(now, "incompatible", DecisionTypeChangeMode, "intrusion", "change_mode")
	incompatible.Constraints.RequiresPhysicalLimit = true
	incompatible.Constraints.RequiresAuthorization = true
	snapshot.Targets[0].PhysicalLimits = OperationalPhysicalLimits{Known: true, MaxValue: 100, Unit: "priority", Source: "test-policy"}
	publication, err := authority.PublishDecision(context.Background(), incompatible, snapshot)
	if err != nil || publication.Status != DecisionPublicationDenied || publication.Verdict.Status != SafetyInvariantViolation {
		t.Fatalf("incompatible publication=%+v err=%v", publication, err)
	}
	if len(publication.Verdict.Violations) != 1 || publication.Verdict.Violations[0].Code != "decision_conflict" {
		t.Fatalf("unexpected conflict verdict=%+v", publication.Verdict)
	}
}

func TestNonAuthoritativePublicationsDoNotCreateConflicts(t *testing.T) {
	now := time.Now().UTC()
	for _, mode := range []AuthorityMode{AuthorityModeShadow, AuthorityModeAdvisory} {
		authority, err := NewDecisionAuthority(mode, nil, nil, &MemoryDecisionStore{})
		if err != nil {
			t.Fatal(err)
		}
		snapshot := authorityTestSnapshot(now, mode)
		for _, id := range []string{"one", "two"} {
			decision := conflictDecision(now, id, DecisionTypeNotify, "suspicious", "notify")
			publication, publishErr := authority.PublishDecision(context.Background(), decision, snapshot)
			if publishErr != nil || publication.Status == DecisionPublicationDenied {
				t.Fatalf("mode=%s id=%s publication=%+v err=%v", mode, id, publication, publishErr)
			}
		}
	}
}

func TestIdenticalDecisionPublicationIsIdempotent(t *testing.T) {
	now := time.Now().UTC()
	store := &MemoryDecisionStore{}
	authority, err := NewDecisionAuthority(AuthorityModeShadow, nil, nil, store)
	if err != nil {
		t.Fatal(err)
	}
	decision := conflictDecision(now, "idempotent", DecisionTypeNotify, "suspicious", "notify")
	snapshot := authorityTestSnapshot(now, AuthorityModeShadow)
	first, err := authority.PublishDecision(context.Background(), decision, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := authority.PublishDecision(context.Background(), decision, snapshot)
	if err != nil || second.Status != first.Status || second.DecisionID != first.DecisionID {
		t.Fatalf("idempotent publication first=%+v second=%+v err=%v", first, second, err)
	}
	records, err := authority.Decisions(context.Background())
	if err != nil || len(records) != 1 {
		t.Fatalf("idempotent publication duplicated records=%+v err=%v", records, err)
	}
}

func TestAuthorityPublicationModesAreFailClosed(t *testing.T) {
	now := time.Now().UTC()
	planner := &authorityTestPlanner{}
	store := &MemoryDecisionStore{}
	authority, err := NewDecisionAuthority(AuthorityModeAdvisory, nil, planner, store)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := authority.PublishDecision(context.Background(), authorityTestDecision(now), authorityTestSnapshot(now, AuthorityModeAdvisory))
	if err != nil || publication.Status != DecisionPublishedAdvisory || planner.calls != 0 {
		t.Fatalf("advisory publication=%+v err=%v planner_calls=%d", publication, err, planner.calls)
	}
	authoritative, err := NewDecisionAuthority(AuthorityModeAuthoritative, nil, nil, &MemoryDecisionStore{})
	if err != nil {
		t.Fatal(err)
	}
	publication, err = authoritative.PublishDecision(context.Background(), authorityTestDecision(now), authorityTestSnapshot(now, AuthorityModeAuthoritative))
	if err != nil || publication.Status != DecisionPublicationDenied || publication.Verdict.Status != SafetyDenied {
		t.Fatalf("authoritative publication=%+v err=%v", publication, err)
	}
	if len(publication.Verdict.Violations) != 1 || publication.Verdict.Violations[0].Code != ErrExecutionPlannerUnavailable.Error() {
		t.Fatalf("planner denial=%+v", publication.Verdict)
	}
}

func TestActionResultFeedbackIsCorrelatedAndPersisted(t *testing.T) {
	now := time.Now().UTC()
	store := &MemoryDecisionStore{}
	authority, err := NewDecisionAuthority(AuthorityModeAdvisory, nil, nil, store)
	if err != nil {
		t.Fatal(err)
	}
	decision := authorityTestDecision(now)
	if _, err := authority.PublishDecision(context.Background(), decision, authorityTestSnapshot(now, AuthorityModeAdvisory)); err != nil {
		t.Fatal(err)
	}
	result := ActionResult{SchemaVersion: DecisionActionResultSchema, DecisionID: decision.DecisionID, ActionRequestID: "request-1", Status: ActionResultRejected, Error: "planner gate", Duration: time.Second, BeforeStateFingerprint: "sha256:before", AfterStateFingerprint: "sha256:after", Timestamp: now.Add(time.Minute)}
	if err := authority.RecordActionResult(context.Background(), result); !errors.Is(err, ErrActionResultUnauthorized) {
		t.Fatalf("advisory action result accepted: %v", err)
	}

	authoritativeStore := &MemoryDecisionStore{}
	authoritative, err := NewDecisionAuthority(AuthorityModeAuthoritative, nil, &authorityTestPlanner{}, authoritativeStore)
	if err != nil {
		t.Fatal(err)
	}
	if publication, err := authoritative.PublishDecision(context.Background(), decision, authorityTestSnapshot(now, AuthorityModeAuthoritative)); err != nil || publication.Status != DecisionPublishedAuthoritative {
		t.Fatalf("authoritative publication=%+v err=%v", publication, err)
	}
	if err := authoritative.RecordActionResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	results, err := authoritative.ActionResults(context.Background())
	if err != nil || len(results) != 1 || results[0].DecisionID != decision.DecisionID {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if err := authoritative.RecordActionResult(context.Background(), result); err != nil {
		t.Fatalf("idempotent feedback: %v", err)
	}
	wrongRequest := result
	wrongRequest.ActionRequestID = "other-request"
	if err := authoritative.RecordActionResult(context.Background(), wrongRequest); !errors.Is(err, ErrActionResultUnauthorized) {
		t.Fatalf("wrong request accepted: %v", err)
	}
	unknown := result
	unknown.DecisionID = "missing"
	if err := authoritative.RecordActionResult(context.Background(), unknown); !errors.Is(err, ErrUnknownDecision) {
		t.Fatalf("unknown decision feedback accepted: %v", err)
	}
}

func TestDecisionIdempotenceAndInvariantProtection(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	kernel, err := NewSafetyKernel(AuthorityModeShadow, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	decision := authorityTestDecision(now)
	snapshot := authorityTestSnapshot(now, AuthorityModeShadow)
	snapshot.UsedIdempotencyKeys = []string{"idem-1"}
	if verdict := kernel.ValidateDecision(context.Background(), decision, snapshot); verdict.Status != SafetyInvariantViolation {
		t.Fatalf("replay verdict=%+v", verdict)
	}
	registry := NewChainRegistry()
	if err := registry.Register(ChainVersion{Reference: ChainReference{ChainID: "safety", Version: 1, Class: ChainClassInvariant, RevisionHash: "sha256:safety"}, Status: ChainStatusActive, Scope: "global", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	learned := ChainReference{ChainID: "safety", Version: 2, Class: ChainClassLearned, RevisionHash: "sha256:learned"}
	if err := registry.Register(ChainVersion{Reference: learned, Status: ChainStatusCandidate, Scope: "global", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	_, err = registry.Promote("safety", learned, PromotionEvidence{CandidateOccurrences: 10, ObservationWindow: time.Hour, CandidatePerformance: .9, ActivePerformance: .7, StableAfterRestart: true, RollbackAvailable: true, CandidateScope: "global", ActiveScope: "global"}, now, ChainPromotionPolicy{MinimumOccurrences: 3, MinimumWindow: time.Hour, MinimumPerformanceGain: .1})
	if !errors.Is(err, ErrInvalidPromotion) {
		t.Fatalf("invariant replacement accepted: %v", err)
	}
}

func TestLearnedChainPromotionCreatesNewActiveVersion(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	registry := NewChainRegistry()
	critical := ChainReference{ChainID: "response", Version: 1, Class: ChainClassCritical, RevisionHash: "sha256:critical"}
	learned := ChainReference{ChainID: "response", Version: 2, Class: ChainClassLearned, RevisionHash: "sha256:learned"}
	if err := registry.Register(ChainVersion{Reference: critical, Status: ChainStatusActive, Scope: "home", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(ChainVersion{Reference: learned, Status: ChainStatusCandidate, Scope: "home/kitchen", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	active, err := registry.Promote("response", learned, PromotionEvidence{CandidateOccurrences: 10, ObservationWindow: 2 * time.Hour, CandidatePerformance: .9, ActivePerformance: .7, StableAfterRestart: true, RollbackAvailable: true, CandidateScope: "home/kitchen", ActiveScope: "home"}, now.Add(time.Hour), ChainPromotionPolicy{MinimumOccurrences: 3, MinimumWindow: time.Hour, MinimumPerformanceGain: .1})
	if err != nil {
		t.Fatal(err)
	}
	if active.Class != ChainClassLearned || active.Version != 3 {
		t.Fatalf("active=%+v", active)
	}
	selected, ok := registry.Select("response")
	if !ok || selected != active {
		t.Fatalf("selected=%+v ok=%v active=%+v", selected, ok, active)
	}
}
