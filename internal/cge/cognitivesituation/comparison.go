package cognitivesituation

import "sort"

func Compare(previous, current CognitiveSituation) (CognitiveSituationDiff, error) {
	if previous.EpisodeID == "" || current.EpisodeID == "" || previous.EpisodeID != current.EpisodeID {
		return CognitiveSituationDiff{}, ErrInvalidDiff
	}
	diff := CognitiveSituationDiff{
		EpisodeID:                current.EpisodeID,
		PreviousFingerprint:      previous.Fingerprint,
		CurrentFingerprint:       current.Fingerprint,
		PreviousPhase:            previous.Phase,
		CurrentPhase:             current.Phase,
		PhaseChanged:             previous.Phase != current.Phase,
		LeadingHypothesisChanged: previous.Hypotheses.LeadingHypothesisID != current.Hypotheses.LeadingHypothesisID,
		KnowledgeCoverageChanged: previous.Knowledge.OverallCoveragePermille != current.Knowledge.OverallCoveragePermille,
		AdvisoryChanged:          previous.Advisory != current.Advisory,
		CapabilityChanged:        previous.Capability != current.Capability,
		AuthorizationChanged:     previous.Authorization != current.Authorization,
		ReadinessChanged:         previous.RecommendationReadiness.Status != current.RecommendationReadiness.Status || previous.RecommendationReadiness.Ready != current.RecommendationReadiness.Ready,
	}
	if diff.PhaseChanged {
		diff.ReasonCodes = append(diff.ReasonCodes, "phase_changed")
	}
	if diff.LeadingHypothesisChanged {
		diff.ReasonCodes = append(diff.ReasonCodes, "leading_hypothesis_changed")
	}
	if diff.KnowledgeCoverageChanged {
		diff.ReasonCodes = append(diff.ReasonCodes, "knowledge_coverage_changed")
	}
	if diff.AdvisoryChanged {
		diff.ReasonCodes = append(diff.ReasonCodes, "advisory_changed")
	}
	if diff.CapabilityChanged {
		diff.ReasonCodes = append(diff.ReasonCodes, "capability_changed")
	}
	if diff.AuthorizationChanged {
		diff.ReasonCodes = append(diff.ReasonCodes, "authorization_changed")
	}
	if diff.ReadinessChanged {
		diff.ReasonCodes = append(diff.ReasonCodes, "readiness_changed")
	}
	sort.Strings(diff.ReasonCodes)
	diff.Fingerprint = diffFingerprint(diff)
	return diff, nil
}
