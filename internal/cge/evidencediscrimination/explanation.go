package evidencediscrimination

import (
	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

func Explain(candidate EvidenceCandidate) (EvidenceExplanation, error) {
	if candidate.ID == "" || candidate.Fingerprint == "" {
		return EvidenceExplanation{}, ErrInvalidExplanation
	}
	out := EvidenceExplanation{CandidateID: candidate.ID, Kind: candidate.Kind, Dimension: candidate.Dimension, SummaryCode: "descriptive_dimension_confirmation", HypothesisPairs: append([]HypothesisPair(nil), candidate.Discriminates...), RequiredFactCodes: append([]situationfacts.FactCode(nil), candidate.RequiredFactCodes...), DiscriminationPermille: candidate.DiscriminationPermille, CoverageGainPermille: candidate.CoverageGainPermille, RedundancyPermille: candidate.RedundancyPermille, UtilityPermille: candidate.UtilityPermille, CostClass: candidate.CostClass, LatencyClass: candidate.LatencyClass, SensitivityClass: candidate.SensitivityClass, NotACommand: true, NotAProbability: true, NoSecurityMeaning: true}
	for _, o := range candidate.Outcomes {
		out.OutcomeExplanations = append(out.OutcomeExplanations, OutcomeExplanation{OutcomeID: o.ID, DescriptionCode: o.DescriptionCode, FactCode: o.FactCode, Supports: append([]situationhypotheses.HypothesisKind(nil), o.Supports...), Contradicts: append([]situationhypotheses.HypothesisKind(nil), o.Contradicts...), ReducesMissingFor: append([]situationhypotheses.HypothesisKind(nil), o.ReducesMissingFor...)})
	}
	return out, nil
}
