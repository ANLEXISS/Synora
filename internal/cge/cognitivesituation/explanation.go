package cognitivesituation

import "sort"

func Explain(situation CognitiveSituation, policy Policy) (CognitiveSituationExplanation, error) {
	if err := situation.Validate(policy); err != nil {
		return CognitiveSituationExplanation{}, ErrInvalidExplanation
	}
	out := CognitiveSituationExplanation{
		SituationID:             situation.ID,
		EpisodeID:               situation.EpisodeID,
		Phase:                   situation.Phase,
		SummaryCode:             "cognitive_situation." + string(situation.Phase),
		LayerStates:             cloneKnowledge(situation.Knowledge).LayerStates,
		LeadingHypothesisKind:   situation.Hypotheses.LeadingHypothesisKind,
		ActiveAdvisoryCount:     situation.Advisory.Active,
		CapabilityAvailable:     situation.Capability.Available,
		AuthorizationEligible:   situation.Authorization.AuthorizationEligible,
		RecommendationReadiness: situation.RecommendationReadiness.Status,
		NotADecision:            true, NotAProbability: true, NotAuthorization: true,
		NotACommand: true, NotAnAction: true, NoSecurityMeaning: true,
	}
	for _, alternative := range situation.Hypotheses.Alternatives {
		if alternative.Kind != situation.Hypotheses.LeadingHypothesisKind {
			out.AlternativeHypothesisKinds = append(out.AlternativeHypothesisKinds, alternative.Kind)
		}
	}
	out.ReasonCodes = append(out.ReasonCodes, situation.RecommendationReadiness.BlockingReasonCodes...)
	out.ReasonCodes = append(out.ReasonCodes, situation.RecommendationReadiness.SupportingReasonCodes...)
	out.MissingInformationCodes = append(out.MissingInformationCodes, situation.Evidence.MissingRequirementCodes...)
	sort.Strings(out.AlternativeHypothesisKinds)
	sort.Strings(out.MissingInformationCodes)
	sort.Strings(out.ReasonCodes)
	return out, nil
}
