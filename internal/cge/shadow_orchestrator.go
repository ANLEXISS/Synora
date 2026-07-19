package cge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/durable"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/hypotheses"
)

// ShadowHypothesisAction describes only append-only hypothesis work. It is
// intentionally unable to represent resolution or lifecycle selection.
type ShadowHypothesisAction string

const (
	ShadowHypothesisNone                ShadowHypothesisAction = "none"
	ShadowHypothesisOpened              ShadowHypothesisAction = "opened"
	ShadowHypothesisRebased             ShadowHypothesisAction = "rebased"
	ShadowHypothesisSuperseded          ShadowHypothesisAction = "superseded"
	ShadowHypothesisUnchanged           ShadowHypothesisAction = "unchanged"
	ShadowHypothesisTerminalPreserved   ShadowHypothesisAction = "terminal_preserved"
	ShadowHypothesisResolutionCandidate ShadowHypothesisAction = "resolution_candidate_only"
)

// ShadowOrchestrationResult is a detached, bounded summary. It contains no
// snapshots, payloads, alternatives, or embedding data.
type ShadowOrchestrationResult struct {
	ObservationID string

	AssociationDecision association.Decision
	AssociationApplied  bool
	ChainID             chains.ChainID

	EvidenceDecision evidence.Decision
	EvidenceApplied  bool

	HypothesisAction ShadowHypothesisAction

	ReevaluationsAttempted int
	ReevaluationsCompleted int

	Stale      bool
	Idempotent bool
	Degraded   bool
	ErrorCode  string
	Deviation  ShadowDeviationResult
}

// ShadowOrchestrator is deliberately independent of Core and has no
// background work. All timestamps come from the injected clock.
type ShadowOrchestrator struct {
	coordinator       *durable.Coordinator
	associationPolicy association.Policy
	evidencePolicy    evidence.Policy
	clock             Clock
	config            CognitiveShadowConfig
	actor             string
	metrics           *shadowMetrics
}

func newShadowOrchestrator(coordinator *durable.Coordinator, config ShadowConfig, clock Clock, metrics *shadowMetrics) *ShadowOrchestrator {
	return &ShadowOrchestrator{coordinator: coordinator, associationPolicy: config.AssociationPolicy, evidencePolicy: config.EvidencePolicy, clock: clock, config: config.Cognitive, actor: config.Actor, metrics: metrics}
}

// ProcessObservation executes association, targeted evidence evaluation, and
// bounded hypothesis maintenance. It never calls ResolveHypothesis.
func (o *ShadowOrchestrator) ProcessObservation(ctx context.Context, observation chains.ObservationRef, situationKind string) (result ShadowOrchestrationResult, err error) {
	if err := contextErr(ctx); err != nil {
		return result, err
	}
	result.ObservationID = observation.ID
	if o == nil || o.coordinator == nil {
		return result, errors.New("shadow orchestrator is not configured")
	}
	if o.coordinator.Status().State == durable.StateDegraded {
		return o.degradedResult(result), durable.ErrCoordinatorDegraded
	}

	plannedAt := o.nowAt(observation.Timestamp)
	plan, err := o.coordinator.PlanAssociation(association.Input{Observation: observation, SituationKind: situationKind}, plannedAt, o.associationPolicy)
	if err != nil {
		o.count("association_errors")
		result.ErrorCode = ErrorCode(err)
		return result, err
	}
	result.AssociationDecision = plan.Decision
	if o.metrics != nil {
		o.metrics.plan(string(plan.Decision))
	}
	switch plan.Decision {
	case association.DecisionAmbiguous:
		o.count("association_ambiguous")
		result.HypothesisAction, err = o.processAssociationAmbiguity(ctx, plan, plannedAt)
		if err != nil {
			result.ErrorCode = ErrorCode(err)
			o.count("association_errors")
		}
		return result, err
	case association.DecisionAlreadyAttached:
		result.ChainID = plan.SelectedChainID
		result.Idempotent = true
		o.count("association_already_attached")
		return result, nil
	case association.DecisionAttachExisting, association.DecisionCreateCandidate:
		mutationAt := o.nowAt(observation.Timestamp)
		correlation := shadowCorrelation(observation.ID, "association")
		applied, applyErr := o.coordinator.ApplyAssociationPlanWithReason(ctx, plan, o.actor, correlation, "shadow.association_applied", mutationAt, mutationAt)
		if applyErr != nil {
			result.ErrorCode = ErrorCode(applyErr)
			if isStale(applyErr) {
				result.Stale = true
			}
			if o.coordinator.Status().State == durable.StateDegraded {
				return o.degradedResult(result), applyErr
			}
			o.count("association_errors")
			return result, applyErr
		}
		result.AssociationApplied = applied.Applied
		result.ChainID = applied.ChainID
		result.Idempotent = applied.Idempotent
		if o.metrics != nil {
			o.metrics.applied(string(plan.Decision), mutationAt, applied.Idempotent)
		}
		if applied.Idempotent {
			o.count("association_already_attached")
		} else if plan.Decision == association.DecisionAttachExisting {
			o.count("association_attach_applied")
		} else {
			o.count("association_create_applied")
		}
	default:
		return result, fmt.Errorf("unsupported association decision %q", plan.Decision)
	}

	if o.coordinator.Status().State == durable.StateDegraded {
		return o.degradedResult(result), durable.ErrCoordinatorDegraded
	}
	chainSnapshot, err := o.coordinator.Get(result.ChainID)
	if err != nil {
		result.ErrorCode = ErrorCode(err)
		o.count("evidence_errors")
		return result, err
	}
	evaluatedAt := o.nowAt(observation.Timestamp)
	evaluation, err := evidence.EvaluateObservation(chainSnapshot, observation.ID, evaluatedAt, o.evidencePolicy)
	if err != nil {
		result.ErrorCode = ErrorCode(err)
		o.count("evidence_errors")
		return result, err
	}
	result.EvidenceDecision = evaluation.Decision
	o.count("evidence_evaluated")
	o.countEvidenceDecision(evaluation.Decision)
	result.HypothesisAction, result.EvidenceApplied, result.Idempotent, err = o.processEvidenceEvaluation(ctx, evaluation, evaluatedAt)
	if err != nil {
		result.ErrorCode = ErrorCode(err)
		if isStale(err) {
			result.Stale = true
		}
		if o.coordinator.Status().State == durable.StateDegraded {
			return o.degradedResult(result), err
		}
		return result, err
	}
	if o.coordinator.Status().State == durable.StateDegraded {
		return o.degradedResult(result), durable.ErrCoordinatorDegraded
	}
	o.reevaluateOpenEvidence(ctx, &result, evaluatedAt)
	return result, nil
}

func (o *ShadowOrchestrator) processAssociationAmbiguity(ctx context.Context, plan association.Plan, at time.Time) (ShadowHypothesisAction, error) {
	current, found, err := o.coordinator.FindCurrentAssociationSubject(plan.Observation.ID)
	if err != nil {
		return ShadowHypothesisNone, err
	}
	if found && isTerminal(current.Status) {
		o.count("association_hypothesis_terminal")
		return ShadowHypothesisTerminalPreserved, nil
	}
	if !found {
		set, err := hypotheses.FromAmbiguousAssociation(plan, at, o.mutation(at, "shadow.association_hypothesis_opened", plan.Observation.ID, "association"))
		if err != nil {
			return ShadowHypothesisNone, err
		}
		opened, err := o.coordinator.AddHypothesis(ctx, set, at)
		if err != nil {
			if isIdempotentHypothesisError(err) {
				o.count("association_hypothesis_idempotent")
				return ShadowHypothesisUnchanged, nil
			}
			return ShadowHypothesisNone, err
		}
		if opened.Idempotent {
			o.count("association_hypothesis_idempotent")
			return ShadowHypothesisUnchanged, nil
		}
		o.count("association_hypothesis_opened")
		return ShadowHypothesisOpened, nil
	}
	proposal, err := hypotheses.ProposeAssociationRebase(current, plan, at)
	if err != nil {
		if errors.Is(err, hypotheses.ErrHypothesisRebaseUnchanged) {
			o.count("association_hypothesis_idempotent")
			return ShadowHypothesisUnchanged, nil
		}
		return ShadowHypothesisNone, err
	}
	command, err := proposal.Command(o.mutation(at, "shadow.association_hypothesis_rebased", plan.Observation.ID, "association"))
	if err != nil {
		return ShadowHypothesisNone, err
	}
	rebased, err := o.coordinator.RebaseHypothesis(ctx, command, at)
	if err != nil {
		if errors.Is(err, hypotheses.ErrHypothesisRebaseUnchanged) {
			o.count("association_hypothesis_idempotent")
			return ShadowHypothesisUnchanged, nil
		}
		return ShadowHypothesisNone, err
	}
	if rebased.Idempotent {
		o.count("association_hypothesis_idempotent")
		return ShadowHypothesisUnchanged, nil
	}
	o.count("association_hypothesis_rebased")
	return ShadowHypothesisRebased, nil
}

func (o *ShadowOrchestrator) processEvidenceEvaluation(ctx context.Context, evaluation evidence.EvidenceEvaluation, at time.Time) (ShadowHypothesisAction, bool, bool, error) {
	current, found, err := o.coordinator.FindCurrentEvidenceSubject(evaluation.ChainID, evaluation.TargetObservationID)
	if err != nil {
		return ShadowHypothesisNone, false, false, err
	}
	if found && isTerminal(current.Status) {
		o.count("evidence_hypothesis_terminal")
		return ShadowHypothesisTerminalPreserved, false, false, nil
	}
	switch evaluation.Decision {
	case evidence.DecisionAlreadyEvaluated:
		return ShadowHypothesisUnchanged, false, true, nil
	case evidence.DecisionInsufficientEvidence:
		return ShadowHypothesisNone, false, false, nil
	case evidence.DecisionAmbiguous:
		if !found {
			set, buildErr := hypotheses.FromAmbiguousEvidence(evaluation, at, o.mutation(at, "shadow.evidence_hypothesis_opened", string(evaluation.ChainID)+":"+evaluation.TargetObservationID, "evidence"))
			if buildErr != nil {
				return ShadowHypothesisNone, false, false, buildErr
			}
			opened, addErr := o.coordinator.AddHypothesis(ctx, set, at)
			if addErr != nil {
				if isIdempotentHypothesisError(addErr) {
					o.count("evidence_hypothesis_idempotent")
					return ShadowHypothesisUnchanged, false, true, nil
				}
				return ShadowHypothesisNone, false, false, addErr
			}
			if opened.Idempotent {
				o.count("evidence_hypothesis_idempotent")
				return ShadowHypothesisUnchanged, false, true, nil
			}
			o.count("evidence_hypothesis_opened")
			return ShadowHypothesisOpened, false, false, nil
		}
		if current.Subject.EvidenceFingerprint == evaluation.EvidenceFingerprint {
			proposal, proposalErr := hypotheses.ProposeEvidenceRebase(current, evaluation, at)
			if proposalErr != nil {
				if errors.Is(proposalErr, hypotheses.ErrHypothesisRebaseUnchanged) {
					o.count("evidence_hypothesis_idempotent")
					return ShadowHypothesisUnchanged, false, true, nil
				}
				return ShadowHypothesisNone, false, false, proposalErr
			}
			command, commandErr := proposal.Command(o.mutation(at, "shadow.evidence_hypothesis_rebased", string(evaluation.ChainID)+":"+evaluation.TargetObservationID, "evidence"))
			if commandErr != nil {
				return ShadowHypothesisNone, false, false, commandErr
			}
			rebased, rebaseErr := o.coordinator.RebaseHypothesis(ctx, command, at)
			if rebaseErr != nil {
				if errors.Is(rebaseErr, hypotheses.ErrHypothesisRebaseUnchanged) {
					o.count("evidence_hypothesis_idempotent")
					return ShadowHypothesisUnchanged, false, true, nil
				}
				return ShadowHypothesisNone, false, false, rebaseErr
			}
			if rebased.Idempotent {
				o.count("evidence_hypothesis_idempotent")
				return ShadowHypothesisUnchanged, false, true, nil
			}
			o.count("evidence_hypothesis_rebased")
			return ShadowHypothesisRebased, false, false, nil
		}
		proposal, proposalErr := hypotheses.ProposeEvidenceSupersession(current, evaluation, at)
		if proposalErr != nil {
			return ShadowHypothesisNone, false, false, proposalErr
		}
		command, commandErr := proposal.Command(o.mutation(at, "shadow.evidence_hypothesis_superseded", string(evaluation.ChainID)+":"+evaluation.TargetObservationID, "evidence"))
		if commandErr != nil {
			return ShadowHypothesisNone, false, false, commandErr
		}
		superseded, supersedeErr := o.coordinator.SupersedeHypothesis(ctx, command, at)
		if supersedeErr != nil {
			return ShadowHypothesisNone, false, false, supersedeErr
		}
		if superseded.Idempotent {
			o.count("evidence_hypothesis_idempotent")
			return ShadowHypothesisUnchanged, false, true, nil
		}
		o.count("evidence_hypothesis_superseded")
		return ShadowHypothesisSuperseded, false, false, nil
	default:
		if evaluation.Proposal == nil {
			return ShadowHypothesisNone, false, false, fmt.Errorf("evidence decision %q has no proposal", evaluation.Decision)
		}
		if found {
			o.count("evidence_resolution_candidate")
			return ShadowHypothesisResolutionCandidate, false, false, nil
		}
		if !o.config.AutoApplyDecisiveEvidence {
			return ShadowHypothesisNone, false, false, nil
		}
		mutationAt := o.nowAt(at)
		command, commandErr := evaluation.Proposal.Command(o.mutation(mutationAt, "shadow.evidence_contribution_applied", string(evaluation.ChainID)+":"+evaluation.TargetObservationID, "evidence"))
		if commandErr != nil {
			return ShadowHypothesisNone, false, false, commandErr
		}
		if existing, getErr := o.coordinator.Get(evaluation.ChainID); getErr == nil {
			for _, contribution := range existing.Contributions {
				if contribution.ID == command.Contribution.ID {
					o.count("evidence_contribution_idempotent")
					return ShadowHypothesisUnchanged, false, true, nil
				}
			}
		}
		applied, applyErr := o.coordinator.AddContribution(ctx, command, mutationAt)
		if applyErr != nil {
			if isStale(applyErr) {
				o.count("evidence_contribution_stale")
			}
			return ShadowHypothesisNone, false, false, applyErr
		}
		if applied.Published {
			switch evaluation.Decision {
			case evidence.DecisionProposeSupport:
				o.count("evidence_contribution_support_applied")
			case evidence.DecisionProposeContradiction:
				o.count("evidence_contribution_contradiction_applied")
			case evidence.DecisionProposeNeutral:
				o.count("evidence_contribution_neutral_applied")
			}
		}
		return ShadowHypothesisNone, applied.Published, false, nil
	}
}

func (o *ShadowOrchestrator) reevaluateOpenEvidence(ctx context.Context, result *ShadowOrchestrationResult, at time.Time) {
	if result == nil || result.ChainID == "" || o.config.MaxEvidenceReevaluationsPerObservation <= 0 {
		return
	}
	open, err := o.coordinator.ListOpenEvidenceForChain(result.ChainID)
	if err != nil {
		return
	}
	chainSnapshot, err := o.coordinator.Get(result.ChainID)
	if err != nil {
		return
	}
	targetTimes := make(map[string]time.Time, len(chainSnapshot.Observations))
	for _, observation := range chainSnapshot.Observations {
		targetTimes[observation.ID] = observation.Timestamp
	}
	type candidate struct {
		set      hypotheses.Snapshot
		distance time.Duration
		target   time.Time
	}
	candidates := make([]candidate, 0, len(open))
	for _, set := range open {
		if set.Subject.ObservationID == result.ObservationID {
			continue
		}
		target, ok := targetTimes[set.Subject.ObservationID]
		if !ok || target.After(at) || at.Sub(target) > o.evidencePolicy.ContextWindow {
			continue
		}
		candidates = append(candidates, candidate{set: set, distance: at.Sub(target), target: target})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		if !candidates[i].target.Equal(candidates[j].target) {
			return candidates[i].target.Before(candidates[j].target)
		}
		return candidates[i].set.ID < candidates[j].set.ID
	})
	if len(candidates) > o.config.MaxEvidenceReevaluationsPerObservation {
		o.count("hypothesis_reevaluation_limit_reached")
		candidates = candidates[:o.config.MaxEvidenceReevaluationsPerObservation]
	}
	for _, item := range candidates {
		if o.coordinator.Status().State == durable.StateDegraded {
			result.Degraded = true
			o.count("orchestration_degraded")
			return
		}
		result.ReevaluationsAttempted++
		current, getErr := o.coordinator.Get(result.ChainID)
		if getErr != nil {
			continue
		}
		evaluation, evalErr := evidence.EvaluateObservation(current, item.set.Subject.ObservationID, at, o.evidencePolicy)
		if evalErr != nil {
			continue
		}
		o.count("hypotheses_reevaluated")
		result.ReevaluationsCompleted++
		o.countEvidenceDecision(evaluation.Decision)
		action, _, _, processErr := o.processEvidenceEvaluation(ctx, evaluation, at)
		if processErr != nil {
			if isStale(processErr) {
				result.Stale = true
			}
			continue
		}
		if action == ShadowHypothesisResolutionCandidate {
			o.count("evidence_resolution_candidate")
		}
	}
}

func (o *ShadowOrchestrator) mutation(at time.Time, reason, seed, phase string) chains.MutationContext {
	return chains.MutationContext{At: at, Actor: o.actor, Reason: reason, CorrelationID: shadowCorrelation(seed, phase)}
}

func (o *ShadowOrchestrator) nowAt(minimum time.Time) time.Time {
	now := time.Time{}
	if o != nil && o.clock != nil {
		now = o.clock.Now().UTC()
	}
	if now.IsZero() || now.Before(minimum) {
		return minimum.UTC()
	}
	return now
}

func (o *ShadowOrchestrator) degradedResult(result ShadowOrchestrationResult) ShadowOrchestrationResult {
	result.Degraded = true
	o.count("orchestration_degraded")
	return result
}

func (o *ShadowOrchestrator) count(name string) {
	if o != nil && o.metrics != nil {
		o.metrics.cognitive(name)
	}
}

func (o *ShadowOrchestrator) countEvidenceDecision(decision evidence.Decision) {
	switch decision {
	case evidence.DecisionProposeSupport:
		o.count("evidence_support_proposed")
	case evidence.DecisionProposeContradiction:
		o.count("evidence_contradiction_proposed")
	case evidence.DecisionProposeNeutral:
		o.count("evidence_neutral_proposed")
	case evidence.DecisionInsufficientEvidence:
		o.count("evidence_insufficient")
	case evidence.DecisionAmbiguous:
		o.count("evidence_ambiguous")
	case evidence.DecisionAlreadyEvaluated:
		o.count("evidence_already_evaluated")
	}
}

func isTerminal(status hypotheses.Status) bool {
	return status == hypotheses.StatusResolved || status == hypotheses.StatusInvalidated || status == hypotheses.StatusSuperseded
}

func isStale(err error) bool {
	return errors.Is(err, association.ErrStaleAssociationPlan) || errors.Is(err, durable.ErrStaleContributionCommand) || errors.Is(err, durable.ErrStaleHypothesisRebase) || errors.Is(err, durable.ErrStaleHypothesisSupersession) || errors.Is(err, hypotheses.ErrStaleHypothesisRebase) || errors.Is(err, hypotheses.ErrStaleHypothesisSupersession)
}

func isIdempotentHypothesisError(err error) bool {
	return errors.Is(err, hypotheses.ErrHypothesisAlreadyExists) || errors.Is(err, hypotheses.ErrHypothesisRebaseUnchanged)
}

func shadowCorrelation(seed, phase string) string {
	digest := sha256.Sum256([]byte(seed))
	return "cge-shadow:" + hex.EncodeToString(digest[:8]) + ":" + phase
}
