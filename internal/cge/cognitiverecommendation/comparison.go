package cognitiverecommendation

import "sort"

func Compare(previous, current CognitiveRecommendationSet) (CognitiveRecommendationDiff, error) {
	if previous.EpisodeID == "" || current.EpisodeID == "" || previous.EpisodeID != current.EpisodeID {
		return CognitiveRecommendationDiff{}, ErrInvalidRecommendationDiff
	}
	if err := previous.Validate(DefaultPolicy()); err != nil {
		return CognitiveRecommendationDiff{}, ErrInvalidRecommendationDiff
	}
	if err := current.Validate(DefaultPolicy()); err != nil {
		return CognitiveRecommendationDiff{}, ErrInvalidRecommendationDiff
	}
	diff := CognitiveRecommendationDiff{
		EpisodeID: current.EpisodeID, PreviousSetFingerprint: previous.Fingerprint, CurrentSetFingerprint: current.Fingerprint,
		PreviousPrimaryID: previous.PrimaryRecommendationID, CurrentPrimaryID: current.PrimaryRecommendationID,
		PrimaryChanged:       previous.PrimaryRecommendationID != current.PrimaryRecommendationID,
		AmbiguityChanged:     previous.Ambiguous != current.Ambiguous,
		ApplicabilityChanged: previous.HasApplicableRecommendation != current.HasApplicableRecommendation,
	}
	before := make(map[string]CognitiveRecommendation, len(previous.Recommendations))
	for _, value := range previous.Recommendations {
		before[value.ID] = value
	}
	after := make(map[string]CognitiveRecommendation, len(current.Recommendations))
	for _, value := range current.Recommendations {
		after[value.ID] = value
	}
	for id, value := range after {
		old, ok := before[id]
		if !ok {
			diff.AddedRecommendationIDs = append(diff.AddedRecommendationIDs, id)
			continue
		}
		if old.Status != value.Status {
			diff.StatusChangedRecommendationIDs = append(diff.StatusChangedRecommendationIDs, id)
		}
	}
	for id := range before {
		if _, ok := after[id]; !ok {
			diff.RemovedRecommendationIDs = append(diff.RemovedRecommendationIDs, id)
		}
	}
	if diff.PrimaryChanged {
		diff.ReasonCodes = append(diff.ReasonCodes, "primary_changed")
	}
	if diff.AmbiguityChanged {
		diff.ReasonCodes = append(diff.ReasonCodes, "ambiguity_changed")
	}
	if diff.ApplicabilityChanged {
		diff.ReasonCodes = append(diff.ReasonCodes, "applicability_changed")
	}
	if len(diff.AddedRecommendationIDs) > 0 {
		diff.ReasonCodes = append(diff.ReasonCodes, "recommendation_added")
	}
	if len(diff.RemovedRecommendationIDs) > 0 {
		diff.ReasonCodes = append(diff.ReasonCodes, "recommendation_removed")
	}
	if len(diff.StatusChangedRecommendationIDs) > 0 {
		diff.ReasonCodes = append(diff.ReasonCodes, "recommendation_status_changed")
	}
	sort.Strings(diff.AddedRecommendationIDs)
	sort.Strings(diff.RemovedRecommendationIDs)
	sort.Strings(diff.StatusChangedRecommendationIDs)
	sort.Strings(diff.ReasonCodes)
	diff.Fingerprint = diffFingerprint(diff)
	return diff, nil
}
