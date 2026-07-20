package capabilitymapping

import (
	"sort"

	"synora/internal/cge/advisoryrequests"
)

func Diff(before, after RegistrySnapshot) (CapabilityMappingDiff, error) {
	if before.Digest != "" && before.Digest != registryDigest(before) || after.Digest != "" && after.Digest != registryDigest(after) {
		return CapabilityMappingDiff{}, ErrFingerprintMismatch
	}
	left, right := map[advisoryrequests.AdvisoryRequestID]CapabilityMappingAssessment{}, map[advisoryrequests.AdvisoryRequestID]CapabilityMappingAssessment{}
	for _, value := range before.Assessments {
		left[value.RequestID] = value
	}
	for _, value := range after.Assessments {
		right[value.RequestID] = value
	}
	ids := make([]advisoryrequests.AdvisoryRequestID, 0, len(left)+len(right))
	for id := range left {
		ids = append(ids, id)
	}
	for id := range right {
		if _, ok := left[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	diff := CapabilityMappingDiff{BeforeFingerprint: before.Digest, AfterFingerprint: after.Digest}
	for _, id := range ids {
		l, lok := left[id]
		r, rok := right[id]
		if !lok && rok {
			diff.Added = append(diff.Added, r.Clone())
			continue
		}
		if lok && !rok {
			diff.Removed = append(diff.Removed, id)
			continue
		}
		if l.Fingerprint != r.Fingerprint {
			diff.Updated = append(diff.Updated, CapabilityMappingUpdate{Before: l.Clone(), After: r.Clone()})
		}
	}
	if len(diff.Added) == 1 && len(diff.Updated) == 0 && len(diff.Removed) == 0 {
		diff.RequestID = diff.Added[0].RequestID
		diff.After = assessmentPointer(diff.Added[0])
	}
	if len(diff.Updated) == 1 && len(diff.Added) == 0 && len(diff.Removed) == 0 {
		diff.RequestID = diff.Updated[0].After.RequestID
		diff.Before = assessmentPointer(diff.Updated[0].Before)
		diff.After = assessmentPointer(diff.Updated[0].After)
	}
	if len(diff.Removed) == 1 && len(diff.Added) == 0 && len(diff.Updated) == 0 {
		diff.RequestID = diff.Removed[0]
	}
	return diff, nil
}

func assessmentPointer(value CapabilityMappingAssessment) *CapabilityMappingAssessment {
	copy := value.Clone()
	return &copy
}
