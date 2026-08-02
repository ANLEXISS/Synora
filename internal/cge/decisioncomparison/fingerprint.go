package decisioncomparison

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"synora/internal/cge/cognitiverecommendation"
	"synora/internal/cge/cognitivesituation"
)

func fingerprint(prefix string, value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return prefix + hex.EncodeToString(sum[:])
}

func historicalDecisionFingerprint(value HistoricalDecisionRef) string {
	copy := value.Clone()
	copy.Fingerprint = ""
	sort.Strings(copy.ReasonCodes)
	return fingerprint("historical-decision-ref-v1:", copy)
}

func dimensionFingerprint(value ComparisonDimension) string {
	copy := value.Clone()
	copy.Fingerprint = ""
	sort.Strings(copy.HistoricalCodes)
	sort.Strings(copy.CognitiveCodes)
	sort.Strings(copy.ReasonCodes)
	return fingerprint("historical-decision-comparison-dimension-v1:", copy)
}

func comparisonFingerprint(value HistoricalDecisionComparison) string {
	copy := value.Clone()
	copy.Fingerprint = ""
	copy.PreviousComparisonID = ""
	sort.Slice(copy.Dimensions, func(i, j int) bool { return copy.Dimensions[i].Kind < copy.Dimensions[j].Kind })
	return fingerprint("historical-decision-comparison-v1:", copy)
}

func explanationFingerprint(value HistoricalDecisionComparisonExplanation) string {
	copy := value
	copy.DimensionSummaries = append([]ComparisonDimensionSummary(nil), value.DimensionSummaries...)
	copy.ReasonCodes = append([]string(nil), value.ReasonCodes...)
	sort.Slice(copy.DimensionSummaries, func(i, j int) bool { return copy.DimensionSummaries[i].Kind < copy.DimensionSummaries[j].Kind })
	sort.Strings(copy.ReasonCodes)
	for i := range copy.DimensionSummaries {
		sort.Strings(copy.DimensionSummaries[i].ReasonCodes)
	}
	return fingerprint("historical-decision-comparison-explanation-v1:", copy)
}

func snapshotFingerprint(value HistoricalDecisionComparisonSnapshot) string {
	copy := value.Clone()
	copy.Digest = ""
	sort.Slice(copy.Comparisons, func(i, j int) bool { return copy.Comparisons[i].EpisodeID < copy.Comparisons[j].EpisodeID })
	copy.EpisodeIndex = make(map[string]int, len(copy.Comparisons))
	for i, comparison := range copy.Comparisons {
		copy.EpisodeIndex[comparison.EpisodeID] = i
	}
	return fingerprint("historical-decision-comparison-snapshot-v1:", copy)
}

func HistoricalDecisionFingerprint(value HistoricalDecisionRef) string {
	return historicalDecisionFingerprint(value)
}
func ComparisonDimensionFingerprint(value ComparisonDimension) string {
	return dimensionFingerprint(value)
}
func ComparisonFingerprint(value HistoricalDecisionComparison) string {
	return comparisonFingerprint(value)
}
func ComparisonExplanationFingerprint(value HistoricalDecisionComparisonExplanation) string {
	return explanationFingerprint(value)
}
func ComparisonSnapshotFingerprint(value HistoricalDecisionComparisonSnapshot) string {
	return snapshotFingerprint(value)
}

func sourceFingerprintsMatch(s cognitivesituation.CognitiveSituation, r cognitiverecommendation.CognitiveRecommendationSet) bool {
	return r.SituationID == s.ID && r.SourceSituationFingerprint == s.Fingerprint && r.SourceSituationRevision == s.Revision
}
