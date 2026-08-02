package authorizationboundary

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

func digestJSON(prefix string, value any) string {
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return prefix + hex.EncodeToString(digest[:])
}

func contextFingerprint(context AuthorizationContext) string {
	copy := context.Clone()
	copy.Fingerprint = ""
	copy.RequestedScope = canonicalScopes(copy.RequestedScope)
	return digestJSON("authorization-context-v1:", copy)
}

func ruleFingerprint(rule AuthorizationRule) string {
	copy := rule.Clone()
	copy.PurposeCodes = canonicalPurposes(copy.PurposeCodes)
	sort.Slice(copy.CapabilityKinds, func(i, j int) bool { return copy.CapabilityKinds[i] < copy.CapabilityKinds[j] })
	copy.DomainIDs = canonicalStrings(copy.DomainIDs)
	copy.RequiredScopes = canonicalScopes(copy.RequiredScopes)
	copy.ExcludedScopes = canonicalScopes(copy.ExcludedScopes)
	sort.Slice(copy.RequiredGrantKinds, func(i, j int) bool { return copy.RequiredGrantKinds[i] < copy.RequiredGrantKinds[j] })
	return digestJSON("authorization-rule-v1:", copy)
}

func policySetFingerprint(set AuthorizationPolicySet) string {
	copy := set.Clone()
	copy.Fingerprint = ""
	copy.Rules = sortedPolicyRules(copy.Rules)
	return digestJSON("authorization-policy-set-v1:", copy)
}

func policyFingerprint(policy Policy) string {
	return digestJSON("authorization-boundary-policy-v1:", policy)
}

func grantFingerprint(grant ExternalGrant) string {
	copy := grant.Clone()
	copy.Fingerprint = ""
	copy.PurposeCodes = canonicalPurposes(copy.PurposeCodes)
	sort.Slice(copy.CapabilityKinds, func(i, j int) bool { return copy.CapabilityKinds[i] < copy.CapabilityKinds[j] })
	copy.Scopes = canonicalScopes(copy.Scopes)
	return digestJSON("external-grant-v1:", copy)
}

func grantSnapshotFingerprint(snapshot ExternalGrantSnapshot) string {
	copy := snapshot.Clone()
	copy.Fingerprint = ""
	sortGrants(copy.Grants)
	copy.Index = nil
	return digestJSON("external-grant-snapshot-v1:", copy)
}

func candidateFingerprint(candidate AuthorizationCandidateAssessment) string {
	copy := candidate.Clone()
	copy.Fingerprint = ""
	copy.AppliedRuleIDs = canonicalStrings(copy.AppliedRuleIDs)
	copy.DenyingRuleIDs = canonicalStrings(copy.DenyingRuleIDs)
	copy.ConfirmationRuleIDs = canonicalStrings(copy.ConfirmationRuleIDs)
	copy.DeferredRuleIDs = canonicalStrings(copy.DeferredRuleIDs)
	copy.SatisfiedGrantIDs = canonicalGrantIDs(copy.SatisfiedGrantIDs)
	copy.MissingGrantKinds = canonicalGrantKinds(copy.MissingGrantKinds)
	copy.RejectedGrantIDs = canonicalGrantIDs(copy.RejectedGrantIDs)
	copy.SatisfiedConditions = canonicalStrings(copy.SatisfiedConditions)
	copy.MissingConditions = canonicalStrings(copy.MissingConditions)
	copy.ViolatedConditions = canonicalStrings(copy.ViolatedConditions)
	copy.ReasonCodes = canonicalStrings(copy.ReasonCodes)
	return digestJSON("authorization-candidate-assessment-v1:", copy)
}

func conflictFingerprint(conflict AuthorizationPolicyConflict) string {
	copy := conflict.Clone()
	copy.Fingerprint = ""
	copy.RuleIDs = canonicalStrings(copy.RuleIDs)
	sort.Slice(copy.Effects, func(i, j int) bool { return copy.Effects[i] < copy.Effects[j] })
	return digestJSON("authorization-policy-conflict-v1:", copy)
}

func assessmentFingerprint(assessment AuthorizationBoundaryAssessment) string {
	copy := assessment.Clone()
	copy.Fingerprint = ""
	sort.Slice(copy.Candidates, func(i, j int) bool { return copy.Candidates[i].ID < copy.Candidates[j].ID })
	sort.Slice(copy.Conflicts, func(i, j int) bool { return copy.Conflicts[i].ID < copy.Conflicts[j].ID })
	return digestJSON("authorization-boundary-assessment-v1:", copy)
}

func planFingerprint(plan AuthorizationPlan) string {
	copy := plan.Clone()
	copy.Fingerprint = ""
	sort.Slice(copy.Creates, func(i, j int) bool { return copy.Creates[i].RequestID < copy.Creates[j].RequestID })
	sort.Slice(copy.Updates, func(i, j int) bool { return copy.Updates[i].After.RequestID < copy.Updates[j].After.RequestID })
	sort.Slice(copy.Invalidates, func(i, j int) bool { return copy.Invalidates[i].RequestID < copy.Invalidates[j].RequestID })
	return digestJSON("authorization-plan-v1:", copy)
}

func registryDigest(snapshot RegistrySnapshot) string {
	copy := snapshot.Clone()
	copy.Digest = ""
	sort.Slice(copy.Assessments, func(i, j int) bool { return copy.Assessments[i].RequestID < copy.Assessments[j].RequestID })
	return digestJSON("authorization-boundary-registry-v1:", struct {
		Policy      string
		Assessments []AuthorizationBoundaryAssessment
	}{copy.PolicyFingerprint, copy.Assessments})
}

func AuthorizationContextFingerprint(value AuthorizationContext) string {
	return contextFingerprint(value)
}
func AuthorizationRuleFingerprint(value AuthorizationRule) string { return ruleFingerprint(value) }
func AuthorizationPolicySetFingerprint(value AuthorizationPolicySet) string {
	return policySetFingerprint(value)
}
func ExternalGrantFingerprint(value ExternalGrant) string { return grantFingerprint(value) }
func ExternalGrantSnapshotFingerprint(value ExternalGrantSnapshot) string {
	return grantSnapshotFingerprint(value)
}
func AuthorizationCandidateFingerprint(value AuthorizationCandidateAssessment) string {
	return candidateFingerprint(value)
}
func AuthorizationBoundaryAssessmentFingerprint(value AuthorizationBoundaryAssessment) string {
	return assessmentFingerprint(value)
}
func AuthorizationPlanFingerprint(value AuthorizationPlan) string { return planFingerprint(value) }
func RegistryDigest(value RegistrySnapshot) string                { return registryDigest(value) }

func canonicalGrantIDs(values []ExternalGrantID) []ExternalGrantID {
	out := append([]ExternalGrantID(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func canonicalGrantKinds(values []ExternalGrantKind) []ExternalGrantKind {
	out := append([]ExternalGrantKind(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
