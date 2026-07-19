package durable

import (
	"errors"
	"fmt"

	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/chains/registry"
	"synora/internal/cge/hypotheses"
	hypothesisreplay "synora/internal/cge/hypotheses/replay"
	"synora/internal/cge/routines"
	routinereplay "synora/internal/cge/routines/replay"
)

var (
	ErrCoordinatorNotReady                  = errors.New("coordinator_not_ready")
	ErrCoordinatorDegraded                  = errors.New("coordinator_degraded")
	ErrCoordinatorClosed                    = errors.New("coordinator_closed")
	ErrInvalidCoordinatorInput              = errors.New("invalid_coordinator_input")
	ErrRegistryCloneFailed                  = errors.New("registry_clone_failed")
	ErrMutationPrepareFailed                = errors.New("mutation_prepare_failed")
	ErrJournalAppendFailed                  = errors.New("journal_append_failed")
	ErrJournalAppendAmbiguous               = errors.New("journal_append_ambiguous")
	ErrPublicationFailed                    = errors.New("publication_failed")
	ErrRecoveryFailed                       = errors.New("recovery_failed")
	ErrJournalRegistryDivergence            = errors.New("journal_registry_divergence")
	ErrCheckpointAppendFailed               = errors.New("checkpoint_append_failed")
	ErrCheckpointUncertain                  = errors.New("checkpoint_uncertain")
	ErrSnapshotOrphaned                     = errors.New("snapshot_orphaned")
	ErrInvalidActor                         = errors.New("invalid_actor")
	ErrInvalidCorrelation                   = errors.New("invalid_correlation")
	ErrInvalidTimestamp                     = errors.New("invalid_timestamp")
	ErrInvalidContext                       = errors.New("invalid_context")
	ErrInvalidObservationCommand            = errors.New("invalid_observation_command")
	ErrObservationApplyFailed               = errors.New("observation_apply_failed")
	ErrObservationAppendFailed              = errors.New("observation_append_failed")
	ErrChainNotFound                        = registry.ErrChainNotFound
	ErrStaleObservationCommand              = registry.ErrStaleObservationCommand
	ErrDuplicateObservation                 = registry.ErrDuplicateObservation
	ErrObservationNotAllowed                = registry.ErrObservationNotAllowed
	ErrInvalidContributionCommand           = registry.ErrInvalidContributionCommand
	ErrContributionApplyFailed              = errors.New("contribution_apply_failed")
	ErrContributionAppendFailed             = errors.New("contribution_append_failed")
	ErrContributionResultMismatch           = registry.ErrContributionResultMismatch
	ErrStaleContributionCommand             = registry.ErrStaleContributionCommand
	ErrDuplicateContribution                = registry.ErrDuplicateContribution
	ErrContributionNotAllowed               = registry.ErrContributionNotAllowed
	ErrUnknownObservationReference          = registry.ErrUnknownObservationReference
	ErrInvalidAssociationPlan               = association.ErrInvalidPlan
	ErrStaleAssociationPlan                 = association.ErrStaleAssociationPlan
	ErrCandidateIDCollision                 = association.ErrCandidateIDCollision
	ErrCandidateIDMismatch                  = association.ErrCandidateIDMismatch
	ErrAssociationAmbiguous                 = association.ErrAssociationAmbiguous
	ErrInvalidEvidenceBatchOptions          = evidence.ErrInvalidEvidenceBatchOptions
	ErrInvalidEvidenceBatch                 = evidence.ErrInvalidEvidenceBatch
	ErrDuplicateChainProposal               = errors.New("duplicate_chain_proposal")
	ErrDuplicateContributionProposal        = errors.New("duplicate_contribution_proposal")
	ErrEvidenceBatchApplyFailed             = errors.New("evidence_batch_apply_failed")
	ErrEvidenceProposalStale                = registry.ErrStaleContributionCommand
	ErrEvidenceProposalIdempotent           = errors.New("evidence_proposal_idempotent")
	ErrEvidenceContributionCollision        = evidence.ErrEvidenceContributionCollision
	ErrInvalidHypothesisCommand             = hypotheses.ErrInvalidHypothesisCommand
	ErrHypothesisNotFound                   = hypotheses.ErrHypothesisNotFound
	ErrHypothesisAlreadyExists              = hypotheses.ErrHypothesisAlreadyExists
	ErrHypothesisCollision                  = hypotheses.ErrHypothesisCollision
	ErrStaleHypothesisCommand               = hypotheses.ErrStaleHypothesisCommand
	ErrHypothesisOpenAppendFailed           = errors.New("hypothesis_open_append_failed")
	ErrHypothesisStatusAppendFailed         = errors.New("hypothesis_status_append_failed")
	ErrHypothesisRebaseAppendFailed         = errors.New("hypothesis_rebase_append_failed")
	ErrHypothesisResultMismatch             = errors.New("hypothesis_result_mismatch")
	ErrHypothesisRebaseResultMismatch       = errors.New("hypothesis_rebase_result_mismatch")
	ErrInvalidHypothesisAssessment          = hypotheses.ErrInvalidHypothesisAssessment
	ErrInvalidRebaseProposal                = hypotheses.ErrInvalidRebaseProposal
	ErrInvalidRebaseCommand                 = hypotheses.ErrInvalidRebaseCommand
	ErrRebaseNotAllowed                     = hypotheses.ErrRebaseNotAllowed
	ErrRebaseSubjectMismatch                = hypotheses.ErrRebaseSubjectMismatch
	ErrHypothesisRebaseUnchanged            = hypotheses.ErrHypothesisRebaseUnchanged
	ErrHypothesisAssessmentCollision        = hypotheses.ErrHypothesisAssessmentCollision
	ErrStaleHypothesisRebase                = hypotheses.ErrStaleHypothesisRebase
	ErrInvalidSupersessionProposal          = hypotheses.ErrInvalidSupersessionProposal
	ErrInvalidSupersessionCommand           = hypotheses.ErrInvalidSupersessionCommand
	ErrSupersessionNotAllowed               = hypotheses.ErrSupersessionNotAllowed
	ErrSupersessionNotRequired              = hypotheses.ErrSupersessionNotRequired
	ErrSupersessionSubjectMismatch          = hypotheses.ErrSupersessionSubjectMismatch
	ErrHypothesisSupersessionCollision      = hypotheses.ErrHypothesisSupersessionCollision
	ErrHypothesisSuccessorCollision         = hypotheses.ErrHypothesisSuccessorCollision
	ErrHypothesisLineageDivergence          = hypotheses.ErrHypothesisLineageDivergence
	ErrHypothesisLineageCycle               = hypotheses.ErrHypothesisLineageCycle
	ErrStaleHypothesisSupersession          = hypotheses.ErrStaleHypothesisSupersession
	ErrHypothesisSupersessionAppendFailed   = errors.New("hypothesis_supersession_append_failed")
	ErrHypothesisSupersessionResultMismatch = errors.New("hypothesis_supersession_result_mismatch")
	ErrHypothesisRegistryCloneFailed        = errors.New("hypothesis_registry_clone_failed")
	ErrHypothesisResolutionAppendFailed     = errors.New("hypothesis_resolution_append_failed")
	ErrHypothesisResolutionResultMismatch   = errors.New("hypothesis_resolution_result_mismatch")
	ErrResolutionNotAllowed                 = hypotheses.ErrResolutionNotAllowed
	ErrResolutionMaterialMissing            = hypotheses.ErrResolutionMaterialMissing
	ErrResolutionAlternativeMismatch        = hypotheses.ErrResolutionAlternativeMismatch
	ErrResolutionEffectMismatch             = hypotheses.ErrResolutionEffectMismatch
	ErrResolutionOutcomeMismatch            = hypotheses.ErrResolutionOutcomeMismatch
	ErrStaleHypothesisResolution            = hypotheses.ErrStaleHypothesisResolution
	ErrStaleResolutionChainEffect           = hypotheses.ErrStaleResolutionChainEffect
	ErrHypothesisResolutionCollision        = hypotheses.ErrHypothesisResolutionCollision
	ErrResolutionChainNotFound              = hypotheses.ErrResolutionChainNotFound
	ErrResolutionChainCollision             = hypotheses.ErrResolutionChainCollision
	ErrResolutionObservationCollision       = hypotheses.ErrResolutionObservationCollision
	ErrResolutionContributionCollision      = hypotheses.ErrResolutionContributionCollision
	ErrHypothesisReplayFailed               = hypothesisreplay.ErrHypothesisReplayFailed
	ErrRoutineReplayFailed                  = routinereplay.ErrRoutineReplayFailed
	ErrRoutineNotFound                      = routines.ErrRoutineNotFound
	ErrRoutineOccurrenceCollision           = routines.ErrRoutineOccurrenceCollision
	ErrRoutineRevisionStale                 = routines.ErrRoutineRevisionStale
	ErrRoutineStatusTransition              = routines.ErrRoutineStatusTransition
	ErrRoutineLearningFailed                = errors.New("routine_learning_failed")
	ErrRoutineAppendFailed                  = errors.New("routine_append_failed")
	ErrRoutineResultMismatch                = errors.New("routine_result_mismatch")
)

// CoordinatorError identifies the operation and stage that failed. The cause
// remains available through errors.Is/errors.As.
type CoordinatorError struct {
	Operation               string
	ChainID                 string
	GenerationID            string
	IncludedJournalSequence uint64
	ObservationID           string
	ContributionID          string
	ExpectedRevision        uint64
	CurrentRevision         uint64
	Step                    string
	Err                     error
}

func (e CoordinatorError) Error() string {
	if e.ObservationID != "" {
		return fmt.Sprintf("durable %s failed at %s for chain=%s observation=%s revision=%d current=%d: %v", e.Operation, e.Step, e.ChainID, e.ObservationID, e.ExpectedRevision, e.CurrentRevision, e.Err)
	}
	if e.ContributionID != "" {
		return fmt.Sprintf("durable %s failed at %s for chain=%s contribution=%s revision=%d current=%d: %v", e.Operation, e.Step, e.ChainID, e.ContributionID, e.ExpectedRevision, e.CurrentRevision, e.Err)
	}
	if e.ChainID != "" {
		return fmt.Sprintf("durable %s failed at %s for chain=%s: %v", e.Operation, e.Step, e.ChainID, e.Err)
	}
	if e.GenerationID != "" {
		return fmt.Sprintf("durable %s failed at %s for generation=%s sequence=%d: %v", e.Operation, e.Step, e.GenerationID, e.IncludedJournalSequence, e.Err)
	}
	return fmt.Sprintf("durable %s failed at %s: %v", e.Operation, e.Step, e.Err)
}

func (e CoordinatorError) Unwrap() error { return e.Err }

// AppendFailure records whether an append error was cleanly rejected or its
// durability could not be determined. An uncertain result always degrades the
// coordinator; the journal is never truncated to compensate.
type AppendFailure struct {
	Outcome AppendOutcome
	Err     error
}

func (e AppendFailure) Error() string {
	if e.Outcome == AppendUncertain {
		return fmt.Sprintf("%s: %v", ErrJournalAppendAmbiguous, e.Err)
	}
	return fmt.Sprintf("%s: %v", ErrJournalAppendFailed, e.Err)
}

func (e AppendFailure) Unwrap() error { return e.Err }

func (e AppendFailure) Is(target error) bool {
	if e.Outcome == AppendUncertain && target == ErrJournalAppendAmbiguous {
		return true
	}
	if e.Outcome == AppendRejected && target == ErrJournalAppendFailed {
		return true
	}
	return errors.Is(e.Err, target)
}
