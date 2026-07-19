package routines

import (
	"fmt"
	"time"

	"synora/internal/cge/chains"
)

type RevisionOperation string

const (
	OperationRoutineCreated  RevisionOperation = "routine.created"
	OperationOccurrenceAdded RevisionOperation = "routine.occurrence_added"
	OperationStatusChanged   RevisionOperation = "routine.status_changed"
)

type RevisionRecord struct {
	RoutineID        RoutineID         `json:"routine_id"`
	Operation        RevisionOperation `json:"operation"`
	PreviousRevision uint64            `json:"previous_revision"`
	NewRevision      uint64            `json:"new_revision"`
	At               time.Time         `json:"at"`
	Actor            string            `json:"actor"`
	Reason           string            `json:"reason"`
	CorrelationID    string            `json:"correlation_id"`
	OccurrenceID     OccurrenceID      `json:"occurrence_id,omitempty"`
	PreviousStatus   Status            `json:"previous_status,omitempty"`
	NewStatus        Status            `json:"new_status,omitempty"`
}

func validateMutation(m chains.MutationContext) error {
	if err := m.Validate(); err != nil {
		return err
	}
	return nil
}
func validStatus(s Status) bool {
	switch s {
	case StatusCandidate, StatusActive, StatusDeclining, StatusDormant, StatusArchived, StatusInvalidated:
		return true
	}
	return false
}
func canStatusTransition(from, to Status) bool {
	switch from {
	case StatusCandidate:
		return to == StatusActive || to == StatusInvalidated
	case StatusActive:
		return to == StatusDeclining || to == StatusDormant || to == StatusInvalidated
	case StatusDeclining:
		return to == StatusActive || to == StatusDormant || to == StatusArchived || to == StatusInvalidated
	case StatusDormant:
		return to == StatusActive || to == StatusArchived || to == StatusInvalidated
	case StatusArchived:
		return to == StatusActive || to == StatusInvalidated
	default:
		return false
	}
}
func (h RevisionRecord) validate(id RoutineID) error {
	if h.RoutineID != id || h.NewRevision != h.PreviousRevision+1 || h.At.IsZero() || h.Actor == "" || h.Reason == "" || h.CorrelationID != stringsTrim(h.CorrelationID) {
		return fmt.Errorf("%w: history", ErrInvalidRoutine)
	}
	switch h.Operation {
	case OperationRoutineCreated:
		if h.PreviousRevision != 0 || h.NewRevision != 1 {
			return ErrInvalidRoutine
		}
	case OperationOccurrenceAdded:
		if !validOccurrenceID(h.OccurrenceID) {
			return ErrInvalidRoutine
		}
	case OperationStatusChanged:
		if !validStatus(h.PreviousStatus) || !validStatus(h.NewStatus) {
			return ErrInvalidRoutine
		}
	default:
		return ErrInvalidRoutine
	}
	return nil
}
func stringsTrim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
