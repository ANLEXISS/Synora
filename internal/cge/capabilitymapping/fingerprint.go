package capabilitymapping

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

func catalogFingerprint(c CapabilityCatalog) string {
	copy := c
	copy.Fingerprint = ""
	copy.Definitions = cloneDefinitions(c.Definitions)
	for i := range copy.Definitions {
		sort.Strings(copy.Definitions[i].SupportedRequestKinds)
		sort.Strings(copy.Definitions[i].SupportedDimensions)
		sort.Strings(copy.Definitions[i].OutputFactCodes)
		sort.Strings(copy.Definitions[i].RequiredInputContextCodes)
		sort.Slice(copy.Definitions[i].SupportedQualityDimensions, func(a, b int) bool {
			return copy.Definitions[i].SupportedQualityDimensions[a] < copy.Definitions[i].SupportedQualityDimensions[b]
		})
	}
	sort.Slice(copy.Definitions, func(i, j int) bool { return copy.Definitions[i].Kind < copy.Definitions[j].Kind })
	return digestJSON("capability-catalog-v1:", copy)
}

func definitionFingerprint(d CapabilityDefinition) string {
	copy := d.Clone()
	sort.Strings(copy.SupportedRequestKinds)
	sort.Strings(copy.SupportedDimensions)
	sort.Strings(copy.OutputFactCodes)
	sort.Strings(copy.RequiredInputContextCodes)
	sort.Slice(copy.SupportedQualityDimensions, func(i, j int) bool { return copy.SupportedQualityDimensions[i] < copy.SupportedQualityDimensions[j] })
	return digestJSON("capability-definition-v1:", copy)
}

func instanceFingerprint(i CapabilityInstance) string {
	copy := i.Clone()
	copy.Fingerprint = ""
	sortScopes(copy.SupportedScopes)
	sortConstraints(copy.Constraints)
	return digestJSON("capability-instance-v1:", copy)
}

func inventoryFingerprint(i CapabilityInventory) string {
	copy := i.Clone()
	copy.Fingerprint = ""
	sort.Slice(copy.Instances, func(a, b int) bool { return copy.Instances[a].ID < copy.Instances[b].ID })
	for n := range copy.Instances {
		sortScopes(copy.Instances[n].SupportedScopes)
		sortConstraints(copy.Instances[n].Constraints)
	}
	return digestJSON("capability-inventory-v1:", copy)
}

func requirementFingerprint(r CapabilityRequirement) string {
	copy := r.Clone()
	copy.Fingerprint = ""
	sort.Slice(copy.RequiredKinds, func(i, j int) bool { return copy.RequiredKinds[i] < copy.RequiredKinds[j] })
	sort.Strings(copy.RequiredDimensions)
	sort.Strings(copy.RequiredFactCodes)
	sortScopes(copy.RequiredScopes)
	sortConstraints(copy.RequiredConstraints)
	return digestJSON("capability-requirement-v1:", copy)
}

func candidateFingerprint(c CapabilityMappingCandidate) string {
	copy := c.Clone()
	copy.Fingerprint = ""
	sort.Strings(copy.SatisfiedRequirements)
	sort.Strings(copy.MissingRequirements)
	sort.Strings(copy.ViolatedConstraints)
	sort.Strings(copy.ReasonCodes)
	return digestJSON("capability-mapping-candidate-v1:", copy)
}

func assessmentFingerprint(a CapabilityMappingAssessment) string {
	copy := a.Clone()
	copy.Fingerprint = ""
	sort.Slice(copy.Candidates, func(i, j int) bool { return copy.Candidates[i].ID < copy.Candidates[j].ID })
	return digestJSON("capability-mapping-assessment-v1:", copy)
}

func planFingerprint(p MappingPlan) string {
	copy := p.Clone()
	copy.Fingerprint = ""
	sort.Slice(copy.Creates, func(i, j int) bool { return copy.Creates[i].RequestID < copy.Creates[j].RequestID })
	sort.Slice(copy.Updates, func(i, j int) bool { return copy.Updates[i].After.RequestID < copy.Updates[j].After.RequestID })
	sort.Slice(copy.Invalidates, func(i, j int) bool { return copy.Invalidates[i].RequestID < copy.Invalidates[j].RequestID })
	return digestJSON("capability-mapping-plan-v1:", copy)
}

func registryDigest(s RegistrySnapshot) string {
	copy := s.Clone()
	copy.Digest = ""
	sort.Slice(copy.Assessments, func(i, j int) bool { return copy.Assessments[i].RequestID < copy.Assessments[j].RequestID })
	return digestJSON("capability-mapping-registry-v1:", struct {
		Catalog, Policy string
		Assessments     []CapabilityMappingAssessment
	}{copy.CatalogFingerprint, copy.PolicyFingerprint, copy.Assessments})
}

func CatalogFingerprintOf(c CapabilityCatalog) string               { return catalogFingerprint(c) }
func CapabilityDefinitionFingerprint(d CapabilityDefinition) string { return definitionFingerprint(d) }
func CapabilityInstanceFingerprint(i CapabilityInstance) string     { return instanceFingerprint(i) }
func CapabilityInventoryFingerprint(i CapabilityInventory) string   { return inventoryFingerprint(i) }
func CapabilityRequirementFingerprint(r CapabilityRequirement) string {
	return requirementFingerprint(r)
}
func CapabilityMappingCandidateFingerprint(c CapabilityMappingCandidate) string {
	return candidateFingerprint(c)
}
func CapabilityMappingAssessmentFingerprint(a CapabilityMappingAssessment) string {
	return assessmentFingerprint(a)
}
func MappingPlanFingerprint(p MappingPlan) string { return planFingerprint(p) }
func RegistryDigest(s RegistrySnapshot) string    { return registryDigest(s) }

func cloneDefinitions(values []CapabilityDefinition) []CapabilityDefinition {
	out := make([]CapabilityDefinition, len(values))
	for i, value := range values {
		out[i] = value.Clone()
	}
	return out
}
