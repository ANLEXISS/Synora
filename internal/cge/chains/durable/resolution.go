package durable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/registry"
	"synora/internal/cge/hypotheses"
)

// ChainResolutionDelta is the journal union for the chain half of one
// hypothesis.resolved record.
type ChainResolutionDelta = journal.ResolutionChainDelta

// ResolveHypothesis applies one caller-selected alternative to cloned chain
// and hypothesis registries, appends one WAL record, then publishes both.
func (c *Coordinator) ResolveHypothesis(ctx context.Context, command hypotheses.ResolveCommand, recordedAt time.Time) (HypothesisResolutionResult, error) {
	if err := validateContext(ctx); err != nil {
		return HypothesisResolutionResult{}, err
	}
	if c == nil {
		return HypothesisResolutionResult{}, ErrCoordinatorClosed
	}
	command = command.Clone()
	if err := command.Validate(); err != nil {
		return HypothesisResolutionResult{}, coordinatorError("hypothesis.resolved", "validate", string(command.SetID), err)
	}
	if !validRecordedAt(recordedAt) || recordedAt.Before(command.Mutation.At) {
		return HypothesisResolutionResult{}, coordinatorError("hypothesis.resolved", "validate", string(command.SetID), ErrInvalidTimestamp)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return HypothesisResolutionResult{}, err
	}
	if c.current == nil || c.currentHypotheses == nil {
		return HypothesisResolutionResult{}, ErrCoordinatorNotReady
	}
	beforeHypothesis, err := c.currentHypotheses.Get(command.SetID)
	if err != nil {
		return HypothesisResolutionResult{}, coordinatorError("hypothesis.resolved", "prepare", string(command.SetID), err)
	}
	if beforeHypothesis.Status == hypotheses.StatusResolved && beforeHypothesis.Resolution != nil {
		fingerprint, fpErr := command.Effect.Fingerprint()
		if fpErr == nil && beforeHypothesis.Resolution.AssessmentVersion == command.AssessmentVersion && beforeHypothesis.Resolution.AssessmentID == command.AssessmentID && beforeHypothesis.Resolution.AssessmentFingerprint == command.AssessmentFingerprint && beforeHypothesis.Resolution.AlternativeID == command.AlternativeID && beforeHypothesis.Resolution.AlternativeKind == command.AlternativeKind && beforeHypothesis.Resolution.EffectKind == command.Effect.Kind && beforeHypothesis.Resolution.EffectFingerprint == fingerprint {
			return c.idempotentResolutionResult(beforeHypothesis), nil
		}
		return HypothesisResolutionResult{}, coordinatorError("hypothesis.resolved", "prepare", string(command.SetID), ErrHypothesisResolutionCollision)
	}
	if !hypotheses.CanResolve(beforeHypothesis.Status) || beforeHypothesis.Lineage.SuccessorSetID != "" {
		return HypothesisResolutionResult{}, coordinatorError("hypothesis.resolved", "prepare", string(command.SetID), ErrStaleHypothesisResolution)
	}
	chainCandidate, err := cloneRegistry(ctx, c.current)
	if err != nil {
		return HypothesisResolutionResult{}, coordinatorError("hypothesis.resolved", "clone_chains", string(command.SetID), err)
	}
	hypothesisCandidate, err := cloneHypothesisRegistry(ctx, c.currentHypotheses)
	if err != nil {
		return HypothesisResolutionResult{}, coordinatorError("hypothesis.resolved", "clone_hypotheses", string(command.SetID), err)
	}
	outcome, delta, err := applyResolutionEffect(chainCandidate, command.Effect, command.Mutation)
	if err != nil {
		return HypothesisResolutionResult{}, coordinatorError("hypothesis.resolved", "prepare_chain", string(command.SetID), err)
	}
	afterHypothesis, err := hypothesisCandidate.Resolve(command, outcome)
	if err != nil {
		return HypothesisResolutionResult{}, coordinatorError("hypothesis.resolved", "prepare_hypothesis", string(command.SetID), err)
	}
	if err := validateResolutionCandidates(beforeHypothesis, afterHypothesis, outcome, delta); err != nil {
		return HypothesisResolutionResult{}, coordinatorError("hypothesis.resolved", "prepare", string(command.SetID), err)
	}
	revision := afterHypothesis.History[len(afterHypothesis.History)-1]
	payload := journal.HypothesisResolvedPayload{SetID: command.SetID, PreviousRevision: beforeHypothesis.Revision, NewRevision: afterHypothesis.Revision, PreviousStatus: beforeHypothesis.Status, NewStatus: afterHypothesis.Status, AssessmentVersion: command.AssessmentVersion, AssessmentID: command.AssessmentID, AssessmentFingerprint: command.AssessmentFingerprint, AlternativeID: command.AlternativeID, AlternativeKind: command.AlternativeKind, Effect: command.Effect.Clone(), EffectFingerprint: afterHypothesis.Resolution.EffectFingerprint, Outcome: outcome.Clone(), HypothesisRevision: revision, ChainDelta: delta}
	expectation := appendExpectation{kind: journal.RecordKindHypothesisResolved, recordedAt: recordedAt, actor: command.Mutation.Actor, correlation: command.Mutation.CorrelationID, hypothesisResolved: &payload}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, err := c.journal.AppendHypothesisResolved(ctx, journal.HypothesisResolvedInput{SetID: payload.SetID, PreviousRevision: payload.PreviousRevision, NewRevision: payload.NewRevision, PreviousStatus: payload.PreviousStatus, NewStatus: payload.NewStatus, AssessmentVersion: payload.AssessmentVersion, AssessmentID: payload.AssessmentID, AssessmentFingerprint: payload.AssessmentFingerprint, AlternativeID: payload.AlternativeID, AlternativeKind: payload.AlternativeKind, Effect: payload.Effect, EffectFingerprint: payload.EffectFingerprint, Outcome: payload.Outcome, HypothesisRevision: payload.HypothesisRevision, ChainDelta: payload.ChainDelta, RecordedAt: recordedAt, Actor: command.Mutation.Actor, CorrelationID: command.Mutation.CorrelationID})
	result := c.resolutionResult(beforeHypothesis, afterHypothesis, outcome, c.resolutionChainSnapshots(beforeHypothesis, outcome, chainCandidate), record)
	if err != nil {
		observed, failure := c.classifyAppendErrorLocked(beforeSequence, beforeHash, expectation, errors.Join(ErrHypothesisResolutionAppendFailed, err))
		if failure.Outcome == AppendUncertain && observed.RecordHash != "" {
			result.JournalSequence, result.JournalRecordHash = observed.Sequence, observed.RecordHash
		}
		return result, coordinatorError("hypothesis.resolved", "append", string(command.SetID), failure)
	}
	if err := c.acceptAppendLocked("hypothesis.resolved", string(command.SetID), expectation, record, beforeSequence, beforeHash); err != nil {
		return result, err
	}
	result.JournalSequence, result.JournalRecordHash, result.Applied = record.Sequence, record.RecordHash, true
	if c.publishHook != nil {
		if err := c.publishHook(); err != nil {
			c.state = StateDegraded
			c.degradedReason = "publication_failed"
			return result, coordinatorError("hypothesis.resolved", "publish", string(command.SetID), fmt.Errorf("%w: %v", ErrPublicationFailed, err))
		}
	}
	c.current = chainCandidate
	c.currentHypotheses = hypothesisCandidate
	result.Published = true
	return result, nil
}

func (c *Coordinator) idempotentResolutionResult(snapshot hypotheses.Snapshot) HypothesisResolutionResult {
	return HypothesisResolutionResult{SetID: snapshot.ID, AlternativeID: snapshot.Resolution.AlternativeID, AlternativeKind: snapshot.Resolution.AlternativeKind, EffectKind: snapshot.Resolution.EffectKind, HypothesisBefore: snapshot, HypothesisAfter: snapshot, Outcome: snapshot.Resolution.Outcome.Clone(), HypothesisRevision: snapshot.History[len(snapshot.History)-1], Applied: false, Idempotent: true, Published: true}
}

func (c *Coordinator) resolutionResult(before, after hypotheses.Snapshot, outcome hypotheses.ResolutionOutcome, chainSnapshots [2]*chains.Snapshot, _ journal.Record) HypothesisResolutionResult {
	return HypothesisResolutionResult{SetID: after.ID, AlternativeID: after.Resolution.AlternativeID, AlternativeKind: after.Resolution.AlternativeKind, EffectKind: after.Resolution.EffectKind, HypothesisBefore: before, HypothesisAfter: after, ChainBefore: chainSnapshots[0], ChainAfter: chainSnapshots[1], Outcome: outcome.Clone(), HypothesisRevision: after.History[len(after.History)-1]}
}

func (c *Coordinator) resolutionChainSnapshots(before hypotheses.Snapshot, outcome hypotheses.ResolutionOutcome, candidate *registry.Registry) [2]*chains.Snapshot {
	var result [2]*chains.Snapshot
	var id chains.ChainID
	if outcome.AttachObservation != nil {
		id = outcome.AttachObservation.ChainID
		if snapshot, err := c.current.Get(id); err == nil {
			result[0] = &snapshot
		}
	} else if outcome.AddContribution != nil {
		id = outcome.AddContribution.ChainID
		if snapshot, err := c.current.Get(id); err == nil {
			result[0] = &snapshot
		}
	} else if outcome.CreateCandidate != nil {
		id = outcome.CreateCandidate.ChainID
	}
	if id != "" {
		if snapshot, err := candidate.Get(id); err == nil {
			result[1] = &snapshot
		}
	}
	return result
}

func validateResolutionCandidates(before, after hypotheses.Snapshot, outcome hypotheses.ResolutionOutcome, delta ChainResolutionDelta) error {
	if after.Status != hypotheses.StatusResolved || after.Revision != before.Revision+1 || len(after.History) != len(before.History)+1 || delta.Kind != outcome.Kind {
		return ErrHypothesisResolutionResultMismatch
	}
	if after.Resolution == nil || after.Resolution.Outcome.Kind != outcome.Kind || !reflect.DeepEqual(after.Resolution.Outcome, outcome) {
		return ErrHypothesisResolutionResultMismatch
	}
	return nil
}

// applyResolutionEffect mutates only an already-cloned candidate registry. It
// never appends journal records and never selects an alternative.
func applyResolutionEffect(candidate *registry.Registry, effect hypotheses.ResolutionEffect, mutation chains.MutationContext) (hypotheses.ResolutionOutcome, ChainResolutionDelta, error) {
	if candidate == nil {
		return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, ErrRegistryCloneFailed
	}
	if err := mutation.Validate(); err != nil {
		return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, fmt.Errorf("%w: %v", hypotheses.ErrInvalidContext, err)
	}
	fingerprint, err := effect.Fingerprint()
	if err != nil || fingerprint == "" {
		return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, fmt.Errorf("%w: invalid effect", hypotheses.ErrResolutionEffectMismatch)
	}
	_ = fingerprint
	switch effect.Kind {
	case hypotheses.ResolutionEffectAttachObservation:
		if effect.AttachObservation == nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrInvalidResolutionEffect
		}
		command, err := effect.AttachObservation.Command(mutation)
		if err != nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, err
		}
		result, err := candidate.AddObservation(command)
		if err != nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, resolutionChainError(err)
		}
		if result.After.ID != command.ChainID || result.Before.Revision != command.SourceRevision || result.After.Revision != result.Before.Revision+1 || result.After.Status != result.Before.Status || result.After.CurrentConfidence != result.Before.CurrentConfidence || len(result.After.History) != len(result.Before.History)+1 {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrResolutionOutcomeMismatch
		}
		revision := result.After.History[len(result.After.History)-1]
		if revision.Operation != chains.OperationObservationAdded || revision.ObservationIDs == nil || len(revision.ObservationIDs) != 1 || revision.ObservationIDs[0] != command.Observation.ID {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrResolutionOutcomeMismatch
		}
		outcome := hypotheses.ResolutionOutcome{Kind: effect.Kind, AttachObservation: &hypotheses.AttachObservationOutcome{ChainID: result.After.ID, PreviousRevision: result.Before.Revision, NewRevision: result.After.Revision, ObservationID: command.Observation.ID}}
		delta := ChainResolutionDelta{Kind: effect.Kind, ObservationAdded: &journal.ObservationAddedPayload{ChainID: result.After.ID, PreviousRevision: result.Before.Revision, NewRevision: result.After.Revision, Observation: command.Observation, Revision: revision}}
		return outcome, delta, nil
	case hypotheses.ResolutionEffectCreateCandidate:
		if effect.CreateCandidate == nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrInvalidResolutionEffect
		}
		if _, err := candidate.Get(effect.CreateCandidate.ChainID); err == nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrResolutionChainCollision
		} else if !errors.Is(err, registry.ErrChainNotFound) {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, err
		}
		chain, err := effect.CreateCandidate.BuildChain(mutation)
		if err != nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, err
		}
		if err := candidate.Add(chain); err != nil {
			if errors.Is(err, registry.ErrChainAlreadyExists) {
				return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrResolutionChainCollision
			}
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, err
		}
		after, err := candidate.Get(effect.CreateCandidate.ChainID)
		if err != nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, err
		}
		if after.ID != effect.CreateCandidate.ChainID || after.Status != chains.StatusCandidate || after.Revision != 2 || after.CurrentConfidence != effect.CreateCandidate.InitialConfidence || len(after.Contributions) != 0 || len(after.Observations) != 1 || after.Observations[0] != effect.CreateCandidate.Observation || len(after.History) != 2 || after.History[0].Operation != chains.OperationChainCreated || after.History[1].Operation != chains.OperationObservationAdded {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrResolutionOutcomeMismatch
		}
		outcome := hypotheses.ResolutionOutcome{Kind: effect.Kind, CreateCandidate: &hypotheses.CreateCandidateOutcome{ChainID: after.ID, NewRevision: after.Revision, Status: after.Status}}
		return outcome, ChainResolutionDelta{Kind: effect.Kind, ChainAdded: &journal.ChainAddedPayload{Chain: after}}, nil
	case hypotheses.ResolutionEffectAddContribution:
		if effect.AddContribution == nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrInvalidResolutionEffect
		}
		command, err := effect.AddContribution.Command(mutation)
		if err != nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, err
		}
		result, err := candidate.AddContribution(command)
		if err != nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, resolutionChainError(err)
		}
		if result.Before.ID != command.ChainID || result.Before.Revision != command.SourceRevision || result.After.Revision != result.Before.Revision+1 || result.After.Status != result.Before.Status || result.After.HistoricalReliability != result.Before.HistoricalReliability || !reflect.DeepEqual(result.After.Observations, result.Before.Observations) || len(result.After.Contributions) != len(result.Before.Contributions)+1 {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrResolutionOutcomeMismatch
		}
		if !reflect.DeepEqual(result.After.Contributions[len(result.After.Contributions)-1], command.Contribution) {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrResolutionOutcomeMismatch
		}
		revision := result.After.History[len(result.After.History)-1]
		outcome := hypotheses.ResolutionOutcome{Kind: effect.Kind, AddContribution: &hypotheses.AddContributionOutcome{ChainID: result.After.ID, PreviousRevision: result.Before.Revision, NewRevision: result.After.Revision, ContributionID: command.Contribution.ID, PreviousConfidence: result.Before.CurrentConfidence, NewConfidence: result.After.CurrentConfidence}}
		delta := ChainResolutionDelta{Kind: effect.Kind, ContributionAdded: &journal.ContributionAddedPayload{ChainID: result.After.ID, PreviousRevision: result.Before.Revision, NewRevision: result.After.Revision, Contribution: command.Contribution, PreviousConfidence: result.Before.CurrentConfidence, NewConfidence: result.After.CurrentConfidence, PreviousSupportCount: result.Before.ConfirmationCount, NewSupportCount: result.After.ConfirmationCount, PreviousContradictionCount: result.Before.ContradictionCount, NewContradictionCount: result.After.ContradictionCount, Revision: revision}}
		return outcome, delta, nil
	case hypotheses.ResolutionEffectNoChain:
		if effect.NoChainEffect == nil {
			return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrInvalidResolutionEffect
		}
		outcome := hypotheses.ResolutionOutcome{Kind: effect.Kind, NoChainEffect: &hypotheses.NoChainEffectOutcome{ReasonCode: effect.NoChainEffect.ReasonCode}}
		return outcome, ChainResolutionDelta{Kind: effect.Kind, NoChainEffect: &journal.ResolutionNoChainEffectPayload{ReasonCode: effect.NoChainEffect.ReasonCode}}, nil
	default:
		return hypotheses.ResolutionOutcome{}, ChainResolutionDelta{}, hypotheses.ErrUnsupportedResolutionEffect
	}
}

func resolutionChainError(err error) error {
	switch {
	case errors.Is(err, registry.ErrStaleObservationCommand), errors.Is(err, registry.ErrStaleContributionCommand):
		return fmt.Errorf("%w: %v", hypotheses.ErrStaleResolutionChainEffect, err)
	case errors.Is(err, registry.ErrChainNotFound):
		return errors.Join(hypotheses.ErrStaleHypothesisResolution, fmt.Errorf("%w: %v", hypotheses.ErrResolutionChainNotFound, err))
	case errors.Is(err, registry.ErrDuplicateObservation):
		return fmt.Errorf("%w: %v", hypotheses.ErrResolutionObservationCollision, err)
	case errors.Is(err, registry.ErrDuplicateContribution):
		return fmt.Errorf("%w: %v", hypotheses.ErrResolutionContributionCollision, err)
	default:
		return err
	}
}
