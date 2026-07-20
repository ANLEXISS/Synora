package evidencediscrimination

import (
	"reflect"
	"sort"

	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

func ValidateCandidate(c EvidenceCandidate, catalog EvidenceCatalog, policy Policy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if err := ValidateCatalog(catalog); err != nil {
		return err
	}
	if c.ID == "" || c.EpisodeID == "" || c.SourceFactSetFingerprint == "" || c.SourceHypothesisSetFingerprint == "" || !validCandidateKind(c.Kind) || c.Dimension == "" || !validCost(c.CostClass) || !validLatency(c.LatencyClass) || !validSensitivity(c.SensitivityClass) || c.Fingerprint == "" || candidateFingerprint(c) != c.Fingerprint {
		return ErrInvalidCandidate
	}
	if len(c.RequiredFactCodes) > policy.MaxFactCodesPerCandidate || len(c.Outcomes) > policy.MaxOutcomesPerCandidate || len(c.Discriminates) > policy.MaxPairsPerCandidate {
		return ErrInvalidCandidate
	}
	for _, reason := range c.ReasonCodes {
		if forbiddenCandidateTerm(reason) {
			return ErrInvalidCandidate
		}
	}
	seenCodes := map[situationfacts.FactCode]struct{}{}
	for _, code := range c.RequiredFactCodes {
		if !validFactCode(code) {
			return ErrUnknownFactCode
		}
		if _, ok := seenCodes[code]; ok {
			return ErrInvalidCandidate
		}
		seenCodes[code] = struct{}{}
	}
	seenOut := map[string]struct{}{}
	for _, o := range c.Outcomes {
		if o.ID == "" || o.Fingerprint == "" || o.Fingerprint != outcomeFingerprint(o) {
			return ErrInvalidOutcome
		}
		if _, ok := seenOut[o.ID]; ok {
			return ErrOutcomeIDCollision
		}
		seenOut[o.ID] = struct{}{}
		if _, ok := situationfacts.Schema().Definition(o.FactCode); !ok {
			return ErrUnknownFactCode
		}
	}
	seenPairs := map[HypothesisPair]struct{}{}
	for _, pair := range c.Discriminates {
		if pair.First == "" || pair.Second == "" || pair.First >= pair.Second {
			return ErrInvalidHypothesisPair
		}
		if _, ok := seenPairs[pair]; ok {
			return ErrInvalidHypothesisPair
		}
		seenPairs[pair] = struct{}{}
	}
	for _, value := range []int{c.DiscriminationPermille, c.CoverageGainPermille, c.RedundancyPermille, c.UtilityPermille} {
		if value < 0 || value > 1000 {
			return ErrInvalidCandidate
		}
	}
	return nil
}

func ValidateAssessment(a DiscriminationAssessment, catalog EvidenceCatalog, policy Policy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if err := ValidateCatalog(catalog); err != nil {
		return err
	}
	if a.EpisodeID == "" || a.SourceFactSetFingerprint == "" || a.SourceHypothesisSetFingerprint == "" || a.CatalogFingerprint != CatalogFingerprint(catalog) || a.PolicyFingerprint != policy.Fingerprint() || a.Revision == 0 || a.Fingerprint == "" || assessmentFingerprint(a) != a.Fingerprint {
		return ErrInvalidCandidate
	}
	if len(a.Candidates) > policy.MaxCandidates {
		return ErrCandidateLimitReached
	}
	seen := map[EvidenceCandidateID]struct{}{}
	for _, c := range a.Candidates {
		if _, ok := seen[c.ID]; ok {
			return ErrCandidateIDCollision
		}
		seen[c.ID] = struct{}{}
		if err := ValidateCandidate(c, catalog, policy); err != nil {
			return err
		}
	}
	if a.BestCandidateID != "" {
		if _, ok := seen[a.BestCandidateID]; !ok {
			return ErrInvalidCandidate
		}
	}
	return nil
}

func Diff(before, after DiscriminationAssessment) (DiscriminationAssessmentDiff, error) {
	if before.EpisodeID == "" || after.EpisodeID == "" || before.EpisodeID != after.EpisodeID || before.Fingerprint == "" || after.Fingerprint == "" || assessmentFingerprint(before) != before.Fingerprint || assessmentFingerprint(after) != after.Fingerprint {
		return DiscriminationAssessmentDiff{}, ErrInvalidDiff
	}
	d := DiscriminationAssessmentDiff{EpisodeID: before.EpisodeID, BeforeFingerprint: before.Fingerprint, AfterFingerprint: after.Fingerprint}
	left := map[EvidenceCandidateID]EvidenceCandidate{}
	right := map[EvidenceCandidateID]EvidenceCandidate{}
	for _, c := range before.Candidates {
		left[c.ID] = c
	}
	for _, c := range after.Candidates {
		right[c.ID] = c
	}
	for id, c := range right {
		old, ok := left[id]
		if !ok {
			d.Added = append(d.Added, c.Clone())
		} else if old.Fingerprint != c.Fingerprint {
			d.Changed = append(d.Changed, EvidenceCandidateUpdate{Before: old.Clone(), After: c.Clone()})
		}
	}
	for id, c := range left {
		if _, ok := right[id]; !ok {
			d.Removed = append(d.Removed, c.Clone())
		}
	}
	sort.Slice(d.Added, func(i, j int) bool { return d.Added[i].ID < d.Added[j].ID })
	sort.Slice(d.Removed, func(i, j int) bool { return d.Removed[i].ID < d.Removed[j].ID })
	sort.Slice(d.Changed, func(i, j int) bool { return d.Changed[i].After.ID < d.Changed[j].After.ID })
	return d, nil
}

func ReevaluateFromDiff(previousFacts, currentFacts situationfacts.FactSet, factDiff situationfacts.FactSetDiff, previousHypotheses, currentHypotheses situationhypotheses.CompetingHypothesisSet, previousAssessment DiscriminationAssessment, catalog EvidenceCatalog, policy Policy) (DiscriminationAssessment, error) {
	if previousFacts.EpisodeID != currentFacts.EpisodeID || string(previousFacts.EpisodeID) != previousAssessment.EpisodeID || factDiff.EpisodeID != currentFacts.EpisodeID || factDiff.BeforeFingerprint != previousFacts.Fingerprint || factDiff.AfterFingerprint != currentFacts.Fingerprint || factDiff.BeforeEpisodeRevision != previousFacts.EpisodeRevision || factDiff.AfterEpisodeRevision != currentFacts.EpisodeRevision {
		return DiscriminationAssessment{}, ErrInvalidDiff
	}
	if previousAssessment.SourceFactSetFingerprint != previousFacts.Fingerprint || previousAssessment.SourceHypothesisSetFingerprint != previousHypotheses.Fingerprint {
		return DiscriminationAssessment{}, ErrStaleFactSet
	}
	expected, err := situationfacts.Diff(previousFacts, currentFacts)
	if err != nil {
		return DiscriminationAssessment{}, err
	}
	if !sameFactDiff(expected, factDiff) {
		return DiscriminationAssessment{}, ErrFingerprintMismatch
	}
	result, err := Analyze(AnalysisInput{FactSet: currentFacts, HypothesisSet: currentHypotheses, HypothesisSchema: situationhypotheses.Schema(), PreviousAssessment: &previousAssessment}, catalog, policy)
	if err != nil {
		return DiscriminationAssessment{}, err
	}
	return result, nil
}

func sameFactDiff(a, b situationfacts.FactSetDiff) bool {
	if a.EpisodeID != b.EpisodeID || a.BeforeEpisodeRevision != b.BeforeEpisodeRevision || a.AfterEpisodeRevision != b.AfterEpisodeRevision || a.BeforeFingerprint != b.BeforeFingerprint || a.AfterFingerprint != b.AfterFingerprint {
		return false
	}
	return reflect.DeepEqual(a.Added, b.Added) && reflect.DeepEqual(a.Removed, b.Removed) && reflect.DeepEqual(a.Changed, b.Changed) && reflect.DeepEqual(a.ConflictsAdded, b.ConflictsAdded) && reflect.DeepEqual(a.ConflictsRemoved, b.ConflictsRemoved)
}
