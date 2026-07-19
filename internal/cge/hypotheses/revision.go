package hypotheses

import (
	"fmt"
	"time"

	"synora/internal/cge/chains"
)

func (r RevisionRecord) validate(family Family, setID SetID) error {
	if err := validSetID(r.SetID); err != nil {
		return err
	}
	if r.SetID != setID {
		return fmt.Errorf("revision set id is inconsistent")
	}
	if r.Operation != OperationHypothesisOpened && r.Operation != OperationHypothesisStatusChanged && r.Operation != OperationHypothesisRebased && r.Operation != OperationHypothesisSuperseded && r.Operation != OperationHypothesisResolved {
		return fmt.Errorf("unsupported hypothesis revision operation %q", r.Operation)
	}
	if r.NewRevision != r.PreviousRevision+1 {
		return fmt.Errorf("hypothesis revision is not contiguous")
	}
	if r.At.IsZero() || r.Actor == "" || r.Reason == "" {
		return fmt.Errorf("hypothesis revision provenance is incomplete")
	}
	if err := validText(r.Actor, "revision actor", true, 128); err != nil {
		return err
	}
	if err := validText(r.Reason, "revision reason", true, 256); err != nil {
		return err
	}
	if err := validText(r.CorrelationID, "revision correlation id", false, 128); err != nil {
		return err
	}
	if err := family.Validate(); err != nil {
		return err
	}
	if r.PreviousStatus != "" {
		if err := r.PreviousStatus.Validate(); err != nil {
			return err
		}
	}
	if err := r.NewStatus.Validate(); err != nil {
		return err
	}
	if r.Operation == OperationHypothesisOpened {
		if r.PreviousRevision != 0 || r.PreviousStatus != "" || r.NewStatus != StatusOpen {
			return fmt.Errorf("opening revision has invalid state")
		}
	} else if r.Operation == OperationHypothesisStatusChanged {
		if r.PreviousStatus == "" || r.NewStatus == StatusSuperseded || !CanTransition(r.PreviousStatus, r.NewStatus) {
			return fmt.Errorf("status revision has invalid transition")
		}
	} else if r.Operation == OperationHypothesisRebased {
		if r.PreviousStatus == "" || r.PreviousStatus != r.NewStatus || r.PreviousAssessmentVersion == 0 || r.NewAssessmentVersion != r.PreviousAssessmentVersion+1 || r.PreviousAssessmentID == "" || r.NewAssessmentID == "" || !validFingerprint(r.PreviousAssessmentFingerprint) || !validFingerprint(r.NewAssessmentFingerprint) {
			return fmt.Errorf("rebase revision has invalid assessment transition")
		}
	} else if r.Operation == OperationHypothesisSuperseded {
		if r.PreviousStatus != StatusOpen && r.PreviousStatus != StatusUnderReview || r.NewStatus != StatusSuperseded || r.PreviousSuccessorSetID != "" || r.NewSuccessorSetID == "" || r.SuccessorGeneration == 0 {
			return fmt.Errorf("supersession revision has invalid state")
		}
	} else if r.Operation == OperationHypothesisResolved {
		if (r.PreviousStatus != StatusOpen && r.PreviousStatus != StatusUnderReview) || r.NewStatus != StatusResolved || r.SelectedAssessmentVersion == 0 || r.SelectedAssessmentID == "" || !validFingerprint(r.SelectedAssessmentFingerprint) || r.SelectedAlternativeID == "" || r.SelectedAlternativeKind.Validate() != nil || r.SelectedEffectKind.Validate() != nil || !validFingerprint(r.SelectedEffectFingerprint) {
			return fmt.Errorf("resolution revision has invalid selection")
		}
	}
	return nil
}

// Validate exposes structural revision validation to persistence boundaries.
// The family does not affect the local status machine, so the association
// family is sufficient for this record-level check.
func (r RevisionRecord) Validate() error { return r.validate(FamilyAssociation, r.SetID) }

func openHypothesis(id SetID, family Family, subject Subject, alternatives []Alternative, provenance Provenance, reasonCode, reason string, createdAt time.Time, mutation chains.MutationContext) (*HypothesisSet, error) {
	if createdAt.IsZero() {
		return nil, hypothesisError(ErrInvalidHypothesis, family, id, "open", 0, 0, fmt.Errorf("created timestamp must not be zero"))
	}
	if err := mutation.Validate(); err != nil {
		return nil, hypothesisError(ErrInvalidContext, family, id, "open", 0, 0, err)
	}
	if mutation.At.Before(createdAt) {
		return nil, hypothesisError(ErrInvalidContext, family, id, "open", 0, 0, fmt.Errorf("mutation timestamp is older than creation timestamp"))
	}
	h := &HypothesisSet{
		id: id, family: family, status: StatusOpen, subject: subject,
		alternatives: cloneAlternatives(alternatives), provenance: provenance,
		reasonCode: reasonCode, reason: reason, createdAt: createdAt, updatedAt: mutation.At,
		revision: 1,
		history:  []RevisionRecord{{SetID: id, Operation: OperationHypothesisOpened, PreviousRevision: 0, NewRevision: 1, At: mutation.At, Actor: mutation.Actor, Reason: mutation.Reason, CorrelationID: mutation.CorrelationID, NewStatus: StatusOpen}},
		lineage:  Lineage{RootSetID: id, Generation: 1},
	}
	fingerprint, err := DeriveAssessmentFingerprint(family, subject, alternatives, provenance)
	if err != nil {
		return nil, hypothesisError(ErrInvalidHypothesisAssessment, family, id, "open", 0, 0, err)
	}
	assessmentID, err := DeriveAssessmentID(id, 1, fingerprint)
	if err != nil {
		return nil, hypothesisError(ErrInvalidHypothesisAssessment, family, id, "open", 0, 0, err)
	}
	h.currentAssessmentVersion = 1
	resolutionSchemaVersion := ResolutionSchemaLegacy
	for _, alternative := range alternatives {
		if alternative.ResolutionEffect != nil {
			resolutionSchemaVersion = ResolutionSchemaV1
			break
		}
	}
	h.assessments = []AssessmentVersion{{Version: 1, ID: assessmentID, Fingerprint: fingerprint, Alternatives: cloneAlternatives(alternatives), Provenance: provenance, CreatedAt: provenance.PlannedOrEvaluatedAt, ResolutionSchemaVersion: resolutionSchemaVersion}}
	if err := h.Validate(); err != nil {
		return nil, err
	}
	return h, nil
}
