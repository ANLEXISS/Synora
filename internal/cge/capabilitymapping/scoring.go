package capabilitymapping

import "sort"

func candidateLess(a, b CapabilityMappingCandidate) bool {
	if a.Compatible != b.Compatible {
		return a.Compatible
	}
	if a.UtilityPermille != b.UtilityPermille {
		return a.UtilityPermille > b.UtilityPermille
	}
	if a.CompatibilityPermille != b.CompatibilityPermille {
		return a.CompatibilityPermille > b.CompatibilityPermille
	}
	if a.QualityPermille != b.QualityPermille {
		return a.QualityPermille > b.QualityPermille
	}
	if a.ConstraintPermille != b.ConstraintPermille {
		return a.ConstraintPermille > b.ConstraintPermille
	}
	if a.ScopePermille != b.ScopePermille {
		return a.ScopePermille > b.ScopePermille
	}
	if a.CapabilityKind != b.CapabilityKind {
		return a.CapabilityKind < b.CapabilityKind
	}
	if a.Status != b.Status {
		return a.Status < b.Status
	}
	if a.CapabilityInstanceID != b.CapabilityInstanceID {
		return a.CapabilityInstanceID < b.CapabilityInstanceID
	}
	return a.ID < b.ID
}

func rankCandidates(values []CapabilityMappingCandidate) {
	sort.Slice(values, func(i, j int) bool { return candidateLess(values[i], values[j]) })
}

func sameCandidateRank(a, b CapabilityMappingCandidate) bool {
	return a.Compatible == b.Compatible && a.UtilityPermille == b.UtilityPermille && a.CompatibilityPermille == b.CompatibilityPermille && a.QualityPermille == b.QualityPermille && a.ConstraintPermille == b.ConstraintPermille && a.ScopePermille == b.ScopePermille && a.CapabilityKind == b.CapabilityKind && a.Status == b.Status
}
