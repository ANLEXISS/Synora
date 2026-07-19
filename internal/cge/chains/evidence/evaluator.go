package evidence

import (
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/chains"
)

// EvidenceEvaluation is a complete, detached explanation of one target
// observation at one chain revision.
type EvidenceEvaluation struct {
	ChainID        chains.ChainID
	SourceRevision uint64

	TargetObservationID string
	EvaluatedAt         time.Time

	PolicyNamespace     string
	PolicyVersion       string
	EvidenceFingerprint string
	ResolutionValues    ResolutionValues

	Decision Decision

	SupportScore       int64
	ContradictionScore int64
	DecisionMargin     int64

	ContextObservationIDs []string
	Facts                 []EvidenceFact

	Proposal *ContributionProposal

	ReasonCode string
	Reason     string
}

// Evaluation is the shorter compatibility name for EvidenceEvaluation.
type Evaluation = EvidenceEvaluation

// EvaluateObservation evaluates only the explicitly named observation in the
// supplied snapshot. It is pure: no snapshot, chain, registry, journal, or
// global state is modified.
func EvaluateObservation(snapshot chains.Snapshot, targetObservationID string, evaluatedAt time.Time, policy Policy) (EvidenceEvaluation, error) {
	return Evaluate(Input{Chain: snapshot, TargetObservationID: targetObservationID}, evaluatedAt, policy)
}

// Evaluate is the Input-oriented form of EvaluateObservation.
func Evaluate(input Input, evaluatedAt time.Time, policy Policy) (EvidenceEvaluation, error) {
	if err := policy.Validate(); err != nil {
		return EvidenceEvaluation{}, err
	}
	if evaluatedAt.IsZero() {
		return EvidenceEvaluation{}, evidenceError(ErrInvalidEvaluationTime, "evaluate", string(input.Chain.ID), input.TargetObservationID, "zero", nil)
	}
	if err := input.Validate(); err != nil {
		return EvidenceEvaluation{}, err
	}
	if !IsEvidenceEvaluationAllowed(input.Chain.Status) {
		return EvidenceEvaluation{}, evidenceError(ErrEvidenceEvaluationNotAllowed, "evaluate", string(input.Chain.ID), input.TargetObservationID, string(input.Chain.Status), nil)
	}
	target, found := findObservation(input.Chain, input.TargetObservationID)
	if !found {
		return EvidenceEvaluation{}, evidenceError(ErrTargetObservationNotFound, "evaluate", string(input.Chain.ID), input.TargetObservationID, "target", nil)
	}
	if evaluatedAt.Before(target.Timestamp) {
		return EvidenceEvaluation{}, evidenceError(ErrInvalidEvaluationTime, "evaluate", string(input.Chain.ID), input.TargetObservationID, "before_target", nil)
	}
	if err := validateObservationType(target); err != nil {
		return EvidenceEvaluation{}, err
	}

	context := selectContext(input.Chain.Observations, target, evaluatedAt, policy)
	facts := buildFacts(input.Chain, target, context, policy)
	support, contradiction, err := scoreFacts(facts)
	if err != nil {
		return EvidenceEvaluation{}, evidenceError(ErrInvalidEvidenceInput, "score", string(input.Chain.ID), target.ID, "fact", err)
	}
	fingerprint, err := evidenceFingerprint(policy.Namespace, input.Chain.ID, target.ID, observationIDs(context))
	if err != nil {
		return EvidenceEvaluation{}, evidenceError(ErrInvalidEvidenceInput, "fingerprint", string(input.Chain.ID), target.ID, "sha256", err)
	}

	decision := decide(target, context, support, contradiction, facts, policy)
	result := EvidenceEvaluation{
		ChainID:             input.Chain.ID,
		SourceRevision:      input.Chain.Revision,
		TargetObservationID: target.ID,
		EvaluatedAt:         evaluatedAt,
		PolicyNamespace:     policy.Namespace,
		PolicyVersion:       policy.Version,
		EvidenceFingerprint: fingerprint,
		ResolutionValues: ResolutionValues{
			SupportValue:       policy.SupportValue,
			ContradictionValue: policy.ContradictionValue,
			NeutralValue:       policy.NeutralValue,
		},
		Decision:              decision,
		SupportScore:          support,
		ContradictionScore:    contradiction,
		DecisionMargin:        absoluteDifference(support, contradiction),
		ContextObservationIDs: append([]string(nil), observationIDs(context)...),
		Facts:                 cloneFacts(facts),
		ReasonCode:            reasonCode(decision),
		Reason:                fmt.Sprintf("%s support=%d contradiction=%d policy=%s", reasonCode(decision), support, contradiction, policy.Version),
	}

	if isContributive(decision) {
		proposal, err := makeProposal(result, target, context, fingerprint, policy)
		if err != nil {
			return EvidenceEvaluation{}, err
		}
		result.Proposal = proposal
	}
	if existing, ok := findContribution(input.Chain.Contributions, contributionID(fingerprint)); ok {
		if result.Proposal != nil {
			if !sameContribution(existing, result.Proposal.Contribution) {
				return EvidenceEvaluation{}, evidenceError(ErrEvidenceContributionCollision, "idempotence", string(input.Chain.ID), target.ID, "contribution_id", nil)
			}
		} else {
			// A same fingerprint that no longer yields the same directional
			// proposal cannot be silently treated as idempotent. The caller must
			// explicitly resolve the policy/data change.
			return EvidenceEvaluation{}, evidenceError(ErrEvidenceContributionCollision, "idempotence", string(input.Chain.ID), target.ID, "contribution_id", nil)
		}
		result.Decision = DecisionAlreadyEvaluated
		result.ReasonCode = reasonCode(result.Decision)
		result.Reason = fmt.Sprintf("%s policy=%s", result.ReasonCode, policy.Version)
		result.Proposal = nil
	}

	return cloneEvaluation(result), nil
}

func validateObservationType(observation chains.ObservationRef) error {
	switch observation.EventType {
	case "vision.identity", "vision.unknown", "vision.uncertain":
		return nil
	default:
		return evidenceError(ErrUnsupportedObservationType, "input", "", observation.ID, observation.EventType, nil)
	}
}

type contextObservation struct {
	Observation chains.ObservationRef
	Distance    time.Duration
}

func selectContext(observations []chains.ObservationRef, target chains.ObservationRef, evaluatedAt time.Time, policy Policy) []chains.ObservationRef {
	selected := make([]contextObservation, 0, len(observations))
	for _, observation := range observations {
		if observation.ID == target.ID || observation.Timestamp.After(evaluatedAt) {
			continue
		}
		distance := absoluteTimeDistance(target.Timestamp, observation.Timestamp)
		if distance <= policy.ContextWindow {
			selected = append(selected, contextObservation{Observation: observation, Distance: distance})
		}
	}
	sort.SliceStable(selected, func(i, j int) bool {
		left, right := selected[i], selected[j]
		if left.Distance != right.Distance {
			return left.Distance < right.Distance
		}
		if !left.Observation.Timestamp.Equal(right.Observation.Timestamp) {
			return left.Observation.Timestamp.Before(right.Observation.Timestamp)
		}
		return left.Observation.ID < right.Observation.ID
	})
	if len(selected) > policy.MaxContextObservations {
		selected = selected[:policy.MaxContextObservations]
	}
	result := make([]chains.ObservationRef, len(selected))
	for i, item := range selected {
		result[i] = item.Observation
	}
	return result
}

func buildFacts(snapshot chains.Snapshot, target chains.ObservationRef, context []chains.ObservationRef, policy Policy) []EvidenceFact {
	facts := make([]EvidenceFact, 0, 4+len(context)*8)
	switch target.EventType {
	case "vision.identity":
		facts = append(facts, EvidenceFact{Code: "type.identity", Side: EvidenceNeutral, Detail: "identity observation"})
	case "vision.uncertain":
		facts = append(facts, EvidenceFact{Code: "type.uncertain", Side: EvidenceNeutral, Detail: "uncertain observation"})
		if policy.UncertainEvidencePenalty > 0 {
			facts = append(facts, EvidenceFact{Code: "type.uncertain_penalty", Side: EvidenceNeutral, Score: -policy.UncertainEvidencePenalty, Detail: "uncertain evidence penalty"})
		}
	case "vision.unknown":
		facts = append(facts, EvidenceFact{Code: "type.unknown", Side: EvidenceNeutral, Detail: "unknown observation"})
	}

	if snapshot.EntityID != "" && target.EntityID != "" {
		if snapshot.EntityID == target.EntityID {
			facts = append(facts, EvidenceFact{Code: "entity.assigned_same", Side: EvidenceSupport, Score: policy.SameAssignedEntityScore, ObservationIDs: []string{target.ID}, Detail: "target matches assigned entity"})
		} else if target.EventType == "vision.identity" {
			facts = append(facts, EvidenceFact{Code: "entity.assigned_conflict", Side: EvidenceContradiction, Score: policy.AssignedEntityConflictScore, ObservationIDs: []string{target.ID}, Detail: "target conflicts with assigned entity"})
		} else {
			facts = append(facts, EvidenceFact{Code: "entity.assigned_conflict", Side: EvidenceNeutral, ObservationIDs: []string{target.ID}, Detail: "uncertain or unknown identity is not a strong conflict"})
		}
	}

	mixedIDs := make([]string, 0)
	for _, other := range context {
		ids := []string{target.ID, other.ID}
		if target.EntityID != "" && other.EntityID != "" {
			if target.EntityID == other.EntityID {
				facts = append(facts, EvidenceFact{Code: "entity.context_same", Side: EvidenceSupport, Score: policy.SameObservedEntityScore, ObservationIDs: ids, Detail: "target matches context entity"})
			} else {
				mixedIDs = append(mixedIDs, other.ID)
				if target.EventType == "vision.identity" && strongContinuity(target, other) {
					facts = append(facts, EvidenceFact{Code: "entity.context_conflict", Side: EvidenceContradiction, Score: policy.ObservedEntityConflictScore, ObservationIDs: ids, Detail: "linked context contains a different entity"})
				}
			}
		}
		if equalNonEmpty(target.SequenceKey, other.SequenceKey) {
			facts = append(facts, EvidenceFact{Code: "sequence.same", Side: EvidenceSupport, Score: policy.SameSequenceScore, ObservationIDs: ids, Detail: "same sequence"})
		}
		if equalNonEmpty(target.ActivationID, other.ActivationID) {
			facts = append(facts, EvidenceFact{Code: "activation.same", Side: EvidenceSupport, Score: policy.SameActivationScore, ObservationIDs: ids, Detail: "same activation"})
		}
		if equalNonEmpty(target.TrackID, other.TrackID) && sameOrAbsentDevice(target, other) {
			facts = append(facts, EvidenceFact{Code: "track.same", Side: EvidenceSupport, Score: policy.SameTrackScore, ObservationIDs: ids, Detail: "same track in compatible scope"})
		}
		if equalNonEmpty(target.DeviceID, other.DeviceID) {
			facts = append(facts, EvidenceFact{Code: "device.same", Side: EvidenceSupport, Score: policy.SameDeviceScore, ObservationIDs: ids, Detail: "same device"})
		}
		if equalNonEmpty(target.NodeID, other.NodeID) {
			facts = append(facts, EvidenceFact{Code: "node.same", Side: EvidenceSupport, Score: policy.SameNodeScore, ObservationIDs: ids, Detail: "same node"})
		}
		facts = append(facts, EvidenceFact{Code: "time.close", Side: EvidenceSupport, Score: policy.TemporalCloseScore, ObservationIDs: ids, Detail: "within context window"})
	}
	if len(context) == 0 {
		facts = append(facts, EvidenceFact{Code: "context.empty", Side: EvidenceNeutral, ObservationIDs: []string{target.ID}, Detail: "no observation in context window"})
	} else if len(mixedIDs) > 0 {
		ids := append([]string{target.ID}, mixedIDs...)
		facts = append(facts, EvidenceFact{Code: "context.mixed_entities", Side: EvidenceNeutral, ObservationIDs: ids, Detail: "context includes an unlinked entity difference"})
	}
	return facts
}

func strongContinuity(left, right chains.ObservationRef) bool {
	return equalNonEmpty(left.SequenceKey, right.SequenceKey) ||
		equalNonEmpty(left.ActivationID, right.ActivationID) ||
		(equalNonEmpty(left.TrackID, right.TrackID) && equalNonEmpty(left.DeviceID, right.DeviceID))
}

func sameOrAbsentDevice(left, right chains.ObservationRef) bool {
	return left.DeviceID == "" || right.DeviceID == "" || left.DeviceID == right.DeviceID
}

func equalNonEmpty(left, right string) bool { return left != "" && right != "" && left == right }

func decide(target chains.ObservationRef, context []chains.ObservationRef, support, contradiction int64, facts []EvidenceFact, policy Policy) Decision {
	if len(context) == 0 {
		// A target without nearby context is never directional evidence. Even a
		// permissive caller-supplied threshold cannot turn absence of context
		// into support or contradiction.
		return DecisionInsufficientEvidence
	}
	supportEligible := support >= policy.MinimumSupportScore
	contradictionEligible := contradiction >= policy.MinimumContradictionScore
	difference := absoluteDifference(support, contradiction)
	if supportEligible && contradictionEligible {
		return DecisionAmbiguous
	}
	if supportEligible && difference >= policy.MinimumDecisionMargin && !contradictionEligible {
		return DecisionProposeSupport
	}
	if contradictionEligible && difference >= policy.MinimumDecisionMargin && !supportEligible {
		return DecisionProposeContradiction
	}
	if support > 0 && contradiction > 0 && difference < policy.MinimumDecisionMargin &&
		(support+policy.MinimumDecisionMargin >= policy.MinimumSupportScore || contradiction+policy.MinimumDecisionMargin >= policy.MinimumContradictionScore) {
		return DecisionAmbiguous
	}
	if (target.EventType == "vision.unknown" || target.EventType == "vision.uncertain") && len(context) > 0 && support > 0 && contradiction == 0 {
		return DecisionProposeNeutral
	}
	return DecisionInsufficientEvidence
}

func makeProposal(result EvidenceEvaluation, target chains.ObservationRef, context []chains.ObservationRef, fingerprint string, policy Policy) (*ContributionProposal, error) {
	var kind chains.ContributionKind
	var value float64
	switch result.Decision {
	case DecisionProposeSupport:
		kind, value = chains.ContributionSupport, policy.SupportValue
	case DecisionProposeContradiction:
		kind, value = chains.ContributionContradiction, policy.ContradictionValue
	case DecisionProposeNeutral:
		kind, value = chains.ContributionNeutral, policy.NeutralValue
	default:
		return nil, evidenceError(ErrInvalidContributionProposal, "proposal", string(result.ChainID), target.ID, "decision", nil)
	}
	observationIDs := append([]string{target.ID}, observationIDs(context)...)
	contribution := chains.ConfidenceContribution{
		ID:             contributionID(fingerprint),
		Source:         sourceFor(policy),
		Kind:           kind,
		Value:          value,
		ObservationIDs: observationIDs,
		Reason:         proposalReason(result.Decision, result.SupportScore, result.ContradictionScore, policy.Version),
		CreatedAt:      result.EvaluatedAt,
	}
	proposal := &ContributionProposal{
		ChainID:             result.ChainID,
		SourceRevision:      result.SourceRevision,
		Contribution:        contribution,
		PolicyNamespace:     policy.Namespace,
		PolicyVersion:       policy.Version,
		TargetObservationID: target.ID,
		EvidenceFingerprint: fingerprint,
		ReasonCode:          result.ReasonCode,
		Decision:            result.Decision,
	}
	if err := proposal.Validate(); err != nil {
		return nil, evidenceError(ErrInvalidContributionProposal, "proposal", string(result.ChainID), target.ID, "validation", err)
	}
	return proposal, nil
}

func findContribution(contributions []chains.ConfidenceContribution, id string) (chains.ConfidenceContribution, bool) {
	for _, contribution := range contributions {
		if contribution.ID == id {
			return contribution.Clone(), true
		}
	}
	return chains.ConfidenceContribution{}, false
}

func observationIDs(observations []chains.ObservationRef) []string {
	result := make([]string, len(observations))
	for i, observation := range observations {
		result[i] = observation.ID
	}
	return result
}

func isContributive(decision Decision) bool {
	return decision == DecisionProposeSupport || decision == DecisionProposeContradiction || decision == DecisionProposeNeutral
}

func reasonCode(decision Decision) string { return "evidence." + string(decision) }

func absoluteDifference(left, right int64) int64 {
	if left >= right {
		return left - right
	}
	return right - left
}

func absoluteTimeDistance(left, right time.Time) time.Duration {
	if left.Before(right) {
		return right.Sub(left)
	}
	return left.Sub(right)
}

func cloneEvaluation(result EvidenceEvaluation) EvidenceEvaluation {
	result.ContextObservationIDs = append([]string(nil), result.ContextObservationIDs...)
	result.Facts = cloneFacts(result.Facts)
	if result.Proposal != nil {
		proposal := *result.Proposal
		proposal.Contribution = proposal.Contribution.Clone()
		result.Proposal = &proposal
	}
	return result
}
