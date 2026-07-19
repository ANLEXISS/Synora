package routines

import (
	"fmt"
	"time"

	"synora/internal/cge/chains"
	cgecontext "synora/internal/cge/context"
)

type SkippedOccurrence struct {
	Kind          Kind     `json:"kind"`
	ObservationID string   `json:"observation_id"`
	Code          SkipCode `json:"code"`
}
type LearningPlan struct {
	ChainID             chains.ChainID      `json:"chain_id"`
	SourceChainRevision uint64              `json:"source_chain_revision"`
	TargetObservationID string              `json:"target_observation_id"`
	PolicyNamespace     string              `json:"policy_namespace"`
	PolicyVersion       string              `json:"policy_version"`
	Occurrences         []Occurrence        `json:"occurrences"`
	Skipped             []SkippedOccurrence `json:"skipped"`
	PlannedAt           time.Time           `json:"planned_at"`
}

func PlanLearning(chain chains.Snapshot, targetObservationID string, topology cgecontext.TopologySnapshot, plannedAt time.Time, policy ExtractionPolicy) (LearningPlan, error) {
	if err := policy.Validate(); err != nil {
		return LearningPlan{}, err
	}
	if plannedAt.IsZero() {
		return LearningPlan{}, fmt.Errorf("%w: planned timestamp", ErrInvalidPolicy)
	}
	if _, ok := findObservation(chain, targetObservationID); !ok {
		return LearningPlan{}, NotApplicableError{SkipTargetObservationMissing}
	}
	plan := LearningPlan{ChainID: chain.ID, SourceChainRevision: chain.Revision, TargetObservationID: targetObservationID, PolicyNamespace: policy.Namespace, PolicyVersion: policy.Version, PlannedAt: plannedAt}
	presence, presenceErr := ExtractPresenceOccurrence(chain, targetObservationID, policy)
	if presenceErr == nil {
		plan.Occurrences = append(plan.Occurrences, presence)
	} else {
		if code, ok := skipCode(presenceErr); ok {
			plan.Skipped = append(plan.Skipped, SkippedOccurrence{Kind: KindPresence, ObservationID: targetObservationID, Code: code})
		} else {
			return LearningPlan{}, presenceErr
		}
	}
	transition, transitionErr := ExtractTransitionOccurrence(chain, targetObservationID, topology, policy)
	if transitionErr == nil {
		plan.Occurrences = append(plan.Occurrences, transition)
	} else {
		if code, ok := skipCode(transitionErr); ok {
			plan.Skipped = append(plan.Skipped, SkippedOccurrence{Kind: KindTransition, ObservationID: targetObservationID, Code: code})
		} else {
			return LearningPlan{}, transitionErr
		}
	}
	return plan, nil
}
func skipCode(err error) (SkipCode, bool) {
	if e, ok := err.(NotApplicableError); ok {
		return e.Code, true
	}
	if e, ok := err.(*NotApplicableError); ok {
		return e.Code, true
	}
	return "", false
}
