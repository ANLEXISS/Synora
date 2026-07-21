package cognitiverecommendation

import (
	"fmt"
	"sort"

	"synora/internal/cge/cognitivesituation"
)

func validatePlanInput(input PlanInput, policy Policy) error {
	if policy.Validate() != nil {
		return ErrInvalidPolicy
	}
	if err := input.Situation.Validate(cognitivesituation.DefaultPolicy()); err != nil {
		return fmt.Errorf("%w: situation", ErrInvalidSituation)
	}
	if input.Situation.ID == "" || input.Situation.EpisodeID == "" {
		return ErrInvalidPlanInput
	}
	if input.SituationDiff != nil {
		if input.SituationDiff.CurrentFingerprint != input.Situation.Fingerprint {
			return ErrSourceFingerprintMismatch
		}
		if input.Previous != nil && input.SituationDiff.PreviousFingerprint != input.Previous.SourceSituationFingerprint {
			return ErrSourceFingerprintMismatch
		}
	}
	if input.Previous != nil && input.Previous.EpisodeID != input.Situation.EpisodeID {
		return ErrSourceRevisionConflict
	}
	return nil
}

func (r CognitiveRecommendation) Validate(policy Policy) error {
	if policy.Validate() != nil {
		return ErrInvalidPolicy
	}
	if r.ID == "" || r.SituationID == "" || r.EpisodeID == "" || !validKind(r.Kind) || !validStatus(r.Status) {
		return ErrInvalidRecommendation
	}
	if !validTargetKind(r.Target.Kind) || r.Target.Fingerprint == "" || r.Target.Fingerprint != targetFingerprint(r.Target) {
		return ErrInvalidRecommendationTarget
	}
	if r.Fingerprint == "" || r.Fingerprint != recommendationFingerprint(r) {
		return ErrInvalidRecommendation
	}
	if r.ApplicabilityPermille < 0 || r.ApplicabilityPermille > 1000 || r.InformationValuePermille < 0 || r.InformationValuePermille > 1000 || r.StabilityPermille < 0 || r.StabilityPermille > 1000 || r.UrgencyPermille < 0 || r.UrgencyPermille > 1000 {
		return ErrInvalidRecommendation
	}
	if len(r.SupportingReasonCodes) > policy.MaxReasonCodes || len(r.BlockingReasonCodes) > policy.MaxReasonCodes {
		return ErrInvalidRecommendation
	}
	if !r.Markers.NotADecision || !r.Markers.NotAProbability || !r.Markers.NotAuthorization || !r.Markers.NotACommand || !r.Markers.NotAnAction || !r.Markers.NotAnAlert || !r.Markers.NoSecurityMeaning || !r.Markers.RequiresSeparateDecisionAuthority {
		return ErrInvalidRecommendation
	}
	return nil
}

func (s CognitiveRecommendationSet) Validate(policy Policy) error {
	if policy.Validate() != nil {
		return ErrInvalidPolicy
	}
	if s.ID == "" || s.SituationID == "" || s.EpisodeID == "" || len(s.Recommendations) > policy.MaxRecommendations || s.Fingerprint == "" || s.Fingerprint != setFingerprint(s) {
		return ErrInvalidRecommendationSet
	}
	if s.PrimaryMarginPermille < 0 || s.PrimaryMarginPermille > 1000 {
		return ErrInvalidRecommendationSet
	}
	seen := map[string]struct{}{}
	for _, recommendation := range s.Recommendations {
		if _, ok := seen[recommendation.ID]; ok {
			return ErrInvalidRecommendationSet
		}
		seen[recommendation.ID] = struct{}{}
		if err := recommendation.Validate(policy); err != nil {
			return err
		}
	}
	if s.PrimaryRecommendationID != "" {
		recommendation, ok := findRecommendation(s.Recommendations, s.PrimaryRecommendationID)
		if !ok || recommendation.Status != RecommendationApplicable || s.Ambiguous {
			return ErrInvalidRecommendationSet
		}
	}
	if !s.Markers.NotADecision || !s.Markers.NotAuthorization || !s.Markers.NotACommand || !s.Markers.NotAnAction || !s.Markers.NoSecurityMeaning {
		return ErrInvalidRecommendationSet
	}
	return nil
}

func findRecommendation(values []CognitiveRecommendation, id string) (CognitiveRecommendation, bool) {
	for _, value := range values {
		if value.ID == id {
			return value, true
		}
	}
	return CognitiveRecommendation{}, false
}

func (s CognitiveRecommendationSnapshot) Validate(policy Policy) error {
	if policy.Validate() != nil || s.Digest == "" || s.Digest != snapshotFingerprint(s) || len(s.EpisodeIndex) != len(s.RecommendationSets) {
		return ErrInvalidRecommendationSet
	}
	seen := map[string]struct{}{}
	for index, set := range s.RecommendationSets {
		if _, ok := seen[set.EpisodeID]; ok || s.EpisodeIndex[set.EpisodeID] != index {
			return ErrInvalidRecommendationSet
		}
		seen[set.EpisodeID] = struct{}{}
		if err := set.Validate(policy); err != nil {
			return err
		}
	}
	return nil
}

func sortRecommendations(values []CognitiveRecommendation) {
	sort.SliceStable(values, func(i, j int) bool {
		left, right := values[i], values[j]
		if left.Status != right.Status {
			return left.Status == RecommendationApplicable
		}
		if left.ApplicabilityPermille != right.ApplicabilityPermille {
			return left.ApplicabilityPermille > right.ApplicabilityPermille
		}
		if left.InformationValuePermille != right.InformationValuePermille {
			return left.InformationValuePermille > right.InformationValuePermille
		}
		if left.StabilityPermille != right.StabilityPermille {
			return left.StabilityPermille > right.StabilityPermille
		}
		if left.UrgencyPermille != right.UrgencyPermille {
			return left.UrgencyPermille > right.UrgencyPermille
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Target.Fingerprint != right.Target.Fingerprint {
			return left.Target.Fingerprint < right.Target.Fingerprint
		}
		return left.ID < right.ID
	})
	for i := range values {
		values[i].Rank = i + 1
		values[i].Fingerprint = recommendationFingerprint(values[i])
	}
}
