package authorizationboundary

import (
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
	"unsafe"

	"synora/internal/cge/capabilitymapping"
)

var testAuthorizationTime = time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

func testMapping(t testing.TB, status capabilitymapping.CapabilityMappingStatus, compatible bool, utility int) capabilitymapping.CapabilityMappingAssessment {
	t.Helper()
	candidate := capabilitymapping.CapabilityMappingCandidate{ID: "mapping-candidate-1", CapabilityInstanceID: "capability-instance-01", CapabilityKind: capabilitymapping.CapabilityIdentityObservation, Status: status, CostClass: capabilitymapping.CapabilityCostLow, LatencyClass: capabilitymapping.CapabilityLatencyShort, SensitivityClass: capabilitymapping.CapabilitySensitivityLow, QualityCalibrated: true, Compatible: compatible, CompatibilityPermille: 1000, QualityPermille: 900, ConstraintPermille: 1000, ScopePermille: 1000, AvailabilityPermille: 1000, UtilityPermille: utility, SourceRequestFingerprint: "source-request", SourceInventoryFingerprint: "source-inventory"}
	candidate.Fingerprint = capabilitymapping.CapabilityMappingCandidateFingerprint(candidate)
	assessment := capabilitymapping.CapabilityMappingAssessment{EpisodeID: "episode-01", SourceRequestFingerprint: "source-request", SourceInventoryFingerprint: "source-inventory", CatalogFingerprint: "source-catalog", Requirement: capabilitymapping.CapabilityRequirement{}, Candidates: []capabilitymapping.CapabilityMappingCandidate{candidate}, MappingAvailable: compatible, Revision: 1}
	reflect.ValueOf(&assessment).Elem().FieldByName("RequestID").SetString("request-01")
	assessment.Fingerprint = capabilitymapping.CapabilityMappingAssessmentFingerprint(assessment)
	return assessment
}

func testContext() AuthorizationContext {
	context := AuthorizationContext{ID: "context-01", DomainID: "home", PurposeCode: PurposeResolveIdentityAmbiguity, RequestedScope: []AuthorizationScope{{Kind: "domain", Ref: "home"}, {Kind: "zone", Ref: "entry"}}, RequestedAt: testAuthorizationTime, ValidUntil: testAuthorizationTime.Add(15 * time.Minute), RequestActorClass: "cognitive-engine", RequestOrigin: "synthetic-domain", Revision: 1}
	context.Fingerprint = AuthorizationContextFingerprint(context)
	return context
}

func testPolicySet(t testing.TB, rules ...AuthorizationRule) AuthorizationPolicySet {
	t.Helper()
	set := AuthorizationPolicySet{ID: "policy-set-01", Version: "authorization-policy-v1", DefaultEffect: EffectDeny, Rules: rules, Revision: 1}
	set.Fingerprint = AuthorizationPolicySetFingerprint(set)
	return set
}

func allowRule() AuthorizationRule {
	return AuthorizationRule{ID: "rule-allow-identity", Effect: EffectAllowEligibility, PurposeCodes: []AuthorizationPurposeCode{PurposeResolveIdentityAmbiguity}, CapabilityKinds: []capabilitymapping.CapabilityKind{capabilitymapping.CapabilityIdentityObservation}, DomainIDs: []string{"home"}, RequiredScopes: []AuthorizationScope{{Kind: "domain", Ref: "home"}}, ReasonCode: "policy.identity.allow", Priority: 10}
}

func denyRule() AuthorizationRule {
	rule := allowRule()
	rule.ID = "rule-deny-identity"
	rule.Effect = EffectDeny
	rule.ReasonCode = "policy.identity.deny"
	rule.Priority = 20
	return rule
}

func emptyGrants() ExternalGrantSnapshot {
	snapshot := ExternalGrantSnapshot{Revision: 1, Grants: []ExternalGrant{}, Index: map[ExternalGrantID]int{}}
	snapshot.Fingerprint = ExternalGrantSnapshotFingerprint(snapshot)
	return snapshot
}

func testGrant(kind ExternalGrantKind, validFrom, validUntil time.Time) ExternalGrant {
	grant := ExternalGrant{ID: ExternalGrantID("grant-" + string(kind)), Kind: kind, SubjectClass: "cognitive-engine", DomainID: "home", PurposeCodes: []AuthorizationPurposeCode{PurposeResolveIdentityAmbiguity}, CapabilityKinds: []capabilitymapping.CapabilityKind{capabilitymapping.CapabilityIdentityObservation}, Scopes: []AuthorizationScope{{Kind: "domain", Ref: "home"}, {Kind: "zone", Ref: "entry"}}, ValidFrom: validFrom, ValidUntil: validUntil, IssuerID: "issuer-alpha", Revision: 1}
	grant.Fingerprint = ExternalGrantFingerprint(grant)
	return grant
}

func grantSnapshot(grants ...ExternalGrant) ExternalGrantSnapshot {
	sort.Slice(grants, func(i, j int) bool { return grants[i].ID < grants[j].ID })
	index := map[ExternalGrantID]int{}
	for i, grant := range grants {
		index[grant.ID] = i
	}
	snapshot := ExternalGrantSnapshot{Revision: 1, Grants: grants, Index: index}
	snapshot.Fingerprint = ExternalGrantSnapshotFingerprint(snapshot)
	return snapshot
}

func analyzeTest(t testing.TB, mapping capabilitymapping.CapabilityMappingAssessment, set AuthorizationPolicySet, grants ExternalGrantSnapshot) AuthorizationBoundaryAssessment {
	t.Helper()
	assessment, err := Analyze(AnalysisInput{Mapping: mapping, Context: testContext(), PolicySet: set, Grants: grants}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return assessment
}

func TestAllowEligibilityAndPreferredCandidate(t *testing.T) {
	assessment := analyzeTest(t, testMapping(t, capabilitymapping.MappingCompatible, true, 950), testPolicySet(t, allowRule()), emptyGrants())
	if assessment.Status != AssessmentEligible || !assessment.AuthorizationEligible || assessment.PreferredEligibleCandidateID == "" || assessment.Candidates[0].Status != EligibilityEligible {
		t.Fatalf("unexpected allow assessment: %+v", assessment)
	}
}

func TestDefaultDenyAndExplicitDeny(t *testing.T) {
	mapping := testMapping(t, capabilitymapping.MappingCompatible, true, 1000)
	byDefault := analyzeTest(t, mapping, testPolicySet(t), emptyGrants())
	if byDefault.Status != AssessmentDenied || byDefault.Candidates[0].Status != EligibilityDeniedByDefault || !byDefault.DeniedByDefault {
		t.Fatalf("expected default deny: %+v", byDefault)
	}
	explicit := analyzeTest(t, mapping, testPolicySet(t, denyRule()), emptyGrants())
	if explicit.Candidates[0].Status != EligibilityDenied || explicit.AuthorizationEligible {
		t.Fatalf("expected explicit deny: %+v", explicit)
	}
}

func TestDenyOverridesAllowAndPreservesConflict(t *testing.T) {
	assessment := analyzeTest(t, testMapping(t, capabilitymapping.MappingCompatible, true, 1000), testPolicySet(t, allowRule(), denyRule()), emptyGrants())
	if assessment.Candidates[0].Status != EligibilityPolicyConflict || len(assessment.Conflicts) != 1 || assessment.AuthorizationEligible {
		t.Fatalf("expected conflict and deny precedence: %+v", assessment)
	}
}

func TestGrantRequiredStates(t *testing.T) {
	rule := allowRule()
	rule.RequiredGrantKinds = []ExternalGrantKind{GrantPrivacyConsent}
	set := testPolicySet(t, rule)
	mapping := testMapping(t, capabilitymapping.MappingCompatible, true, 900)
	missing := analyzeTest(t, mapping, set, emptyGrants())
	if missing.Candidates[0].Status != EligibilityRequiresExternalConfirmation || len(missing.Candidates[0].MissingGrantKinds) != 1 {
		t.Fatalf("expected missing grant: %+v", missing)
	}
	valid := analyzeTest(t, mapping, set, grantSnapshot(testGrant(GrantPrivacyConsent, testAuthorizationTime.Add(-time.Minute), testAuthorizationTime.Add(time.Hour))))
	if valid.Candidates[0].Status != EligibilityEligible || len(valid.Candidates[0].SatisfiedGrantIDs) != 1 {
		t.Fatalf("expected valid grant: %+v", valid)
	}
	expiredGrant := testGrant(GrantPrivacyConsent, testAuthorizationTime.Add(-time.Hour), testAuthorizationTime.Add(-time.Minute))
	expired := analyzeTest(t, mapping, set, grantSnapshot(expiredGrant))
	if expired.Candidates[0].Status != EligibilityRequiresExternalConfirmation || !containsString(expired.Candidates[0].ReasonCodes, "grant.expired") {
		t.Fatalf("expected expired grant state: %+v", expired)
	}
	futureGrant := testGrant(GrantPrivacyConsent, testAuthorizationTime.Add(time.Minute), testAuthorizationTime.Add(time.Hour))
	future := analyzeTest(t, mapping, set, grantSnapshot(futureGrant))
	if future.Candidates[0].Status != EligibilityRequiresExternalConfirmation || !containsString(future.Candidates[0].ReasonCodes, "grant.not_yet_valid") {
		t.Fatalf("expected not-yet-valid grant state: %+v", future)
	}
	revokedGrant := testGrant(GrantPrivacyConsent, testAuthorizationTime.Add(-time.Minute), testAuthorizationTime.Add(time.Hour))
	revokedGrant.Revoked = true
	revokedGrant.Fingerprint = ExternalGrantFingerprint(revokedGrant)
	revoked := analyzeTest(t, mapping, set, grantSnapshot(revokedGrant))
	if revoked.Candidates[0].Status != EligibilityRequiresExternalConfirmation || !containsString(revoked.Candidates[0].ReasonCodes, "grant.revoked") {
		t.Fatalf("expected revoked grant state: %+v", revoked)
	}
}

func TestGrantPurposeScopeDomainAndContextTimeMismatches(t *testing.T) {
	rule := allowRule()
	rule.RequiredGrantKinds = []ExternalGrantKind{GrantPrivacyConsent}
	set := testPolicySet(t, rule)
	mapping := testMapping(t, capabilitymapping.MappingCompatible, true, 900)
	grant := testGrant(GrantPrivacyConsent, testAuthorizationTime.Add(-time.Minute), testAuthorizationTime.Add(time.Hour))
	grant.PurposeCodes = []AuthorizationPurposeCode{PurposeVerifyContextState}
	grant.Fingerprint = ExternalGrantFingerprint(grant)
	result := analyzeTest(t, mapping, set, grantSnapshot(grant))
	if !containsString(result.Candidates[0].ReasonCodes, "grant.purpose_mismatch") {
		t.Fatalf("expected purpose mismatch: %+v", result)
	}
	grant.PurposeCodes = []AuthorizationPurposeCode{PurposeResolveIdentityAmbiguity}
	grant.Scopes = []AuthorizationScope{{Kind: "domain", Ref: "home"}}
	grant.Fingerprint = ExternalGrantFingerprint(grant)
	result = analyzeTest(t, mapping, set, grantSnapshot(grant))
	if !containsString(result.Candidates[0].ReasonCodes, "grant.scope_mismatch") {
		t.Fatalf("expected scope mismatch: %+v", result)
	}
	grant.Scopes = []AuthorizationScope{{Kind: "domain", Ref: "home"}, {Kind: "zone", Ref: "entry"}}
	grant.DomainID = "other-domain"
	grant.Fingerprint = ExternalGrantFingerprint(grant)
	result = analyzeTest(t, mapping, set, grantSnapshot(grant))
	if !containsString(result.Candidates[0].ReasonCodes, "grant.domain_mismatch") {
		t.Fatalf("expected domain mismatch: %+v", result)
	}
	context := testContext()
	context.ValidUntil = context.RequestedAt
	context.Fingerprint = AuthorizationContextFingerprint(context)
	_, err := Analyze(AnalysisInput{Mapping: mapping, Context: context, PolicySet: set, Grants: emptyGrants()}, DefaultPolicy())
	if !errors.Is(err, ErrContextExpired) {
		t.Fatalf("expected expired context, got %v", err)
	}
}

func TestMultipleEligibleCandidatesRemainAmbiguous(t *testing.T) {
	mapping := testMapping(t, capabilitymapping.MappingCompatible, true, 900)
	second := mapping.Candidates[0].Clone()
	second.ID = "mapping-candidate-2"
	second.CapabilityInstanceID = "capability-instance-02"
	second.Fingerprint = capabilitymapping.CapabilityMappingCandidateFingerprint(second)
	mapping.Candidates = append(mapping.Candidates, second)
	mapping.Fingerprint = capabilitymapping.CapabilityMappingAssessmentFingerprint(mapping)
	assessment := analyzeTest(t, mapping, testPolicySet(t, allowRule()), emptyGrants())
	if assessment.EligibleCandidateCount != 2 || !assessment.AuthorizationAmbiguous || assessment.PreferredEligibleCandidateID != "" {
		t.Fatalf("expected ambiguous eligible candidates: %+v", assessment)
	}
}

func TestPurposeScopeSensitivityAndMappingUnavailable(t *testing.T) {
	context := testContext()
	context.PurposeCode = PurposeVerifyContextState
	context.Fingerprint = AuthorizationContextFingerprint(context)
	mapping := testMapping(t, capabilitymapping.MappingCompatible, true, 1000)
	assessment, err := Analyze(AnalysisInput{Mapping: mapping, Context: context, PolicySet: testPolicySet(t, allowRule()), Grants: emptyGrants()}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Candidates[0].Status != EligibilityDenied || !containsString(assessment.Candidates[0].ReasonCodes, "authorization.purpose_mismatch") {
		t.Fatalf("expected purpose mismatch: %+v", assessment)
	}

	highSensitivityRule := allowRule()
	highSensitivityRule.MaximumSensitivityClass = capabilitymapping.CapabilitySensitivityLow
	highSensitivityRule.RequiredScopes = []AuthorizationScope{{Kind: "zone", Ref: "entry"}}
	withContextScope := analyzeTest(t, mapping, testPolicySet(t, highSensitivityRule), emptyGrants())
	if withContextScope.Candidates[0].Status != EligibilityEligible {
		t.Fatalf("expected compatible scope and sensitivity: %+v", withContextScope)
	}

	unavailable := analyzeTest(t, testMapping(t, capabilitymapping.MappingUnavailable, false, 0), testPolicySet(t, allowRule()), emptyGrants())
	if unavailable.Candidates[0].Status != EligibilityMappingUnavailable || unavailable.AuthorizationEligible {
		t.Fatalf("expected mapping unavailable: %+v", unavailable)
	}
}

func TestHighSensitivityNeedsPrivacyConsentAndDefer(t *testing.T) {
	mapping := testMapping(t, capabilitymapping.MappingCompatible, true, 900)
	mapping.Candidates[0].SensitivityClass = capabilitymapping.CapabilitySensitivityHigh
	mapping.Candidates[0].Fingerprint = capabilitymapping.CapabilityMappingCandidateFingerprint(mapping.Candidates[0])
	mapping.Fingerprint = capabilitymapping.CapabilityMappingAssessmentFingerprint(mapping)
	withoutConsent := analyzeTest(t, mapping, testPolicySet(t, allowRule()), emptyGrants())
	if withoutConsent.Candidates[0].Status != EligibilityRequiresExternalConfirmation || !containsString(withoutConsent.Candidates[0].ReasonCodes, "authorization.high_sensitivity_confirmation_required") {
		t.Fatalf("expected high sensitivity confirmation: %+v", withoutConsent)
	}
	withConsent := analyzeTest(t, mapping, testPolicySet(t, allowRule()), grantSnapshot(testGrant(GrantPrivacyConsent, testAuthorizationTime.Add(-time.Minute), testAuthorizationTime.Add(time.Hour))))
	if withConsent.Candidates[0].Status != EligibilityEligible {
		t.Fatalf("expected high sensitivity eligibility with consent: %+v", withConsent)
	}
	deferRule := allowRule()
	deferRule.Effect = EffectDefer
	deferred := analyzeTest(t, testMapping(t, capabilitymapping.MappingCompatible, true, 900), testPolicySet(t, deferRule), emptyGrants())
	if deferred.Candidates[0].Status != EligibilityDeferred || deferred.Status != AssessmentDeferred {
		t.Fatalf("expected deferred status: %+v", deferred)
	}
}

func TestUtilityCannotOverrideDeny(t *testing.T) {
	mapping := testMapping(t, capabilitymapping.MappingCompatible, true, 1000)
	assessment := analyzeTest(t, mapping, testPolicySet(t, denyRule()), emptyGrants())
	if assessment.Candidates[0].Status != EligibilityDenied || assessment.Candidates[0].EligibilityPermille != 0 {
		t.Fatalf("cognitive utility bypassed policy: %+v", assessment)
	}
}

func TestFingerprintAndInvalidInput(t *testing.T) {
	context := testContext()
	context.Fingerprint = "forged"
	_, err := Analyze(AnalysisInput{Mapping: testMapping(t, capabilitymapping.MappingCompatible, true, 900), Context: context, PolicySet: testPolicySet(t, allowRule()), Grants: emptyGrants()}, DefaultPolicy())
	if !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("expected fingerprint mismatch, got %v", err)
	}
	invalidSet := testPolicySet(t)
	invalidSet.DefaultEffect = EffectAllowEligibility
	invalidSet.Fingerprint = AuthorizationPolicySetFingerprint(invalidSet)
	_, err = Analyze(AnalysisInput{Mapping: testMapping(t, capabilitymapping.MappingCompatible, true, 900), Context: testContext(), PolicySet: invalidSet, Grants: emptyGrants()}, DefaultPolicy())
	if !errors.Is(err, ErrInvalidPolicySet) {
		t.Fatalf("expected invalid default effect, got %v", err)
	}
}

func TestExplanationLifecycleAndReadiness(t *testing.T) {
	assessment := analyzeTest(t, testMapping(t, capabilitymapping.MappingCompatible, true, 900), testPolicySet(t, allowRule()), emptyGrants())
	explanation, err := Explain(assessment.Candidates[0])
	if err != nil || ValidateExplanation(explanation) != nil {
		t.Fatalf("invalid explanation: %v %+v", err, explanation)
	}
	if !explanation.NotAnExecutionGrant || !explanation.NotACommand || !explanation.NotAReservation || !explanation.NotAProbability || !explanation.NoSecurityMeaning || !explanation.RequiresSeparateGrantIssuance {
		t.Fatal("missing non-execution markers")
	}
	if !EvaluateAuthorizationLifecycle(AssessmentActive, AssessmentEligible).Allowed || EvaluateAuthorizationLifecycle(AssessmentObsolete, AssessmentEligible).Allowed {
		t.Fatal("invalid lifecycle transition")
	}
	readiness := Readiness()
	if !readiness.ReadyForDurableCognitiveWorkflow || readiness.RuntimeIntegrated || readiness.Durable || readiness.ExecutionGrantIssuanceImplemented || readiness.SecurityAuthority {
		t.Fatalf("unexpected readiness: %+v", readiness)
	}
}

func TestPlanRegistryIdempotenceAndReevaluation(t *testing.T) {
	mapping := testMapping(t, capabilitymapping.MappingCompatible, true, 900)
	input := AnalysisInput{Mapping: mapping, Context: testContext(), PolicySet: testPolicySet(t, allowRule()), Grants: emptyGrants()}
	registry := NewRegistry()
	plan, err := Plan(input, registry.Snapshot(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	first, err := registry.ApplyPlan(plan)
	if err != nil || !first.Applied {
		t.Fatalf("first apply: %v %+v", err, first)
	}
	snapshot := registry.Snapshot()
	secondPlan, err := Plan(input, snapshot, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.ApplyPlan(secondPlan)
	if err != nil || !second.Idempotent || second.RegistryRevision != first.RegistryRevision {
		t.Fatalf("idempotence failed: %v %+v", err, second)
	}
	changed := testContext()
	changed.RequestOrigin = "other-origin"
	changed.Fingerprint = AuthorizationContextFingerprint(changed)
	revised, err := Reevaluate(first.After, mapping, changed, input.PolicySet, input.Grants, DefaultPolicy())
	if err != nil || revised.Revision != first.After.Revision+1 {
		t.Fatalf("reevaluation failed: %v %+v", err, revised)
	}
}

func TestConcurrentPlansConflictAndSnapshotIsolation(t *testing.T) {
	mapping := testMapping(t, capabilitymapping.MappingCompatible, true, 900)
	input := AnalysisInput{Mapping: mapping, Context: testContext(), PolicySet: testPolicySet(t, allowRule()), Grants: emptyGrants()}
	registry := NewRegistry()
	planOne, err := Plan(input, registry.Snapshot(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	changed := testContext()
	changed.RequestOrigin = "second-origin"
	changed.Fingerprint = AuthorizationContextFingerprint(changed)
	input.Context = changed
	planTwo, err := Plan(input, registry.Snapshot(), DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, plan := range []AuthorizationPlan{planOne, planTwo} {
		plan := plan
		wg.Add(1)
		go func() { defer wg.Done(); _, applyErr := registry.ApplyPlan(plan); results <- applyErr }()
	}
	wg.Wait()
	close(results)
	var success, conflict int
	for err := range results {
		if err == nil {
			success++
		}
		if errors.Is(err, ErrSourceRevisionConflict) {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("expected one success and one conflict, got success=%d conflict=%d", success, conflict)
	}
	snapshot := registry.Snapshot()
	if len(snapshot.Assessments) != 1 {
		t.Fatal("unexpected assessment count")
	}
	snapshot.Assessments[0].Candidates[0].ReasonCodes[0] = "mutated"
	if registry.List()[0].Candidates[0].ReasonCodes[0] == "mutated" {
		t.Fatal("snapshot leaked mutable candidate")
	}
}

func TestDeterministicOrderingAndEmptyInventoryEquivalent(t *testing.T) {
	grants := emptyGrants()
	a := analyzeTest(t, testMapping(t, capabilitymapping.MappingCompatible, true, 900), testPolicySet(t, allowRule()), grants)
	b := analyzeTest(t, testMapping(t, capabilitymapping.MappingCompatible, true, 900), testPolicySet(t, allowRule()), grants)
	if a.Fingerprint != b.Fingerprint {
		t.Fatal("same logical input produced different fingerprint")
	}
	emptyMapping := testMapping(t, capabilitymapping.MappingCompatible, true, 900)
	emptyMapping.Candidates = nil
	emptyMapping.MappingAvailable = false
	emptyMapping.Fingerprint = capabilitymapping.CapabilityMappingAssessmentFingerprint(emptyMapping)
	empty := analyzeTest(t, emptyMapping, testPolicySet(t, allowRule()), grants)
	if empty.AuthorizationEligible || empty.Status != AssessmentActive {
		t.Fatalf("unexpected empty assessment: %+v", empty)
	}
}

func TestCompactStorageShapes(t *testing.T) {
	t.Logf("shallow sizes: authorization rule=%d, external grant=%d, candidate=%d, assessment=%d", unsafe.Sizeof(AuthorizationRule{}), unsafe.Sizeof(ExternalGrant{}), unsafe.Sizeof(AuthorizationCandidateAssessment{}), unsafe.Sizeof(AuthorizationBoundaryAssessment{}))
}
