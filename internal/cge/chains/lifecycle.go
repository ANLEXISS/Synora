package chains

import (
	"errors"
	"fmt"
)

// ErrInvalidTransition identifies a lifecycle transition rejected by the
// cognitive-chain policy.
var ErrInvalidTransition = errors.New("invalid chain status transition")

// InvalidTransitionError includes both lifecycle endpoints for callers and
// logs that need to explain a rejected transition.
type InvalidTransitionError struct {
	From Status
	To   Status
}

func (e InvalidTransitionError) Error() string {
	return fmt.Sprintf("%s: %s -> %s", ErrInvalidTransition, e.From, e.To)
}

func (e InvalidTransitionError) Unwrap() error { return ErrInvalidTransition }

// IsActiveModelStatus reports states that participate in the active cognitive
// model. Candidate is deliberately pre-model; dormant and archived are memory
// states.
func (s Status) IsActiveModelStatus() bool {
	switch s {
	case StatusActive, StatusConfirmed, StatusDeclining, StatusReactivated:
		return true
	default:
		return false
	}
}

// IsActive is retained as a concise compatibility alias for active-model
// membership. It excludes candidate, which is only a pre-model state.
func (s Status) IsActive() bool { return s.IsActiveModelStatus() }

// IsHistoricalStatus reports states retained as cognitive memory rather than
// used by the active model. Archived remains reactivatable by explicit action.
func (s Status) IsHistoricalStatus() bool {
	switch s {
	case StatusDormant, StatusArchived:
		return true
	default:
		return false
	}
}

// IsReplacementStatus reports labels reserved for future merge and split
// operations. No public mutation currently produces these states.
func (s Status) IsReplacementStatus() bool {
	return s == StatusMerged || s == StatusSplit
}

// IsPreModelStatus reports the candidate state before active-model admission.
func (s Status) IsPreModelStatus() bool { return s == StatusCandidate }

// IsTerminal reports states with no lifecycle exits. Archived is intentionally
// not terminal because an explicit reactivation is allowed.
func (s Status) IsTerminal() bool {
	return s == StatusInvalidated || s.IsReplacementStatus()
}

// IsHistorical is retained as the broad historical-or-terminal classification.
func (s Status) IsHistorical() bool {
	return s.IsHistoricalStatus() || s.IsTerminal()
}

// allowedTransitions is the single lifecycle policy source of truth.
var allowedTransitions = map[Status]map[Status]struct{}{
	StatusCandidate: {
		StatusActive:      {},
		StatusInvalidated: {},
	},
	StatusActive: {
		StatusConfirmed:   {},
		StatusDeclining:   {},
		StatusDormant:     {},
		StatusInvalidated: {},
	},
	StatusConfirmed: {
		StatusDeclining:   {},
		StatusDormant:     {},
		StatusInvalidated: {},
	},
	StatusDeclining: {
		StatusActive:      {},
		StatusConfirmed:   {},
		StatusDormant:     {},
		StatusArchived:    {},
		StatusInvalidated: {},
	},
	StatusDormant: {
		StatusReactivated: {},
		StatusArchived:    {},
		StatusInvalidated: {},
	},
	StatusArchived: {
		StatusReactivated: {},
		StatusInvalidated: {},
	},
	StatusReactivated: {
		StatusActive:      {},
		StatusConfirmed:   {},
		StatusDeclining:   {},
		StatusDormant:     {},
		StatusInvalidated: {},
	},
}

// CanTransition reports whether a lifecycle transition is explicitly allowed.
// It performs no mutation.
func CanTransition(from, to Status) bool {
	if from.Validate() != nil || to.Validate() != nil || from == to {
		return false
	}
	_, ok := allowedTransitions[from][to]
	return ok
}

// ValidateTransition validates one lifecycle transition without mutating a
// chain. Unknown, same-state, replacement, and terminal exits are rejected.
func ValidateTransition(from, to Status) error {
	if err := from.Validate(); err != nil {
		return err
	}
	if err := to.Validate(); err != nil {
		return err
	}
	if !CanTransition(from, to) {
		return InvalidTransitionError{From: from, To: to}
	}
	return nil
}

func operationForTransition(from, to Status) RevisionOperation {
	switch to {
	case StatusArchived:
		return OperationChainArchived
	case StatusReactivated:
		return OperationChainReactivated
	default:
		return OperationStatusChanged
	}
}
