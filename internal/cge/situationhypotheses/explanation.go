package situationhypotheses

import "synora/internal/cge/situationfacts"

type ExplanationReason struct {
	ReasonCode     string
	Role           ContributionRole
	FactIDs        []situationfacts.FactID
	FactCodes      []situationfacts.FactCode
	Values         []situationfacts.FactValue
	WeightPermille int
}

type HypothesisExplanation struct {
	HypothesisID HypothesisID
	Kind         HypothesisKind
	Status       HypothesisStatus

	SummaryCode string

	SupportingReasons    []ExplanationReason
	ContradictingReasons []ExplanationReason
	MissingReasons       []ExplanationReason

	PlausibilityPermille int
	CoveragePermille     int

	NotAProbability   bool
	NoSecurityMeaning bool
}

func Explain(hypothesis SituationHypothesis, factSet situationfacts.FactSet) (HypothesisExplanation, error) {
	known, err := validateFactSet(factSet, situationfacts.Schema())
	if err != nil {
		return HypothesisExplanation{}, err
	}
	if hypothesis.EpisodeID != string(factSet.EpisodeID) {
		return HypothesisExplanation{}, ErrInvalidHypothesis
	}
	byID := make(map[situationfacts.FactID]situationfacts.Fact, len(factSet.Facts))
	for _, fact := range factSet.Facts {
		byID[fact.ID] = fact
	}
	result := HypothesisExplanation{HypothesisID: hypothesis.ID, Kind: hypothesis.Kind, Status: hypothesis.Status, SummaryCode: "hypothesis." + string(hypothesis.Kind) + "." + string(hypothesis.Status), PlausibilityPermille: hypothesis.PlausibilityPermille, CoveragePermille: hypothesis.CoveragePermille, NotAProbability: true, NoSecurityMeaning: true}
	appendReasons := func(values []Contribution, target *[]ExplanationReason) error {
		for _, contribution := range values {
			reason := ExplanationReason{ReasonCode: contribution.ReasonCode, Role: contribution.Role, FactIDs: append([]situationfacts.FactID(nil), contribution.FactIDs...), WeightPermille: contribution.WeightPermille}
			for _, id := range contribution.FactIDs {
				if _, ok := known[id]; !ok {
					return ErrUnknownFactReference
				}
				fact := byID[id]
				reason.FactCodes = append(reason.FactCodes, fact.Code)
				reason.Values = append(reason.Values, fact.Value.Clone())
			}
			*target = append(*target, reason)
		}
		return nil
	}
	if err := appendReasons(hypothesis.Support, &result.SupportingReasons); err != nil {
		return HypothesisExplanation{}, err
	}
	if err := appendReasons(hypothesis.Contradiction, &result.ContradictingReasons); err != nil {
		return HypothesisExplanation{}, err
	}
	for _, missing := range hypothesis.Missing {
		result.MissingReasons = append(result.MissingReasons, ExplanationReason{ReasonCode: missing.ReasonCode, Role: ContributionNeutral, FactCodes: []situationfacts.FactCode{missing.RequiredFactCode}, WeightPermille: missing.ImportancePermille})
	}
	return result, nil
}
