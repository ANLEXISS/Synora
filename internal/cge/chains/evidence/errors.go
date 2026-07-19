package evidence

import (
	"errors"
	"fmt"

	"synora/internal/cge/chains"
)

var (
	ErrInvalidEvidencePolicy         = errors.New("invalid_evidence_policy")
	ErrInvalidEvidenceInput          = errors.New("invalid_evidence_input")
	ErrTargetObservationNotFound     = errors.New("target_observation_not_found")
	ErrEvidenceEvaluationNotAllowed  = errors.New("evidence_evaluation_not_allowed")
	ErrUnsupportedObservationType    = errors.New("unsupported_observation_type")
	ErrEvidenceContributionCollision = errors.New("evidence_contribution_collision")
	ErrInvalidContributionProposal   = errors.New("invalid_contribution_proposal")
	ErrInsufficientContext           = errors.New("insufficient_context")
	ErrInvalidEvaluationTime         = errors.New("invalid_evaluation_time")
)

// Error adds only stable context to an evidence error. It never contains an
// observation payload or a complete contribution.
type Error struct {
	Code     error
	Step     string
	ChainID  chains.ChainID
	TargetID string
	Rule     string
	Cause    error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	message := e.Code.Error()
	if e.Step != "" {
		message += " step=" + e.Step
	}
	if e.ChainID != "" {
		message += " chain=" + string(e.ChainID)
	}
	if e.TargetID != "" {
		message += " target=" + e.TargetID
	}
	if e.Rule != "" {
		message += " rule=" + e.Rule
	}
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.Cause != nil {
		return e.Cause
	}
	return e.Code
}

// Is keeps the stable evidence sentinel discoverable even when a structured
// error also carries a lower-level validation cause.
func (e *Error) Is(target error) bool {
	if e == nil {
		return false
	}
	return target == e.Code || (e.Cause != nil && errors.Is(e.Cause, target))
}

func evidenceError(code error, step string, chainID string, targetID string, rule string, cause error) error {
	return &Error{Code: code, Step: step, ChainID: chains.ChainID(chainID), TargetID: targetID, Rule: rule, Cause: cause}
}

func invalidPolicy(rule string, cause error) error {
	return evidenceError(ErrInvalidEvidencePolicy, "policy", "", "", rule, cause)
}

func invalidInput(chainID, targetID, rule string, cause error) error {
	return evidenceError(ErrInvalidEvidenceInput, "input", chainID, targetID, rule, cause)
}

func formatPolicyError(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
