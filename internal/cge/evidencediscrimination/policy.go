package evidencediscrimination

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type Policy struct {
	MaxCandidates                             int
	MaxOutcomesPerCandidate                   int
	MaxPairsPerCandidate                      int
	MaxFactCodesPerCandidate                  int
	MinRelevantHypothesisPlausibilityPermille int
	MaxResolvedMarginPermille                 int
	MinDiscriminationPermille                 int
	MinCoverageGainPermille                   int
	MinUtilityPermille                        int
	MinBestCandidateMarginPermille            int
	DiscriminationWeightPermille              int
	CoverageWeightPermille                    int
	RedundancyPenaltyPermille                 int
	IncludeHighSensitivityCandidates          bool
}

func DefaultPolicy() Policy {
	return Policy{MaxCandidates: 32, MaxOutcomesPerCandidate: 16, MaxPairsPerCandidate: 64, MaxFactCodesPerCandidate: 32, MinRelevantHypothesisPlausibilityPermille: 150, MaxResolvedMarginPermille: 300, MinDiscriminationPermille: 200, MinCoverageGainPermille: 100, MinUtilityPermille: 250, MinBestCandidateMarginPermille: 75, DiscriminationWeightPermille: 700, CoverageWeightPermille: 300, RedundancyPenaltyPermille: 600}
}

func (p Policy) Validate() error {
	if p.MaxCandidates <= 0 || p.MaxOutcomesPerCandidate <= 0 || p.MaxPairsPerCandidate <= 0 || p.MaxFactCodesPerCandidate <= 0 || p.MinRelevantHypothesisPlausibilityPermille < 0 || p.MinRelevantHypothesisPlausibilityPermille > 1000 || p.MaxResolvedMarginPermille < 0 || p.MaxResolvedMarginPermille > 1000 || p.MinDiscriminationPermille < 0 || p.MinDiscriminationPermille > 1000 || p.MinCoverageGainPermille < 0 || p.MinCoverageGainPermille > 1000 || p.MinUtilityPermille < 0 || p.MinUtilityPermille > 1000 || p.MinBestCandidateMarginPermille < 0 || p.MinBestCandidateMarginPermille > 1000 || p.DiscriminationWeightPermille < 0 || p.DiscriminationWeightPermille > 1000 || p.CoverageWeightPermille < 0 || p.CoverageWeightPermille > 1000 || p.DiscriminationWeightPermille+p.CoverageWeightPermille != 1000 || p.RedundancyPenaltyPermille < 0 || p.RedundancyPenaltyPermille > 1000 {
		return ErrInvalidPolicy
	}
	return nil
}

func (p Policy) Fingerprint() string {
	payload, _ := json.Marshal(struct {
		A, B, C, D, E, F, G, H, I, J, K, L, M int
		N                                     bool
	}{p.MaxCandidates, p.MaxOutcomesPerCandidate, p.MaxPairsPerCandidate, p.MaxFactCodesPerCandidate, p.MinRelevantHypothesisPlausibilityPermille, p.MaxResolvedMarginPermille, p.MinDiscriminationPermille, p.MinCoverageGainPermille, p.MinUtilityPermille, p.MinBestCandidateMarginPermille, p.DiscriminationWeightPermille, p.CoverageWeightPermille, p.RedundancyPenaltyPermille, p.IncludeHighSensitivityCandidates})
	d := sha256.Sum256(payload)
	return "evidence-discrimination-policy-v1:" + hex.EncodeToString(d[:])
}

func clamp(value int) int {
	if value < 0 {
		return 0
	}
	if value > 1000 {
		return 1000
	}
	return value
}
