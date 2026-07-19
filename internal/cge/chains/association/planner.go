package association

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/chains"
)

// PlanAssociation is a pure deterministic planner over defensive snapshots.
func PlanAssociation(snapshots []chains.Snapshot, input Input, plannedAt time.Time, policy Policy) (Plan, error) {
	if err := input.Validate(); err != nil {
		return Plan{}, err
	}
	if plannedAt.IsZero() {
		return Plan{}, fmt.Errorf("%w: planned_at is zero", ErrInvalidInput)
	}
	if err := policy.Validate(); err != nil {
		return Plan{}, err
	}
	plan := Plan{PolicyVersion: policy.Version, PlannedAt: plannedAt, Observation: input.Observation, ReasonCode: ReasonCreateCandidate}
	attachments := make([]chains.ChainID, 0)
	seenIDs := make(map[chains.ChainID]struct{}, len(snapshots))
	validSnapshots := make([]chains.Snapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if _, exists := seenIDs[snapshot.ID]; exists {
			return Plan{}, fmt.Errorf("%w: duplicate chain %s", ErrInvalidSnapshot, snapshot.ID)
		}
		seenIDs[snapshot.ID] = struct{}{}
		if _, err := chains.Restore(snapshot); err != nil {
			return Plan{}, fmt.Errorf("%w: chain %s: %v", ErrInvalidSnapshot, snapshot.ID, err)
		}
		for _, observation := range snapshot.Observations {
			if observation.ID == input.Observation.ID {
				attachments = append(attachments, snapshot.ID)
			}
		}
		validSnapshots = append(validSnapshots, snapshot)
	}
	if len(attachments) > 1 {
		sort.Slice(attachments, func(i, j int) bool { return attachments[i] < attachments[j] })
		return Plan{}, MultipleAttachmentError{ObservationID: input.Observation.ID, ChainIDs: append([]chains.ChainID(nil), attachments...)}
	}
	if len(attachments) == 1 {
		for _, snapshot := range validSnapshots {
			if snapshot.ID == attachments[0] {
				plan.Decision = DecisionAlreadyAttached
				plan.SelectedChainID = snapshot.ID
				plan.SelectedSourceRevision = snapshot.Revision
				plan.ReasonCode = ReasonAlreadyAttached
				plan.Reason = "observation is already present in the selected chain"
				return plan, nil
			}
		}
	}

	candidates := make([]CandidateScore, 0, len(validSnapshots))
	for _, snapshot := range validSnapshots {
		candidates = append(candidates, scoreCandidate(snapshot, input, policy))
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].ChainID < candidates[j].ChainID
	})
	plan.RankedCandidates = append([]CandidateScore(nil), candidates...)
	if len(plan.RankedCandidates) > policy.MaxRankedCandidates {
		plan.RankedCandidates = plan.RankedCandidates[:policy.MaxRankedCandidates]
	}

	eligible := make([]CandidateScore, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Eligible {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		return createCandidatePlan(plan, policy)
	}
	best := eligible[0]
	plan.BestScore = best.Score
	secondScore := int64(0)
	if len(eligible) > 1 {
		secondScore = eligible[1].Score
	}
	plan.ScoreMargin = best.Score - secondScore
	if best.Score >= policy.MinimumAttachScore && plan.ScoreMargin >= policy.MinimumScoreMargin {
		plan.Decision = DecisionAttachExisting
		plan.SelectedChainID = best.ChainID
		plan.SelectedSourceRevision = best.SourceRevision
		plan.ReasonCode = ReasonAttachExisting
		plan.Reason = "best eligible candidate reaches the threshold with the required margin"
		return plan, nil
	}

	plausibleThreshold := policy.MinimumAttachScore - policy.MinimumScoreMargin
	if plausibleThreshold < 1 {
		plausibleThreshold = 1
	}
	plausible := 0
	for _, candidate := range eligible {
		if candidate.Score >= plausibleThreshold {
			plausible++
		}
	}
	if (best.Score >= policy.MinimumAttachScore && plan.ScoreMargin < policy.MinimumScoreMargin) || plausible >= 2 {
		plan.Decision = DecisionAmbiguous
		plan.ReasonCode = ReasonAmbiguous
		plan.Reason = "multiple plausible candidates or insufficient score margin prevents a safe attachment"
		if len(plan.RankedCandidates) < 2 {
			plan.RankedCandidates = append(plan.RankedCandidates, eligible[1])
		}
		return plan, nil
	}
	return createCandidatePlan(plan, policy)
}

func createCandidatePlan(plan Plan, policy Policy) (Plan, error) {
	id, err := DeriveCandidateChainID(Input{Observation: plan.Observation}, policy)
	if err != nil {
		return Plan{}, err
	}
	plan.Decision = DecisionCreateCandidate
	plan.NewChainID = id
	plan.ReasonCode = ReasonCreateCandidate
	plan.Reason = "no eligible candidate reaches the attach threshold without a competing plausible hypothesis"
	return plan, nil
}

// DeriveCandidateChainID deterministically namespaces the policy version and
// observation ID. The raw observation is never embedded in the resulting ID.
func DeriveCandidateChainID(input Input, policy Policy) (chains.ChainID, error) {
	if err := input.Validate(); err != nil {
		return "", err
	}
	if err := policy.Validate(); err != nil {
		return "", err
	}
	return deriveCandidateChainID(input.Observation.ID, policy.Version)
}

// ValidateCandidateChainID verifies that a plan's candidate ID is the exact
// deterministic ID for its observation and policy version.
func ValidateCandidateChainID(input Input, policyVersion string, id chains.ChainID) error {
	if err := input.Validate(); err != nil {
		return err
	}
	if err := validBoundedText(policyVersion, "policy version", 64, true); err != nil {
		return fmt.Errorf("%w: %v", ErrCandidateIDMismatch, err)
	}
	expected, err := deriveCandidateChainID(input.Observation.ID, policyVersion)
	if err != nil {
		return err
	}
	if id != expected {
		return fmt.Errorf("%w: expected=%s found=%s", ErrCandidateIDMismatch, expected, id)
	}
	return nil
}

func deriveCandidateChainID(observationID, policyVersion string) (chains.ChainID, error) {
	digest := sha256.Sum256([]byte("synora.cge.association.candidate\x00" + policyVersion + "\x00" + observationID))
	id, err := chains.NewChainID("cge-" + hex.EncodeToString(digest[:]))
	if err != nil {
		return "", fmt.Errorf("derive candidate chain id: %w", err)
	}
	return id, nil
}

func scoreCandidate(snapshot chains.Snapshot, input Input, policy Policy) CandidateScore {
	candidate := CandidateScore{ChainID: snapshot.ID, SourceRevision: snapshot.Revision, Status: snapshot.Status, Eligible: true}
	add := func(code string, score int64, detail string) {
		candidate.Facts = append(candidate.Facts, ScoreFact{Code: code, Score: score, Detail: detail})
		candidate.Score += score
	}
	if !IsAssociationEligible(snapshot.Status) {
		candidate.Eligible = false
		candidate.RejectionCode = "status.ineligible"
		candidate.Facts = append(candidate.Facts, ScoreFact{Code: "status.ineligible", Detail: snapshot.Status.String()})
		return candidate
	}
	chainEntity := knownEntity(snapshot)
	observationEntity := input.Observation.EntityID
	if chainEntity != "" && observationEntity != "" && chainEntity != observationEntity {
		candidate.Eligible = false
		candidate.RejectionCode = "entity.conflict"
		candidate.Facts = append(candidate.Facts, ScoreFact{Code: "entity.conflict", Detail: "known entities differ"})
		return candidate
	}
	if chainEntity != "" && chainEntity == observationEntity {
		add("entity.same", policy.SameEntityScore, "known entity matches")
	}
	if snapshot.LastSeenAt.IsZero() == false {
		delta := input.Observation.Timestamp.Sub(snapshot.LastSeenAt)
		if delta > policy.MaxForwardGap {
			candidate.Eligible = false
			candidate.RejectionCode = "time.out_of_window"
			candidate.Facts = append(candidate.Facts, ScoreFact{Code: "time.out_of_window", Detail: "future gap exceeds policy"})
			return candidate
		}
		if delta < -policy.MaxLateArrival {
			candidate.Eligible = false
			candidate.RejectionCode = "time.out_of_window"
			candidate.Facts = append(candidate.Facts, ScoreFact{Code: "time.out_of_window", Detail: "late arrival exceeds policy"})
			return candidate
		}
	}
	if hasObservationField(snapshot.Observations, input.Observation.SequenceKey, func(o chains.ObservationRef) string { return o.SequenceKey }) {
		add("sequence.same", policy.SameSequenceScore, "sequence key matches")
	}
	if hasObservationField(snapshot.Observations, input.Observation.ActivationID, func(o chains.ObservationRef) string { return o.ActivationID }) {
		add("activation.same", policy.SameActivationScore, "activation matches")
	}
	if hasObservationField(snapshot.Observations, input.Observation.TrackID, func(o chains.ObservationRef) string { return o.TrackID }) {
		add("track.same", policy.SameTrackScore, "track matches")
	}
	if hasObservationField(snapshot.Observations, input.Observation.DeviceID, func(o chains.ObservationRef) string { return o.DeviceID }) {
		add("device.same", policy.SameDeviceScore, "device matches")
	}
	if input.Observation.NodeID != "" {
		if len(snapshot.NodePath) > 0 && snapshot.NodePath[len(snapshot.NodePath)-1] == input.Observation.NodeID {
			add("node.last_same", policy.SameLastNodeScore, "node continues last path position")
		}
		for _, node := range snapshot.NodePath {
			if node == input.Observation.NodeID {
				add("node.already_seen", policy.NodeAlreadySeenScore, "node exists in path")
				break
			}
		}
	}
	situation := input.SituationKind
	if situation == "" {
		situation = input.Observation.EventType
	}
	if hasObservationField(snapshot.Observations, situation, func(o chains.ObservationRef) string { return o.EventType }) {
		add("situation.same", policy.SameSituationScore, "situation kind matches")
	}
	if !snapshot.LastSeenAt.IsZero() {
		delta := input.Observation.Timestamp.Sub(snapshot.LastSeenAt)
		if delta >= 0 {
			add("time.forward_close", temporalScore(policy.TemporalContinuityScore, delta, policy.MaxForwardGap), "forward continuity within policy")
		} else {
			add("time.late_valid", temporalScore(policy.TemporalContinuityScore, -delta, policy.MaxLateArrival), "late arrival within policy")
		}
	}
	return candidate
}

func temporalScore(weight int64, distance, maximum time.Duration) int64 {
	if distance <= maximum/10 {
		return weight
	}
	if distance <= maximum/2 {
		return weight * 2 / 3
	}
	return weight / 3
}

func knownEntity(snapshot chains.Snapshot) string {
	if snapshot.EntityID != "" {
		return snapshot.EntityID
	}
	entity := ""
	for _, observation := range snapshot.Observations {
		if observation.EntityID == "" {
			continue
		}
		if entity == "" {
			entity = observation.EntityID
		} else if entity != observation.EntityID {
			return ""
		}
	}
	return entity
}

func hasObservationField(observations []chains.ObservationRef, wanted string, field func(chains.ObservationRef) string) bool {
	if wanted == "" {
		return false
	}
	for _, observation := range observations {
		if field(observation) == wanted {
			return true
		}
	}
	return false
}

// joinError preserves both a stable sentinel and the underlying cause without
// depending on formatting conventions.
func joinError(first, second error) error {
	return fmt.Errorf("%w: %w", first, second)
}
