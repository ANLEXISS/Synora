package cognitivesituation

import (
	"fmt"
	"strings"

	"synora/internal/cge/durableworkflow"
)

type Policy struct {
	MaxHypothesisAlternatives int
	MaxReasonCodes            int
	MaxSourceFingerprints     int

	MinKnowledgeCoveragePermille int
	MinLeadingCoveragePermille   int
	MinLeadingMarginPermille     int

	RequireFreshEpisode                   bool
	RequireFreshFacts                     bool
	RequireFreshHypotheses                bool
	AllowAmbiguousRecommendationReadiness bool
	AllowPartialContext                   bool
}

func DefaultPolicy() Policy {
	return Policy{
		MaxHypothesisAlternatives:             8,
		MaxReasonCodes:                        64,
		MaxSourceFingerprints:                 256,
		MinKnowledgeCoveragePermille:          700,
		MinLeadingCoveragePermille:            500,
		MinLeadingMarginPermille:              75,
		RequireFreshEpisode:                   true,
		RequireFreshFacts:                     true,
		RequireFreshHypotheses:                true,
		AllowAmbiguousRecommendationReadiness: true,
		AllowPartialContext:                   true,
	}
}

func (p Policy) Validate() error {
	if p.MaxHypothesisAlternatives <= 0 || p.MaxHypothesisAlternatives > 128 ||
		p.MaxReasonCodes <= 0 || p.MaxReasonCodes > 1024 ||
		p.MaxSourceFingerprints <= 0 || p.MaxSourceFingerprints > 4096 ||
		p.MinKnowledgeCoveragePermille < 0 || p.MinKnowledgeCoveragePermille > 1000 ||
		p.MinLeadingCoveragePermille < 0 || p.MinLeadingCoveragePermille > 1000 ||
		p.MinLeadingMarginPermille < 0 || p.MinLeadingMarginPermille > 1000 {
		return ErrInvalidPolicy
	}
	return nil
}

func (p Policy) Fingerprint() string {
	if p.Validate() != nil {
		return ""
	}
	return fingerprint("cognitive-situation-policy-v1:", p)
}

func validDepth(depth ExpectedPipelineDepth) bool {
	switch depth {
	case DepthEpisode, DepthSituationFacts, DepthSituationHypotheses,
		DepthEvidenceDiscrimination, DepthAdvisoryRequests,
		DepthCapabilityMapping, DepthAuthorizationBoundary:
		return true
	default:
		return false
	}
}

func depthIndex(depth ExpectedPipelineDepth) int {
	switch depth {
	case DepthEpisode:
		return 0
	case DepthSituationFacts:
		return 1
	case DepthSituationHypotheses:
		return 2
	case DepthEvidenceDiscrimination:
		return 3
	case DepthAdvisoryRequests:
		return 4
	case DepthCapabilityMapping:
		return 5
	case DepthAuthorizationBoundary:
		return 6
	default:
		return -1
	}
}

func expectedLayer(depth ExpectedPipelineDepth, layer durableworkflow.LayerKind) bool {
	i := depthIndex(depth)
	for j, value := range durableworkflow.Layers() {
		if value == layer {
			return i >= j
		}
	}
	return false
}

func minReasonCodes(values []string, limit int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
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

func addReason(values *[]string, reason string, limit int) {
	if reason == "" || len(*values) >= limit {
		return
	}
	for _, value := range *values {
		if value == reason {
			return
		}
	}
	*values = append(*values, reason)
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

func validatePolicyOrError(p Policy) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("%w: policy", err)
	}
	return nil
}
