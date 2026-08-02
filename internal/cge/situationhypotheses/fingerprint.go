package situationhypotheses

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"synora/internal/cge/situationfacts"
)

func hypothesisIDFor(episodeID string, kind HypothesisKind) HypothesisID {
	digest := sha256.Sum256([]byte("situation-hypothesis-v1\x00" + episodeID + "\x00" + string(kind)))
	return HypothesisID("situation-hypothesis-" + hex.EncodeToString(digest[:]))
}

func contributionIDFor(value Contribution) string {
	ids := sortedFactIDs(value.FactIDs)
	payload, _ := json.Marshal(struct {
		RuleID string
		Role   ContributionRole
		IDs    []situationfacts.FactID
	}{value.RuleID, value.Role, ids})
	digest := sha256.Sum256(payload)
	return "situation-contribution-" + hex.EncodeToString(digest[:])
}

func hypothesisFingerprint(value SituationHypothesis) string {
	copy := value
	copy.Fingerprint = ""
	copy.Support = canonicalContributions(copy.Support)
	copy.Contradiction = canonicalContributions(copy.Contradiction)
	copy.Missing = canonicalMissing(copy.Missing)
	payload, _ := json.Marshal(copy)
	digest := sha256.Sum256(payload)
	return "situation-hypothesis-v1:" + hex.EncodeToString(digest[:])
}

func competingSetFingerprint(value CompetingHypothesisSet) string {
	copy := value
	copy.Fingerprint = ""
	copy.Hypotheses = append([]SituationHypothesis(nil), value.Hypotheses...)
	sort.Slice(copy.Hypotheses, func(i, j int) bool { return copy.Hypotheses[i].ID < copy.Hypotheses[j].ID })
	payload, _ := json.Marshal(copy)
	digest := sha256.Sum256(payload)
	return "competing-hypothesis-set-v1:" + hex.EncodeToString(digest[:])
}

func registryDigest(snapshot RegistrySnapshot) string {
	copy := snapshot
	copy.Digest = ""
	copy.EpisodeSets = append([]CompetingHypothesisSet(nil), snapshot.EpisodeSets...)
	sort.Slice(copy.EpisodeSets, func(i, j int) bool { return copy.EpisodeSets[i].EpisodeID < copy.EpisodeSets[j].EpisodeID })
	copy.EpisodeIndex = nil
	copy.HypothesisIndex = nil
	payload, _ := json.Marshal(copy)
	digest := sha256.Sum256(payload)
	return "situation-hypothesis-registry-v1:" + hex.EncodeToString(digest[:])
}

func HypothesisFingerprint(value SituationHypothesis) string { return hypothesisFingerprint(value) }

func HypothesisIDFor(episodeID string, kind HypothesisKind) HypothesisID {
	return hypothesisIDFor(episodeID, kind)
}

func ContributionIDFor(value Contribution) string { return contributionIDFor(value) }

func CompetingHypothesisSetFingerprint(value CompetingHypothesisSet) string {
	return competingSetFingerprint(value)
}

func RegistryDigest(snapshot RegistrySnapshot) string { return registryDigest(snapshot) }
