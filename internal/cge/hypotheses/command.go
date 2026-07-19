package hypotheses

import (
	"fmt"

	"synora/internal/cge/chains"
)

// SetStatusCommand is the explicit optimistic command used by durable owners.
// It carries no mutable aggregate pointer and never selects an alternative.
type SetStatusCommand struct {
	SetID          SetID
	SourceRevision uint64
	Target         Status
	Mutation       chains.MutationContext
}

func (c SetStatusCommand) Validate() error {
	if err := validSetID(c.SetID); err != nil {
		return hypothesisError(ErrInvalidHypothesisCommand, "", c.SetID, "validate", c.SourceRevision, 0, err)
	}
	if c.SourceRevision == 0 {
		return hypothesisError(ErrInvalidHypothesisCommand, "", c.SetID, "validate", c.SourceRevision, 0, fmt.Errorf("source revision must be positive"))
	}
	if err := c.Target.Validate(); err != nil {
		return hypothesisError(ErrInvalidHypothesisCommand, "", c.SetID, "validate", c.SourceRevision, 0, err)
	}
	if err := c.Mutation.Validate(); err != nil {
		return hypothesisError(ErrInvalidContext, "", c.SetID, "validate", c.SourceRevision, 0, err)
	}
	if c.Target == StatusResolved || c.Target == StatusSuperseded {
		return hypothesisError(ErrInvalidHypothesisCommand, "", c.SetID, "validate", c.SourceRevision, 0, fmt.Errorf("target status is reserved"))
	}
	return nil
}

func (c SetStatusCommand) Clone() SetStatusCommand {
	c.Mutation.ObservationIDs = append([]string(nil), c.Mutation.ObservationIDs...)
	return c
}
