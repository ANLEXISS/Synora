package durable

import (
	"context"
	"errors"
	"fmt"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
)

// AssociationApplyResult describes one explicit durable association action.
// All snapshots are detached from the coordinator.
type AssociationApplyResult struct {
	Decision   association.Decision
	Applied    bool
	Idempotent bool

	ChainID chains.ChainID

	Before *chains.Snapshot
	After  *chains.Snapshot

	JournalSequence   uint64
	JournalRecordHash string

	ReasonCode string
}

// PlanAssociation captures a defensive coordinator view and invokes the pure
// association planner. It never appends, creates, or mutates anything.
func (c *Coordinator) PlanAssociation(input association.Input, plannedAt time.Time, policy association.Policy) (association.Plan, error) {
	if c == nil {
		return association.Plan{}, ErrCoordinatorClosed
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed {
		return association.Plan{}, ErrCoordinatorClosed
	}
	if c.current == nil {
		return association.Plan{}, ErrCoordinatorNotReady
	}
	return association.PlanAssociation(c.current.List(), input, plannedAt, policy)
}

// ApplyAssociationPlan explicitly revalidates and applies one optimistic
// plan. It never replans or silently chooses another chain.
func (c *Coordinator) ApplyAssociationPlan(ctx context.Context, plan association.Plan, actor, correlationID string, mutationAt, recordedAt time.Time) (AssociationApplyResult, error) {
	return c.applyAssociationPlan(ctx, plan, actor, correlationID, mutationAt, recordedAt, "")
}

// ApplyAssociationPlanWithReason is the same optimistic mutation boundary
// with an explicit caller-owned stable reason. It exists for Shadow
// provenance and does not alter planning or mutation semantics.
func (c *Coordinator) ApplyAssociationPlanWithReason(ctx context.Context, plan association.Plan, actor, correlationID, reason string, mutationAt, recordedAt time.Time) (AssociationApplyResult, error) {
	return c.applyAssociationPlan(ctx, plan, actor, correlationID, mutationAt, recordedAt, reason)
}

func (c *Coordinator) applyAssociationPlan(ctx context.Context, plan association.Plan, actor, correlationID string, mutationAt, recordedAt time.Time, explicitReason string) (AssociationApplyResult, error) {
	if err := validateContext(ctx); err != nil {
		return AssociationApplyResult{}, err
	}
	if c == nil {
		return AssociationApplyResult{}, ErrCoordinatorClosed
	}
	if err := plan.Validate(); err != nil {
		return AssociationApplyResult{}, CoordinatorError{Operation: "association.apply", Step: "validate", ObservationID: plan.Observation.ID, Err: err}
	}
	if mutationAt.IsZero() {
		return AssociationApplyResult{}, CoordinatorError{Operation: "association.apply", Step: "validate", ObservationID: plan.Observation.ID, Err: ErrInvalidTimestamp}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return AssociationApplyResult{}, err
	}
	if err := validateMutationInput(actor, correlationID, recordedAt); err != nil {
		return AssociationApplyResult{}, CoordinatorError{Operation: "association.apply", Step: "validate", ObservationID: plan.Observation.ID, Err: err}
	}
	attachments := findObservationAttachments(c.current.List(), plan.Observation.ID)
	switch plan.Decision {
	case association.DecisionAlreadyAttached:
		if len(attachments) != 1 || attachments[0].ID != plan.SelectedChainID {
			return AssociationApplyResult{}, staleAssociationError(plan, "already-attached chain changed")
		}
		return associationAlreadyAttachedResult(plan, attachments[0]), nil
	case association.DecisionAmbiguous:
		return AssociationApplyResult{}, CoordinatorError{Operation: "association.apply", Step: "validate", ObservationID: plan.Observation.ID, Err: errors.Join(ErrAssociationAmbiguous, association.ErrAssociationAmbiguous)}
	case association.DecisionAttachExisting:
		if len(attachments) != 0 {
			return AssociationApplyResult{}, staleAssociationError(plan, "observation is no longer unattached")
		}
		selected, err := c.current.Get(plan.SelectedChainID)
		if err != nil {
			return AssociationApplyResult{}, staleAssociationError(plan, "selected chain is missing")
		}
		if selected.Revision != plan.SelectedSourceRevision || !selected.Status.CanAcceptObservation() {
			return AssociationApplyResult{}, staleAssociationError(plan, "selected chain revision or status changed")
		}
		mutationReason := associationAttachReason(plan)
		if explicitReason != "" {
			mutationReason = explicitReason
		}
		command := chains.AddObservationCommand{
			ChainID: plan.SelectedChainID, SourceRevision: plan.SelectedSourceRevision,
			Observation: plan.Observation,
			Mutation: chains.MutationContext{
				At: mutationAt, Actor: actor, Reason: mutationReason, CorrelationID: correlationID,
				ObservationIDs: []string{plan.Observation.ID},
			},
		}
		result, err := c.addObservationLocked(ctx, command, recordedAt)
		if err != nil {
			return AssociationApplyResult{}, err
		}
		return associationMutationResult(plan, result), nil
	case association.DecisionCreateCandidate:
		if err := association.ValidateCandidateChainID(association.Input{Observation: plan.Observation}, plan.PolicyVersion, plan.NewChainID); err != nil {
			return AssociationApplyResult{}, CoordinatorError{Operation: "association.apply", Step: "revalidate", ObservationID: plan.Observation.ID, Err: err}
		}
		if len(attachments) != 0 {
			if len(attachments) == 1 && attachments[0].ID == plan.NewChainID {
				return associationAlreadyAttachedResult(plan, attachments[0]), nil
			}
			return AssociationApplyResult{}, staleAssociationError(plan, "observation is already attached elsewhere")
		}
		if _, err := c.current.Get(plan.NewChainID); err == nil {
			return AssociationApplyResult{}, CoordinatorError{Operation: "association.apply", Step: "validate", ObservationID: plan.Observation.ID, Err: errors.Join(ErrCandidateIDCollision, association.ErrCandidateIDCollision)}
		} else if !errors.Is(err, ErrChainNotFound) {
			return AssociationApplyResult{}, err
		}
		creationReason := associationCreateReason(plan)
		if explicitReason != "" {
			creationReason = explicitReason
		}
		creationAt := mutationAt
		if plan.Observation.Timestamp.Before(creationAt) {
			creationAt = plan.Observation.Timestamp
		}
		creationMutation := chains.MutationContext{At: creationAt, Actor: actor, Reason: creationReason, CorrelationID: correlationID}
		candidate, err := chains.New(plan.NewChainID, creationMutation)
		if err != nil {
			return AssociationApplyResult{}, CoordinatorError{Operation: "association.apply", Step: "prepare", ObservationID: plan.Observation.ID, Err: err}
		}
		if err := candidate.AddObservation(plan.Observation, chains.MutationContext{
			At: mutationAt, Actor: actor, Reason: creationReason, CorrelationID: correlationID,
			ObservationIDs: []string{plan.Observation.ID},
		}); err != nil {
			return AssociationApplyResult{}, CoordinatorError{Operation: "association.apply", Step: "prepare", ObservationID: plan.Observation.ID, Err: err}
		}
		result, err := c.addChainLocked(ctx, candidate, actor, correlationID, recordedAt)
		if err != nil {
			return AssociationApplyResult{}, err
		}
		return associationMutationResult(plan, result), nil
	default:
		return AssociationApplyResult{}, CoordinatorError{Operation: "association.apply", Step: "validate", ObservationID: plan.Observation.ID, Err: ErrInvalidAssociationPlan}
	}
}

func findObservationAttachments(snapshots []chains.Snapshot, observationID string) []chains.Snapshot {
	result := make([]chains.Snapshot, 0, 1)
	for _, snapshot := range snapshots {
		for _, observation := range snapshot.Observations {
			if observation.ID == observationID {
				result = append(result, snapshot)
				break
			}
		}
	}
	return result
}

func associationAlreadyAttachedResult(plan association.Plan, snapshot chains.Snapshot) AssociationApplyResult {
	after := snapshot
	return AssociationApplyResult{Decision: plan.Decision, Idempotent: true, ChainID: snapshot.ID, After: &after, ReasonCode: plan.ReasonCode}
}

func associationMutationResult(plan association.Plan, result MutationResult) AssociationApplyResult {
	after := result.After
	var before *chains.Snapshot
	if result.Before != nil {
		copy := *result.Before
		before = &copy
	}
	return AssociationApplyResult{Decision: plan.Decision, Applied: true, ChainID: result.ChainID, Before: before, After: &after, JournalSequence: result.JournalSequence, JournalRecordHash: result.JournalRecordHash, ReasonCode: plan.ReasonCode}
}

func associationAttachReason(plan association.Plan) string {
	return fmt.Sprintf("association.attach_existing score=%d margin=%d policy=%s", plan.BestScore, plan.ScoreMargin, plan.PolicyVersion)
}

func associationCreateReason(plan association.Plan) string {
	return fmt.Sprintf("association.create_candidate policy=%s", plan.PolicyVersion)
}

func staleAssociationError(plan association.Plan, detail string) error {
	return CoordinatorError{Operation: "association.apply", Step: "revalidate", ChainID: string(plan.SelectedChainID), ObservationID: plan.Observation.ID, ExpectedRevision: plan.SelectedSourceRevision, Err: fmt.Errorf("%w: %s", association.ErrStaleAssociationPlan, detail)}
}
