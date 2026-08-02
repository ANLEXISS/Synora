package advisoryrequests

import "time"

func validateRequest(r AdvisoryEvidenceRequest, p Policy) error {
	if r.ID == "" || len(string(r.ID)) <= len("advisory-request-") || string(r.ID)[:len("advisory-request-")] != "advisory-request-" {
		return ErrInvalidRequestID
	}
	if r.Key == "" || len(string(r.Key)) <= len("advisory-request-key-") || string(r.Key)[:len("advisory-request-key-")] != "advisory-request-key-" {
		return ErrInvalidRequestKey
	}
	if r.EpisodeID == "" || r.CandidateID == "" || r.Generation == 0 || r.Kind == "" || r.Dimension == "" {
		return ErrInvalidRequest
	}
	if r.Key != requestKeyFor(r) || r.ID != requestIDFor(r.Key, r.Generation) {
		return ErrInvalidRequest
	}
	if !validStatus(r.Status) {
		return ErrInvalidStatus
	}
	if !r.Flags.NotACommand || !r.Flags.NotAProbability || !r.Flags.NoSecurityMeaning || !r.Flags.RequiresExternalMapping || !r.Flags.RequiresExternalAuthorization {
		return ErrInvalidRequest
	}
	if forbiddenTerm(string(r.Kind)) || forbiddenTerm(string(r.Dimension)) || forbiddenTerm(r.CostClass) || forbiddenTerm(r.LatencyClass) || forbiddenTerm(r.SensitivityClass) {
		return ErrInvalidRequest
	}
	if r.SourceAssessmentFingerprint == "" || r.SourceCandidateFingerprint == "" {
		return ErrMissingSourceFingerprint
	}
	for _, score := range []int{r.DiscriminationPermille, r.CoverageGainPermille, r.RedundancyPermille, r.UtilityPermille} {
		if score < 0 || score > 1000 {
			return ErrInvalidRequest
		}
	}
	if len(r.RequiredFactCodes) > p.MaxFactCodes {
		return ErrFactCodeLimitReached
	}
	if len(r.HypothesisPairs) > p.MaxHypothesisPairs {
		return ErrHypothesisPairLimitReached
	}
	if len(r.ReasonCodes) > p.MaxReasonCodes {
		return ErrReasonLimitReached
	}
	if !isSortedUnique(r.RequiredFactCodes) || !isSortedUnique(r.ReasonCodes) {
		return ErrInvalidRequest
	}
	for _, code := range r.RequiredFactCodes {
		if code == "" || forbiddenTerm(code) {
			return ErrInvalidRequest
		}
	}
	for _, reason := range r.ReasonCodes {
		if reason == "" || forbiddenTerm(reason) {
			return ErrInvalidRequest
		}
	}
	if !canonicalPairs(r.HypothesisPairs) {
		return ErrInvalidRequest
	}
	if len(r.HypothesisPairs) == 0 && len(r.RequiredFactCodes) == 0 && r.CoverageGainPermille == 0 {
		return ErrInvalidRequest
	}
	if r.FirstProposedAt.IsZero() || r.LastEvaluatedAt.IsZero() || r.StatusChangedAt.IsZero() || r.ExpiresAt.IsZero() || r.FirstProposedAt.Location() != time.UTC || r.LastEvaluatedAt.Location() != time.UTC || r.StatusChangedAt.Location() != time.UTC || r.ExpiresAt.Location() != time.UTC || !r.ExpiresAt.After(r.FirstProposedAt) {
		return ErrInvalidRequest
	}
	if r.Status == StatusDeferred {
		if r.DeferredUntil == nil || r.DeferredUntil.IsZero() || r.DeferredUntil.Location() != time.UTC || !r.DeferredUntil.After(r.LastEvaluatedAt) {
			return ErrDeferUntilInvalid
		}
	} else if r.DeferredUntil != nil {
		return ErrInvalidRequest
	}
	if terminal(r.Status) && r.DeferredUntil != nil {
		return ErrRequestTerminal
	}
	if r.Revision == 0 || r.Fingerprint == "" || requestFingerprint(r) != r.Fingerprint {
		return ErrFingerprintMismatch
	}
	return nil
}

func isSortedUnique(values []string) bool {
	for i, value := range values {
		if value == "" || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}

func canonicalPairs(values []AdvisoryHypothesisPair) bool {
	for i, value := range values {
		if value.FirstID == "" || value.SecondID == "" || value.FirstID >= value.SecondID {
			return false
		}
		if i > 0 && (values[i-1].FirstID > value.FirstID || values[i-1].FirstID == value.FirstID && values[i-1].SecondID >= value.SecondID) {
			return false
		}
	}
	return true
}

func allFlags(r AdvisoryEvidenceRequest) bool {
	return r.Flags.NotACommand && r.Flags.NotAProbability && r.Flags.NoSecurityMeaning && r.Flags.RequiresExternalMapping && r.Flags.RequiresExternalAuthorization
}
