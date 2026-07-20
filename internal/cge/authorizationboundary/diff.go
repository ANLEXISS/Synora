package authorizationboundary

import "sort"

type AuthorizationBoundaryDiff struct {
	RequestID string

	Added       []AuthorizationBoundaryAssessment
	Updated     []AuthorizationAssessmentUpdate
	Invalidated []AuthorizationAssessmentInvalidation
	Removed     []string

	BeforeFingerprint string
	AfterFingerprint  string
}

func Diff(before, after RegistrySnapshot) (AuthorizationBoundaryDiff, error) {
	if before.Digest != "" && before.Digest != registryDigest(before) || after.Digest != "" && after.Digest != registryDigest(after) {
		return AuthorizationBoundaryDiff{}, ErrFingerprintMismatch
	}
	result := AuthorizationBoundaryDiff{BeforeFingerprint: before.Digest, AfterFingerprint: after.Digest}
	beforeByID := map[string]AuthorizationBoundaryAssessment{}
	afterByID := map[string]AuthorizationBoundaryAssessment{}
	for _, assessment := range before.Assessments {
		beforeByID[assessment.RequestID] = assessment
	}
	for _, assessment := range after.Assessments {
		afterByID[assessment.RequestID] = assessment
	}
	ids := make([]string, 0, len(beforeByID)+len(afterByID))
	seen := map[string]struct{}{}
	for id := range beforeByID {
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for id := range afterByID {
		if _, ok := seen[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		oldValue, oldOK := beforeByID[id]
		newValue, newOK := afterByID[id]
		switch {
		case !oldOK:
			result.Added = append(result.Added, newValue.Clone())
		case !newOK:
			result.Removed = append(result.Removed, id)
		case oldValue.Fingerprint != newValue.Fingerprint:
			result.Updated = append(result.Updated, AuthorizationAssessmentUpdate{Before: oldValue.Clone(), After: newValue.Clone()})
		}
	}
	return result, nil
}
