package advisoryrequests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"synora/internal/cge/evidencediscrimination"
)

func digestJSON(prefix string, value any) string {
	payload, _ := json.Marshal(value)
	d := sha256.Sum256(payload)
	return prefix + hex.EncodeToString(d[:])
}

func requestKeyFor(r AdvisoryEvidenceRequest) AdvisoryRequestKey {
	return requestKeyFromParts(r.EpisodeID, string(r.Kind), string(r.Dimension), r.RequiredFactCodes, r.HypothesisPairs)
}
func requestKeyFromParts(episodeID, kind, dimension string, codes []string, pairs []AdvisoryHypothesisPair) AdvisoryRequestKey {
	codes = uniqueStrings(codes)
	pairs = uniquePairs(pairs)
	payload, _ := json.Marshal(struct {
		Episode, Kind, Dimension string
		Codes                    []string
		Pairs                    []AdvisoryHypothesisPair
	}{episodeID, kind, dimension, codes, pairs})
	d := sha256.Sum256(payload)
	return AdvisoryRequestKey("advisory-request-key-" + hex.EncodeToString(d[:]))
}

func RequestKeyForCandidate(assessmentEpisode string, c evidencediscrimination.EvidenceCandidate) AdvisoryRequestKey {
	codes := make([]string, len(c.RequiredFactCodes))
	for i, code := range c.RequiredFactCodes {
		codes[i] = string(code)
	}
	return requestKeyFromParts(assessmentEpisode, string(c.Kind), string(c.Dimension), codes, candidatePairs(c.Discriminates))
}
func requestIDFor(key AdvisoryRequestKey, generation uint64) AdvisoryRequestID {
	payload, _ := json.Marshal(struct {
		Key        string
		Generation uint64
	}{string(key), generation})
	d := sha256.Sum256(payload)
	return AdvisoryRequestID("advisory-request-" + hex.EncodeToString(d[:]))
}
func requestFingerprint(r AdvisoryEvidenceRequest) string {
	copy := r
	copy.Fingerprint = ""
	copy.RequiredFactCodes = uniqueStrings(copy.RequiredFactCodes)
	copy.HypothesisPairs = uniquePairs(copy.HypothesisPairs)
	copy.ReasonCodes = uniqueStrings(copy.ReasonCodes)
	payload, _ := json.Marshal(copy)
	d := sha256.Sum256(payload)
	return "advisory-request-v1:" + hex.EncodeToString(d[:])
}

func requestLogicalFingerprint(r AdvisoryEvidenceRequest) string {
	copy := r
	copy.Revision = 0
	copy.Fingerprint = ""
	return requestFingerprint(copy)
}
func planFingerprint(p AdvisoryPlan) string {
	copy := p
	copy.Fingerprint = ""
	copy.Creates = cloneRequests(p.Creates)
	copy.ResultingRequests = cloneRequests(p.ResultingRequests)
	sortRequests(copy.Creates)
	sortRequests(copy.ResultingRequests)
	sort.Slice(copy.Updates, func(i, j int) bool { return copy.Updates[i].After.ID < copy.Updates[j].After.ID })
	sort.Slice(copy.Transitions, func(i, j int) bool { return copy.Transitions[i].RequestID < copy.Transitions[j].RequestID })
	payload, _ := json.Marshal(copy)
	d := sha256.Sum256(payload)
	return "advisory-plan-v1:" + hex.EncodeToString(d[:])
}
func requestSetDigest(values []AdvisoryEvidenceRequest) string {
	copy := cloneRequests(values)
	sortRequests(copy)
	payload, _ := json.Marshal(copy)
	d := sha256.Sum256(payload)
	return "advisory-request-registry-v1:" + hex.EncodeToString(d[:])
}
func AdvisoryRequestFingerprint(r AdvisoryEvidenceRequest) string        { return requestFingerprint(r) }
func AdvisoryRequestKeyFor(r AdvisoryEvidenceRequest) AdvisoryRequestKey { return requestKeyFor(r) }
func AdvisoryRequestIDFor(key AdvisoryRequestKey, generation uint64) AdvisoryRequestID {
	return requestIDFor(key, generation)
}
func AdvisoryPlanFingerprint(p AdvisoryPlan) string { return planFingerprint(p) }
func RegistryDigest(s RegistrySnapshot) string      { return registryDigest(s) }

func uniqueStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	result := out[:0]
	for _, v := range out {
		if v != "" && (len(result) == 0 || result[len(result)-1] != v) {
			result = append(result, v)
		}
	}
	return result
}
func uniquePairs(values []AdvisoryHypothesisPair) []AdvisoryHypothesisPair {
	out := make([]AdvisoryHypothesisPair, 0, len(values))
	for _, p := range values {
		pair, ok := canonicalPair(p.FirstID, p.SecondID)
		if ok {
			out = append(out, pair)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FirstID != out[j].FirstID {
			return out[i].FirstID < out[j].FirstID
		}
		return out[i].SecondID < out[j].SecondID
	})
	result := out[:0]
	for _, v := range out {
		if len(result) == 0 || result[len(result)-1] != v {
			result = append(result, v)
		}
	}
	return result
}

func candidatePairs(values []evidencediscrimination.HypothesisPair) []AdvisoryHypothesisPair {
	out := make([]AdvisoryHypothesisPair, 0, len(values))
	for _, p := range values {
		if pair, ok := canonicalPair(string(p.First), string(p.Second)); ok {
			out = append(out, pair)
		}
	}
	return uniquePairs(out)
}
