package evidencediscrimination

import (
	"sort"

	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

type factIndex struct {
	byCode    map[situationfacts.FactCode][]situationfacts.Fact
	conflicts map[situationfacts.FactCode]bool
	ids       map[situationfacts.FactID]struct{}
}

func makeFactIndex(set situationfacts.FactSet) factIndex {
	out := factIndex{byCode: map[situationfacts.FactCode][]situationfacts.Fact{}, conflicts: map[situationfacts.FactCode]bool{}, ids: map[situationfacts.FactID]struct{}{}}
	for _, f := range set.Facts {
		out.byCode[f.Code] = append(out.byCode[f.Code], f)
		out.ids[f.ID] = struct{}{}
	}
	for _, c := range set.Conflicts {
		out.conflicts[situationfacts.FactCode(c.Code)] = true
	}
	return out
}
func (i factIndex) facts(code situationfacts.FactCode) []situationfacts.Fact { return i.byCode[code] }
func (i factIndex) state(code situationfacts.FactCode) (asserted, unknown, conflicting, present bool) {
	fs := i.facts(code)
	for _, f := range fs {
		present = true
		switch f.Status {
		case situationfacts.StatusAsserted:
			asserted = true
		case situationfacts.StatusUnknown:
			unknown = true
		case situationfacts.StatusConflicting:
			conflicting = true
		}
	}
	if i.conflicts[code] {
		conflicting = true
	}
	return
}

func Analyze(input AnalysisInput, catalog EvidenceCatalog, policy Policy) (DiscriminationAssessment, error) {
	if err := policy.Validate(); err != nil {
		return DiscriminationAssessment{}, err
	}
	if err := ValidateCatalog(catalog); err != nil {
		return DiscriminationAssessment{}, err
	}
	if err := input.HypothesisSchema.Validate(); err != nil {
		return DiscriminationAssessment{}, ErrInvalidHypothesisSet
	}
	if err := input.FactSet.Validate(situationfacts.Schema(), situationfacts.DefaultPolicy()); err != nil {
		return DiscriminationAssessment{}, ErrInvalidFactSet
	}
	if err := input.HypothesisSet.Validate(input.FactSet, input.HypothesisSchema, situationhypotheses.DefaultPolicy()); err != nil {
		return DiscriminationAssessment{}, ErrInvalidHypothesisSet
	}
	if string(input.FactSet.EpisodeID) == "" || input.HypothesisSet.EpisodeID != string(input.FactSet.EpisodeID) {
		return DiscriminationAssessment{}, ErrInvalidHypothesisSet
	}
	for _, hypothesis := range input.HypothesisSet.Hypotheses {
		if hypothesis.ID != situationhypotheses.HypothesisIDFor(input.HypothesisSet.EpisodeID, hypothesis.Kind) {
			return DiscriminationAssessment{}, ErrUnknownHypothesisReference
		}
	}
	if input.PreviousAssessment != nil {
		if input.PreviousAssessment.EpisodeID != string(input.FactSet.EpisodeID) {
			return DiscriminationAssessment{}, ErrStaleFactSet
		}
		if input.PreviousAssessment.CatalogFingerprint != CatalogFingerprint(catalog) || input.PreviousAssessment.PolicyFingerprint != policy.Fingerprint() {
			return DiscriminationAssessment{}, ErrStaleFactSet
		}
		if input.PreviousAssessment.Fingerprint == "" || assessmentFingerprint(*input.PreviousAssessment) != input.PreviousAssessment.Fingerprint {
			return DiscriminationAssessment{}, ErrFingerprintMismatch
		}
	}
	active := activeHypotheses(input.HypothesisSet, policy)
	pairs := unresolvedPairs(active, input.HypothesisSet, policy)
	idx := makeFactIndex(input.FactSet)
	assessment := DiscriminationAssessment{EpisodeID: string(input.FactSet.EpisodeID), SourceFactSetFingerprint: input.FactSet.Fingerprint, SourceHypothesisSetFingerprint: input.HypothesisSet.Fingerprint, CatalogFingerprint: CatalogFingerprint(catalog), PolicyFingerprint: policy.Fingerprint(), UnresolvedPairCount: len(pairs), Revision: 1}
	if input.PreviousAssessment != nil {
		assessment.Revision = input.PreviousAssessment.Revision + 1
	}
	for _, def := range sortCatalog(catalog).Definitions {
		if def.DefaultSensitivityClass == SensitivityHigh && !policy.IncludeHighSensitivityCandidates {
			continue
		}
		if !applicable(def, active) {
			continue
		}
		candidate, ok := buildCandidate(def, input.FactSet, input.HypothesisSet, active, pairs, idx, policy)
		if !ok {
			continue
		}
		assessment.Candidates = append(assessment.Candidates, candidate)
	}
	if len(assessment.Candidates) > policy.MaxCandidates {
		return DiscriminationAssessment{}, ErrCandidateLimitReached
	}
	sort.Slice(assessment.Candidates, func(i, j int) bool { return candidateLess(assessment.Candidates[i], assessment.Candidates[j]) })
	assessment.CoveredPairCount = coveredPairCount(assessment.Candidates)
	assessment.AmbiguityRelevant = input.HypothesisSet.Ambiguous || len(pairs) > 0
	assessment.EvidenceUseful = len(assessment.Candidates) > 0
	chooseBest(&assessment, policy)
	assessment.Fingerprint = assessmentFingerprint(assessment)
	return assessment, nil
}

func activeHypotheses(set situationhypotheses.CompetingHypothesisSet, policy Policy) []situationhypotheses.SituationHypothesis {
	out := make([]situationhypotheses.SituationHypothesis, 0, len(set.Hypotheses))
	for _, h := range set.Hypotheses {
		if h.Status != situationhypotheses.StatusInvalidated && h.PlausibilityPermille >= policy.MinRelevantHypothesisPlausibilityPermille {
			out = append(out, h.Clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func unresolvedPairs(active []situationhypotheses.SituationHypothesis, set situationhypotheses.CompetingHypothesisSet, p Policy) []HypothesisPair {
	var out []HypothesisPair
	for i := range active {
		for j := i + 1; j < len(active); j++ {
			a, b := active[i], active[j]
			diff := a.PlausibilityPermille - b.PlausibilityPermille
			if diff < 0 {
				diff = -diff
			}
			if set.Ambiguous || diff <= p.MaxResolvedMarginPermille || a.CoveragePermille < 600 || b.CoveragePermille < 600 || a.Status == situationhypotheses.StatusCandidate || b.Status == situationhypotheses.StatusCandidate || a.Status == situationhypotheses.StatusInsufficientInformation || b.Status == situationhypotheses.StatusInsufficientInformation {
				pair, _ := canonicalPair(a.ID, b.ID)
				out = append(out, pair)
			}
		}
	}
	return out
}
func applicable(def EvidenceCandidateDefinition, active []situationhypotheses.SituationHypothesis) bool {
	for _, h := range active {
		for _, kind := range def.ApplicableHypothesisKinds {
			if h.Kind == kind {
				return true
			}
		}
	}
	return false
}

func factEffect(out PotentialOutcome, kind situationhypotheses.HypothesisKind) int {
	for _, v := range out.Supports {
		if v == kind {
			return 1
		}
	}
	for _, v := range out.Contradicts {
		if v == kind {
			return -1
		}
	}
	return 0
}
func buildCandidate(def EvidenceCandidateDefinition, set situationfacts.FactSet, hset situationhypotheses.CompetingHypothesisSet, active []situationhypotheses.SituationHypothesis, pairs []HypothesisPair, idx factIndex, p Policy) (EvidenceCandidate, bool) {
	c := EvidenceCandidate{EpisodeID: string(set.EpisodeID), Kind: def.Kind, Dimension: def.Dimension, SourceFactSetFingerprint: set.Fingerprint, SourceHypothesisSetFingerprint: hset.Fingerprint, CostClass: def.DefaultCostClass, LatencyClass: def.DefaultLatencyClass, SensitivityClass: def.DefaultSensitivityClass, ReasonCodes: []string{"unresolved_pair_or_missing_dimension"}}
	c.RequiredFactCodes = uniqueCodes(def.RequiredFactCodes)
	if len(c.RequiredFactCodes) > p.MaxFactCodesPerCandidate {
		return EvidenceCandidate{}, false
	}
	for _, od := range def.Outcomes {
		if len(c.Outcomes) >= p.MaxOutcomesPerCandidate {
			return EvidenceCandidate{}, false
		}
		o := PotentialOutcome{FactCode: od.FactCode, Operator: od.Operator, DescriptionCode: od.DescriptionCode}
		if od.Value != nil {
			v := od.Value.Clone()
			o.Value = &v
		}
		o.Supports = uniqueKinds(od.Supports)
		o.Contradicts = uniqueKinds(od.Contradicts)
		o.ReducesMissingFor = uniqueKinds(od.ReducesMissingFor)
		o.ID = outcomeIDFor(candidateIDFor(string(set.EpisodeID), def, nil), o)
		o.Fingerprint = outcomeFingerprint(o)
		c.Outcomes = append(c.Outcomes, o)
	}
	c.ID = candidateIDFor(string(set.EpisodeID), def, pairs)
	for i := range c.Outcomes {
		c.Outcomes[i].ID = outcomeIDFor(c.ID, c.Outcomes[i])
		c.Outcomes[i].Fingerprint = outcomeFingerprint(c.Outcomes[i])
	}
	for _, pair := range pairs {
		first, second := findHyp(active, pair.First), findHyp(active, pair.Second)
		if first == nil || second == nil {
			continue
		}
		covered := false
		contrast := 0
		for _, o := range c.Outcomes {
			a, b := factEffect(o, first.Kind), factEffect(o, second.Kind)
			if a != b {
				covered = true
				if a != 0 && b != 0 {
					contrast = max(contrast, 1000)
				} else {
					contrast = max(contrast, 600)
				}
			}
		}
		if covered {
			c.Discriminates = append(c.Discriminates, pair)
			if contrast > 0 {
				c.ReasonCodes = append(c.ReasonCodes, "outcome_effects_differ")
			}
		}
	}
	if len(c.Discriminates) > p.MaxPairsPerCandidate {
		return EvidenceCandidate{}, false
	}
	for _, o := range c.Outcomes {
		for _, kind := range o.Supports {
			if h := findKind(active, kind); h != nil {
				c.SupportingHypothesisIDs = append(c.SupportingHypothesisIDs, h.ID)
			}
		}
		for _, kind := range o.Contradicts {
			if h := findKind(active, kind); h != nil {
				c.WeakeningHypothesisIDs = append(c.WeakeningHypothesisIDs, h.ID)
			}
		}
	}
	c.SupportingHypothesisIDs = uniqueIDs(c.SupportingHypothesisIDs)
	c.WeakeningHypothesisIDs = uniqueIDs(c.WeakeningHypothesisIDs)
	score := scoreCandidate(c, active, pairs, idx, p)
	c.DiscriminationPermille = score.PairSeparationPermille
	c.CoverageGainPermille = score.CoverageGainPermille
	c.RedundancyPermille = score.RedundancyPermille
	c.UtilityPermille = score.UtilityPermille
	if len(c.Discriminates) == 0 && c.CoverageGainPermille < p.MinCoverageGainPermille {
		return EvidenceCandidate{}, false
	}
	if c.DiscriminationPermille < p.MinDiscriminationPermille && c.CoverageGainPermille < p.MinCoverageGainPermille {
		return EvidenceCandidate{}, false
	}
	if c.UtilityPermille < p.MinUtilityPermille {
		return EvidenceCandidate{}, false
	}
	c.ReasonCodes = uniqueReasonCodes(c.ReasonCodes)
	c.Fingerprint = candidateFingerprint(c)
	return c, true
}

func scoreCandidate(c EvidenceCandidate, active []situationhypotheses.SituationHypothesis, pairs []HypothesisPair, idx factIndex, p Policy) DiscriminationScore {
	covered := len(c.Discriminates)
	pair := 0
	relevantPairs := 0
	for _, candidatePair := range pairs {
		first, second := findHyp(active, candidatePair.First), findHyp(active, candidatePair.Second)
		if first == nil || second == nil {
			continue
		}
		relevant := false
		for _, outcome := range c.Outcomes {
			if factEffect(outcome, first.Kind) != 0 || factEffect(outcome, second.Kind) != 0 {
				relevant = true
				break
			}
		}
		if relevant {
			relevantPairs++
		}
	}
	if relevantPairs > 0 {
		pair = covered * 1000 / relevantPairs
	}
	coverage := 0
	targets := 0
	for _, h := range active {
		if !candidateHasKind(c, h.Kind) {
			continue
		}
		targets++
		best := 0
		for _, m := range h.Missing {
			if containsCode(c.RequiredFactCodes, m.RequiredFactCode) && m.ImportancePermille > best {
				best = min(1000, m.ImportancePermille*2)
			}
		}
		if best == 0 {
			for _, code := range c.RequiredFactCodes {
				_, unknown, conflicting, present := idx.state(code)
				if conflicting {
					best = max(best, 1000)
				} else if unknown {
					best = max(best, 900)
				} else if !present {
					best = max(best, 500)
				}
			}
		}
		coverage += best
	}
	if targets > 0 {
		coverage /= targets
	}
	red := redundancy(c.RequiredFactCodes, idx)
	if covered > 0 && red > 300 {
		// A currently asserted dimension can still be useful when its
		// potential outcomes affect competing explanations differently.
		red = 300
	}
	contrast := 0
	if covered > 0 {
		contrast = 1000
	}
	base := (pair*p.DiscriminationWeightPermille + coverage*p.CoverageWeightPermille) / 1000
	utility := clamp(base - red*p.RedundancyPenaltyPermille/1000)
	return DiscriminationScore{PairSeparationPermille: pair, CoverageGainPermille: coverage, OutcomeContrastPermille: contrast, RedundancyPermille: red, UtilityPermille: utility}
}
func redundancy(codes []situationfacts.FactCode, idx factIndex) int {
	if len(codes) == 0 {
		return 0
	}
	total := 0
	for _, code := range codes {
		asserted, unknown, conflicting, present := idx.state(code)
		if conflicting || unknown {
			continue
		}
		if asserted {
			total += 1000
		} else if present {
			total += 500
		}
	}
	return total / len(codes)
}
func candidateHasKind(c EvidenceCandidate, k situationhypotheses.HypothesisKind) bool {
	for _, o := range c.Outcomes {
		for _, v := range o.Supports {
			if v == k {
				return true
			}
		}
		for _, v := range o.Contradicts {
			if v == k {
				return true
			}
		}
		for _, v := range o.ReducesMissingFor {
			if v == k {
				return true
			}
		}
	}
	return false
}
func containsCode(codes []situationfacts.FactCode, code situationfacts.FactCode) bool {
	for _, v := range codes {
		if v == code {
			return true
		}
	}
	return false
}

func uniqueReasonCodes(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	result := out[:0]
	for _, value := range out {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
func findHyp(active []situationhypotheses.SituationHypothesis, id situationhypotheses.HypothesisID) *situationhypotheses.SituationHypothesis {
	for i := range active {
		if active[i].ID == id {
			return &active[i]
		}
	}
	return nil
}
func findKind(active []situationhypotheses.SituationHypothesis, k situationhypotheses.HypothesisKind) *situationhypotheses.SituationHypothesis {
	for i := range active {
		if active[i].Kind == k {
			return &active[i]
		}
	}
	return nil
}
func uniqueCodes(values []situationfacts.FactCode) []situationfacts.FactCode {
	out := append([]situationfacts.FactCode(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	r := out[:0]
	for _, v := range out {
		if len(r) == 0 || r[len(r)-1] != v {
			r = append(r, v)
		}
	}
	return r
}
func uniqueKinds(values []situationhypotheses.HypothesisKind) []situationhypotheses.HypothesisKind {
	out := append([]situationhypotheses.HypothesisKind(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	r := out[:0]
	for _, v := range out {
		if len(r) == 0 || r[len(r)-1] != v {
			r = append(r, v)
		}
	}
	return r
}
func uniqueIDs(values []situationhypotheses.HypothesisID) []situationhypotheses.HypothesisID {
	out := append([]situationhypotheses.HypothesisID(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	r := out[:0]
	for _, v := range out {
		if len(r) == 0 || r[len(r)-1] != v {
			r = append(r, v)
		}
	}
	return r
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func candidateLess(a, b EvidenceCandidate) bool {
	if a.UtilityPermille != b.UtilityPermille {
		return a.UtilityPermille > b.UtilityPermille
	}
	if a.DiscriminationPermille != b.DiscriminationPermille {
		return a.DiscriminationPermille > b.DiscriminationPermille
	}
	if a.CoverageGainPermille != b.CoverageGainPermille {
		return a.CoverageGainPermille > b.CoverageGainPermille
	}
	if a.RedundancyPermille != b.RedundancyPermille {
		return a.RedundancyPermille < b.RedundancyPermille
	}
	if a.SensitivityClass != b.SensitivityClass {
		return a.SensitivityClass < b.SensitivityClass
	}
	if a.CostClass != b.CostClass {
		return a.CostClass < b.CostClass
	}
	if a.LatencyClass != b.LatencyClass {
		return a.LatencyClass < b.LatencyClass
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.ID < b.ID
}
func coveredPairCount(values []EvidenceCandidate) int {
	seen := map[HypothesisPair]struct{}{}
	for _, c := range values {
		for _, p := range c.Discriminates {
			seen[p] = struct{}{}
		}
	}
	return len(seen)
}
func chooseBest(a *DiscriminationAssessment, p Policy) {
	if len(a.Candidates) == 0 {
		return
	}
	first := a.Candidates[0]
	if first.UtilityPermille < p.MinUtilityPermille {
		return
	}
	if len(a.Candidates) == 1 {
		a.BestCandidateID = first.ID
		return
	}
	second := a.Candidates[1]
	if first.UtilityPermille-second.UtilityPermille >= p.MinBestCandidateMarginPermille {
		a.BestCandidateID = first.ID
	} else {
		a.BestCandidateID = ""
	}
}
