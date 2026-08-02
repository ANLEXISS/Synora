package authorizationboundary

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"synora/internal/cge/capabilitymapping"
)

func benchmarkMapping(t testing.TB, candidates int, requestID string) capabilitymapping.CapabilityMappingAssessment {
	t.Helper()
	assessment := testMapping(t, capabilitymapping.MappingCompatible, true, 900)
	reflect.ValueOf(&assessment).Elem().FieldByName("RequestID").SetString(requestID)
	assessment.Candidates = assessment.Candidates[:0]
	for i := 0; i < candidates; i++ {
		candidate := capabilitymapping.CapabilityMappingCandidate{ID: fmt.Sprintf("mapping-candidate-%04d", i), CapabilityInstanceID: capabilitymapping.CapabilityInstanceID(fmt.Sprintf("capability-instance-%04d", i)), CapabilityKind: capabilitymapping.CapabilityIdentityObservation, Status: capabilitymapping.MappingCompatible, CostClass: capabilitymapping.CapabilityCostLow, LatencyClass: capabilitymapping.CapabilityLatencyShort, SensitivityClass: capabilitymapping.CapabilitySensitivityLow, QualityCalibrated: true, Compatible: true, CompatibilityPermille: 1000, QualityPermille: 900 - i%100, ConstraintPermille: 1000, ScopePermille: 1000, AvailabilityPermille: 1000, UtilityPermille: 900 - i%100, SourceRequestFingerprint: "source-request", SourceInventoryFingerprint: "source-inventory"}
		candidate.Fingerprint = capabilitymapping.CapabilityMappingCandidateFingerprint(candidate)
		assessment.Candidates = append(assessment.Candidates, candidate)
	}
	assessment.MappingAvailable = candidates > 0
	assessment.Fingerprint = capabilitymapping.CapabilityMappingAssessmentFingerprint(assessment)
	return assessment
}

func benchmarkPolicySet(t testing.TB, rules int, requireGrant bool) AuthorizationPolicySet {
	t.Helper()
	values := make([]AuthorizationRule, 0, rules)
	for i := 0; i < rules; i++ {
		rule := allowRule()
		rule.ID = fmt.Sprintf("rule-allow-%04d", i)
		rule.Priority = i
		if requireGrant {
			rule.RequiredGrantKinds = []ExternalGrantKind{GrantPrivacyConsent}
		}
		values = append(values, rule)
	}
	set := testPolicySet(t, values...)
	return set
}

func benchmarkGrants(t testing.TB, count int, valid bool) ExternalGrantSnapshot {
	t.Helper()
	values := make([]ExternalGrant, 0, count)
	for i := 0; i < count; i++ {
		from := testAuthorizationTime.Add(-time.Hour)
		until := testAuthorizationTime.Add(time.Hour)
		if !valid {
			until = testAuthorizationTime.Add(-time.Minute)
		}
		grant := testGrant(GrantPrivacyConsent, from, until)
		grant.ID = ExternalGrantID(fmt.Sprintf("grant-%04d", i))
		grant.Fingerprint = ExternalGrantFingerprint(grant)
		values = append(values, grant)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return grantSnapshot(values...)
}

func BenchmarkValidatePolicySet10(b *testing.B)  { benchmarkValidatePolicySet(b, 10) }
func BenchmarkValidatePolicySet100(b *testing.B) { benchmarkValidatePolicySet(b, 100) }
func benchmarkValidatePolicySet(b *testing.B, count int) {
	set := benchmarkPolicySet(b, count, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidatePolicySet(set)
	}
}

func BenchmarkValidateGrantSnapshot10(b *testing.B)  { benchmarkValidateGrantSnapshot(b, 10) }
func BenchmarkValidateGrantSnapshot100(b *testing.B) { benchmarkValidateGrantSnapshot(b, 100) }
func benchmarkValidateGrantSnapshot(b *testing.B, count int) {
	snapshot := benchmarkGrants(b, count, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateGrantSnapshot(snapshot)
	}
}

func BenchmarkAnalyze1Mapping10Rules10Grants(b *testing.B) { benchmarkAnalyze(b, 1, 10, 10, false) }
func BenchmarkAnalyze10Mappings100Rules100Grants(b *testing.B) {
	benchmarkAnalyze(b, 10, 100, 100, false)
}
func BenchmarkAnalyze32Mappings256Rules256Grants(b *testing.B) {
	benchmarkAnalyze(b, 32, 256, 256, false)
}
func benchmarkAnalyze(b *testing.B, mappings, rules, grants int, requireGrant bool) {
	input := AnalysisInput{Mapping: benchmarkMapping(b, mappings, "request-benchmark"), Context: testContext(), PolicySet: benchmarkPolicySet(b, rules, requireGrant), Grants: benchmarkGrants(b, grants, true)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Analyze(input, DefaultPolicy())
	}
}

func BenchmarkExplicitAllow(b *testing.B) { benchmarkEffect(b, testPolicySet(b, allowRule())) }
func BenchmarkExplicitDeny(b *testing.B)  { benchmarkEffect(b, testPolicySet(b, denyRule())) }
func BenchmarkGrantRequired(b *testing.B) {
	rule := allowRule()
	rule.RequiredGrantKinds = []ExternalGrantKind{GrantPrivacyConsent}
	benchmarkEffect(b, testPolicySet(b, rule))
}
func BenchmarkGrantValid(b *testing.B) {
	rule := allowRule()
	rule.RequiredGrantKinds = []ExternalGrantKind{GrantPrivacyConsent}
	benchmarkEffectWithGrants(b, testPolicySet(b, rule), benchmarkGrants(b, 1, true))
}
func BenchmarkGrantRevoked(b *testing.B) {
	rule := allowRule()
	rule.RequiredGrantKinds = []ExternalGrantKind{GrantPrivacyConsent}
	benchmarkEffectWithGrants(b, testPolicySet(b, rule), benchmarkGrants(b, 1, false))
}

func benchmarkEffect(b *testing.B, set AuthorizationPolicySet) {
	benchmarkEffectWithGrants(b, set, emptyGrants())
}
func benchmarkEffectWithGrants(b *testing.B, set AuthorizationPolicySet, grants ExternalGrantSnapshot) {
	input := AnalysisInput{Mapping: benchmarkMapping(b, 1, "request-effect"), Context: testContext(), PolicySet: set, Grants: grants}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Analyze(input, DefaultPolicy())
	}
}

func BenchmarkReevaluatePolicyChanged(b *testing.B) {
	mapping := benchmarkMapping(b, 1, "request-reevaluate")
	set := testPolicySet(b, allowRule())
	previous, _ := Analyze(AnalysisInput{Mapping: mapping, Context: testContext(), PolicySet: set, Grants: emptyGrants()}, DefaultPolicy())
	changed := testPolicySet(b, denyRule())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Reevaluate(previous, mapping, testContext(), changed, emptyGrants(), DefaultPolicy())
	}
}

func BenchmarkReevaluateGrantAdded(b *testing.B) {
	rule := allowRule()
	rule.RequiredGrantKinds = []ExternalGrantKind{GrantPrivacyConsent}
	set := testPolicySet(b, rule)
	mapping := benchmarkMapping(b, 1, "request-grant-added")
	previous, _ := Analyze(AnalysisInput{Mapping: mapping, Context: testContext(), PolicySet: set, Grants: emptyGrants()}, DefaultPolicy())
	grants := benchmarkGrants(b, 1, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Reevaluate(previous, mapping, testContext(), set, grants, DefaultPolicy())
	}
}

func BenchmarkReevaluateGrantRevoked(b *testing.B) {
	rule := allowRule()
	rule.RequiredGrantKinds = []ExternalGrantKind{GrantPrivacyConsent}
	set := testPolicySet(b, rule)
	mapping := benchmarkMapping(b, 1, "request-grant-revoked")
	grants := benchmarkGrants(b, 1, true)
	previous, _ := Analyze(AnalysisInput{Mapping: mapping, Context: testContext(), PolicySet: set, Grants: grants}, DefaultPolicy())
	revoked := benchmarkGrants(b, 1, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Reevaluate(previous, mapping, testContext(), set, revoked, DefaultPolicy())
	}
}

func BenchmarkApplyPlan(b *testing.B) {
	input := AnalysisInput{Mapping: benchmarkMapping(b, 1, "request-apply"), Context: testContext(), PolicySet: testPolicySet(b, allowRule()), Grants: emptyGrants()}
	registry := NewRegistry()
	plan, _ := Plan(input, registry.Snapshot(), DefaultPolicy())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fresh := NewRegistry()
		_, _ = fresh.ApplyPlan(plan)
	}
}

func BenchmarkPublicSnapshot10(b *testing.B)   { benchmarkSnapshot(b, 10) }
func BenchmarkPublicSnapshot100(b *testing.B)  { benchmarkSnapshot(b, 100) }
func BenchmarkPublicSnapshot1000(b *testing.B) { benchmarkSnapshot(b, 1000) }
func benchmarkSnapshot(b *testing.B, count int) {
	registry := NewRegistry()
	for i := 0; i < count; i++ {
		mapping := benchmarkMapping(b, 1, fmt.Sprintf("request-snapshot-%04d", i))
		input := AnalysisInput{Mapping: mapping, Context: testContext(), PolicySet: testPolicySet(b, allowRule()), Grants: emptyGrants()}
		plan, _ := Plan(input, registry.Snapshot(), DefaultPolicy())
		_, _ = registry.ApplyPlan(plan)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry.Snapshot()
	}
}

func BenchmarkRegistryDigest(b *testing.B) {
	registry := NewRegistry()
	mapping := benchmarkMapping(b, 10, "request-digest")
	input := AnalysisInput{Mapping: mapping, Context: testContext(), PolicySet: testPolicySet(b, allowRule()), Grants: emptyGrants()}
	plan, _ := Plan(input, registry.Snapshot(), DefaultPolicy())
	_, _ = registry.ApplyPlan(plan)
	snapshot := registry.Snapshot()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RegistryDigest(snapshot)
	}
}

func BenchmarkExplain(b *testing.B) {
	assessment := analyzeTest(b, testMapping(b, capabilitymapping.MappingCompatible, true, 900), testPolicySet(b, allowRule()), emptyGrants())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Explain(assessment.Candidates[0])
	}
}
