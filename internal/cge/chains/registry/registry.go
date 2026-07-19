// Package registry owns mutable cognitive chains in memory.
package registry

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"synora/internal/cge/chains"
)

var (
	ErrChainAlreadyExists          = errors.New("cognitive chain already exists")
	ErrChainNotFound               = errors.New("cognitive chain not found")
	ErrStaleProposal               = errors.New("stale lifecycle proposal")
	ErrInvalidProposal             = errors.New("invalid lifecycle proposal")
	ErrStaleObservationCommand     = errors.New("stale_observation_command")
	ErrInvalidObservationCommand   = errors.New("invalid_observation_command")
	ErrDuplicateObservation        = chains.ErrDuplicateObservation
	ErrObservationNotAllowed       = chains.ErrObservationNotAllowed
	ErrStaleContributionCommand    = errors.New("stale_contribution_command")
	ErrInvalidContributionCommand  = errors.New("invalid_contribution_command")
	ErrContributionResultMismatch  = errors.New("contribution_result_mismatch")
	ErrDuplicateContribution       = chains.ErrDuplicateContribution
	ErrContributionNotAllowed      = chains.ErrContributionNotAllowed
	ErrUnknownObservationReference = chains.ErrUnknownObservationReference
)

// Registry is the sole owner of the mutable Chain instances it stores. The
// first implementation deliberately uses one RWMutex for correctness and
// simple atomicity; access granularity can evolve only if measurements justify
// it. Callers receive snapshots, never stored pointers.
type Registry struct {
	mu     sync.RWMutex
	chains map[chains.ChainID]*chains.Chain
}

// ObservationApplyResult contains detached before/after state and the exact
// domain revision produced by one successful observation mutation.
type ObservationApplyResult struct {
	Before   chains.Snapshot
	After    chains.Snapshot
	Revision chains.RevisionRecord
}

// ContributionApplyResult contains detached before/after state and the exact
// domain revision produced by one contribution mutation.
type ContributionApplyResult struct {
	Before   chains.Snapshot
	After    chains.Snapshot
	Revision chains.RevisionRecord
}

// StaleObservationCommandError describes an optimistic-concurrency rejection.
type StaleObservationCommandError struct {
	ChainID          chains.ChainID
	ExpectedRevision uint64
	CurrentRevision  uint64
}

// StaleContributionCommandError describes an optimistic-concurrency rejection.
type StaleContributionCommandError struct {
	ChainID          chains.ChainID
	ExpectedRevision uint64
	CurrentRevision  uint64
}

func (e StaleContributionCommandError) Error() string {
	return fmt.Sprintf("%s: chain=%s expected revision=%d current revision=%d", ErrStaleContributionCommand, e.ChainID, e.ExpectedRevision, e.CurrentRevision)
}

func (e StaleContributionCommandError) Unwrap() error { return ErrStaleContributionCommand }

func (e StaleObservationCommandError) Error() string {
	return fmt.Sprintf("%s: chain=%s expected revision=%d current revision=%d", ErrStaleObservationCommand, e.ChainID, e.ExpectedRevision, e.CurrentRevision)
}

func (e StaleObservationCommandError) Unwrap() error { return ErrStaleObservationCommand }

// New constructs an empty in-memory registry.
func New() *Registry {
	return &Registry{chains: make(map[chains.ChainID]*chains.Chain)}
}

// CloneShallow creates a transaction candidate by copying only the ownership
// table. Mutating operations clone their target before replacing its entry.
// It is used behind the durable coordinator's WAL boundary; callers still
// receive snapshots and never stored chain pointers.
func (r *Registry) CloneShallow() (*Registry, error) {
	if r == nil {
		return nil, errors.New("registry is nil")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	clone := New()
	clone.chains = make(map[chains.ChainID]*chains.Chain, len(r.chains))
	for id, chain := range r.chains {
		clone.chains[id] = chain
	}
	return clone, nil
}

// Add validates and deeply clones chain before taking ownership of the clone.
// Mutating the caller's original after this method returns cannot affect the
// registry.
func (r *Registry) Add(chain *chains.Chain) error {
	if r == nil {
		return errors.New("registry is nil")
	}
	if chain == nil {
		return errors.New("chain is nil")
	}
	owned, err := chain.Clone()
	if err != nil {
		return fmt.Errorf("add chain: %w", err)
	}
	id := owned.Snapshot().ID

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.chains == nil {
		r.chains = make(map[chains.ChainID]*chains.Chain)
	}
	if _, exists := r.chains[id]; exists {
		return fmt.Errorf("%w: %s", ErrChainAlreadyExists, id)
	}
	r.chains[id] = owned
	return nil
}

// Get returns a defensive snapshot of one chain.
func (r *Registry) Get(id chains.ChainID) (chains.Snapshot, error) {
	if r == nil {
		return chains.Snapshot{}, errors.New("registry is nil")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	chain, ok := r.chains[id]
	if !ok {
		return chains.Snapshot{}, fmt.Errorf("%w: %s", ErrChainNotFound, id)
	}
	return chain.Snapshot(), nil
}

// List returns defensive snapshots in ChainID order.
func (r *Registry) List() []chains.Snapshot {
	if r == nil {
		return []chains.Snapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]chains.ChainID, 0, len(r.chains))
	for id := range r.chains {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]chains.Snapshot, 0, len(ids))
	for _, id := range ids {
		result = append(result, r.chains[id].Snapshot())
	}
	return result
}

// Count returns the number of owned chains.
func (r *Registry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.chains)
}

// AddObservation applies one explicit observation command to a cloned chain,
// validates the complete candidate, and replaces the owned chain atomically.
func (r *Registry) AddObservation(command chains.AddObservationCommand) (ObservationApplyResult, error) {
	if r == nil {
		return ObservationApplyResult{}, ErrInvalidObservationCommand
	}
	if err := command.Validate(); err != nil {
		return ObservationApplyResult{}, fmt.Errorf("%w: %v", ErrInvalidObservationCommand, err)
	}
	command = command.Clone()
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.chains[command.ChainID]
	if !ok {
		return ObservationApplyResult{}, fmt.Errorf("%w: %s", ErrChainNotFound, command.ChainID)
	}
	before := stored.Snapshot()
	if before.Revision != command.SourceRevision {
		return ObservationApplyResult{}, StaleObservationCommandError{ChainID: command.ChainID, ExpectedRevision: command.SourceRevision, CurrentRevision: before.Revision}
	}
	if err := before.Status.ValidateObservationMutation(); err != nil {
		return ObservationApplyResult{}, err
	}
	updated, err := stored.Clone()
	if err != nil {
		return ObservationApplyResult{}, fmt.Errorf("clone observation chain: %w", err)
	}
	if err := updated.AddObservation(command.Observation, command.Mutation); err != nil {
		return ObservationApplyResult{}, err
	}
	if err := updated.Validate(); err != nil {
		return ObservationApplyResult{}, fmt.Errorf("validate observation result: %w", err)
	}
	after := updated.Snapshot()
	if err := validateObservationDelta(before, after, command.Observation); err != nil {
		return ObservationApplyResult{}, err
	}
	revision := after.History[len(after.History)-1]
	r.chains[command.ChainID] = updated
	return ObservationApplyResult{Before: before, After: after, Revision: revision}, nil
}

func validateObservationDelta(before, after chains.Snapshot, observation chains.ObservationRef) error {
	if len(after.History) != len(before.History)+1 || after.Revision != before.Revision+1 {
		return fmt.Errorf("%w: observation revision is not contiguous", ErrInvalidObservationCommand)
	}
	if after.ID != before.ID || after.EntityID != before.EntityID || after.Status != before.Status || after.CurrentConfidence != before.CurrentConfidence || after.HistoricalReliability != before.HistoricalReliability || after.MaxHistoricalConfidence != before.MaxHistoricalConfidence || after.ConfirmationCount != before.ConfirmationCount || after.ContradictionCount != before.ContradictionCount || after.OccurrenceCount != before.OccurrenceCount+1 || len(after.Contributions) != len(before.Contributions) {
		return fmt.Errorf("%w: observation changed unrelated chain state", ErrInvalidObservationCommand)
	}
	revision := after.History[len(after.History)-1]
	if revision.ChainID != after.ID || revision.Operation != chains.OperationObservationAdded || revision.PreviousRevision != before.Revision || revision.NewRevision != after.Revision || len(revision.ObservationIDs) != 1 || revision.ObservationIDs[0] != observation.ID || len(revision.ContributionIDs) != 0 || revision.PreviousStatus != "" || revision.NewStatus != "" {
		return fmt.Errorf("%w: observation revision record is inconsistent", ErrInvalidObservationCommand)
	}
	return nil
}

// AddContribution applies one explicit contribution to a cloned chain and
// replaces the owned chain only after the complete candidate validates.
func (r *Registry) AddContribution(command chains.AddContributionCommand) (ContributionApplyResult, error) {
	if r == nil {
		return ContributionApplyResult{}, ErrInvalidContributionCommand
	}
	if err := command.Validate(); err != nil {
		return ContributionApplyResult{}, fmt.Errorf("%w: %v", ErrInvalidContributionCommand, err)
	}
	command = command.Clone()
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.chains[command.ChainID]
	if !ok {
		return ContributionApplyResult{}, fmt.Errorf("%w: %s", ErrChainNotFound, command.ChainID)
	}
	before := stored.Snapshot()
	if before.Revision != command.SourceRevision {
		return ContributionApplyResult{}, StaleContributionCommandError{ChainID: command.ChainID, ExpectedRevision: command.SourceRevision, CurrentRevision: before.Revision}
	}
	if err := before.Status.ValidateContributionMutation(); err != nil {
		return ContributionApplyResult{}, err
	}
	updated, err := stored.Clone()
	if err != nil {
		return ContributionApplyResult{}, fmt.Errorf("clone contribution chain: %w", err)
	}
	if err := updated.AddContribution(command.Contribution, command.Mutation); err != nil {
		return ContributionApplyResult{}, err
	}
	if err := updated.Validate(); err != nil {
		return ContributionApplyResult{}, fmt.Errorf("validate contribution result: %w", err)
	}
	after := updated.Snapshot()
	if err := validateContributionDelta(before, after, command); err != nil {
		return ContributionApplyResult{}, err
	}
	revision := after.History[len(after.History)-1]
	r.chains[command.ChainID] = updated
	return ContributionApplyResult{Before: before, After: after, Revision: revision}, nil
}

func validateContributionDelta(before, after chains.Snapshot, command chains.AddContributionCommand) error {
	if len(after.History) != len(before.History)+1 || after.Revision != before.Revision+1 {
		return fmt.Errorf("%w: contribution revision is not contiguous", ErrContributionResultMismatch)
	}
	if after.ID != before.ID || after.Status != before.Status || after.EntityID != before.EntityID || after.HistoricalReliability != before.HistoricalReliability || after.OccurrenceCount != before.OccurrenceCount || after.FirstSeenAt != before.FirstSeenAt || after.LastSeenAt != before.LastSeenAt || !reflect.DeepEqual(after.Observations, before.Observations) || !reflect.DeepEqual(after.NodePath, before.NodePath) || len(after.Contributions) != len(before.Contributions)+1 {
		return fmt.Errorf("%w: contribution changed unrelated chain state", ErrContributionResultMismatch)
	}
	expectedConfidence, err := chains.ProjectedConfidence(before.CurrentConfidence, command.Contribution)
	if err != nil || after.CurrentConfidence != expectedConfidence {
		return fmt.Errorf("%w: contribution confidence result is inconsistent", ErrContributionResultMismatch)
	}
	expectedConfirmation := before.ConfirmationCount
	expectedContradiction := before.ContradictionCount
	switch command.Contribution.Kind {
	case chains.ContributionSupport:
		expectedConfirmation++
	case chains.ContributionContradiction:
		expectedContradiction++
	}
	expectedMaxHistorical := before.MaxHistoricalConfidence
	if expectedConfidence > expectedMaxHistorical {
		expectedMaxHistorical = expectedConfidence
	}
	if after.ConfirmationCount != expectedConfirmation || after.ContradictionCount != expectedContradiction || after.MaxHistoricalConfidence != expectedMaxHistorical || !reflect.DeepEqual(after.Contributions[len(after.Contributions)-1], command.Contribution) {
		return fmt.Errorf("%w: contribution counters or value are inconsistent", ErrContributionResultMismatch)
	}
	last := after.History[len(after.History)-1]
	if last.ChainID != after.ID || last.Operation != chains.OperationContributionAdded || last.PreviousRevision != before.Revision || last.NewRevision != after.Revision || len(last.ContributionIDs) != 1 || last.ContributionIDs[0] != command.Contribution.ID || last.PreviousStatus != "" || last.NewStatus != "" || last.PreviousConfidence != nil || last.NewConfidence != nil || last.PreviousHistoricalReliability != nil || last.NewHistoricalReliability != nil {
		return fmt.Errorf("%w: contribution revision record is inconsistent", ErrContributionResultMismatch)
	}
	return nil
}

// StaleProposalError explains an optimistic-concurrency rejection.
type StaleProposalError struct {
	ChainID          chains.ChainID
	ExpectedRevision uint64
	CurrentRevision  uint64
	ExpectedStatus   chains.Status
	CurrentStatus    chains.Status
}

func (e StaleProposalError) Error() string {
	return fmt.Sprintf("%s: chain=%s expected revision=%d status=%s, current revision=%d status=%s", ErrStaleProposal, e.ChainID, e.ExpectedRevision, e.ExpectedStatus, e.CurrentRevision, e.CurrentStatus)
}

func (e StaleProposalError) Unwrap() error { return ErrStaleProposal }

// ApplyLifecycleProposal explicitly applies one proposal transactionally.
// The proposal's source revision and source status implement optimistic
// concurrency: only one concurrent application of the same proposal can win.
func (r *Registry) ApplyLifecycleProposal(proposal chains.TransitionProposal, actor, correlationID string) (chains.Snapshot, error) {
	if r == nil {
		return chains.Snapshot{}, errors.New("registry is nil")
	}
	if _, err := chains.NewChainID(string(proposal.ChainID)); err != nil {
		return chains.Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidProposal, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.chains[proposal.ChainID]
	if !ok {
		return chains.Snapshot{}, fmt.Errorf("%w: %s", ErrChainNotFound, proposal.ChainID)
	}
	before := stored.Snapshot()
	if proposal.SourceRevision != before.Revision || proposal.From != before.Status {
		return chains.Snapshot{}, StaleProposalError{
			ChainID:          proposal.ChainID,
			ExpectedRevision: proposal.SourceRevision,
			CurrentRevision:  before.Revision,
			ExpectedStatus:   proposal.From,
			CurrentStatus:    before.Status,
		}
	}
	if proposal.ChainID != before.ID {
		return chains.Snapshot{}, fmt.Errorf("%w: proposal chain identity does not match stored chain", ErrInvalidProposal)
	}
	if err := chains.ValidateTransition(before.Status, proposal.To); err != nil {
		return chains.Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidProposal, err)
	}
	if err := validateProposalTime(proposal, before); err != nil {
		return chains.Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidProposal, err)
	}
	mutation, err := proposal.MutationContext(actor, correlationID)
	if err != nil {
		return chains.Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidProposal, err)
	}

	// Mutate only a validated clone. The stored pointer is replaced only after
	// the complete domain validation succeeds, so failures leave it untouched.
	updated, err := stored.Clone()
	if err != nil {
		return chains.Snapshot{}, fmt.Errorf("clone chain for mutation: %w", err)
	}
	if err := updated.SetStatus(proposal.To, mutation); err != nil {
		return chains.Snapshot{}, fmt.Errorf("apply lifecycle proposal: %w", err)
	}
	if err := updated.Validate(); err != nil {
		return chains.Snapshot{}, fmt.Errorf("validate lifecycle proposal result: %w", err)
	}
	after := updated.Snapshot()
	r.chains[proposal.ChainID] = updated
	return after, nil
}

func validateProposalTime(proposal chains.TransitionProposal, snapshot chains.Snapshot) error {
	if proposal.EvaluatedAt.IsZero() {
		return errors.New("proposal evaluation timestamp must not be zero")
	}
	createdAt := snapshot.CreatedAt()
	statusChangedAt := snapshot.StatusChangedAt()
	lastSeenAt := snapshot.LastSeenAt
	if lastSeenAt.IsZero() {
		lastSeenAt = createdAt
	}
	if createdAt.IsZero() || statusChangedAt.IsZero() {
		return errors.New("stored chain has invalid temporal anchors")
	}
	if proposal.EvaluatedAt.Before(createdAt) {
		return errors.New("proposal timestamp precedes chain creation")
	}
	if proposal.EvaluatedAt.Before(lastSeenAt) {
		return errors.New("proposal timestamp precedes last observation")
	}
	if proposal.EvaluatedAt.Before(statusChangedAt) {
		return errors.New("proposal timestamp precedes current status entry")
	}
	if proposal.StatusChangedAt.IsZero() || !proposal.StatusChangedAt.Equal(statusChangedAt) ||
		proposal.Age != proposal.EvaluatedAt.Sub(createdAt) ||
		proposal.InactiveFor != proposal.EvaluatedAt.Sub(lastSeenAt) ||
		proposal.InCurrentStatusFor != proposal.EvaluatedAt.Sub(statusChangedAt) ||
		proposal.CurrentConfidence != snapshot.CurrentConfidence ||
		proposal.HistoricalReliability != snapshot.HistoricalReliability {
		return errors.New("proposal temporal or confidence values do not match source snapshot")
	}
	return nil
}
