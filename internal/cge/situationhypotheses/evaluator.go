package situationhypotheses

import (
	"sort"
	"strings"

	"synora/internal/cge/situationfacts"
)

type ruleMatch struct {
	matched bool
	facts   []situationfacts.Fact
}

func Evaluate(input EvaluationInput, schema HypothesisSchema, policy Policy) (EvaluationResult, error) {
	if err := policy.Validate(); err != nil {
		return EvaluationResult{}, err
	}
	if err := schema.Validate(); err != nil {
		return EvaluationResult{}, err
	}
	known, err := validateFactSet(input.FactSet, situationfacts.Schema())
	if err != nil {
		return EvaluationResult{}, err
	}
	if input.PreviousSet != nil {
		if input.PreviousSet.Fingerprint != competingSetFingerprint(*input.PreviousSet) || input.PreviousSet.EpisodeID != string(input.FactSet.EpisodeID) || input.PreviousSet.FactSetFingerprint == "" {
			return EvaluationResult{}, ErrFingerprintMismatch
		}
	}
	facts := append([]situationfacts.Fact(nil), input.FactSet.Facts...)
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	result := CompetingHypothesisSet{EpisodeID: string(input.FactSet.EpisodeID), FactSetFingerprint: input.FactSet.Fingerprint, FactRegistryRevision: input.FactSet.EpisodeRevision, SchemaFingerprint: schemaFingerprint(schema), PolicyFingerprint: policy.Fingerprint(), Revision: 1}
	previousByKind := map[HypothesisKind]SituationHypothesis{}
	if input.PreviousSet != nil {
		result.Revision = input.PreviousSet.Revision + 1
		for _, hypothesis := range input.PreviousSet.Hypotheses {
			previousByKind[hypothesis.Kind] = hypothesis
		}
	}
	activeKinds := map[HypothesisKind]struct{}{}
	for _, definition := range schema.Definitions {
		hypothesis := evaluateDefinition(definition, input.FactSet, facts, known, policy, result.Revision)
		if hypothesis == nil {
			continue
		}
		if previous, ok := previousByKind[definition.Kind]; ok {
			hypothesis.ID = previous.ID
			hypothesis.CreatedFromFactRevision = previous.CreatedFromFactRevision
			hypothesis.Revision = previous.Revision + 1
			hypothesis.Status = lifecycleStatus(previous.Status, hypothesis.Status, hypothesis.PlausibilityPermille, hypothesis.SupportPermille, hypothesis.ContradictionPermille)
		}
		refreshHypothesisFingerprint(hypothesis)
		result.Hypotheses = append(result.Hypotheses, *hypothesis)
		activeKinds[definition.Kind] = struct{}{}
	}
	if input.PreviousSet != nil {
		for _, previous := range input.PreviousSet.Hypotheses {
			if _, ok := activeKinds[previous.Kind]; ok || previous.Status == StatusInvalidated {
				continue
			}
			invalidated := previous.Clone()
			invalidated.Status = StatusInvalidated
			invalidated.EvaluatedFactRevision = input.FactSet.EpisodeRevision
			invalidated.Revision = previous.Revision + 1
			invalidated.Support = nil
			invalidated.Contradiction = nil
			invalidated.Missing = nil
			invalidated.SupportPermille, invalidated.ContradictionPermille, invalidated.CoveragePermille, invalidated.PlausibilityPermille = 0, 0, 0, 0
			refreshHypothesisFingerprint(&invalidated)
			result.Hypotheses = append(result.Hypotheses, invalidated)
		}
	}
	if len(result.Hypotheses) > policy.MaxHypothesesPerEpisode {
		return EvaluationResult{}, ErrHypothesisLimitReached
	}
	sortHypotheses(result.Hypotheses)
	result.InsufficientInformation = hasInsufficient(result.Hypotheses)
	assignLeading(&result, policy)
	result.Fingerprint = competingSetFingerprint(result)
	result.Hypotheses = cloneHypotheses(result.Hypotheses)
	return EvaluationResult{Set: result, CompetingSet: result.Clone(), HypothesisSet: result.Clone(), Mode: EvaluationModeFull}, nil
}

func evaluateDefinition(definition HypothesisDefinition, factSet situationfacts.FactSet, facts []situationfacts.Fact, known map[situationfacts.FactID]struct{}, policy Policy, revision uint64) *SituationHypothesis {
	hypothesis := &SituationHypothesis{ID: hypothesisIDFor(string(factSet.EpisodeID), definition.Kind), EpisodeID: string(factSet.EpisodeID), Kind: definition.Kind, CreatedFromFactRevision: factSet.EpisodeRevision, EvaluatedFactRevision: factSet.EpisodeRevision, Revision: revision}
	for _, rule := range definition.SupportRules {
		if match := matchRule(rule, facts, factSet); match.matched && len(match.facts) > 0 {
			contribution := makeContribution(rule, ContributionSupport, match.facts, factSet.Fingerprint)
			hypothesis.Support = appendUniqueContribution(hypothesis.Support, contribution)
		}
	}
	for _, rule := range definition.ContradictionRules {
		if match := matchRule(rule, facts, factSet); match.matched && len(match.facts) > 0 {
			contribution := makeContribution(rule, ContributionContradiction, match.facts, factSet.Fingerprint)
			hypothesis.Contradiction = appendUniqueContribution(hypothesis.Contradiction, contribution)
		}
	}
	for _, rule := range definition.MissingRules {
		if !hasFactCode(rule.RequiredFactCode, rule.RequiredScope, facts) {
			hypothesis.Missing = append(hypothesis.Missing, MissingRequirement{RuleID: rule.ID, RequiredFactCode: rule.RequiredFactCode, ReasonCode: rule.ReasonCode, ImportancePermille: rule.ImportancePermille})
		}
	}
	hypothesis.Support = canonicalContributions(hypothesis.Support)
	hypothesis.Contradiction = canonicalContributions(hypothesis.Contradiction)
	hypothesis.Missing = canonicalMissing(hypothesis.Missing)
	if len(hypothesis.Support)+len(hypothesis.Contradiction) > policy.MaxContributionsPerHypothesis || len(hypothesis.Missing) > policy.MaxMissingRequirements {
		return nil
	}
	hypothesis.SupportPermille = contributionScore(hypothesis.Support)
	hypothesis.ContradictionPermille = contributionScore(hypothesis.Contradiction)
	hypothesis.CoveragePermille = coverageScore(hypothesis.Missing)
	unknownCount := 0
	for _, fact := range facts {
		if fact.Status == situationfacts.StatusUnknown {
			unknownCount++
		}
	}
	if penalty := unknownCount * 100; penalty > 0 {
		if penalty >= hypothesis.CoveragePermille {
			hypothesis.CoveragePermille = 0
		} else {
			hypothesis.CoveragePermille -= penalty
		}
	}
	hypothesis.PlausibilityPermille = plausibilityScore(hypothesis.SupportPermille, hypothesis.ContradictionPermille, hypothesis.CoveragePermille)
	hypothesis.Status = statusFor(hypothesis, policy)
	if hypothesis.Kind != KindInsufficientInformation && len(hypothesis.Support) == 0 && len(hypothesis.Contradiction) == 0 {
		return nil
	}
	if hypothesis.Kind == KindInsufficientInformation && len(hypothesis.Support) == 0 {
		return nil
	}
	refreshHypothesisFingerprint(hypothesis)
	return hypothesis
}

func matchRule(rule EvidenceRule, facts []situationfacts.Fact, factSet situationfacts.FactSet) ruleMatch {
	if rule.Operator == OperatorConflictExists {
		var matched []situationfacts.Fact
		for _, conflict := range factSet.Conflicts {
			for _, id := range conflict.FactIDs {
				for _, fact := range facts {
					if fact.ID == id {
						matched = append(matched, fact)
					}
				}
			}
		}
		return ruleMatch{matched: len(matched) > 0, facts: uniqueFacts(matched)}
	}
	matched := make([]situationfacts.Fact, 0, 2)
	for _, fact := range facts {
		if fact.Code != rule.FactCode || fact.Scope != rule.Scope || fact.Status == situationfacts.StatusUnknown && rule.Operator != OperatorStatusIs || !operatorMatches(rule, fact) {
			continue
		}
		matched = append(matched, fact)
	}
	if rule.Operator == OperatorNotExists {
		return ruleMatch{matched: len(matched) == 0}
	}
	return ruleMatch{matched: len(matched) > 0, facts: uniqueFacts(matched)}
}

func operatorMatches(rule EvidenceRule, fact situationfacts.Fact) bool {
	if rule.Operator == OperatorExists {
		switch fact.Value.Kind {
		case situationfacts.ValueString, situationfacts.ValueRef:
			return fact.Value.StringValueOrRef() != ""
		case situationfacts.ValueStringSet:
			return len(fact.Value.StringSetValue) > 0
		case situationfacts.ValueStringList:
			return len(fact.Value.StringListValue) > 0
		case situationfacts.ValueBool:
			return fact.Value.BoolValue
		default:
			return true
		}
	}
	if rule.Operator == OperatorStatusIs {
		return fact.Status == rule.ExpectedStatus
	}
	if rule.ExpectedValue == nil {
		return false
	}
	value := fact.Value
	want := *rule.ExpectedValue
	switch rule.Operator {
	case OperatorEquals:
		return value.Canonical() == want.Canonical()
	case OperatorNotEquals:
		return value.Canonical() != want.Canonical()
	case OperatorGreaterThan, OperatorGreaterOrEqual, OperatorLessThan:
		left, right, ok := numericValue(value), numericValue(want), numericComparable(value, want)
		if !ok {
			return false
		}
		if rule.Operator == OperatorGreaterThan {
			return left > right
		}
		if rule.Operator == OperatorGreaterOrEqual {
			return left >= right
		}
		return left < right
	case OperatorContains:
		return value.Kind == situationfacts.ValueString && strings.Contains(value.StringValue, want.StringValue)
	case OperatorSetContains:
		if value.Kind != situationfacts.ValueStringSet || want.Kind != situationfacts.ValueString {
			return false
		}
		for _, item := range value.StringSetValue {
			if item == want.StringValue {
				return true
			}
		}
	}
	return false
}

func numericComparable(left, right situationfacts.FactValue) bool {
	return (left.Kind == situationfacts.ValueInt || left.Kind == situationfacts.ValueDurationMS || left.Kind == situationfacts.ValuePermille) && left.Kind == right.Kind
}

func numericValue(value situationfacts.FactValue) int64 {
	if value.Kind == situationfacts.ValuePermille {
		return value.PermilleValue
	}
	return value.IntValue
}

func hasFactCode(code situationfacts.FactCode, scope situationfacts.FactScope, facts []situationfacts.Fact) bool {
	for _, fact := range facts {
		if fact.Code == code && fact.Scope == scope && fact.Status != situationfacts.StatusUnknown {
			return true
		}
	}
	return false
}

func uniqueFacts(values []situationfacts.Fact) []situationfacts.Fact {
	seen := map[situationfacts.FactID]struct{}{}
	out := make([]situationfacts.Fact, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value.ID]; ok {
			continue
		}
		seen[value.ID] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func makeContribution(rule EvidenceRule, role ContributionRole, facts []situationfacts.Fact, fingerprint string) Contribution {
	ids := make([]situationfacts.FactID, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.ID)
	}
	ids = sortedFactIDs(ids)
	value := Contribution{Role: role, RuleID: rule.ID, ReasonCode: rule.ReasonCode, FactIDs: ids, WeightPermille: rule.WeightPermille, FactSetFingerprint: fingerprint}
	value.ID = contributionIDFor(value)
	return value
}

func appendUniqueContribution(values []Contribution, value Contribution) []Contribution {
	for _, existing := range values {
		if existing.ID == value.ID {
			return values
		}
	}
	return append(values, value)
}

func contributionScore(values []Contribution) int {
	best := map[situationfacts.FactID]int{}
	for _, contribution := range values {
		for _, id := range contribution.FactIDs {
			if contribution.WeightPermille > best[id] {
				best[id] = contribution.WeightPermille
			}
		}
	}
	if len(best) == 0 {
		return 0
	}
	total := 0
	for _, weight := range best {
		total += weight
	}
	if total > 1000 {
		return 1000
	}
	return total
}

func coverageScore(values []MissingRequirement) int {
	missing := 0
	for _, value := range values {
		missing += value.ImportancePermille
	}
	if missing > 1000 {
		missing = 1000
	}
	return 1000 - missing
}

func plausibilityScore(support, contradiction, coverage int) int {
	value := support * (1000 - contradiction) / 1000
	return value * coverage / 1000
}

func statusFor(hypothesis *SituationHypothesis, policy Policy) HypothesisStatus {
	if hypothesis.CoveragePermille < policy.MinCandidateCoveragePermille {
		return StatusInsufficientInformation
	}
	if hypothesis.ContradictionPermille >= policy.ContradictedThresholdPermille {
		return StatusContradicted
	}
	if hypothesis.PlausibilityPermille >= policy.MinSupportedPlausibilityPermille {
		return StatusSupported
	}
	if hypothesis.ContradictionPermille > 0 {
		return StatusWeakened
	}
	return StatusCandidate
}

func refreshHypothesisFingerprint(hypothesis *SituationHypothesis) {
	hypothesis.Fingerprint = hypothesisFingerprint(*hypothesis)
}

func sortHypotheses(values []SituationHypothesis) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].PlausibilityPermille != values[j].PlausibilityPermille {
			return values[i].PlausibilityPermille > values[j].PlausibilityPermille
		}
		if values[i].CoveragePermille != values[j].CoveragePermille {
			return values[i].CoveragePermille > values[j].CoveragePermille
		}
		if values[i].ContradictionPermille != values[j].ContradictionPermille {
			return values[i].ContradictionPermille < values[j].ContradictionPermille
		}
		if values[i].Kind != values[j].Kind {
			return values[i].Kind < values[j].Kind
		}
		return values[i].ID < values[j].ID
	})
}

func cloneHypotheses(values []SituationHypothesis) []SituationHypothesis {
	out := make([]SituationHypothesis, len(values))
	for i, value := range values {
		out[i] = value.Clone()
	}
	return out
}

func hasInsufficient(values []SituationHypothesis) bool {
	for _, value := range values {
		if value.Kind == KindInsufficientInformation && value.Status != StatusInvalidated {
			return true
		}
	}
	return false
}

func assignLeading(set *CompetingHypothesisSet, policy Policy) {
	set.LeadingHypothesisID = ""
	set.LeadingMarginPermille = 0
	candidates := make([]SituationHypothesis, 0, len(set.Hypotheses))
	for _, hypothesis := range set.Hypotheses {
		if hypothesis.Kind != KindInsufficientInformation && hypothesis.Status != StatusInvalidated && hypothesis.PlausibilityPermille > 0 {
			candidates = append(candidates, hypothesis)
		}
	}
	sortHypotheses(candidates)
	if len(candidates) == 0 {
		return
	}
	margin := candidates[0].PlausibilityPermille
	if len(candidates) > 1 {
		margin -= candidates[1].PlausibilityPermille
	}
	if margin < 0 {
		margin = 0
	}
	set.LeadingMarginPermille = margin
	set.Ambiguous = len(candidates) > 1 && margin < policy.MinLeadingMarginPermille
	if !set.Ambiguous && candidates[0].PlausibilityPermille >= policy.MinSupportedPlausibilityPermille {
		set.LeadingHypothesisID = candidates[0].ID
	}
}
