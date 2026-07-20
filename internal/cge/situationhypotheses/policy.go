package situationhypotheses

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type Policy struct {
	MaxHypothesesPerEpisode       int
	MaxContributionsPerHypothesis int
	MaxMissingRequirements        int

	MinCandidateCoveragePermille     int
	MinSupportedPlausibilityPermille int
	MinLeadingMarginPermille         int
	ContradictedThresholdPermille    int

	MaxFactIDsPerContribution int
}

func DefaultPolicy() Policy {
	return Policy{
		MaxHypothesesPerEpisode: 16, MaxContributionsPerHypothesis: 128, MaxMissingRequirements: 64,
		MinCandidateCoveragePermille: 150, MinSupportedPlausibilityPermille: 600,
		MinLeadingMarginPermille: 100, ContradictedThresholdPermille: 700,
		MaxFactIDsPerContribution: 64,
	}
}

func (p Policy) Validate() error {
	if p.MaxHypothesesPerEpisode <= 0 || p.MaxContributionsPerHypothesis <= 0 || p.MaxMissingRequirements <= 0 || p.MaxFactIDsPerContribution <= 0 ||
		p.MinCandidateCoveragePermille < 0 || p.MinCandidateCoveragePermille > 1000 || p.MinSupportedPlausibilityPermille < 0 || p.MinSupportedPlausibilityPermille > 1000 ||
		p.MinLeadingMarginPermille < 0 || p.MinLeadingMarginPermille > 1000 || p.ContradictedThresholdPermille < 0 || p.ContradictedThresholdPermille > 1000 {
		return ErrInvalidPolicy
	}
	return nil
}

func (p Policy) Fingerprint() string {
	if p.Validate() != nil {
		return "situation-hypotheses-policy-v1:invalid"
	}
	payload, _ := json.Marshal(struct {
		H, C, M, Coverage, Support, Margin, Contradiction, FactIDs int
	}{p.MaxHypothesesPerEpisode, p.MaxContributionsPerHypothesis, p.MaxMissingRequirements, p.MinCandidateCoveragePermille, p.MinSupportedPlausibilityPermille, p.MinLeadingMarginPermille, p.ContradictedThresholdPermille, p.MaxFactIDsPerContribution})
	digest := sha256.Sum256(payload)
	return "situation-hypotheses-policy-v1:" + hex.EncodeToString(digest[:])
}
