package cognitiverecommendation

import "strings"

type Policy struct {
	MaxRecommendations int
	MaxReasonCodes     int

	MinApplicabilityPermille    int
	MinInformationValuePermille int
	MinStabilityPermille        int
	MinPrimaryMarginPermille    int

	AllowPrimaryWhenAmbiguous      bool
	AllowObservationRecommendation bool
	AllowTransitionRecommendation  bool
	RequireRecommendationReadiness bool
	RequireFreshSituation          bool
}

func DefaultPolicy() Policy {
	return Policy{
		MaxRecommendations:             8,
		MaxReasonCodes:                 64,
		MinApplicabilityPermille:       500,
		MinInformationValuePermille:    250,
		MinStabilityPermille:           500,
		MinPrimaryMarginPermille:       75,
		AllowPrimaryWhenAmbiguous:      false,
		AllowObservationRecommendation: true,
		AllowTransitionRecommendation:  true,
		RequireRecommendationReadiness: true,
		RequireFreshSituation:          true,
	}
}

func (p Policy) Validate() error {
	if p.MaxRecommendations <= 0 || p.MaxRecommendations > 128 ||
		p.MaxReasonCodes <= 0 || p.MaxReasonCodes > 1024 ||
		p.MinApplicabilityPermille < 0 || p.MinApplicabilityPermille > 1000 ||
		p.MinInformationValuePermille < 0 || p.MinInformationValuePermille > 1000 ||
		p.MinStabilityPermille < 0 || p.MinStabilityPermille > 1000 ||
		p.MinPrimaryMarginPermille < 0 || p.MinPrimaryMarginPermille > 1000 {
		return ErrInvalidPolicy
	}
	return nil
}

func (p Policy) Fingerprint() string {
	if p.Validate() != nil {
		return ""
	}
	return fingerprint("cognitive-recommendation-policy-v1:", p)
}

func validKind(value RecommendationKind) bool {
	switch value {
	case RecommendationContinueObservation, RecommendationMaintainInterpretation,
		RecommendationAdditionalEvidence, RecommendationReassessContext,
		RecommendationReassessObservation, RecommendationPreserveAmbiguity,
		RecommendationCognitiveTransition, RecommendationNone:
		return true
	default:
		return false
	}
}

func validTargetKind(value RecommendationTargetKind) bool {
	switch value {
	case TargetSituation, TargetHypothesis, TargetEvidenceRequest, TargetContext, TargetFutureObservation:
		return true
	default:
		return false
	}
}

func validStatus(value RecommendationStatus) bool {
	switch value {
	case RecommendationCandidate, RecommendationApplicable, RecommendationBlocked,
		RecommendationInsufficientInformation, RecommendationSuperseded,
		RecommendationWithdrawn, RecommendationInvalidated:
		return true
	default:
		return false
	}
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

func addReason(values *[]string, reason string, limit int) {
	if strings.TrimSpace(reason) == "" || len(*values) >= limit {
		return
	}
	for _, value := range *values {
		if value == reason {
			return
		}
	}
	*values = append(*values, reason)
}

func dedupeStrings(values []string, limit int) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
}
