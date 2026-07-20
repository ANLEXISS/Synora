package authorizationboundary

import "synora/internal/cge/capabilitymapping"

func Plan(input AnalysisInput, current RegistrySnapshot, policy Policy) (AuthorizationPlan, error) {
	if err := policy.Validate(); err != nil {
		return AuthorizationPlan{}, err
	}
	if current.Digest != "" && current.Digest != registryDigest(current) {
		return AuthorizationPlan{}, ErrFingerprintMismatch
	}
	if before, ok := assessmentByRequest(current.Assessments, string(input.Mapping.RequestID)); ok {
		if before.SourceMappingAssessmentFingerprint != input.Mapping.Fingerprint || before.SourcePolicyFingerprint != input.PolicySet.Fingerprint || before.SourceGrantSnapshotFingerprint != input.Grants.Fingerprint || before.SourceContextFingerprint != input.Context.Fingerprint {
			input.PreviousAssessment = &before
		}
	}
	assessment, err := Analyze(input, policy)
	if err != nil {
		return AuthorizationPlan{}, err
	}
	if before, ok := assessmentByRequest(current.Assessments, string(input.Mapping.RequestID)); ok && input.PreviousAssessment == nil {
		assessment.Revision = before.Revision
		assessment.Fingerprint = assessmentFingerprint(assessment)
	}
	plan := AuthorizationPlan{RequestID: string(input.Mapping.RequestID), SourceMappingFingerprint: input.Mapping.Fingerprint, SourcePolicyFingerprint: input.PolicySet.Fingerprint, SourceGrantSnapshotFingerprint: input.Grants.Fingerprint, SourceContextFingerprint: input.Context.Fingerprint, SourceRegistryRevision: current.Revision, ResultingAssessment: assessment, ReasonCodes: []string{"authorization_boundary_plan"}}
	if before, ok := assessmentByRequest(current.Assessments, string(input.Mapping.RequestID)); ok {
		if before.Fingerprint != assessment.Fingerprint {
			plan.Updates = append(plan.Updates, AuthorizationAssessmentUpdate{Before: before, After: assessment})
		}
	} else {
		plan.Creates = append(plan.Creates, assessment)
	}
	plan.Fingerprint = planFingerprint(plan)
	return plan, nil
}

func Reevaluate(previous AuthorizationBoundaryAssessment, mapping capabilitymapping.CapabilityMappingAssessment, context AuthorizationContext, policySet AuthorizationPolicySet, grants ExternalGrantSnapshot, policy Policy) (AuthorizationBoundaryAssessment, error) {
	if previous.RequestID != string(mapping.RequestID) {
		return AuthorizationBoundaryAssessment{}, ErrSourceRevisionConflict
	}
	return Analyze(AnalysisInput{Mapping: mapping, Context: context, PolicySet: policySet, Grants: grants, PreviousAssessment: &previous}, policy)
}

func assessmentByRequest(values []AuthorizationBoundaryAssessment, requestID string) (AuthorizationBoundaryAssessment, bool) {
	for _, value := range values {
		if value.RequestID == requestID {
			return value.Clone(), true
		}
	}
	return AuthorizationBoundaryAssessment{}, false
}
