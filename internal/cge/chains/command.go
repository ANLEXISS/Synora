package chains

import "fmt"

// AddObservationCommand is an explicit optimistic-concurrency command. The
// caller chooses the chain; this type never performs chain selection or
// creates a chain.
type AddObservationCommand struct {
	ChainID        ChainID
	SourceRevision uint64
	Observation    ObservationRef
	Mutation       MutationContext
}

// AddContributionCommand is an explicit optimistic-concurrency command. The
// caller chooses the chain and supplies a detached contribution.
type AddContributionCommand struct {
	ChainID        ChainID
	SourceRevision uint64
	Contribution   ConfidenceContribution
	Mutation       MutationContext
}

// Validate checks the contribution command without mutating or retaining
// caller-owned slices.
func (c AddContributionCommand) Validate() error {
	if _, err := NewChainID(string(c.ChainID)); err != nil {
		return fmt.Errorf("invalid contribution command chain: %w", err)
	}
	if c.SourceRevision == 0 {
		return fmt.Errorf("source revision must be positive")
	}
	if err := c.Contribution.Validate(); err != nil {
		return fmt.Errorf("invalid contribution command contribution: %w", err)
	}
	if err := c.Mutation.Validate(); err != nil {
		return fmt.Errorf("invalid contribution command mutation: %w", err)
	}
	return nil
}

// Clone returns a defensive command copy for transaction boundaries.
func (c AddContributionCommand) Clone() AddContributionCommand {
	c.Contribution = c.Contribution.Clone()
	c.Mutation.ObservationIDs = append([]string(nil), c.Mutation.ObservationIDs...)
	return c
}

// Validate checks the command without mutating or retaining caller-owned
// slices. The mutation timestamp and journal-record timestamp are deliberately
// separate; the latter belongs to the durable coordinator.
func (c AddObservationCommand) Validate() error {
	if _, err := NewChainID(string(c.ChainID)); err != nil {
		return fmt.Errorf("invalid observation command chain: %w", err)
	}
	if c.SourceRevision == 0 {
		return fmt.Errorf("source revision must be positive")
	}
	if err := c.Observation.Validate(); err != nil {
		return fmt.Errorf("invalid observation command observation: %w", err)
	}
	if err := c.Mutation.Validate(); err != nil {
		return fmt.Errorf("invalid observation command mutation: %w", err)
	}
	return nil
}

// Clone returns a defensive command copy for transaction boundaries.
func (c AddObservationCommand) Clone() AddObservationCommand {
	c.Mutation.ObservationIDs = append([]string(nil), c.Mutation.ObservationIDs...)
	return c
}
