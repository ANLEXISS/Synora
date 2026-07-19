package hypotheses

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidHypothesis                   = errors.New("invalid_hypothesis")
	ErrInvalidHypothesisAssessment         = errors.New("invalid_hypothesis_assessment")
	ErrInvalidRebaseProposal               = errors.New("invalid_rebase_proposal")
	ErrInvalidRebaseCommand                = errors.New("invalid_rebase_command")
	ErrRebaseNotAllowed                    = errors.New("rebase_not_allowed")
	ErrRebaseSubjectMismatch               = errors.New("rebase_subject_mismatch")
	ErrHypothesisRebaseUnchanged           = errors.New("hypothesis_rebase_unchanged")
	ErrHypothesisAssessmentCollision       = errors.New("hypothesis_assessment_collision")
	ErrStaleHypothesisRebase               = errors.New("stale_hypothesis_rebase")
	ErrSupersessionNotAllowed              = errors.New("supersession_not_allowed")
	ErrInvalidSupersessionProposal         = errors.New("invalid_supersession_proposal")
	ErrInvalidSupersessionCommand          = errors.New("invalid_supersede_command")
	ErrSupersessionNotRequired             = errors.New("supersession_not_required")
	ErrSupersessionSubjectMismatch         = errors.New("supersession_subject_mismatch")
	ErrHypothesisSupersessionCollision     = errors.New("hypothesis_supersession_collision")
	ErrHypothesisSuccessorCollision        = errors.New("hypothesis_successor_collision")
	ErrHypothesisLineageDivergence         = errors.New("hypothesis_lineage_divergence")
	ErrHypothesisLineageCycle              = errors.New("hypothesis_lineage_cycle")
	ErrStaleHypothesisSupersession         = errors.New("stale_hypothesis_supersession")
	ErrInvalidHypothesisCommand            = errors.New("invalid_hypothesis_command")
	ErrInvalidHypothesisSubject            = errors.New("invalid_hypothesis_subject")
	ErrInvalidHypothesisAlternative        = errors.New("invalid_hypothesis_alternative")
	ErrInvalidHypothesisProvenance         = errors.New("invalid_hypothesis_provenance")
	ErrInvalidHypothesisTransition         = errors.New("invalid_hypothesis_transition")
	ErrHypothesisNotFound                  = errors.New("hypothesis_not_found")
	ErrHypothesisAlreadyExists             = errors.New("hypothesis_already_exists")
	ErrHypothesisCollision                 = errors.New("hypothesis_collision")
	ErrStaleHypothesisCommand              = errors.New("stale_hypothesis_command")
	ErrAssociationNotAmbiguous             = errors.New("association_not_ambiguous")
	ErrEvidenceNotAmbiguous                = errors.New("evidence_not_ambiguous")
	ErrInsufficientHypothesisAlternatives  = errors.New("insufficient_hypothesis_alternatives")
	ErrInvalidContext                      = errors.New("invalid_context")
	ErrInvalidResolutionEffect             = errors.New("invalid_resolution_effect")
	ErrInvalidResolutionSchema             = errors.New("invalid_resolution_schema")
	ErrResolutionObservationMismatch       = errors.New("resolution_observation_mismatch")
	ErrResolutionChainMismatch             = errors.New("resolution_chain_mismatch")
	ErrResolutionContributionMismatch      = errors.New("resolution_contribution_mismatch")
	ErrHypothesisResolutionMaterialMissing = errors.New("hypothesis_resolution_material_missing")
	ErrHypothesisResolutionNotAllowed      = errors.New("hypothesis_resolution_not_allowed")
	ErrAlternativeNotFound                 = errors.New("alternative_not_found")
	ErrInvalidResolutionPlan               = errors.New("invalid_resolution_plan")
	ErrUnsupportedResolutionEffect         = errors.New("unsupported_resolution_effect")
	ErrInvalidTimestampResolution          = errors.New("invalid_timestamp")
	ErrInvalidResolveCommand               = errors.New("invalid_resolve_command")
	ErrResolutionNotAllowed                = errors.New("resolution_not_allowed")
	ErrResolutionMaterialMissing           = errors.New("resolution_material_missing")
	ErrResolutionAlternativeMismatch       = errors.New("resolution_alternative_mismatch")
	ErrResolutionEffectMismatch            = errors.New("resolution_effect_mismatch")
	ErrResolutionOutcomeMismatch           = errors.New("resolution_outcome_mismatch")
	ErrStaleHypothesisResolution           = errors.New("stale_hypothesis_resolution")
	ErrStaleResolutionChainEffect          = errors.New("stale_resolution_chain_effect")
	ErrHypothesisResolutionCollision       = errors.New("hypothesis_resolution_collision")
	ErrResolutionChainNotFound             = errors.New("resolution_chain_not_found")
	ErrResolutionChainCollision            = errors.New("resolution_chain_collision")
	ErrResolutionObservationCollision      = errors.New("resolution_observation_collision")
	ErrResolutionContributionCollision     = errors.New("resolution_contribution_collision")
)

// Error carries bounded structural context while preserving errors.Is/errors.As.
type Error struct {
	Code             error
	SetID            SetID
	Family           Family
	Step             string
	ExpectedRevision uint64
	CurrentRevision  uint64
	Cause            error
}

func (e Error) Error() string {
	message := e.Code.Error()
	if e.Step != "" {
		message += " step=" + e.Step
	}
	if e.SetID != "" {
		message += " set=" + string(e.SetID)
	}
	if e.Family != "" {
		message += " family=" + string(e.Family)
	}
	if e.ExpectedRevision != 0 || e.CurrentRevision != 0 {
		message += fmt.Sprintf(" expected=%d current=%d", e.ExpectedRevision, e.CurrentRevision)
	}
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

func (e Error) Unwrap() error { return e.CauseOrCode() }

func (e Error) CauseOrCode() error {
	if e.Cause != nil {
		return e.Cause
	}
	return e.Code
}

func (e Error) Is(target error) bool {
	return target == e.Code || (e.Cause != nil && errors.Is(e.Cause, target))
}

func hypothesisError(code error, family Family, setID SetID, step string, expected, current uint64, cause error) error {
	return &Error{Code: code, Family: family, SetID: setID, Step: step, ExpectedRevision: expected, CurrentRevision: current, Cause: cause}
}
