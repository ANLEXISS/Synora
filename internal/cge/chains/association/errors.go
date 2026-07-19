package association

import (
	"errors"
	"fmt"
	"strings"

	"synora/internal/cge/chains"
)

var (
	ErrInvalidInput                   = errors.New("invalid_association_input")
	ErrInvalidPolicy                  = errors.New("invalid_association_policy")
	ErrInvalidSnapshot                = errors.New("invalid_association_snapshot")
	ErrInvalidPlan                    = errors.New("invalid_association_plan")
	ErrObservationMultipleAttachments = errors.New("observation_multiple_attachments")
	ErrAssociationAmbiguous           = errors.New("association_ambiguous")
	ErrStaleAssociationPlan           = errors.New("stale_association_plan")
	ErrCandidateIDCollision           = errors.New("candidate_id_collision")
	ErrCandidateIDMismatch            = errors.New("candidate_id_mismatch")
)

// MultipleAttachmentError identifies an observation that is already present
// in more than one chain. Chain IDs are detached and sorted.
type MultipleAttachmentError struct {
	ObservationID string
	ChainIDs      []chains.ChainID
}

func (e MultipleAttachmentError) Error() string {
	return fmt.Sprintf("%s: observation=%s chains=%v", ErrObservationMultipleAttachments, e.ObservationID, e.ChainIDs)
}

func (e MultipleAttachmentError) Unwrap() error { return ErrObservationMultipleAttachments }

func validBoundedText(value, field string, max int, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if len([]rune(value)) > max {
		return fmt.Errorf("%s exceeds %d characters", field, max)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must not contain newlines", field)
	}
	return nil
}
