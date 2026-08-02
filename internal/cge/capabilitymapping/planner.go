package capabilitymapping

import (
	"sort"

	"synora/internal/cge/advisoryrequests"
)

func Analyze(input AnalysisInput, policy Policy) (CapabilityMappingAssessment, error) {
	if err := policy.Validate(); err != nil {
		return CapabilityMappingAssessment{}, err
	}
	if err := ValidateCatalog(input.Catalog); err != nil {
		return CapabilityMappingAssessment{}, err
	}
	if err := ValidateInventory(input.Inventory, input.Catalog); err != nil {
		return CapabilityMappingAssessment{}, err
	}
	if err := validateRequest(input.Request, policy); err != nil {
		return CapabilityMappingAssessment{}, err
	}
	requirement := CapabilityRequirement{}
	var err error
	if input.Requirement != nil {
		requirement = input.Requirement.Clone()
		if err = ValidateRequirement(requirement); err != nil {
			return CapabilityMappingAssessment{}, err
		}
		if requirement.RequestID != input.Request.ID || requirement.RequestKey != input.Request.Key {
			return CapabilityMappingAssessment{}, ErrInvalidRequirement
		}
	} else {
		requirement, err = BuildRequirement(input.Request, policy)
		if err != nil {
			return CapabilityMappingAssessment{}, err
		}
	}

	inventory := normalizeInventory(input.Inventory)
	definitionByKind := map[CapabilityKind]CapabilityDefinition{}
	for _, definition := range input.Catalog.Definitions {
		definitionByKind[definition.Kind] = definition.Clone()
	}
	candidates := make([]CapabilityMappingCandidate, 0, len(inventory.Instances))
	capabilityUnavailable := false
	for _, instance := range inventory.Instances {
		definition, ok := definitionByKind[instance.Kind]
		if !ok {
			return CapabilityMappingAssessment{}, ErrUnknownCapabilityKind
		}
		result := evaluateCompatibility(requirement, definition, instance, policy, input.Request.Fingerprint, input.Inventory.Fingerprint)
		candidate := scoreCandidate(result.candidate, instance, policy)
		candidates = append(candidates, candidate)
		if result.unavailable {
			capabilityUnavailable = true
		}
	}
	rankCandidates(candidates)
	if len(candidates) > policy.MaxCandidatesPerRequest {
		if !policy.PreserveIncompatibleCandidates {
			candidates = candidates[:policy.MaxCandidatesPerRequest]
		} else {
			candidates = append([]CapabilityMappingCandidate(nil), candidates[:policy.MaxCandidatesPerRequest]...)
		}
	}
	assessment := CapabilityMappingAssessment{RequestID: input.Request.ID, RequestKey: input.Request.Key, EpisodeID: input.Request.EpisodeID, SourceRequestFingerprint: input.Request.Fingerprint, SourceInventoryFingerprint: input.Inventory.Fingerprint, CatalogFingerprint: input.Catalog.Fingerprint, PolicyFingerprint: policy.Fingerprint(), Requirement: requirement, Candidates: candidates, CapabilityUnavailable: capabilityUnavailable, Revision: 1}
	if input.PreviousAssessment != nil {
		if input.PreviousAssessment.RequestID != input.Request.ID {
			return CapabilityMappingAssessment{}, ErrStaleRequest
		}
		if input.PreviousAssessment.Fingerprint == "" || assessmentFingerprint(*input.PreviousAssessment) != input.PreviousAssessment.Fingerprint {
			return CapabilityMappingAssessment{}, ErrFingerprintMismatch
		}
		assessment.Revision = input.PreviousAssessment.Revision + 1
	}
	assessment.MappingAvailable = false
	for _, candidate := range candidates {
		if candidate.Compatible {
			assessment.MappingAvailable = true
			break
		}
	}
	compatible := make([]CapabilityMappingCandidate, 0)
	for _, candidate := range candidates {
		if candidate.Compatible {
			compatible = append(compatible, candidate)
		}
	}
	if len(compatible) >= 2 && sameCandidateRank(compatible[0], compatible[1]) {
		assessment.MappingAmbiguous = true
	}
	if len(compatible) > 0 && compatible[0].UtilityPermille >= policy.MinUtilityPermille && compatible[0].QualityPermille >= policy.MinQualityPermille && !assessment.MappingAmbiguous {
		margin := 1000
		if len(compatible) > 1 {
			margin = compatible[0].UtilityPermille - compatible[1].UtilityPermille
			if margin < 0 {
				margin = 0
			}
		}
		assessment.PreferredMarginPermille = margin
		if margin >= policy.MinPreferredMarginPermille {
			assessment.PreferredCandidateID = compatible[0].ID
		}
	}
	assessment.Fingerprint = assessmentFingerprint(assessment)
	return assessment, nil
}

func Plan(input AnalysisInput, current RegistrySnapshot, policy Policy) (MappingPlan, error) {
	if err := policy.Validate(); err != nil {
		return MappingPlan{}, err
	}
	if current.PolicyFingerprint != "" && current.PolicyFingerprint != policy.Fingerprint() {
		return MappingPlan{}, ErrFingerprintMismatch
	}
	if current.CatalogFingerprint != "" && current.CatalogFingerprint != input.Catalog.Fingerprint {
		return MappingPlan{}, ErrFingerprintMismatch
	}
	if current.Digest != "" && current.Digest != registryDigest(current) {
		return MappingPlan{}, ErrFingerprintMismatch
	}
	if before, ok := assessmentByRequest(current.Assessments, input.Request.ID); ok {
		if before.SourceRequestFingerprint != input.Request.Fingerprint || before.SourceInventoryFingerprint != input.Inventory.Fingerprint || before.CatalogFingerprint != input.Catalog.Fingerprint || before.PolicyFingerprint != policy.Fingerprint() {
			input.PreviousAssessment = &before
		}
	}
	assessment, err := Analyze(input, policy)
	if err != nil {
		return MappingPlan{}, err
	}
	if before, ok := assessmentByRequest(current.Assessments, input.Request.ID); ok && input.PreviousAssessment == nil {
		assessment.Revision = before.Revision
		assessment.Fingerprint = assessmentFingerprint(assessment)
	}
	plan := MappingPlan{RequestID: input.Request.ID, SourceRequestFingerprint: input.Request.Fingerprint, SourceInventoryFingerprint: input.Inventory.Fingerprint, SourceRegistryRevision: current.Revision, ResultingAssessment: assessment, ReasonCodes: []string{"capability_mapping_plan"}}
	if before, ok := assessmentByRequest(current.Assessments, input.Request.ID); ok {
		if before.Fingerprint != assessment.Fingerprint {
			plan.Updates = append(plan.Updates, CapabilityMappingUpdate{Before: before, After: assessment})
		}
	} else {
		plan.Creates = append(plan.Creates, assessment)
	}
	plan.Fingerprint = planFingerprint(plan)
	return plan, nil
}

func Reevaluate(previous CapabilityMappingAssessment, request advisoryrequests.AdvisoryEvidenceRequest, inventory CapabilityInventory, catalog CapabilityCatalog, policy Policy) (CapabilityMappingAssessment, error) {
	if previous.RequestID != request.ID || previous.SourceRequestFingerprint != request.Fingerprint {
		return CapabilityMappingAssessment{}, ErrStaleRequest
	}
	return Analyze(AnalysisInput{Request: request, Catalog: catalog, Inventory: inventory, PreviousAssessment: &previous}, policy)
}

func assessmentByRequest(values []CapabilityMappingAssessment, requestID advisoryrequests.AdvisoryRequestID) (CapabilityMappingAssessment, bool) {
	for _, value := range values {
		if value.RequestID == requestID {
			return value.Clone(), true
		}
	}
	return CapabilityMappingAssessment{}, false
}

func sortCandidateIDs(values []CapabilityMappingCandidate) {
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
}
