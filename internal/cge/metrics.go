package cge

import (
	"sync"
	"time"

	"synora/internal/cge/fieldtrial"
)

// MetricsSnapshot is an in-memory, detached shadow diagnostic view. It never
// contains event, chain, device, resident, or payload identifiers.
type MetricsSnapshot struct {
	EventsObserved  uint64
	EventsEligible  uint64
	EventsSkipped   uint64
	EventsMalformed uint64

	PlansAttachExisting  uint64
	PlansCreateCandidate uint64
	PlansAmbiguous       uint64
	PlansAlreadyAttached uint64

	AppliedAttachExisting  uint64
	AppliedCreateCandidate uint64
	IdempotentNoops        uint64

	AdaptationErrors    uint64
	PlanningErrors      uint64
	ApplyErrors         uint64
	PanicRecoveries     uint64
	CoordinatorDegraded uint64

	AssociationAttachApplied        uint64
	AssociationCreateApplied        uint64
	AssociationAmbiguous            uint64
	AssociationAlreadyAttached      uint64
	AssociationErrors               uint64
	AssociationHypothesisOpened     uint64
	AssociationHypothesisRebased    uint64
	AssociationHypothesisIdempotent uint64
	AssociationHypothesisTerminal   uint64

	EvidenceEvaluated                        uint64
	EvidenceSupportProposed                  uint64
	EvidenceContradictionProposed            uint64
	EvidenceNeutralProposed                  uint64
	EvidenceInsufficient                     uint64
	EvidenceAmbiguous                        uint64
	EvidenceAlreadyEvaluated                 uint64
	EvidenceErrors                           uint64
	EvidenceContributionSupportApplied       uint64
	EvidenceContributionContradictionApplied uint64
	EvidenceContributionNeutralApplied       uint64
	EvidenceContributionIdempotent           uint64
	EvidenceContributionStale                uint64
	EvidenceHypothesisOpened                 uint64
	EvidenceHypothesisRebased                uint64
	EvidenceHypothesisSuperseded             uint64
	EvidenceHypothesisIdempotent             uint64
	EvidenceHypothesisTerminal               uint64
	EvidenceResolutionCandidate              uint64
	HypothesesReevaluated                    uint64
	HypothesisReevaluationLimitReached       uint64
	OrchestrationDegraded                    uint64
	OrchestrationPanics                      uint64

	ContextResolutionAttempted     uint64
	ContextResolutionComplete      uint64
	ContextResolutionPartial       uint64
	ContextResolutionMissing       uint64
	ContextResolutionErrors        uint64
	ContextTopologyUnknownNode     uint64
	ContextTopologyRevisionChanges uint64
	ContextTransitionEvaluated     uint64
	ContextTransitionUnreachable   uint64

	RoutineLearningPlanned          uint64
	RoutineLearningSkipped          uint64
	RoutinePresenceExtracted        uint64
	RoutineTransitionExtracted      uint64
	RoutineCreated                  uint64
	RoutineOccurrenceAdded          uint64
	RoutineOccurrenceIdempotent     uint64
	RoutineOccurrenceCollision      uint64
	RoutineLearningErrors           uint64
	RoutineContextMissing           uint64
	RoutinePartialDisallowed        uint64
	RoutinePreviousContextMissing   uint64
	RoutineTransitionGapExceeded    uint64
	RoutineTopologyMissing          uint64
	RoutineTopologyRevisionMismatch uint64
	RoutineTransitionUnreachable    uint64
	RoutineRecoveryCompleted        uint64
	RoutineRecoveryErrors           uint64
	RoutineOrchestrationDegraded    uint64

	DeviationEvaluationAttempted    uint64
	DeviationEvaluationCompleted    uint64
	DeviationEvaluationErrors       uint64
	DeviationEvaluated              uint64
	DeviationPartial                uint64
	DeviationInsufficientHistory    uint64
	DeviationAmbiguous              uint64
	DeviationAlreadyEvaluated       uint64
	DeviationNotApplicable          uint64
	DeviationBandAligned            uint64
	DeviationBandLow                uint64
	DeviationBandModerate           uint64
	DeviationBandHigh               uint64
	DeviationBandUnknown            uint64
	DeviationContextComplete        uint64
	DeviationContextPartial         uint64
	DeviationContextUnknown         uint64
	DeviationCandidateLimitExceeded uint64
	DeviationStoreAdded             uint64
	DeviationStoreEvicted           uint64
	DeviationStoreDisabled          uint64
	DeviationSkippedNoChain         uint64
	DeviationSkippedNoOccurrence    uint64

	FieldTrialRecordAttempted  uint64
	FieldTrialRecordWritten    uint64
	FieldTrialRecordErrors     uint64
	FieldTrialRecordPanics     uint64
	FieldTrialSegmentsCreated  uint64
	FieldTrialRotations        uint64
	FieldTrialRecoveries       uint64
	FieldTrialRecoveryErrors   uint64
	FieldTrialEventsDropped    uint64
	FieldTrialSyncErrors       uint64
	FieldTrialAnnotationsAdded uint64
	FieldTrialAnnotationErrors uint64
	FieldTrialExports          uint64
	FieldTrialExportErrors     uint64
	FieldTrialRetentionDeleted uint64
	FieldTrialRetentionErrors  uint64
	FieldTrialTopologyLoaded   uint64
	FieldTrialTopologyErrors   uint64

	LastSuccessAt time.Time
	LastErrorAt   time.Time
	LastErrorCode string
}

type shadowMetrics struct {
	mu    sync.RWMutex
	value MetricsSnapshot
}

func (m *shadowMetrics) observed(now time.Time) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value.EventsObserved++
	return m.value.EventsObserved
}

func (m *shadowMetrics) eligible() {
	m.mu.Lock()
	m.value.EventsEligible++
	m.mu.Unlock()
}

func (m *shadowMetrics) skipped() {
	m.mu.Lock()
	m.value.EventsSkipped++
	m.mu.Unlock()
}

func (m *shadowMetrics) malformed(now time.Time, code string) {
	m.mu.Lock()
	m.value.EventsMalformed++
	m.value.AdaptationErrors++
	m.setErrorLocked(now, code)
	m.mu.Unlock()
}

func (m *shadowMetrics) plan(decision string) {
	m.mu.Lock()
	switch decision {
	case "attach_existing":
		m.value.PlansAttachExisting++
	case "create_candidate":
		m.value.PlansCreateCandidate++
	case "ambiguous":
		m.value.PlansAmbiguous++
	case "already_attached":
		m.value.PlansAlreadyAttached++
	}
	m.mu.Unlock()
}

func (m *shadowMetrics) applied(decision string, now time.Time, idempotent bool) {
	m.mu.Lock()
	if idempotent {
		m.value.IdempotentNoops++
	} else {
		switch decision {
		case "attach_existing":
			m.value.AppliedAttachExisting++
		case "create_candidate":
			m.value.AppliedCreateCandidate++
		}
	}
	m.value.LastSuccessAt = now
	m.mu.Unlock()
}

func (m *shadowMetrics) planningError(now time.Time, code string) {
	m.mu.Lock()
	m.value.PlanningErrors++
	m.setErrorLocked(now, code)
	m.mu.Unlock()
}

func (m *shadowMetrics) applyError(now time.Time, code string) {
	m.mu.Lock()
	m.value.ApplyErrors++
	m.setErrorLocked(now, code)
	m.mu.Unlock()
}

func (m *shadowMetrics) panicRecovered(now time.Time) {
	m.mu.Lock()
	m.value.PanicRecoveries++
	m.setErrorLocked(now, "panic_recovered")
	m.mu.Unlock()
}

func (m *shadowMetrics) degraded(now time.Time) {
	m.mu.Lock()
	m.value.CoordinatorDegraded++
	m.setErrorLocked(now, "coordinator_degraded")
	m.mu.Unlock()
}

func (m *shadowMetrics) deviationError(now time.Time, code string) {
	m.mu.Lock()
	m.value.DeviationEvaluationErrors++
	m.setErrorLocked(now, code)
	m.mu.Unlock()
}

// cognitive increments one bounded, identifier-free orchestration counter.
func (m *shadowMetrics) cognitive(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch name {
	case "association_attach_applied":
		m.value.AssociationAttachApplied++
	case "association_create_applied":
		m.value.AssociationCreateApplied++
	case "association_ambiguous":
		m.value.AssociationAmbiguous++
	case "association_already_attached":
		m.value.AssociationAlreadyAttached++
	case "association_errors":
		m.value.AssociationErrors++
	case "association_hypothesis_opened":
		m.value.AssociationHypothesisOpened++
	case "association_hypothesis_rebased":
		m.value.AssociationHypothesisRebased++
	case "association_hypothesis_idempotent":
		m.value.AssociationHypothesisIdempotent++
	case "association_hypothesis_terminal":
		m.value.AssociationHypothesisTerminal++
	case "evidence_evaluated":
		m.value.EvidenceEvaluated++
	case "evidence_support_proposed":
		m.value.EvidenceSupportProposed++
	case "evidence_contradiction_proposed":
		m.value.EvidenceContradictionProposed++
	case "evidence_neutral_proposed":
		m.value.EvidenceNeutralProposed++
	case "evidence_insufficient":
		m.value.EvidenceInsufficient++
	case "evidence_ambiguous":
		m.value.EvidenceAmbiguous++
	case "evidence_already_evaluated":
		m.value.EvidenceAlreadyEvaluated++
	case "evidence_errors":
		m.value.EvidenceErrors++
	case "evidence_contribution_support_applied":
		m.value.EvidenceContributionSupportApplied++
	case "evidence_contribution_contradiction_applied":
		m.value.EvidenceContributionContradictionApplied++
	case "evidence_contribution_neutral_applied":
		m.value.EvidenceContributionNeutralApplied++
	case "evidence_contribution_idempotent":
		m.value.EvidenceContributionIdempotent++
	case "evidence_contribution_stale":
		m.value.EvidenceContributionStale++
	case "evidence_hypothesis_opened":
		m.value.EvidenceHypothesisOpened++
	case "evidence_hypothesis_rebased":
		m.value.EvidenceHypothesisRebased++
	case "evidence_hypothesis_superseded":
		m.value.EvidenceHypothesisSuperseded++
	case "evidence_hypothesis_idempotent":
		m.value.EvidenceHypothesisIdempotent++
	case "evidence_hypothesis_terminal":
		m.value.EvidenceHypothesisTerminal++
	case "evidence_resolution_candidate":
		m.value.EvidenceResolutionCandidate++
	case "hypotheses_reevaluated":
		m.value.HypothesesReevaluated++
	case "hypothesis_reevaluation_limit_reached":
		m.value.HypothesisReevaluationLimitReached++
	case "orchestration_degraded":
		m.value.OrchestrationDegraded++
	case "orchestration_panics":
		m.value.OrchestrationPanics++
	case "context_resolution_attempted":
		m.value.ContextResolutionAttempted++
	case "context_resolution_complete":
		m.value.ContextResolutionComplete++
	case "context_resolution_partial":
		m.value.ContextResolutionPartial++
	case "context_resolution_missing":
		m.value.ContextResolutionMissing++
	case "context_resolution_errors":
		m.value.ContextResolutionErrors++
	case "context_topology_unknown_node":
		m.value.ContextTopologyUnknownNode++
	case "context_topology_revision_changes":
		m.value.ContextTopologyRevisionChanges++
	case "context_transition_evaluated":
		m.value.ContextTransitionEvaluated++
	case "context_transition_unreachable":
		m.value.ContextTransitionUnreachable++
	case "routine_learning_planned":
		m.value.RoutineLearningPlanned++
	case "routine_learning_skipped":
		m.value.RoutineLearningSkipped++
	case "routine_presence_extracted":
		m.value.RoutinePresenceExtracted++
	case "routine_transition_extracted":
		m.value.RoutineTransitionExtracted++
	case "routine_created":
		m.value.RoutineCreated++
	case "routine_occurrence_added":
		m.value.RoutineOccurrenceAdded++
	case "routine_occurrence_idempotent":
		m.value.RoutineOccurrenceIdempotent++
	case "routine_occurrence_collision":
		m.value.RoutineOccurrenceCollision++
	case "routine_learning_errors":
		m.value.RoutineLearningErrors++
	case "routine_context_missing":
		m.value.RoutineContextMissing++
	case "routine_partial_disallowed":
		m.value.RoutinePartialDisallowed++
	case "routine_previous_context_missing":
		m.value.RoutinePreviousContextMissing++
	case "routine_transition_gap_exceeded":
		m.value.RoutineTransitionGapExceeded++
	case "routine_topology_missing":
		m.value.RoutineTopologyMissing++
	case "routine_topology_revision_mismatch":
		m.value.RoutineTopologyRevisionMismatch++
	case "routine_transition_unreachable":
		m.value.RoutineTransitionUnreachable++
	case "routine_recovery_completed":
		m.value.RoutineRecoveryCompleted++
	case "routine_recovery_errors":
		m.value.RoutineRecoveryErrors++
	case "routine_orchestration_degraded":
		m.value.RoutineOrchestrationDegraded++
	case "deviation_evaluation_attempted":
		m.value.DeviationEvaluationAttempted++
	case "deviation_evaluation_completed":
		m.value.DeviationEvaluationCompleted++
	case "deviation_evaluated":
		m.value.DeviationEvaluated++
	case "deviation_partial":
		m.value.DeviationPartial++
	case "deviation_insufficient_history":
		m.value.DeviationInsufficientHistory++
	case "deviation_ambiguous":
		m.value.DeviationAmbiguous++
	case "deviation_already_evaluated":
		m.value.DeviationAlreadyEvaluated++
	case "deviation_not_applicable":
		m.value.DeviationNotApplicable++
	case "deviation_band_aligned":
		m.value.DeviationBandAligned++
	case "deviation_band_low":
		m.value.DeviationBandLow++
	case "deviation_band_moderate":
		m.value.DeviationBandModerate++
	case "deviation_band_high":
		m.value.DeviationBandHigh++
	case "deviation_band_unknown":
		m.value.DeviationBandUnknown++
	case "deviation_context_complete":
		m.value.DeviationContextComplete++
	case "deviation_context_partial":
		m.value.DeviationContextPartial++
	case "deviation_context_unknown":
		m.value.DeviationContextUnknown++
	case "deviation_candidate_limit_exceeded":
		m.value.DeviationCandidateLimitExceeded++
	case "deviation_store_added":
		m.value.DeviationStoreAdded++
	case "deviation_store_evicted":
		m.value.DeviationStoreEvicted++
	case "deviation_store_disabled":
		m.value.DeviationStoreDisabled++
	case "deviation_skipped_no_chain":
		m.value.DeviationSkippedNoChain++
	case "deviation_skipped_no_occurrence":
		m.value.DeviationSkippedNoOccurrence++
	case "field_trial_record_attempted":
		m.value.FieldTrialRecordAttempted++
	case "field_trial_record_written":
		m.value.FieldTrialRecordWritten++
	case "field_trial_record_errors":
		m.value.FieldTrialRecordErrors++
	case "field_trial_record_panics":
		m.value.FieldTrialRecordPanics++
	case "field_trial_segments_created":
		m.value.FieldTrialSegmentsCreated++
	case "field_trial_rotations":
		m.value.FieldTrialRotations++
	case "field_trial_recoveries":
		m.value.FieldTrialRecoveries++
	case "field_trial_recovery_errors":
		m.value.FieldTrialRecoveryErrors++
	case "field_trial_events_dropped":
		m.value.FieldTrialEventsDropped++
	case "field_trial_sync_errors":
		m.value.FieldTrialSyncErrors++
	case "field_trial_annotations_added":
		m.value.FieldTrialAnnotationsAdded++
	case "field_trial_annotation_errors":
		m.value.FieldTrialAnnotationErrors++
	case "field_trial_exports":
		m.value.FieldTrialExports++
	case "field_trial_export_errors":
		m.value.FieldTrialExportErrors++
	case "field_trial_retention_deleted_sessions":
		m.value.FieldTrialRetentionDeleted++
	case "field_trial_retention_errors":
		m.value.FieldTrialRetentionErrors++
	case "field_trial_topology_loaded":
		m.value.FieldTrialTopologyLoaded++
	case "field_trial_topology_errors":
		m.value.FieldTrialTopologyErrors++
	}
}

func (m *shadowMetrics) fieldTrialDelta(before, after fieldtrial.Stats) {
	m.mu.Lock()
	m.value.FieldTrialRecordAttempted += after.RecordAttempted - before.RecordAttempted
	m.value.FieldTrialRecordWritten += after.RecordWritten - before.RecordWritten
	m.value.FieldTrialRecordErrors += after.RecordErrors - before.RecordErrors
	m.value.FieldTrialRecordPanics += after.RecordPanics - before.RecordPanics
	m.value.FieldTrialSegmentsCreated += after.SegmentsCreated - before.SegmentsCreated
	m.value.FieldTrialRotations += after.Rotations - before.Rotations
	m.value.FieldTrialRecoveries += after.Recoveries - before.Recoveries
	m.value.FieldTrialRecoveryErrors += after.RecoveryErrors - before.RecoveryErrors
	m.value.FieldTrialEventsDropped += after.EventsDropped - before.EventsDropped
	m.value.FieldTrialSyncErrors += after.SyncErrors - before.SyncErrors
	m.value.FieldTrialAnnotationsAdded += after.AnnotationsAdded - before.AnnotationsAdded
	m.value.FieldTrialAnnotationErrors += after.AnnotationErrors - before.AnnotationErrors
	m.mu.Unlock()
}

func (m *shadowMetrics) setErrorLocked(now time.Time, code string) {
	m.value.LastErrorAt = now
	m.value.LastErrorCode = code
}

func (m *shadowMetrics) snapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.value
}
