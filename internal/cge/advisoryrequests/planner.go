package advisoryrequests

import (
	"sort"
	"time"

	"synora/internal/cge/evidencediscrimination"
)

type PlanInput struct {
	Assessment       evidencediscrimination.DiscriminationAssessment
	RegistrySnapshot RegistrySnapshot
	EvaluatedAt      time.Time
}

// Plan derives a complete desired state for the assessment episode. It is
// deliberately clock-free: EvaluatedAt is the only temporal input.
func Plan(input PlanInput, policy Policy) (AdvisoryPlan, error) {
	if err := policy.Validate(); err != nil {
		return AdvisoryPlan{}, err
	}
	if err := validateAssessment(input.Assessment, policy); err != nil {
		return AdvisoryPlan{}, err
	}
	if input.EvaluatedAt.IsZero() || input.EvaluatedAt.Location() != time.UTC {
		return AdvisoryPlan{}, ErrInvalidPlan
	}
	if err := validateSnapshot(input.RegistrySnapshot, policy); err != nil {
		return AdvisoryPlan{}, err
	}

	before := requestsForEpisode(input.RegistrySnapshot.Requests, input.Assessment.EpisodeID)
	desired := cloneRequests(before)
	latest := latestByKey(desired)

	// A malformed assessment may contain two candidates for one semantic need.
	// Keep the deterministically strongest one; the key remains the identity.
	candidatesByKey := make(map[AdvisoryRequestKey][]evidencediscrimination.EvidenceCandidate)
	for _, candidate := range input.Assessment.Candidates {
		key := RequestKeyForCandidate(input.Assessment.EpisodeID, candidate)
		candidatesByKey[key] = append(candidatesByKey[key], candidate)
	}
	keys := make([]AdvisoryRequestKey, 0, len(candidatesByKey))
	for key := range candidatesByKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	ordered := make([]evidencediscrimination.EvidenceCandidate, 0, len(keys))
	for _, key := range keys {
		group := candidatesByKey[key]
		sort.Slice(group, func(i, j int) bool { return candidateLess(group[i], group[j]) })
		ordered = append(ordered, group[0])
	}
	sort.Slice(ordered, func(i, j int) bool { return candidateLess(ordered[i], ordered[j]) })

	seen := make(map[AdvisoryRequestKey]struct{}, len(ordered))
	activeCount := countActive(desired)
	for _, candidate := range ordered {
		key := RequestKeyForCandidate(input.Assessment.EpisodeID, candidate)
		seen[key] = struct{}{}
		idx, exists := latest[key]
		if exists && terminal(desired[idx].Status) {
			exists = false
		}
		usable, suppressionReason := candidateDecision(candidate, policy)
		if exists {
			updateExisting(&desired[idx], candidate, input.Assessment, input.EvaluatedAt, policy, usable, suppressionReason, &activeCount)
			continue
		}
		if !usable && !policy.PreserveSuppressedRequests {
			continue
		}
		if len(desired) >= policy.MaxStoredRequestsPerEpisode {
			return AdvisoryPlan{}, ErrRequestLimitReached
		}
		generation := highestGeneration(desired, key) + 1
		status := StatusProposed
		if !usable || activeCount >= policy.MaxActiveRequestsPerEpisode {
			if !policy.PreserveSuppressedRequests {
				continue
			}
			status = StatusSuppressed
		}
		request := requestFromCandidate(candidate, input.Assessment.EpisodeID, input.Assessment.Fingerprint, key, generation, status, input.EvaluatedAt, policy)
		if suppressionReason != "" {
			request.ReasonCodes = uniqueStrings(append(request.ReasonCodes, suppressionReason))
			request.Fingerprint = requestFingerprint(request)
		}
		desired = append(desired, request)
		latest[key] = len(desired) - 1
		if active(status) {
			activeCount++
		}
	}

	// A missing source candidate can satisfy a request only when the new
	// assessment says that the descriptive ambiguity is gone. Otherwise its
	// old occurrence remains traceable and can expire by TTL.
	for i := range desired {
		r := &desired[i]
		if terminal(r.Status) {
			continue
		}
		if _, present := seen[r.Key]; !present {
			if !input.Assessment.EvidenceUseful || !input.Assessment.AmbiguityRelevant {
				setStatus(r, StatusSatisfied, "ambiguity_no_longer_relevant", input.EvaluatedAt)
				continue
			}
			if deferredBefore(r, input.EvaluatedAt) {
				continue
			}
			if !input.EvaluatedAt.Before(r.ExpiresAt) {
				setStatus(r, StatusExpired, "request_ttl_elapsed", input.EvaluatedAt)
			}
		}
	}

	result := cloneRequests(desired)
	if len(result) > policy.MaxStoredRequestsPerEpisode {
		return AdvisoryPlan{}, ErrRequestLimitReached
	}
	rankRequests(result, policy)
	finalizeRequestFingerprints(result, before)
	sortRequests(result)
	plan := AdvisoryPlan{
		EpisodeID:                   input.Assessment.EpisodeID,
		SourceAssessmentFingerprint: input.Assessment.Fingerprint,
		SourceRegistryRevision:      input.RegistrySnapshot.Revision,
		PolicyFingerprint:           policy.Fingerprint(),
		ResultingRequests:           result,
		ReasonCodes:                 []string{"advisory_request_plan"},
	}
	for _, r := range result {
		if r.AdvisoryRank == 1 && active(r.Status) {
			plan.PreferredRequestID = r.ID
		}
	}
	if plan.PreferredRequestID != "" {
		plan.PreferredMarginPermille = preferredMargin(result)
		if !preferredAllowed(result, policy) {
			plan.PreferredRequestID = ""
		}
	}

	for _, r := range result {
		old, ok := requestByID(before, r.ID)
		if !ok {
			plan.Creates = append(plan.Creates, r.Clone())
			continue
		}
		if old.Fingerprint != r.Fingerprint {
			plan.Updates = append(plan.Updates, AdvisoryRequestUpdate{Before: old.Clone(), After: r.Clone()})
		}
		if old.Status != r.Status {
			plan.Transitions = append(plan.Transitions, AdvisoryRequestTransition{RequestID: r.ID, Before: old.Status, After: r.Status, ReasonCode: transitionReason(old.Status, r.Status)})
		}
	}
	sort.Slice(plan.Creates, func(i, j int) bool { return plan.Creates[i].ID < plan.Creates[j].ID })
	sort.Slice(plan.Updates, func(i, j int) bool { return plan.Updates[i].After.ID < plan.Updates[j].After.ID })
	sort.Slice(plan.Transitions, func(i, j int) bool { return plan.Transitions[i].RequestID < plan.Transitions[j].RequestID })
	plan.Fingerprint = planFingerprint(plan)
	return plan, nil
}

func validateAssessment(a evidencediscrimination.DiscriminationAssessment, policy Policy) error {
	if a.EpisodeID == "" || a.Fingerprint == "" {
		return ErrInvalidAssessment
	}
	if a.SourceFactSetFingerprint == "" || a.SourceHypothesisSetFingerprint == "" {
		return ErrMissingSourceFingerprint
	}
	if evidencediscrimination.AssessmentFingerprint(a) != a.Fingerprint {
		return ErrFingerprintMismatch
	}
	if a.Revision == 0 {
		return ErrInvalidAssessment
	}
	seen := map[evidencediscrimination.EvidenceCandidateID]struct{}{}
	for _, c := range a.Candidates {
		if c.ID == "" || c.EpisodeID != a.EpisodeID {
			return ErrUnknownCandidateReference
		}
		if _, exists := seen[c.ID]; exists {
			return ErrUnknownCandidateReference
		}
		seen[c.ID] = struct{}{}
		if c.SourceFactSetFingerprint == "" || c.SourceHypothesisSetFingerprint == "" {
			return ErrMissingSourceFingerprint
		}
		if c.SourceFactSetFingerprint != a.SourceFactSetFingerprint || c.SourceHypothesisSetFingerprint != a.SourceHypothesisSetFingerprint {
			return ErrStaleAssessment
		}
		if c.Fingerprint == "" || evidencediscrimination.CandidateFingerprint(c) != c.Fingerprint {
			return ErrFingerprintMismatch
		}
		if c.Kind == "" || c.Dimension == "" || forbiddenTerm(string(c.Kind)) || forbiddenTerm(string(c.Dimension)) {
			return ErrInvalidAssessment
		}
		if len(c.RequiredFactCodes) > policy.MaxFactCodes || len(c.Discriminates) > policy.MaxHypothesisPairs || len(c.ReasonCodes) > policy.MaxReasonCodes {
			return ErrInvalidAssessment
		}
		for _, code := range c.RequiredFactCodes {
			if code == "" || forbiddenTerm(string(code)) {
				return ErrInvalidAssessment
			}
		}
		for _, reason := range c.ReasonCodes {
			if reason == "" || forbiddenTerm(reason) {
				return ErrInvalidAssessment
			}
		}
		for _, pair := range c.Discriminates {
			if pair.First == "" || pair.Second == "" || pair.First >= pair.Second {
				return ErrInvalidAssessment
			}
		}
		for _, score := range []int{c.DiscriminationPermille, c.CoverageGainPermille, c.RedundancyPermille, c.UtilityPermille} {
			if score < 0 || score > 1000 {
				return ErrInvalidAssessment
			}
		}
	}
	return nil
}

func validateSnapshot(s RegistrySnapshot, policy Policy) error {
	if s.PolicyFingerprint != "" && s.PolicyFingerprint != policy.Fingerprint() {
		return ErrFingerprintMismatch
	}
	if s.Digest != "" && s.Digest != registryDigest(s) {
		return ErrFingerprintMismatch
	}
	seen := map[AdvisoryRequestID]struct{}{}
	expectedRequestIndex := map[AdvisoryRequestID]int{}
	expectedKeyIndex := map[AdvisoryRequestKey][]AdvisoryRequestID{}
	expectedEpisodeIndex := map[string][]AdvisoryRequestID{}
	for _, r := range s.Requests {
		if _, ok := seen[r.ID]; ok {
			return ErrRequestIDCollision
		}
		seen[r.ID] = struct{}{}
		if err := validateRequest(r, policy); err != nil {
			return err
		}
	}
	for i, r := range s.Requests {
		expectedRequestIndex[r.ID] = i
		expectedKeyIndex[r.Key] = append(expectedKeyIndex[r.Key], r.ID)
		expectedEpisodeIndex[r.EpisodeID] = append(expectedEpisodeIndex[r.EpisodeID], r.ID)
	}
	if len(s.RequestIndex) > 0 && !equalIntIndex(s.RequestIndex, expectedRequestIndex) || len(s.KeyIndex) > 0 && !equalIDIndex(s.KeyIndex, expectedKeyIndex) || len(s.EpisodeIndex) > 0 && !equalStringIndex(s.EpisodeIndex, expectedEpisodeIndex) {
		return ErrFingerprintMismatch
	}
	return nil
}

func equalIntIndex(a, b map[AdvisoryRequestID]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func equalIDIndex(a, b map[AdvisoryRequestKey][]AdvisoryRequestID) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !equalIDs(v, b[k]) {
			return false
		}
	}
	return true
}

func equalStringIndex(a, b map[string][]AdvisoryRequestID) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !equalIDs(v, b[k]) {
			return false
		}
	}
	return true
}

func equalIDs(a, b []AdvisoryRequestID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func candidateDecision(c evidencediscrimination.EvidenceCandidate, p Policy) (bool, string) {
	if c.SensitivityClass == evidencediscrimination.SensitivityHigh && !p.IncludeHighSensitivityRequests {
		return false, "high_sensitivity_excluded"
	}
	if c.RedundancyPermille >= p.SuppressRedundancyPermille {
		return false, "excessive_redundancy"
	}
	if c.UtilityPermille < p.MinUtilityPermille {
		return false, "utility_below_threshold"
	}
	if c.DiscriminationPermille < p.MinDiscriminationPermille && c.CoverageGainPermille < p.MinCoverageGainPermille {
		return false, "discrimination_and_coverage_below_threshold"
	}
	if len(c.Discriminates) == 0 && c.CoverageGainPermille < p.MinCoverageGainPermille {
		return false, "no_discriminating_pair_or_coverage_gain"
	}
	return true, ""
}

func updateExisting(r *AdvisoryEvidenceRequest, c evidencediscrimination.EvidenceCandidate, a evidencediscrimination.DiscriminationAssessment, at time.Time, p Policy, usable bool, reason string, activeCount *int) {
	before := r.Clone()
	beforeStatus := r.Status
	applyCandidate(r, c, a, at, p)
	if !usable {
		if active(r.Status) {
			(*activeCount)--
		}
		r.Status = StatusSuppressed
		if reason != "" {
			r.ReasonCodes = uniqueStrings(append(r.ReasonCodes, reason))
		}
	} else if r.Status == StatusSuppressed && *activeCount < p.MaxActiveRequestsPerEpisode {
		r.Status = StatusProposed
		(*activeCount)++
	} else if r.Status == StatusDeferred && r.DeferredUntil != nil && !at.Before(*r.DeferredUntil) {
		r.Status = StatusProposed
		r.DeferredUntil = nil
	}
	if r.Status != beforeStatus {
		r.StatusChangedAt = at
	}
	if requestLogicalFingerprint(*r) != requestLogicalFingerprint(before) {
		r.Revision = before.Revision + 1
	} else {
		r.Revision = before.Revision
		r.Fingerprint = before.Fingerprint
		return
	}
	r.Fingerprint = requestFingerprint(*r)
}

func requestFromCandidate(c evidencediscrimination.EvidenceCandidate, episode, assessment string, key AdvisoryRequestKey, generation uint64, status AdvisoryRequestStatus, at time.Time, p Policy) AdvisoryEvidenceRequest {
	r := AdvisoryEvidenceRequest{ID: requestIDFor(key, generation), Key: key, Generation: generation, EpisodeID: episode, Status: status, CandidateID: c.ID, Kind: c.Kind, Dimension: c.Dimension, DiscriminationPermille: clamp(c.DiscriminationPermille), CoverageGainPermille: clamp(c.CoverageGainPermille), RedundancyPermille: clamp(c.RedundancyPermille), UtilityPermille: clamp(c.UtilityPermille), CostClass: string(c.CostClass), LatencyClass: string(c.LatencyClass), SensitivityClass: string(c.SensitivityClass), FirstProposedAt: at, LastEvaluatedAt: at, StatusChangedAt: at, ExpiresAt: at.Add(p.DefaultTTL), SourceAssessmentFingerprint: assessment, SourceCandidateFingerprint: c.Fingerprint, Revision: 1, Flags: AdvisoryRequestFlags{true, true, true, true, true}}
	r.RequiredFactCodes = candidateFactCodes(c)
	r.HypothesisPairs = candidatePairs(c.Discriminates)
	r.ReasonCodes = uniqueStrings(c.ReasonCodes)
	r.Fingerprint = requestFingerprint(r)
	return r
}

func applyCandidate(r *AdvisoryEvidenceRequest, c evidencediscrimination.EvidenceCandidate, a evidencediscrimination.DiscriminationAssessment, at time.Time, p Policy) {
	r.CandidateID = c.ID
	r.Kind = c.Kind
	r.Dimension = c.Dimension
	r.RequiredFactCodes = candidateFactCodes(c)
	r.HypothesisPairs = candidatePairs(c.Discriminates)
	r.DiscriminationPermille, r.CoverageGainPermille, r.RedundancyPermille, r.UtilityPermille = clamp(c.DiscriminationPermille), clamp(c.CoverageGainPermille), clamp(c.RedundancyPermille), clamp(c.UtilityPermille)
	r.CostClass, r.LatencyClass, r.SensitivityClass = string(c.CostClass), string(c.LatencyClass), string(c.SensitivityClass)
	r.ReasonCodes = uniqueStrings(c.ReasonCodes)
	r.LastEvaluatedAt = at
	r.ExpiresAt = at.Add(p.DefaultTTL)
	r.SourceAssessmentFingerprint = a.Fingerprint
	r.SourceCandidateFingerprint = c.Fingerprint
	r.Flags = AdvisoryRequestFlags{true, true, true, true, true}
}

func candidateFactCodes(c evidencediscrimination.EvidenceCandidate) []string {
	codes := make([]string, len(c.RequiredFactCodes))
	for i, code := range c.RequiredFactCodes {
		codes[i] = string(code)
	}
	return uniqueStrings(codes)
}

func latestByKey(values []AdvisoryEvidenceRequest) map[AdvisoryRequestKey]int {
	out := map[AdvisoryRequestKey]int{}
	for i := range values {
		if old, ok := out[values[i].Key]; !ok || values[i].Generation > values[old].Generation {
			out[values[i].Key] = i
		}
	}
	return out
}

func highestGeneration(values []AdvisoryEvidenceRequest, key AdvisoryRequestKey) uint64 {
	var highest uint64
	for _, r := range values {
		if r.Key == key && r.Generation > highest {
			highest = r.Generation
		}
	}
	return highest
}

func requestByID(values []AdvisoryEvidenceRequest, id AdvisoryRequestID) (AdvisoryEvidenceRequest, bool) {
	for _, r := range values {
		if r.ID == id {
			return r.Clone(), true
		}
	}
	return AdvisoryEvidenceRequest{}, false
}

func setStatus(r *AdvisoryEvidenceRequest, status AdvisoryRequestStatus, reason string, at time.Time) {
	if r.Status == status {
		return
	}
	r.Status = status
	r.StatusChangedAt = at
	r.LastEvaluatedAt = at
	r.DeferredUntil = nil
	r.ReasonCodes = uniqueStrings(append(r.ReasonCodes, reason))
	r.Revision++
	r.Fingerprint = requestFingerprint(*r)
}

func deferredBefore(r *AdvisoryEvidenceRequest, at time.Time) bool {
	return r.Status == StatusDeferred && r.DeferredUntil != nil && at.Before(*r.DeferredUntil)
}

func requestsForEpisode(values []AdvisoryEvidenceRequest, episode string) []AdvisoryEvidenceRequest {
	out := make([]AdvisoryEvidenceRequest, 0)
	for _, r := range values {
		if r.EpisodeID == episode {
			out = append(out, r.Clone())
		}
	}
	sortRequests(out)
	return out
}

func countActive(values []AdvisoryEvidenceRequest) int {
	count := 0
	for _, r := range values {
		if active(r.Status) {
			count++
		}
	}
	return count
}

func finalizeRequestFingerprints(values, before []AdvisoryEvidenceRequest) {
	oldByID := make(map[AdvisoryRequestID]AdvisoryEvidenceRequest, len(before))
	for _, old := range before {
		oldByID[old.ID] = old
	}
	for i := range values {
		old, existed := oldByID[values[i].ID]
		if existed {
			if requestLogicalFingerprint(values[i]) != requestLogicalFingerprint(old) {
				if values[i].Revision <= old.Revision {
					values[i].Revision = old.Revision + 1
				}
			} else {
				values[i].Revision = old.Revision
			}
		}
		values[i].Fingerprint = requestFingerprint(values[i])
	}
}

func candidateLess(a, b evidencediscrimination.EvidenceCandidate) bool {
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
	return a.ID < b.ID
}

func transitionReason(before, after AdvisoryRequestStatus) string {
	if after == StatusSatisfied {
		return "ambiguity_no_longer_relevant"
	}
	if after == StatusExpired {
		return "request_ttl_elapsed"
	}
	if after == StatusSuppressed {
		return "request_suppressed"
	}
	if before == StatusDeferred && after == StatusProposed {
		return "defer_recheck_due"
	}
	return "request_re_evaluated"
}

func preferredMargin(values []AdvisoryEvidenceRequest) int {
	activeValues := make([]AdvisoryEvidenceRequest, 0)
	for _, r := range values {
		if active(r.Status) {
			activeValues = append(activeValues, r)
		}
	}
	sort.Slice(activeValues, func(i, j int) bool { return requestLess(activeValues[i], activeValues[j]) })
	if len(activeValues) < 2 {
		return 1000
	}
	return max(0, activeValues[0].UtilityPermille-activeValues[1].UtilityPermille)
}

func preferredAllowed(values []AdvisoryEvidenceRequest, p Policy) bool {
	activeValues := make([]AdvisoryEvidenceRequest, 0)
	for _, r := range values {
		if active(r.Status) {
			activeValues = append(activeValues, r)
		}
	}
	if len(activeValues) < 2 {
		return true
	}
	sort.Slice(activeValues, func(i, j int) bool { return requestLess(activeValues[i], activeValues[j]) })
	if sameRankingTuple(activeValues[0], activeValues[1]) {
		return false
	}
	return activeValues[0].UtilityPermille-activeValues[1].UtilityPermille >= p.MinPreferredMarginPermille
}

func sameRankingTuple(a, b AdvisoryEvidenceRequest) bool {
	return a.UtilityPermille == b.UtilityPermille && a.DiscriminationPermille == b.DiscriminationPermille && a.CoverageGainPermille == b.CoverageGainPermille && a.RedundancyPermille == b.RedundancyPermille && a.SensitivityClass == b.SensitivityClass && a.CostClass == b.CostClass && a.LatencyClass == b.LatencyClass
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
