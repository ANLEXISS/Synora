package authorizationboundary

func validateAssessment(assessment AuthorizationBoundaryAssessment) error {
	if assessment.RequestID == "" || assessment.EpisodeID == "" || assessment.SourceMappingAssessmentFingerprint == "" || assessment.SourcePolicyFingerprint == "" || assessment.SourceGrantSnapshotFingerprint == "" || assessment.SourceContextFingerprint == "" || !validAssessmentStatus(assessment.Status) || assessment.Revision == 0 || assessment.Fingerprint == "" || assessmentFingerprint(assessment) != assessment.Fingerprint {
		return ErrInvalidAssessment
	}
	seen := map[string]struct{}{}
	eligible := 0
	confirmation := 0
	denied := 0
	for _, candidate := range assessment.Candidates {
		if candidate.ID == "" || candidate.Fingerprint == "" || candidateFingerprint(candidate) != candidate.Fingerprint || candidate.MappingCandidateID == "" || candidate.CapabilityInstanceID == "" || candidate.CapabilityKind == "" || !validEligibilityStatus(candidate.Status) {
			return ErrInvalidAssessment
		}
		if _, exists := seen[candidate.ID]; exists {
			return ErrInvalidAssessment
		}
		seen[candidate.ID] = struct{}{}
		for _, value := range []int{candidate.PolicyCoveragePermille, candidate.GrantCoveragePermille, candidate.ScopeCoveragePermille, candidate.EligibilityPermille} {
			if value < 0 || value > 1000 {
				return ErrInvalidAssessment
			}
		}
		if candidate.Eligible != (candidate.Status == EligibilityEligible) {
			return ErrInvalidAssessment
		}
		switch candidate.Status {
		case EligibilityEligible:
			eligible++
		case EligibilityRequiresExternalConfirmation:
			confirmation++
		case EligibilityDenied, EligibilityDeniedByDefault, EligibilityPolicyConflict:
			denied++
		}
	}
	if eligible != assessment.EligibleCandidateCount || confirmation != assessment.ConfirmationRequiredCount || denied != assessment.DeniedCandidateCount || assessment.PreferredMarginPermille < 0 || assessment.PreferredMarginPermille > 1000 {
		return ErrInvalidAssessment
	}
	if assessment.AuthorizationEligible != (eligible > 0) || assessment.ExternalConfirmationRequired != (confirmation > 0) {
		return ErrInvalidAssessment
	}
	if assessment.PreferredEligibleCandidateID != "" {
		found := false
		for _, candidate := range assessment.Candidates {
			if candidate.ID == assessment.PreferredEligibleCandidateID && candidate.Eligible {
				found = true
			}
		}
		if !found {
			return ErrInvalidAssessment
		}
	}
	for _, conflict := range assessment.Conflicts {
		if conflict.ID == "" || conflict.CandidateID == "" || conflict.Fingerprint == "" || conflictFingerprint(conflict) != conflict.Fingerprint {
			return ErrInvalidAssessment
		}
	}
	return nil
}

func validAssessmentStatus(value AuthorizationAssessmentStatus) bool {
	return value == AssessmentActive || value == AssessmentEligible || value == AssessmentDenied || value == AssessmentConfirmationRequired || value == AssessmentDeferred || value == AssessmentObsolete || value == AssessmentInvalidated
}
