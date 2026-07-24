package authorizationboundary

import (
	"sort"

	"synora/internal/cge/capabilitymapping"
)

func rankCandidates(values []AuthorizationCandidateAssessment) {
	sort.SliceStable(values, func(i, j int) bool {
		a, b := values[i], values[j]
		if a.Eligible != b.Eligible {
			return a.Eligible
		}
		if a.EligibilityPermille != b.EligibilityPermille {
			return a.EligibilityPermille > b.EligibilityPermille
		}
		if a.PolicyCoveragePermille != b.PolicyCoveragePermille {
			return a.PolicyCoveragePermille > b.PolicyCoveragePermille
		}
		if a.GrantCoveragePermille != b.GrantCoveragePermille {
			return a.GrantCoveragePermille > b.GrantCoveragePermille
		}
		if a.ScopeCoveragePermille != b.ScopeCoveragePermille {
			return a.ScopeCoveragePermille > b.ScopeCoveragePermille
		}
		if mappingClassRank(string(a.SensitivityClass), 2) != mappingClassRank(string(b.SensitivityClass), 2) {
			return mappingClassRank(string(a.SensitivityClass), 2) < mappingClassRank(string(b.SensitivityClass), 2)
		}
		if mappingClassRank(string(a.CostClass), 0) != mappingClassRank(string(b.CostClass), 0) {
			return mappingClassRank(string(a.CostClass), 0) < mappingClassRank(string(b.CostClass), 0)
		}
		if mappingClassRank(string(a.LatencyClass), 1) != mappingClassRank(string(b.LatencyClass), 1) {
			return mappingClassRank(string(a.LatencyClass), 1) < mappingClassRank(string(b.LatencyClass), 1)
		}
		if a.CapabilityKind != b.CapabilityKind {
			return a.CapabilityKind < b.CapabilityKind
		}
		if a.CapabilityInstanceID != b.CapabilityInstanceID {
			return a.CapabilityInstanceID < b.CapabilityInstanceID
		}
		return a.ID < b.ID
	})
}

func eligibleCandidates(values []AuthorizationCandidateAssessment) []AuthorizationCandidateAssessment {
	out := make([]AuthorizationCandidateAssessment, 0, len(values))
	for _, value := range values {
		if value.Status == EligibilityEligible && value.Eligible {
			out = append(out, value)
		}
	}
	return out
}

func sameEligibilityRank(a, b AuthorizationCandidateAssessment) bool {
	return a.EligibilityPermille == b.EligibilityPermille && a.PolicyCoveragePermille == b.PolicyCoveragePermille && a.GrantCoveragePermille == b.GrantCoveragePermille && a.ScopeCoveragePermille == b.ScopeCoveragePermille && a.CapabilityKind == b.CapabilityKind
}

func mappingClassRank(value string, kind int) int {
	if value == "unknown" {
		return 3
	}
	if kind == 0 {
		switch value {
		case string(capabilitymapping.CapabilityCostLow):
			return 0
		case string(capabilitymapping.CapabilityCostMedium):
			return 1
		case string(capabilitymapping.CapabilityCostHigh):
			return 2
		}
	}
	if kind == 1 {
		switch value {
		case string(capabilitymapping.CapabilityLatencyImmediate):
			return 0
		case string(capabilitymapping.CapabilityLatencyShort):
			return 1
		case string(capabilitymapping.CapabilityLatencyExtended):
			return 2
		}
	}
	if kind == 2 {
		switch value {
		case string(capabilitymapping.CapabilitySensitivityLow):
			return 0
		case string(capabilitymapping.CapabilitySensitivityModerate):
			return 1
		case string(capabilitymapping.CapabilitySensitivityHigh):
			return 2
		}
	}
	return 3
}
