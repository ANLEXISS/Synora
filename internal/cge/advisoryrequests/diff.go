package advisoryrequests

import "sort"

func Diff(before, after RegistrySnapshot) (AdvisoryRequestDiff, error) {
	if before.Digest != "" && before.Digest != registryDigest(before) || after.Digest != "" && after.Digest != registryDigest(after) {
		return AdvisoryRequestDiff{}, ErrFingerprintMismatch
	}
	left, right := map[AdvisoryRequestID]AdvisoryEvidenceRequest{}, map[AdvisoryRequestID]AdvisoryEvidenceRequest{}
	for _, r := range before.Requests {
		left[r.ID] = r
	}
	for _, r := range after.Requests {
		right[r.ID] = r
	}
	d := AdvisoryRequestDiff{BeforeFingerprint: before.Digest, AfterFingerprint: after.Digest}
	episodes := map[string]struct{}{}
	for id, r := range right {
		if old, ok := left[id]; !ok {
			d.Added = append(d.Added, r.Clone())
			episodes[r.EpisodeID] = struct{}{}
		} else if old.Fingerprint != r.Fingerprint {
			d.Updated = append(d.Updated, AdvisoryRequestUpdate{Before: old.Clone(), After: r.Clone()})
			episodes[r.EpisodeID] = struct{}{}
			if old.Status != r.Status {
				d.Transitioned = append(d.Transitioned, AdvisoryRequestTransition{RequestID: id, Before: old.Status, After: r.Status, ReasonCode: transitionReason(old.Status, r.Status)})
			}
		}
	}
	for id, r := range left {
		if _, ok := right[id]; !ok {
			d.Removed = append(d.Removed, id)
			episodes[r.EpisodeID] = struct{}{}
		}
	}
	if len(episodes) == 1 {
		for episode := range episodes {
			d.EpisodeID = episode
		}
	}
	sort.Slice(d.Added, func(i, j int) bool { return d.Added[i].ID < d.Added[j].ID })
	sort.Slice(d.Updated, func(i, j int) bool { return d.Updated[i].After.ID < d.Updated[j].After.ID })
	sort.Slice(d.Transitioned, func(i, j int) bool { return d.Transitioned[i].RequestID < d.Transitioned[j].RequestID })
	sort.Slice(d.Removed, func(i, j int) bool { return d.Removed[i] < d.Removed[j] })
	return d, nil
}
