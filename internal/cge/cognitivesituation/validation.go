package cognitivesituation

import (
	"fmt"

	"synora/internal/cge/durableworkflow"
)

func Validate(input BuildInput, policy Policy) error {
	if err := policy.Validate(); err != nil {
		return ErrInvalidPolicy
	}
	if !validDepth(input.ExpectedDepth) || input.EpisodeID == "" {
		return ErrInvalidBuildInput
	}
	if err := durableworkflow.ValidateWorkflowState(input.Workflow); err != nil {
		return fmt.Errorf("%w: %v", ErrWorkflowInvalid, err)
	}
	return nil
}
