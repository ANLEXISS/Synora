package facegallery

import (
	"errors"

	"synora/pkg/contract"
)

// ApplySyntheticResult projects only the bounded semantic result to Evidence
// V1. It is intentionally unavailable to real/replay provenance, and exports
// no score, resident reference, sample key, episode key, or track key.
func ApplySyntheticResult(evidence contract.VisionEvidenceV1, result SemanticResult) (contract.VisionEvidenceV1, error) {
	if evidence.Provenance != "simulated_test" {
		return contract.VisionEvidenceV1{}, errors.New("face test result requires simulated_test provenance")
	}
	availability := contract.VisionEvaluated
	state := result.Result
	switch state {
	case "candidate", "recognized", "unknown", "ambiguous":
	case "unavailable":
		availability, state = contract.VisionUnavailable, "unknown"
	default:
		return contract.VisionEvidenceV1{}, errors.New("invalid face semantic result")
	}
	support := contract.VisionSupportV1{Continuity: "unknown"}
	if availability == contract.VisionEvaluated {
		support = contract.VisionSupportV1{ValidEvaluations: 1, Continuity: "continuous"}
	}
	evidence.Face = contract.VisionSemanticResultV1{Availability: availability, Result: state, Confidence: 0, Quality: 0, Support: support}
	if err := evidence.Validate(); err != nil {
		return contract.VisionEvidenceV1{}, err
	}
	return evidence, nil
}
