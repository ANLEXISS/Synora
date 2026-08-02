package cognitiverecommendation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"synora/internal/cge/cognitivesituation"
)

func fingerprint(prefix string, value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return prefix + hex.EncodeToString(sum[:])
}

func targetFingerprint(value RecommendationTarget) string {
	copy := value
	copy.Fingerprint = ""
	return fingerprint("cognitive-recommendation-target-v1:", copy)
}

func recommendationFingerprint(value CognitiveRecommendation) string {
	copy := value.Clone()
	copy.Fingerprint = ""
	copy.PreviousRecommendationID = ""
	copy.SupportingReasonCodes = dedupeStrings(copy.SupportingReasonCodes, len(copy.SupportingReasonCodes))
	copy.BlockingReasonCodes = dedupeStrings(copy.BlockingReasonCodes, len(copy.BlockingReasonCodes))
	return fingerprint("cognitive-recommendation-v1:", copy)
}

func setFingerprint(value CognitiveRecommendationSet) string {
	copy := value.Clone()
	copy.Fingerprint = ""
	copy.Revision = 0
	sort.Slice(copy.Recommendations, func(i, j int) bool { return copy.Recommendations[i].ID < copy.Recommendations[j].ID })
	return fingerprint("cognitive-recommendation-set-v1:", copy)
}

func diffFingerprint(value CognitiveRecommendationDiff) string {
	copy := value
	copy.Fingerprint = ""
	sort.Strings(copy.AddedRecommendationIDs)
	sort.Strings(copy.RemovedRecommendationIDs)
	sort.Strings(copy.StatusChangedRecommendationIDs)
	sort.Strings(copy.ReasonCodes)
	return fingerprint("cognitive-recommendation-diff-v1:", copy)
}

func explanationFingerprint(value CognitiveRecommendationExplanation) string {
	copy := value
	copy.SupportingReasonCodes = append([]string(nil), value.SupportingReasonCodes...)
	copy.BlockingReasonCodes = append([]string(nil), value.BlockingReasonCodes...)
	sort.Strings(copy.SupportingReasonCodes)
	sort.Strings(copy.BlockingReasonCodes)
	return fingerprint("cognitive-recommendation-explanation-v1:", copy)
}

func snapshotFingerprint(value CognitiveRecommendationSnapshot) string {
	copy := value.Clone()
	copy.Digest = ""
	sort.Slice(copy.RecommendationSets, func(i, j int) bool {
		return copy.RecommendationSets[i].EpisodeID < copy.RecommendationSets[j].EpisodeID
	})
	copy.EpisodeIndex = make(map[string]int, len(copy.RecommendationSets))
	for index, set := range copy.RecommendationSets {
		copy.EpisodeIndex[set.EpisodeID] = index
	}
	return fingerprint("cognitive-recommendation-snapshot-v1:", copy)
}

func RecommendationFingerprint(value CognitiveRecommendation) string {
	return recommendationFingerprint(value)
}
func RecommendationSetFingerprint(value CognitiveRecommendationSet) string {
	return setFingerprint(value)
}
func RecommendationDiffFingerprint(value CognitiveRecommendationDiff) string {
	return diffFingerprint(value)
}
func RecommendationExplanationFingerprint(value CognitiveRecommendationExplanation) string {
	return explanationFingerprint(value)
}
func RecommendationSnapshotFingerprint(value CognitiveRecommendationSnapshot) string {
	return snapshotFingerprint(value)
}

func recommendationID(situation cognitivesituation.CognitiveSituation, kind RecommendationKind, target RecommendationTarget) string {
	payload := struct {
		Situation string
		Kind      RecommendationKind
		Target    RecommendationTarget
	}{situation.Fingerprint, kind, target}
	return "cognitive-recommendation-" + fingerprint("id-v1:", payload)[len("id-v1:"):]
}
