package capabilitymapping

import "sort"

func Explain(candidate CapabilityMappingCandidate) (MappingExplanation, error) {
	if candidate.ID == "" || candidate.RequestID == "" || candidate.CapabilityInstanceID == "" || candidate.Fingerprint == "" || candidateFingerprint(candidate) != candidate.Fingerprint {
		return MappingExplanation{}, ErrInvalidExplanation
	}
	return MappingExplanation{RequestID: candidate.RequestID, CandidateID: candidate.ID, CapabilityInstanceID: candidate.CapabilityInstanceID, CapabilityKind: candidate.CapabilityKind, Compatible: candidate.Compatible, Status: candidate.Status, SummaryCode: "capability_mapping_descriptive_assessment", SatisfiedRequirements: uniqueSorted(append([]string(nil), candidate.SatisfiedRequirements...)), MissingRequirements: uniqueSorted(append([]string(nil), candidate.MissingRequirements...)), ViolatedConstraints: uniqueSorted(append([]string(nil), candidate.ViolatedConstraints...)), ReasonCodes: uniqueSorted(append([]string(nil), candidate.ReasonCodes...)), CompatibilityPermille: candidate.CompatibilityPermille, QualityPermille: candidate.QualityPermille, ConstraintPermille: candidate.ConstraintPermille, ScopePermille: candidate.ScopePermille, AvailabilityPermille: candidate.AvailabilityPermille, UtilityPermille: candidate.UtilityPermille, NotACommand: true, NotAuthorization: true, NotAProbability: true, NoSecurityMeaning: true}, nil
}

func ValidateExplanation(explanation MappingExplanation) error {
	if explanation.RequestID == "" || explanation.CandidateID == "" || explanation.CapabilityInstanceID == "" || explanation.SummaryCode == "" || !explanation.NotACommand || !explanation.NotAuthorization || !explanation.NotAProbability || !explanation.NoSecurityMeaning {
		return ErrInvalidExplanation
	}
	for _, score := range []int{explanation.CompatibilityPermille, explanation.QualityPermille, explanation.ConstraintPermille, explanation.ScopePermille, explanation.AvailabilityPermille, explanation.UtilityPermille} {
		if score < 0 || score > 1000 {
			return ErrInvalidExplanation
		}
	}
	if !sortedStrings(explanation.SatisfiedRequirements) || !sortedStrings(explanation.MissingRequirements) || !sortedStrings(explanation.ViolatedConstraints) || !sortedStrings(explanation.ReasonCodes) {
		return ErrInvalidExplanation
	}
	return nil
}

func sortedStrings(values []string) bool {
	copy := append([]string(nil), values...)
	sort.Strings(copy)
	for i := range values {
		if values[i] != copy[i] || values[i] == "" || i > 0 && values[i-1] == values[i] {
			return false
		}
	}
	return true
}
