package evidencediscrimination

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

func candidateIDFor(episodeID string, def EvidenceCandidateDefinition, pairs []HypothesisPair) EvidenceCandidateID {
	codes := append([]string(nil), make([]string, len(def.RequiredFactCodes))...)
	for i, v := range def.RequiredFactCodes {
		codes[i] = string(v)
	}
	sort.Strings(codes)
	payload, _ := json.Marshal(struct {
		Version, Episode, Kind, Dimension string
		Codes                             []string
		Pairs                             []HypothesisPair
	}{"v1", episodeID, string(def.Kind), string(def.Dimension), codes, pairs})
	d := sha256.Sum256(payload)
	return EvidenceCandidateID("evidence-candidate-" + hex.EncodeToString(d[:]))
}

func outcomeIDFor(candidate EvidenceCandidateID, o PotentialOutcome) string {
	value := ""
	if o.Value != nil {
		value = o.Value.Canonical()
	}
	payload, _ := json.Marshal(struct {
		Candidate string
		Code      string
		Operator  string
		Value     string
	}{string(candidate), string(o.FactCode), string(o.Operator), value})
	d := sha256.Sum256(payload)
	return "evidence-outcome-" + hex.EncodeToString(d[:])
}

func outcomeFingerprint(o PotentialOutcome) string {
	copy := o
	copy.Fingerprint = ""
	payload, _ := json.Marshal(copy)
	d := sha256.Sum256(payload)
	return "evidence-outcome-v1:" + hex.EncodeToString(d[:])
}
func candidateFingerprint(c EvidenceCandidate) string {
	copy := c
	copy.Fingerprint = ""
	copy.Outcomes = cloneOutcomes(c.Outcomes)
	sortCandidate(&copy)
	payload, _ := json.Marshal(copy)
	d := sha256.Sum256(payload)
	return "evidence-candidate-v1:" + hex.EncodeToString(d[:])
}
func assessmentFingerprint(a DiscriminationAssessment) string {
	copy := a
	copy.Fingerprint = ""
	copy.Candidates = cloneCandidates(a.Candidates)
	sort.Slice(copy.Candidates, func(i, j int) bool { return copy.Candidates[i].ID < copy.Candidates[j].ID })
	payload, _ := json.Marshal(copy)
	d := sha256.Sum256(payload)
	return "evidence-discrimination-assessment-v1:" + hex.EncodeToString(d[:])
}
func registryDigest(s RegistrySnapshot) string {
	copy := s
	copy.Digest = ""
	copy.Assessments = cloneAssessments(s.Assessments)
	sort.Slice(copy.Assessments, func(i, j int) bool { return copy.Assessments[i].EpisodeID < copy.Assessments[j].EpisodeID })
	copy.EpisodeIndex = nil
	payload, _ := json.Marshal(copy)
	d := sha256.Sum256(payload)
	return "evidence-discrimination-registry-v1:" + hex.EncodeToString(d[:])
}

func CandidateFingerprint(c EvidenceCandidate) string         { return candidateFingerprint(c) }
func AssessmentFingerprint(a DiscriminationAssessment) string { return assessmentFingerprint(a) }
func RegistryDigest(s RegistrySnapshot) string                { return registryDigest(s) }

func cloneOutcomes(values []PotentialOutcome) []PotentialOutcome {
	out := make([]PotentialOutcome, len(values))
	for i, v := range values {
		out[i] = v.Clone()
	}
	return out
}
func cloneCandidates(values []EvidenceCandidate) []EvidenceCandidate {
	out := make([]EvidenceCandidate, len(values))
	for i, v := range values {
		out[i] = v.Clone()
	}
	return out
}
func cloneAssessments(values []DiscriminationAssessment) []DiscriminationAssessment {
	out := make([]DiscriminationAssessment, len(values))
	for i, v := range values {
		out[i] = v.Clone()
	}
	return out
}
func sortCandidate(c *EvidenceCandidate) {
	sort.Strings(c.ReasonCodes)
	sort.Slice(c.RequiredFactCodes, func(i, j int) bool { return c.RequiredFactCodes[i] < c.RequiredFactCodes[j] })
	sort.Slice(c.Outcomes, func(i, j int) bool { return c.Outcomes[i].ID < c.Outcomes[j].ID })
	sort.Slice(c.Discriminates, func(i, j int) bool {
		if c.Discriminates[i].First != c.Discriminates[j].First {
			return c.Discriminates[i].First < c.Discriminates[j].First
		}
		return c.Discriminates[i].Second < c.Discriminates[j].Second
	})
	sort.Slice(c.SupportingHypothesisIDs, func(i, j int) bool { return c.SupportingHypothesisIDs[i] < c.SupportingHypothesisIDs[j] })
	sort.Slice(c.WeakeningHypothesisIDs, func(i, j int) bool { return c.WeakeningHypothesisIDs[i] < c.WeakeningHypothesisIDs[j] })
}
