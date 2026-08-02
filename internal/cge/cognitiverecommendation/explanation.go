package cognitiverecommendation

import "sort"

func Explain(value CognitiveRecommendation, policy Policy) (CognitiveRecommendationExplanation, error) {
	if err := value.Validate(policy); err != nil {
		return CognitiveRecommendationExplanation{}, ErrInvalidExplanation
	}
	out := CognitiveRecommendationExplanation{
		RecommendationID: value.ID, SituationID: value.SituationID, EpisodeID: value.EpisodeID,
		Kind: value.Kind, Status: value.Status, SummaryCode: "cognitive_recommendation." + string(value.Kind), Target: value.Target,
		ApplicabilityPermille: value.ApplicabilityPermille, InformationValuePermille: value.InformationValuePermille,
		StabilityPermille: value.StabilityPermille, ReviewPriorityPermille: value.UrgencyPermille,
		SupportingReasonCodes: append([]string(nil), value.SupportingReasonCodes...), BlockingReasonCodes: append([]string(nil), value.BlockingReasonCodes...),
		NotADecision: true, NotAProbability: true, NotAuthorization: true, NotACommand: true, NotAnAction: true, NotAnAlert: true, NoSecurityMeaning: true, RequiresSeparateDecisionAuthority: true,
	}
	sort.Strings(out.SupportingReasonCodes)
	sort.Strings(out.BlockingReasonCodes)
	return out, nil
}
