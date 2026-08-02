package evidencediscrimination

import (
	"sort"

	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

func Plan(factSet situationfacts.FactSet, hypotheses situationhypotheses.CompetingHypothesisSet, current RegistrySnapshot, catalog EvidenceCatalog, policy Policy) (EvidencePlan, error) {
	if err := policy.Validate(); err != nil {
		return EvidencePlan{}, err
	}
	if err := ValidateCatalog(catalog); err != nil {
		return EvidencePlan{}, err
	}
	var previous *DiscriminationAssessment
	if index, ok := current.EpisodeIndex[string(factSet.EpisodeID)]; ok && index >= 0 && index < len(current.Assessments) {
		value := current.Assessments[index].Clone()
		previous = &value
	}
	if current.Revision == 0 && previous != nil {
		return EvidencePlan{}, ErrInvalidPlan
	}
	if current.Revision > 0 && (current.CatalogFingerprint != CatalogFingerprint(catalog) || current.PolicyFingerprint != policy.Fingerprint()) {
		return EvidencePlan{}, ErrFingerprintMismatch
	}
	result, err := Analyze(AnalysisInput{FactSet: factSet, HypothesisSet: hypotheses, HypothesisSchema: situationhypotheses.Schema(), PreviousAssessment: previous}, catalog, policy)
	if err != nil {
		return EvidencePlan{}, err
	}
	plan := EvidencePlan{EpisodeID: string(factSet.EpisodeID), SourceAssessmentFingerprint: "", SourceRegistryRevision: current.Revision, ResultingAssessment: result, ReasonCodes: []string{"descriptive_evidence_plan"}}
	if previous != nil {
		plan.SourceAssessmentFingerprint = previous.Fingerprint
		plan.SourceAssessment = *previous
	}
	if previous == nil {
		plan.Creates = cloneCandidates(result.Candidates)
	} else {
		plan.Updates, plan.Removes = assessmentChanges(*previous, result)
	}
	return plan, nil
}

func BuildPlan(factSet situationfacts.FactSet, hypotheses situationhypotheses.CompetingHypothesisSet, current RegistrySnapshot, catalog EvidenceCatalog, policy Policy) (EvidencePlan, error) {
	return Plan(factSet, hypotheses, current, catalog, policy)
}

func assessmentChanges(before, after DiscriminationAssessment) ([]EvidenceCandidateUpdate, []EvidenceCandidateRemoval) {
	left := map[EvidenceCandidateID]EvidenceCandidate{}
	right := map[EvidenceCandidateID]EvidenceCandidate{}
	for _, c := range before.Candidates {
		left[c.ID] = c
	}
	for _, c := range after.Candidates {
		right[c.ID] = c
	}
	var updates []EvidenceCandidateUpdate
	var removes []EvidenceCandidateRemoval
	for id, c := range right {
		if old, ok := left[id]; ok && old.Fingerprint != c.Fingerprint {
			updates = append(updates, EvidenceCandidateUpdate{Before: old.Clone(), After: c.Clone()})
		}
	}
	for id, c := range left {
		if _, ok := right[id]; !ok {
			removes = append(removes, EvidenceCandidateRemoval{Candidate: c.Clone(), ReasonCode: "candidate_no_longer_useful"})
		}
	}
	sort.Slice(updates, func(i, j int) bool { return updates[i].After.ID < updates[j].After.ID })
	sort.Slice(removes, func(i, j int) bool { return removes[i].Candidate.ID < removes[j].Candidate.ID })
	return updates, removes
}
