package advisoryrequests

type RegistrySnapshot struct {
	Revision          uint64
	Requests          []AdvisoryEvidenceRequest
	RequestIndex      map[AdvisoryRequestID]int
	KeyIndex          map[AdvisoryRequestKey][]AdvisoryRequestID
	EpisodeIndex      map[string][]AdvisoryRequestID
	PolicyFingerprint string
	Digest            string
}

func (s RegistrySnapshot) Clone() RegistrySnapshot {
	out := s
	out.Requests = cloneRequests(s.Requests)
	out.RequestIndex = cloneIntIndex(s.RequestIndex)
	out.KeyIndex = cloneIDIndex(s.KeyIndex)
	out.EpisodeIndex = cloneStringIndex(s.EpisodeIndex)
	return out
}

func cloneIntIndex(in map[AdvisoryRequestID]int) map[AdvisoryRequestID]int {
	out := make(map[AdvisoryRequestID]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneIDIndex(in map[AdvisoryRequestKey][]AdvisoryRequestID) map[AdvisoryRequestKey][]AdvisoryRequestID {
	out := make(map[AdvisoryRequestKey][]AdvisoryRequestID, len(in))
	for k, v := range in {
		out[k] = append([]AdvisoryRequestID(nil), v...)
	}
	return out
}

func cloneStringIndex(in map[string][]AdvisoryRequestID) map[string][]AdvisoryRequestID {
	out := make(map[string][]AdvisoryRequestID, len(in))
	for k, v := range in {
		out[k] = append([]AdvisoryRequestID(nil), v...)
	}
	return out
}

func buildSnapshot(revision uint64, policy Policy, requests []AdvisoryEvidenceRequest) RegistrySnapshot {
	requests = cloneRequests(requests)
	sortRequests(requests)
	s := RegistrySnapshot{Revision: revision, Requests: requests, RequestIndex: map[AdvisoryRequestID]int{}, KeyIndex: map[AdvisoryRequestKey][]AdvisoryRequestID{}, EpisodeIndex: map[string][]AdvisoryRequestID{}, PolicyFingerprint: policy.Fingerprint()}
	for i, r := range requests {
		s.RequestIndex[r.ID] = i
		s.KeyIndex[r.Key] = append(s.KeyIndex[r.Key], r.ID)
		s.EpisodeIndex[r.EpisodeID] = append(s.EpisodeIndex[r.EpisodeID], r.ID)
	}
	s.Digest = registryDigest(s)
	return s
}

func registryDigest(s RegistrySnapshot) string {
	requests := cloneRequests(s.Requests)
	sortRequests(requests)
	return digestJSON("advisory-request-registry-v1:", struct {
		PolicyFingerprint string
		Requests          []AdvisoryEvidenceRequest
	}{s.PolicyFingerprint, requests})
}
