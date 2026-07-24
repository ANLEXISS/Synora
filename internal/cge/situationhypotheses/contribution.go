package situationhypotheses

import (
	"sort"

	"synora/internal/cge/situationfacts"
)

func (c Contribution) Clone() Contribution {
	out := c
	out.FactIDs = append([]situationfacts.FactID(nil), c.FactIDs...)
	return out
}

func (c Contribution) Validate(policy Policy, known map[situationfacts.FactID]struct{}) error {
	if c.ID == "" || c.RuleID == "" || c.ReasonCode == "" || !validContributionRole(c.Role) || !bounded(c.WeightPermille) || c.FactSetFingerprint == "" || len(c.FactIDs) > policy.MaxFactIDsPerContribution {
		return ErrInvalidContribution
	}
	if c.Role != ContributionNeutral && len(c.FactIDs) == 0 {
		return ErrContributionWithoutFact
	}
	for i, id := range c.FactIDs {
		if id == "" || i > 0 && c.FactIDs[i-1] >= id {
			return ErrInvalidContribution
		}
		if _, ok := known[id]; !ok {
			return ErrUnknownFactReference
		}
	}
	if c.ID != contributionIDFor(c) {
		return ErrFingerprintMismatch
	}
	return nil
}

func (m MissingRequirement) Validate() error {
	if m.RuleID == "" || m.RequiredFactCode == "" || m.ReasonCode == "" || m.ImportancePermille < 0 || m.ImportancePermille > 1000 {
		return ErrInvalidDefinition
	}
	return nil
}

func validateFactSet(factSet situationfacts.FactSet, schema situationfacts.FactSchema) (map[situationfacts.FactID]struct{}, error) {
	if factSet.EpisodeID == "" || factSet.EpisodeRevision == 0 || factSet.Fingerprint == "" || factSet.SchemaFingerprint == "" || factSet.PolicyFingerprint == "" {
		return nil, ErrInvalidFactSet
	}
	if factSet.SchemaFingerprint != situationfacts.SchemaFingerprint() || situationfacts.FactSetFingerprint(factSet) != factSet.Fingerprint {
		return nil, ErrFingerprintMismatch
	}
	if err := factSet.Validate(schema, situationfacts.DefaultPolicy()); err != nil {
		return nil, ErrInvalidFactSet
	}
	known := make(map[situationfacts.FactID]struct{}, len(factSet.Facts))
	for _, fact := range factSet.Facts {
		if err := fact.Validate(schema, situationfacts.DefaultPolicy()); err != nil {
			return nil, ErrInvalidFactSet
		}
		if _, ok := known[fact.ID]; ok {
			return nil, ErrInvalidFactSet
		}
		known[fact.ID] = struct{}{}
	}
	for _, conflict := range factSet.Conflicts {
		for _, id := range conflict.FactIDs {
			if _, ok := known[id]; !ok {
				return nil, ErrInvalidFactSet
			}
		}
	}
	return known, nil
}

func validateHypothesis(hypothesis SituationHypothesis, factSet situationfacts.FactSet, known map[situationfacts.FactID]struct{}, policy Policy) error {
	if hypothesis.ID == "" || hypothesis.EpisodeID != string(factSet.EpisodeID) || hypothesis.Kind == "" || !validHypothesisStatus(hypothesis.Status) || hypothesis.CreatedFromFactRevision == 0 || hypothesis.EvaluatedFactRevision == 0 || hypothesis.Revision == 0 || hypothesis.Fingerprint == "" || hypothesis.Fingerprint != hypothesisFingerprint(hypothesis) {
		return ErrInvalidHypothesis
	}
	if !bounded(hypothesis.SupportPermille) || !bounded(hypothesis.ContradictionPermille) || !bounded(hypothesis.CoveragePermille) || !bounded(hypothesis.PlausibilityPermille) || len(hypothesis.Support)+len(hypothesis.Contradiction) > policy.MaxContributionsPerHypothesis || len(hypothesis.Missing) > policy.MaxMissingRequirements {
		return ErrInvalidHypothesis
	}
	seen := map[string]struct{}{}
	for _, values := range [][]Contribution{hypothesis.Support, hypothesis.Contradiction} {
		for _, contribution := range values {
			if _, ok := seen[contribution.ID]; ok {
				return ErrContributionIDCollision
			}
			seen[contribution.ID] = struct{}{}
			if err := contribution.Validate(policy, known); err != nil {
				return err
			}
		}
	}
	for _, missing := range hypothesis.Missing {
		if err := missing.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateSet(set CompetingHypothesisSet, factSet situationfacts.FactSet, schema HypothesisSchema, policy Policy) error {
	known, err := validateFactSet(factSet, situationfacts.Schema())
	if err != nil {
		return err
	}
	if set.EpisodeID != string(factSet.EpisodeID) || set.FactSetFingerprint != factSet.Fingerprint || set.SchemaFingerprint != schemaFingerprint(schema) || set.PolicyFingerprint != policy.Fingerprint() || set.Revision == 0 || set.Fingerprint == "" || set.Fingerprint != competingSetFingerprint(set) || len(set.Hypotheses) > policy.MaxHypothesesPerEpisode {
		return ErrInvalidHypothesis
	}
	if set.LeadingMarginPermille < 0 || set.LeadingMarginPermille > 1000 {
		return ErrInvalidHypothesis
	}
	if err := schema.Validate(); err != nil {
		return err
	}
	seenIDs := map[HypothesisID]struct{}{}
	for _, hypothesis := range set.Hypotheses {
		if _, ok := seenIDs[hypothesis.ID]; ok {
			return ErrHypothesisIDCollision
		}
		seenIDs[hypothesis.ID] = struct{}{}
		if _, ok := schema.Definition(hypothesis.Kind); !ok {
			return ErrInvalidHypothesis
		}
		if err := validateHypothesis(hypothesis, factSet, known, policy); err != nil {
			return err
		}
	}
	if set.LeadingHypothesisID != "" {
		if _, ok := seenIDs[set.LeadingHypothesisID]; !ok || set.Ambiguous {
			return ErrAmbiguousLeadingHypothesis
		}
	}
	return nil
}

func (h SituationHypothesis) Validate(factSet situationfacts.FactSet, policy Policy) error {
	known, err := validateFactSet(factSet, situationfacts.Schema())
	if err != nil {
		return err
	}
	return validateHypothesis(h, factSet, known, policy)
}

func (s CompetingHypothesisSet) Validate(factSet situationfacts.FactSet, schema HypothesisSchema, policy Policy) error {
	return validateSet(s, factSet, schema, policy)
}

func sortedFactIDs(ids []situationfacts.FactID) []situationfacts.FactID {
	out := append([]situationfacts.FactID(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	result := out[:0]
	for _, id := range out {
		if len(result) == 0 || result[len(result)-1] != id {
			result = append(result, id)
		}
	}
	return result
}
