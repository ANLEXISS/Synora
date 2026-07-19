package hypotheses

import (
	"fmt"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
)

type RebaseProposal struct {
	SetID                     SetID
	SourceRevision            uint64
	PreviousAssessmentVersion uint64
	PreviousAssessmentID      string
	NewAssessment             AssessmentVersionSnapshot
	Family                    Family
	Subject                   Subject
	ReasonCode                string
}

func (p RebaseProposal) Clone() RebaseProposal {
	p.NewAssessment = p.NewAssessment.Clone()
	return p
}

func (p RebaseProposal) Command(mutation chains.MutationContext) (RebaseCommand, error) {
	command := RebaseCommand{SetID: p.SetID, SourceRevision: p.SourceRevision, PreviousAssessmentVersion: p.PreviousAssessmentVersion, PreviousAssessmentID: p.PreviousAssessmentID, Assessment: p.NewAssessment.Clone(), Family: p.Family, Subject: cloneSubject(p.Subject), Mutation: mutation}
	if err := command.Validate(); err != nil {
		return RebaseCommand{}, err
	}
	return command, nil
}

type RebaseCommand struct {
	SetID                     SetID
	SourceRevision            uint64
	PreviousAssessmentVersion uint64
	PreviousAssessmentID      string
	Assessment                AssessmentVersionSnapshot
	Family                    Family
	Subject                   Subject
	Mutation                  chains.MutationContext
}

func (c RebaseCommand) Clone() RebaseCommand {
	c.Assessment = c.Assessment.Clone()
	return c
}

func (c RebaseCommand) Validate() error {
	if err := validSetID(c.SetID); err != nil || c.SourceRevision == 0 || c.PreviousAssessmentVersion == 0 || c.PreviousAssessmentID == "" {
		if err == nil {
			err = fmt.Errorf("rebase command envelope is invalid")
		}
		return hypothesisError(ErrInvalidRebaseCommand, c.Family, c.SetID, "validate", c.SourceRevision, 0, err)
	}
	if err := c.Mutation.Validate(); err != nil {
		return hypothesisError(ErrInvalidContext, c.Family, c.SetID, "validate", c.SourceRevision, 0, err)
	}
	if c.Family != "" {
		if err := c.Family.Validate(); err != nil || c.Subject.validate(c.Family) != nil {
			if err == nil {
				err = c.Subject.validate(c.Family)
			}
			return hypothesisError(ErrInvalidRebaseCommand, c.Family, c.SetID, "validate", c.SourceRevision, 0, err)
		}
	}
	if c.Assessment.Version == 0 || c.Assessment.CreatedAt.IsZero() || !validFingerprint(c.Assessment.Fingerprint) || len(c.Assessment.Alternatives) < 2 || c.Assessment.ID == "" || c.Assessment.Provenance.Source == "" || c.Assessment.Provenance.PlannedOrEvaluatedAt.IsZero() {
		return hypothesisError(ErrInvalidHypothesisAssessment, c.Family, c.SetID, "validate", c.SourceRevision, 0, fmt.Errorf("assessment is structurally incomplete"))
	}
	if c.Family != "" {
		if err := c.Assessment.Provenance.ValidateFor(c.Family); err != nil {
			return hypothesisError(ErrInvalidHypothesisAssessment, c.Family, c.SetID, "validate", c.SourceRevision, 0, err)
		}
	}
	if c.Assessment.Version != c.PreviousAssessmentVersion+1 {
		return hypothesisError(ErrInvalidRebaseCommand, c.Family, c.SetID, "validate", c.SourceRevision, 0, fmt.Errorf("assessment version is not contiguous"))
	}
	return nil
}

func ProposeAssociationRebase(current Snapshot, plan association.Plan, proposedAt time.Time) (RebaseProposal, error) {
	if err := validateRebaseBase(current, FamilyAssociation, proposedAt); err != nil {
		return RebaseProposal{}, err
	}
	owned, err := Restore(current)
	if err != nil {
		return RebaseProposal{}, err
	}
	current = owned.Snapshot()
	if plan.Decision != association.DecisionAmbiguous {
		return RebaseProposal{}, hypothesisError(ErrAssociationNotAmbiguous, FamilyAssociation, current.ID, "association_rebase", current.Revision, current.Revision, nil)
	}
	if err := plan.Validate(); err != nil {
		return RebaseProposal{}, hypothesisError(ErrInvalidRebaseProposal, FamilyAssociation, current.ID, "association_rebase", current.Revision, current.Revision, err)
	}
	if plan.Observation.ID != current.Subject.ObservationID {
		return RebaseProposal{}, hypothesisError(ErrRebaseSubjectMismatch, FamilyAssociation, current.ID, "association_rebase", current.Revision, current.Revision, nil)
	}
	created, err := FromAmbiguousAssociation(plan, proposedAt, chains.MutationContext{At: proposedAt, Actor: "hypothesis-rebase", Reason: "association rebase", CorrelationID: "association-rebase"})
	if err != nil {
		return RebaseProposal{}, err
	}
	assessment := created.Snapshot().Assessments[0]
	assessment.CreatedAt = proposedAt
	return makeRebaseProposal(current, assessment, "association.rebased")
}

func ProposeEvidenceRebase(current Snapshot, evaluation evidence.EvidenceEvaluation, proposedAt time.Time) (RebaseProposal, error) {
	if err := validateRebaseBase(current, FamilyEvidence, proposedAt); err != nil {
		return RebaseProposal{}, err
	}
	owned, err := Restore(current)
	if err != nil {
		return RebaseProposal{}, err
	}
	current = owned.Snapshot()
	if evaluation.Decision != evidence.DecisionAmbiguous {
		return RebaseProposal{}, hypothesisError(ErrEvidenceNotAmbiguous, FamilyEvidence, current.ID, "evidence_rebase", current.Revision, current.Revision, nil)
	}
	if err := validateEvidenceEvaluationForConversion(evaluation); err != nil {
		return RebaseProposal{}, hypothesisError(ErrInvalidRebaseProposal, FamilyEvidence, current.ID, "evidence_rebase", current.Revision, current.Revision, err)
	}
	if current.Subject.ChainID != evaluation.ChainID || current.Subject.ObservationID != evaluation.TargetObservationID {
		return RebaseProposal{}, hypothesisError(ErrRebaseSubjectMismatch, FamilyEvidence, current.ID, "evidence_rebase", current.Revision, current.Revision, nil)
	}
	if current.Subject.EvidenceFingerprint != evaluation.EvidenceFingerprint {
		return RebaseProposal{}, hypothesisError(ErrRebaseSubjectMismatch, FamilyEvidence, current.ID, "evidence_rebase", current.Revision, current.Revision, nil)
	}
	created, err := FromAmbiguousEvidence(evaluation, proposedAt, chains.MutationContext{At: proposedAt, Actor: "hypothesis-rebase", Reason: "evidence rebase", CorrelationID: "evidence-rebase"})
	if err != nil {
		return RebaseProposal{}, err
	}
	assessment := created.Snapshot().Assessments[0]
	assessment.CreatedAt = proposedAt
	return makeRebaseProposal(current, assessment, "evidence.rebased")
}

// Rebase appends one immutable assessment version and updates only the
// aggregate's current assessment view. It never changes status or chains.
func (h *HypothesisSet) Rebase(command RebaseCommand) error {
	if h == nil {
		return hypothesisError(ErrInvalidHypothesis, "", command.SetID, "rebase", command.SourceRevision, 0, fmt.Errorf("hypothesis is nil"))
	}
	command = command.Clone()
	if command.Family == "" {
		command.Family = h.family
	}
	if command.Subject == (Subject{}) {
		command.Subject = h.subject
	}
	if err := command.Validate(); err != nil {
		return err
	}
	if command.SetID != h.id || command.SourceRevision != h.revision {
		return hypothesisError(ErrStaleHypothesisRebase, h.family, h.id, "rebase", command.SourceRevision, h.revision, nil)
	}
	if !CanRebase(h.status) {
		return hypothesisError(ErrRebaseNotAllowed, h.family, h.id, "rebase", command.SourceRevision, h.revision, nil)
	}
	current := h.assessments[len(h.assessments)-1]
	if command.PreviousAssessmentVersion != current.Version || command.PreviousAssessmentID != current.ID {
		return hypothesisError(ErrStaleHypothesisRebase, h.family, h.id, "rebase", command.SourceRevision, h.revision, nil)
	}
	if command.Family != h.family || command.Subject != h.subject || command.Assessment.Version != current.Version+1 {
		return hypothesisError(ErrInvalidRebaseCommand, h.family, h.id, "rebase", command.SourceRevision, h.revision, fmt.Errorf("rebase subject or version is inconsistent"))
	}
	if command.Assessment.Fingerprint == current.Fingerprint {
		return hypothesisError(ErrHypothesisRebaseUnchanged, h.family, h.id, "rebase", command.SourceRevision, h.revision, nil)
	}
	for _, assessment := range h.assessments {
		if assessment.ID == command.Assessment.ID && assessment.Fingerprint != command.Assessment.Fingerprint {
			return hypothesisError(ErrHypothesisAssessmentCollision, h.family, h.id, "rebase", command.SourceRevision, h.revision, nil)
		}
	}
	if err := command.Assessment.validate(h.family, h.subject, h.id); err != nil {
		return hypothesisError(ErrInvalidHypothesisAssessment, h.family, h.id, "rebase", command.SourceRevision, h.revision, err)
	}
	if command.Mutation.At.Before(h.updatedAt) {
		return hypothesisError(ErrInvalidContext, h.family, h.id, "rebase", h.revision, h.revision, fmt.Errorf("mutation timestamp is older than updated timestamp"))
	}
	candidate := *h
	candidate.assessments = cloneAssessments(h.assessments)
	candidate.alternatives = cloneAlternatives(command.Assessment.Alternatives)
	candidate.provenance = command.Assessment.Provenance
	candidate.currentAssessmentVersion = command.Assessment.Version
	candidate.updatedAt = command.Mutation.At
	candidate.revision++
	candidate.history = append(cloneHistory(h.history), RevisionRecord{SetID: h.id, Operation: OperationHypothesisRebased, PreviousRevision: h.revision, NewRevision: candidate.revision, At: command.Mutation.At, Actor: command.Mutation.Actor, Reason: command.Mutation.Reason, CorrelationID: command.Mutation.CorrelationID, PreviousStatus: h.status, NewStatus: h.status, PreviousAssessmentVersion: current.Version, NewAssessmentVersion: command.Assessment.Version, PreviousAssessmentID: current.ID, NewAssessmentID: command.Assessment.ID, PreviousAssessmentFingerprint: current.Fingerprint, NewAssessmentFingerprint: command.Assessment.Fingerprint})
	candidate.assessments = append(candidate.assessments, command.Assessment.Clone())
	if err := candidate.Validate(); err != nil {
		return err
	}
	*h = candidate
	return nil
}

func makeRebaseProposal(current Snapshot, assessment AssessmentVersionSnapshot, reason string) (RebaseProposal, error) {
	if len(current.Assessments) == 0 {
		return RebaseProposal{}, hypothesisError(ErrInvalidRebaseProposal, current.Family, current.ID, "proposal", current.Revision, current.Revision, fmt.Errorf("current assessment is missing"))
	}
	previous := current.Assessments[len(current.Assessments)-1]
	if assessment.CreatedAt.Before(previous.CreatedAt) {
		return RebaseProposal{}, hypothesisError(ErrInvalidRebaseProposal, current.Family, current.ID, "proposal", current.Revision, current.Revision, fmt.Errorf("assessment timestamp precedes current version"))
	}
	assessment.Alternatives = cloneAlternatives(assessment.Alternatives)
	for i := range assessment.Alternatives {
		assessment.Alternatives[i].ID = deriveAlternativeID(current.ID, assessment.Alternatives[i])
	}
	fingerprint, err := DeriveAssessmentFingerprint(current.Family, current.Subject, assessment.Alternatives, assessment.Provenance)
	if err != nil {
		return RebaseProposal{}, hypothesisError(ErrInvalidRebaseProposal, current.Family, current.ID, "proposal", current.Revision, current.Revision, err)
	}
	assessment.Fingerprint = fingerprint
	if assessment.Fingerprint == previous.Fingerprint {
		return RebaseProposal{}, hypothesisError(ErrHypothesisRebaseUnchanged, current.Family, current.ID, "proposal", current.Revision, current.Revision, nil)
	}
	assessment.Version = previous.Version + 1
	id, err := DeriveAssessmentID(current.ID, assessment.Version, assessment.Fingerprint)
	if err != nil {
		return RebaseProposal{}, hypothesisError(ErrInvalidRebaseProposal, current.Family, current.ID, "proposal", current.Revision, current.Revision, err)
	}
	assessment.ID = id
	if err := assessment.validate(current.Family, current.Subject, current.ID); err != nil {
		return RebaseProposal{}, hypothesisError(ErrInvalidRebaseProposal, current.Family, current.ID, "proposal", current.Revision, current.Revision, err)
	}
	return RebaseProposal{SetID: current.ID, SourceRevision: current.Revision, PreviousAssessmentVersion: previous.Version, PreviousAssessmentID: previous.ID, NewAssessment: assessment.Clone(), Family: current.Family, Subject: current.Subject, ReasonCode: reason}, nil
}

func validateRebaseBase(current Snapshot, family Family, proposedAt time.Time) error {
	if proposedAt.IsZero() {
		return hypothesisError(ErrInvalidRebaseProposal, family, current.ID, "validate", current.Revision, current.Revision, fmt.Errorf("proposed timestamp is zero"))
	}
	if _, err := Restore(current); err != nil {
		return hypothesisError(ErrInvalidRebaseProposal, family, current.ID, "validate", current.Revision, current.Revision, err)
	}
	if current.Family != family {
		return hypothesisError(ErrInvalidRebaseProposal, family, current.ID, "validate", current.Revision, current.Revision, fmt.Errorf("family mismatch"))
	}
	if !CanRebase(current.Status) {
		return hypothesisError(ErrRebaseNotAllowed, family, current.ID, "validate", current.Revision, current.Revision, nil)
	}
	return nil
}
