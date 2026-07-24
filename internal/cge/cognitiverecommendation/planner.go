package cognitiverecommendation

import (
	"synora/internal/cge/cognitivesituation"
)

func Plan(input PlanInput, policy Policy) (CognitiveRecommendationSet, error) {
	if err := validatePlanInput(input, policy); err != nil {
		return CognitiveRecommendationSet{}, err
	}
	situation := input.Situation
	if situation.Phase == cognitivesituation.PhaseInvalidated {
		invalidatedInput := input
		invalidatedInput.Previous = nil
		return finishSet(invalidatedInput, policy, []CognitiveRecommendation{newRecommendation(situation, RecommendationNone, targetSituation(situation), RecommendationInvalidated, 0, 0, 0, 0, []string{"situation.invalidated"}, nil, nil)}), nil
	}
	if policy.RequireFreshSituation && situation.Phase == cognitivesituation.PhaseStale {
		return finishSet(input, policy, []CognitiveRecommendation{newRecommendation(situation, RecommendationReassessContext, targetContext(situation), RecommendationBlocked, 250, 250, 0, 1000, nil, []string{"situation.stale"}, input.Previous)}), nil
	}
	if policy.RequireRecommendationReadiness && !situation.RecommendationReadiness.Ready && situation.Phase == cognitivesituation.PhaseCoherent {
		return finishSet(input, policy, []CognitiveRecommendation{newRecommendation(situation, RecommendationNone, targetSituation(situation), RecommendationInsufficientInformation, 0, 0, 0, 0, nil, []string{"recommendation_readiness_not_ready"}, input.Previous)}), nil
	}

	recommendations := make([]CognitiveRecommendation, 0, policy.MaxRecommendations)
	add := func(kind RecommendationKind, target RecommendationTarget, status RecommendationStatus, applicability, information, stability, urgency int, supporting, blocking []string) {
		if len(recommendations) >= policy.MaxRecommendations {
			return
		}
		recommendations = append(recommendations, newRecommendation(situation, kind, target, status, applicability, information, stability, urgency, supporting, blocking, input.Previous))
	}

	switch situation.Phase {
	case cognitivesituation.PhaseObserving:
		if policy.AllowObservationRecommendation {
			add(RecommendationContinueObservation, targetFutureObservation(situation), RecommendationApplicable, 650, 250, stability(situation), 300, []string{"phase.observing"}, nil)
		} else {
			add(RecommendationNone, targetSituation(situation), RecommendationInsufficientInformation, 0, 0, 0, 0, nil, []string{"observation_recommendation_disabled"})
		}
	case cognitivesituation.PhaseBuilding:
		if policy.AllowObservationRecommendation {
			add(RecommendationContinueObservation, targetFutureObservation(situation), RecommendationApplicable, 650, 300, stability(situation), 350, []string{"phase.building"}, nil)
			add(RecommendationReassessObservation, targetFutureObservation(situation), RecommendationApplicable, 600, 350, stability(situation), 400, []string{"new_observation_may_change_situation"}, nil)
		}
	case cognitivesituation.PhaseCoherent:
		add(RecommendationMaintainInterpretation, targetHypothesis(situation), RecommendationApplicable, 850, 250, stability(situation), 200, []string{"phase.coherent", "leading_hypothesis_preserved"}, nil)
	case cognitivesituation.PhaseAmbiguous:
		add(RecommendationPreserveAmbiguity, targetSituation(situation), RecommendationApplicable, 900, 700, stability(situation), 500, []string{"phase.ambiguous", "alternatives_preserved"}, nil)
		addEvidenceIfAvailable(&recommendations, situation, policy, input.Previous)
	case cognitivesituation.PhaseIncomplete:
		add(RecommendationReassessContext, targetContext(situation), RecommendationApplicable, 750, 500, stability(situation), 550, []string{"phase.incomplete"}, nil)
		addEvidenceIfAvailable(&recommendations, situation, policy, input.Previous)
	case cognitivesituation.PhaseAwaitingEvidence:
		if err := addEvidenceIfAvailable(&recommendations, situation, policy, input.Previous); err != nil {
			return CognitiveRecommendationSet{}, err
		}
	case cognitivesituation.PhaseCapabilityUnavailable:
		add(RecommendationPreserveAmbiguity, targetSituation(situation), RecommendationApplicable, 700, 500, stability(situation), 500, []string{"capability_unavailable", "no_capability_fabricated"}, nil)
		add(RecommendationReassessContext, targetContext(situation), RecommendationApplicable, 650, 400, stability(situation), 600, []string{"capability_unavailable"}, nil)
	case cognitivesituation.PhaseAuthorizationConstrained:
		add(RecommendationMaintainInterpretation, targetHypothesis(situation), RecommendationApplicable, 650, 250, stability(situation), 250, []string{"authorization_constraint_does_not_change_cognition"}, []string{"authorization_constrained"})
		add(RecommendationReassessContext, targetContext(situation), RecommendationBlocked, 500, 350, 300, 500, nil, []string{"authorization_constrained"})
	default:
		add(RecommendationNone, targetSituation(situation), RecommendationInsufficientInformation, 0, 0, 0, 0, nil, []string{"no_recommendation_rule"})
	}

	if input.SituationDiff != nil && policy.AllowTransitionRecommendation && situation.Phase != cognitivesituation.PhaseStale && situation.Phase != cognitivesituation.PhaseInvalidated && transitionChanged(*input.SituationDiff) {
		add(RecommendationCognitiveTransition, targetSituation(situation), RecommendationApplicable, 700, 450, stability(situation), 600, []string{"cognitive_transition_detected"}, nil)
	}
	return finishSet(input, policy, recommendations), nil
}

func addEvidenceIfAvailable(values *[]CognitiveRecommendation, situation cognitivesituation.CognitiveSituation, policy Policy, previous *CognitiveRecommendationSet) error {
	if situation.Advisory.Active <= 0 {
		return nil
	}
	if situation.Advisory.PreferredRequestID == "" {
		return ErrMissingAdvisoryReference
	}
	if len(*values) >= policy.MaxRecommendations {
		return nil
	}
	target := RecommendationTarget{Kind: TargetEvidenceRequest, SituationID: situation.ID, AdvisoryRequestID: situation.Advisory.PreferredRequestID, ReferenceCode: situation.Advisory.PreferredCandidateKind}
	target.Fingerprint = targetFingerprint(target)
	*values = append(*values, newRecommendation(situation, RecommendationAdditionalEvidence, target, RecommendationApplicable, 850, maxInt(600, situation.Evidence.BestUtilityPermille), stability(situation), 700, []string{"active_advisory_request", "missing_information_preserved"}, nil, previous))
	return nil
}

func finishSet(input PlanInput, policy Policy, recommendations []CognitiveRecommendation) CognitiveRecommendationSet {
	if input.Previous != nil {
		currentKinds := make(map[RecommendationKind]struct{}, len(recommendations))
		currentIDs := make(map[string]struct{}, len(recommendations))
		for _, value := range recommendations {
			currentKinds[value.Kind] = struct{}{}
			currentIDs[value.ID] = struct{}{}
		}
		for _, previous := range input.Previous.Recommendations {
			if _, present := currentIDs[previous.ID]; present || len(recommendations) >= policy.MaxRecommendations {
				continue
			}
			withdrawn := previous.Clone()
			if _, sameKind := currentKinds[previous.Kind]; sameKind {
				withdrawn.Status = RecommendationSuperseded
			} else {
				withdrawn.Status = RecommendationWithdrawn
			}
			withdrawn.SourceSituationFingerprint = input.Situation.Fingerprint
			withdrawn.SourceSituationRevision = input.Situation.Revision
			withdrawn.PreviousRecommendationID = previous.ID
			withdrawn.SupportingReasonCodes = nil
			withdrawn.BlockingReasonCodes = []string{"previous_recommendation_replaced"}
			withdrawn.Fingerprint = recommendationFingerprint(withdrawn)
			recommendations = append(recommendations, withdrawn)
		}
	}
	sortRecommendations(recommendations)
	if len(recommendations) > policy.MaxRecommendations {
		recommendations = recommendations[:policy.MaxRecommendations]
	}
	set := CognitiveRecommendationSet{
		ID:          "cognitive-recommendation-set-" + shortID(input.Situation.ID),
		SituationID: input.Situation.ID, EpisodeID: input.Situation.EpisodeID,
		SourceSituationFingerprint: input.Situation.Fingerprint, SourceSituationRevision: input.Situation.Revision,
		Recommendations: recommendations,
		Markers:         RecommendationSetMarkers{NotADecision: true, NotAuthorization: true, NotACommand: true, NotAnAction: true, NoSecurityMeaning: true},
	}
	applicable := make([]CognitiveRecommendation, 0, len(recommendations))
	for i := range set.Recommendations {
		if set.Recommendations[i].Status == RecommendationApplicable {
			applicable = append(applicable, set.Recommendations[i])
		}
		if set.Recommendations[i].Kind == RecommendationAdditionalEvidence || set.Recommendations[i].Kind == RecommendationContinueObservation || set.Recommendations[i].Kind == RecommendationReassessObservation {
			set.HasObservationRecommendation = true
		}
		if set.Recommendations[i].Kind == RecommendationCognitiveTransition {
			set.HasCognitiveTransition = true
		}
	}
	set.HasApplicableRecommendation = len(applicable) > 0
	set.Ambiguous = input.Situation.Phase == cognitivesituation.PhaseAmbiguous
	if len(applicable) > 1 {
		margin := applicable[0].ApplicabilityPermille - applicable[1].ApplicabilityPermille
		if margin < 0 {
			margin = 0
		}
		set.PrimaryMarginPermille = clamp(margin)
		if margin < policy.MinPrimaryMarginPermille {
			set.Ambiguous = true
		}
	} else if len(applicable) == 1 {
		set.PrimaryMarginPermille = 1000
	}
	if len(applicable) > 1 && applicable[0].ApplicabilityPermille == applicable[1].ApplicabilityPermille {
		set.Ambiguous = true
	}
	if len(applicable) > 0 && !set.Ambiguous && (input.Situation.Phase == cognitivesituation.PhaseCoherent || policy.AllowPrimaryWhenAmbiguous && input.Situation.Phase == cognitivesituation.PhaseAmbiguous) && applicable[0].ApplicabilityPermille >= policy.MinApplicabilityPermille {
		set.PrimaryRecommendationID = applicable[0].ID
	}
	previousByKind := map[RecommendationKind]string{}
	if input.Previous != nil {
		for _, value := range input.Previous.Recommendations {
			previousByKind[value.Kind] = value.ID
		}
	}
	for i := range set.Recommendations {
		if id := previousByKind[set.Recommendations[i].Kind]; id != "" && id != set.Recommendations[i].ID {
			set.Recommendations[i].PreviousRecommendationID = id
			set.Recommendations[i].Fingerprint = recommendationFingerprint(set.Recommendations[i])
		}
	}
	if input.Previous != nil && input.Previous.Fingerprint == setFingerprint(set) {
		set.Revision = input.Previous.Revision
	} else if input.Previous != nil {
		set.Revision = input.Previous.Revision + 1
	} else {
		set.Revision = 1
	}
	set.Fingerprint = setFingerprint(set)
	return set
}

func newRecommendation(situation cognitivesituation.CognitiveSituation, kind RecommendationKind, target RecommendationTarget, status RecommendationStatus, applicability, information, stability, urgency int, supporting, blocking []string, previous *CognitiveRecommendationSet) CognitiveRecommendation {
	if target.Fingerprint == "" {
		target.Fingerprint = targetFingerprint(target)
	}
	recommendation := CognitiveRecommendation{
		SituationID: situation.ID, EpisodeID: situation.EpisodeID, Kind: kind, Target: target, Status: status,
		ApplicabilityPermille: clamp(applicability), InformationValuePermille: clamp(information), StabilityPermille: clamp(stability), UrgencyPermille: clamp(urgency),
		SupportingReasonCodes: dedupeStrings(supporting, 64), BlockingReasonCodes: dedupeStrings(blocking, 64),
		SourceSituationFingerprint: situation.Fingerprint, SourceSituationRevision: situation.Revision,
		Markers: RecommendationMarkers{NotADecision: true, NotAProbability: true, NotAuthorization: true, NotACommand: true, NotAnAction: true, NotAnAlert: true, NoSecurityMeaning: true, RequiresSeparateDecisionAuthority: true},
	}
	recommendation.ID = recommendationID(situation, kind, target)
	if previous != nil {
		for _, value := range previous.Recommendations {
			if value.Kind == kind {
				recommendation.PreviousRecommendationID = value.ID
				break
			}
		}
	}
	recommendation.Fingerprint = recommendationFingerprint(recommendation)
	return recommendation
}

func targetSituation(situation cognitivesituation.CognitiveSituation) RecommendationTarget {
	target := RecommendationTarget{Kind: TargetSituation, SituationID: situation.ID}
	target.Fingerprint = targetFingerprint(target)
	return target
}

func targetHypothesis(situation cognitivesituation.CognitiveSituation) RecommendationTarget {
	target := RecommendationTarget{Kind: TargetHypothesis, SituationID: situation.ID, HypothesisID: situation.Hypotheses.LeadingHypothesisID}
	target.Fingerprint = targetFingerprint(target)
	return target
}

func targetContext(situation cognitivesituation.CognitiveSituation) RecommendationTarget {
	target := RecommendationTarget{Kind: TargetContext, SituationID: situation.ID, ReferenceCode: string(situation.Phase)}
	target.Fingerprint = targetFingerprint(target)
	return target
}

func targetFutureObservation(situation cognitivesituation.CognitiveSituation) RecommendationTarget {
	target := RecommendationTarget{Kind: TargetFutureObservation, SituationID: situation.ID, ReferenceCode: "future_observation"}
	target.Fingerprint = targetFingerprint(target)
	return target
}

func stability(situation cognitivesituation.CognitiveSituation) int {
	return clamp(situation.Knowledge.OverallCoveragePermille)
}

func transitionChanged(diff cognitivesituation.CognitiveSituationDiff) bool {
	return diff.PhaseChanged || diff.LeadingHypothesisChanged || diff.KnowledgeCoverageChanged || diff.AdvisoryChanged || diff.CapabilityChanged || diff.AuthorizationChanged || diff.ReadinessChanged
}

func shortID(value string) string {
	return fingerprint("cognitive-recommendation-id-v1:", value)[len("cognitive-recommendation-id-v1:"):]
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
