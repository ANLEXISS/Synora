package situationhypotheses

import (
	"sort"

	"synora/internal/cge/situationfacts"
)

func Plan(factSet situationfacts.FactSet, current RegistrySnapshot, schema HypothesisSchema, policy Policy) (PlanResult, error) {
	if err := policy.Validate(); err != nil {
		return PlanResult{}, err
	}
	if err := schema.Validate(); err != nil {
		return PlanResult{}, err
	}
	if current.Digest != "" && current.Digest != registryDigest(current) {
		return PlanResult{}, ErrFingerprintMismatch
	}
	if factSet.EpisodeID == "" {
		return PlanResult{}, ErrInvalidFactSet
	}
	var previous *CompetingHypothesisSet
	if index, ok := current.EpisodeIndex[string(factSet.EpisodeID)]; ok && index >= 0 && index < len(current.EpisodeSets) {
		value := current.EpisodeSets[index].Clone()
		previous = &value
	}
	evaluation, err := Evaluate(EvaluationInput{FactSet: factSet, PreviousSet: previous}, schema, policy)
	if err != nil {
		return PlanResult{}, err
	}
	result := evaluation.Set.Clone()
	result.FactRegistryRevision = current.Revision
	result.SchemaFingerprint = SchemaFingerprint()
	result.PolicyFingerprint = policy.Fingerprint()
	result.Fingerprint = competingSetFingerprint(result)
	plan := PlanResult{EpisodeID: string(factSet.EpisodeID), SourceFactSetFingerprint: factSet.Fingerprint, SourceFactSet: factSet.Clone(), SourceRegistryRevision: current.Revision, ResultingSet: result, ReasonCodes: append([]string(nil), evaluation.ReasonCodes...)}
	previousByID := map[HypothesisID]SituationHypothesis{}
	if previous != nil {
		for _, hypothesis := range previous.Hypotheses {
			previousByID[hypothesis.ID] = hypothesis
		}
	}
	resultByID := map[HypothesisID]SituationHypothesis{}
	for _, hypothesis := range result.Hypotheses {
		resultByID[hypothesis.ID] = hypothesis
		before, ok := previousByID[hypothesis.ID]
		if !ok {
			plan.Creates = append(plan.Creates, hypothesis.Clone())
		} else if before.Fingerprint != hypothesis.Fingerprint {
			if hypothesis.Status == StatusInvalidated {
				plan.Invalidates = append(plan.Invalidates, HypothesisInvalidation{Before: before.Clone(), After: hypothesis.Clone(), ReasonCode: "support_reassessment"})
			} else {
				plan.Updates = append(plan.Updates, HypothesisUpdate{Before: before.Clone(), After: hypothesis.Clone()})
			}
		}
	}
	if previous != nil {
		for _, before := range previous.Hypotheses {
			if _, ok := resultByID[before.ID]; !ok {
				invalidated := before.Clone()
				invalidated.Status = StatusInvalidated
				invalidated.EvaluatedFactRevision = factSet.EpisodeRevision
				invalidated.Revision++
				invalidated.Support, invalidated.Contradiction, invalidated.Missing = nil, nil, nil
				invalidated.SupportPermille, invalidated.ContradictionPermille, invalidated.CoveragePermille, invalidated.PlausibilityPermille = 0, 0, 0, 0
				refreshHypothesisFingerprint(&invalidated)
				plan.Invalidates = append(plan.Invalidates, HypothesisInvalidation{Before: before.Clone(), After: invalidated, ReasonCode: "support_reassessment"})
			}
		}
	}
	sort.Slice(plan.Creates, func(i, j int) bool { return plan.Creates[i].ID < plan.Creates[j].ID })
	sort.Slice(plan.Updates, func(i, j int) bool { return plan.Updates[i].After.ID < plan.Updates[j].After.ID })
	sort.Slice(plan.Invalidates, func(i, j int) bool { return plan.Invalidates[i].After.ID < plan.Invalidates[j].After.ID })
	return plan, nil
}

func ReevaluateFromDiff(previousFacts, currentFacts situationfacts.FactSet, diff situationfacts.FactSetDiff, previousSet CompetingHypothesisSet, schema HypothesisSchema, policy Policy) (EvaluationResult, error) {
	if previousFacts.EpisodeID == "" || currentFacts.EpisodeID == "" || previousFacts.EpisodeID != currentFacts.EpisodeID || diff.EpisodeID != currentFacts.EpisodeID || diff.BeforeFingerprint != previousFacts.Fingerprint || diff.AfterFingerprint != currentFacts.Fingerprint || diff.BeforeEpisodeRevision != previousFacts.EpisodeRevision || diff.AfterEpisodeRevision != currentFacts.EpisodeRevision {
		return EvaluationResult{}, ErrFingerprintMismatch
	}
	if previousSet.EpisodeID != string(previousFacts.EpisodeID) || previousSet.FactSetFingerprint != previousFacts.Fingerprint || previousSet.Fingerprint != competingSetFingerprint(previousSet) {
		return EvaluationResult{}, ErrFingerprintMismatch
	}
	if previousFacts.Fingerprint == currentFacts.Fingerprint {
		if previousFacts.EpisodeRevision != currentFacts.EpisodeRevision {
			return EvaluationResult{}, ErrInvalidPlan
		}
	} else if currentFacts.EpisodeRevision <= previousFacts.EpisodeRevision {
		return EvaluationResult{}, ErrStaleFactSet
	}
	result, err := Evaluate(EvaluationInput{FactSet: currentFacts, PreviousSet: &previousSet}, schema, policy)
	if err != nil {
		return EvaluationResult{}, err
	}
	result.Mode = EvaluationModeDiff
	result.CompetingSet = result.Set.Clone()
	result.HypothesisSet = result.Set.Clone()
	return result, nil
}
