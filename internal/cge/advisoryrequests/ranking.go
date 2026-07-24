package advisoryrequests

import "sort"

func requestLess(a, b AdvisoryEvidenceRequest) bool {
	if a.UtilityPermille != b.UtilityPermille {
		return a.UtilityPermille > b.UtilityPermille
	}
	if a.DiscriminationPermille != b.DiscriminationPermille {
		return a.DiscriminationPermille > b.DiscriminationPermille
	}
	if a.CoverageGainPermille != b.CoverageGainPermille {
		return a.CoverageGainPermille > b.CoverageGainPermille
	}
	if a.RedundancyPermille != b.RedundancyPermille {
		return a.RedundancyPermille < b.RedundancyPermille
	}
	if a.SensitivityClass != b.SensitivityClass {
		return a.SensitivityClass < b.SensitivityClass
	}
	if a.CostClass != b.CostClass {
		return a.CostClass < b.CostClass
	}
	if a.LatencyClass != b.LatencyClass {
		return a.LatencyClass < b.LatencyClass
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Key != b.Key {
		return a.Key < b.Key
	}
	return a.ID < b.ID
}
func rankRequests(values []AdvisoryEvidenceRequest, policy Policy) (AdvisoryRequestID, int) {
	activeRequests := make([]*AdvisoryEvidenceRequest, 0, len(values))
	for i := range values {
		values[i].AdvisoryRank = 0
		if active(values[i].Status) {
			activeRequests = append(activeRequests, &values[i])
		}
	}
	sort.Slice(activeRequests, func(i, j int) bool { return requestLess(*activeRequests[i], *activeRequests[j]) })
	for i, r := range activeRequests {
		r.AdvisoryRank = i + 1
	}
	if len(activeRequests) == 0 {
		return "", 0
	}
	first := activeRequests[0]
	if len(activeRequests) == 1 {
		return first.ID, 1000
	}
	margin := first.UtilityPermille - activeRequests[1].UtilityPermille
	if margin < 0 {
		margin = 0
	}
	if margin < policy.MinPreferredMarginPermille {
		return "", margin
	}
	return first.ID, margin
}
