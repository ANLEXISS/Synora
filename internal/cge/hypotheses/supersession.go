package hypotheses

import (
	"fmt"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/evidence"
)

type SupersessionProposal struct {
	PreviousSetID             SetID
	NewSetID                  SetID
	PreviousSourceRevision    uint64
	PreviousStatus            Status
	PreviousAssessmentVersion uint64
	PreviousAssessmentID      string
	PreviousSubject           Subject
	NewSubject                Subject
	NewSet                    Snapshot
	ProposedAt                time.Time
	ReasonCode                string
}

func (p SupersessionProposal) Clone() SupersessionProposal {
	p.PreviousSubject = cloneSubject(p.PreviousSubject)
	p.NewSubject = cloneSubject(p.NewSubject)
	p.NewSet = cloneSnapshot(p.NewSet)
	return p
}

type SupersedeCommand struct {
	PreviousSetID             SetID
	NewSetID                  SetID
	PreviousSourceRevision    uint64
	PreviousAssessmentVersion uint64
	PreviousAssessmentID      string
	NewSet                    Snapshot
	Mutation                  chains.MutationContext
}

func (c SupersedeCommand) Clone() SupersedeCommand {
	c.NewSet = cloneSnapshot(c.NewSet)
	return c
}

func (c SupersedeCommand) Validate() error {
	if err := validSetID(c.PreviousSetID); err != nil || validSetID(c.NewSetID) != nil || c.PreviousSetID == c.NewSetID || c.PreviousSourceRevision == 0 || c.PreviousAssessmentVersion == 0 || c.PreviousAssessmentID == "" {
		if err == nil {
			err = fmt.Errorf("supersession command envelope is invalid")
		}
		return hypothesisError(ErrInvalidSupersessionCommand, FamilyEvidence, c.PreviousSetID, "validate", c.PreviousSourceRevision, 0, err)
	}
	if err := c.Mutation.Validate(); err != nil {
		return hypothesisError(ErrInvalidContext, FamilyEvidence, c.PreviousSetID, "validate", c.PreviousSourceRevision, 0, err)
	}
	if c.NewSet.ID != c.NewSetID || c.NewSet.Family != FamilyEvidence || c.NewSet.Status != StatusOpen || c.NewSet.Revision != 1 || len(c.NewSet.History) != 1 || c.NewSet.History[0].Operation != OperationHypothesisOpened || c.NewSet.History[0].PreviousRevision != 0 || c.NewSet.History[0].NewRevision != 1 {
		return hypothesisError(ErrInvalidSupersessionCommand, FamilyEvidence, c.PreviousSetID, "validate", c.PreviousSourceRevision, 0, fmt.Errorf("successor is not an initial evidence hypothesis"))
	}
	opening := c.NewSet.History[0]
	if opening.Actor != c.Mutation.Actor || opening.CorrelationID != c.Mutation.CorrelationID || !opening.At.Equal(c.Mutation.At) || !c.NewSet.UpdatedAt.Equal(c.Mutation.At) {
		return hypothesisError(ErrInvalidSupersessionCommand, FamilyEvidence, c.PreviousSetID, "validate", c.PreviousSourceRevision, 0, fmt.Errorf("successor opening provenance does not match mutation"))
	}
	if c.NewSet.Lineage.PredecessorSetID != c.PreviousSetID || c.NewSet.Lineage.SuccessorSetID != "" || c.NewSet.Lineage.Generation < 2 || c.NewSet.Lineage.RootSetID == "" {
		return hypothesisError(ErrInvalidSupersessionCommand, FamilyEvidence, c.PreviousSetID, "validate", c.PreviousSourceRevision, 0, fmt.Errorf("successor lineage is invalid"))
	}
	if c.NewSet.Subject.ChainID == "" || c.NewSet.Subject.ObservationID == "" || c.NewSet.Subject.EvidenceFingerprint == "" {
		return hypothesisError(ErrInvalidSupersessionCommand, FamilyEvidence, c.PreviousSetID, "validate", c.PreviousSourceRevision, 0, fmt.Errorf("successor subject is invalid"))
	}
	if _, err := Restore(c.NewSet); err != nil {
		return hypothesisError(ErrInvalidSupersessionCommand, FamilyEvidence, c.PreviousSetID, "validate", c.PreviousSourceRevision, 0, err)
	}
	return nil
}

func ProposeEvidenceSupersession(current Snapshot, evaluation evidence.EvidenceEvaluation, proposedAt time.Time) (SupersessionProposal, error) {
	if proposedAt.IsZero() {
		return SupersessionProposal{}, hypothesisError(ErrInvalidSupersessionProposal, FamilyEvidence, current.ID, "proposal", current.Revision, current.Revision, fmt.Errorf("proposed timestamp is zero"))
	}
	owned, err := Restore(current)
	if err != nil {
		return SupersessionProposal{}, hypothesisError(ErrInvalidSupersessionProposal, FamilyEvidence, current.ID, "proposal", current.Revision, current.Revision, err)
	}
	current = owned.Snapshot()
	if current.Family != FamilyEvidence || !CanSupersede(current.Status) {
		return SupersessionProposal{}, hypothesisError(ErrSupersessionNotAllowed, current.Family, current.ID, "proposal", current.Revision, current.Revision, nil)
	}
	if evaluation.Decision != evidence.DecisionAmbiguous {
		return SupersessionProposal{}, hypothesisError(ErrEvidenceNotAmbiguous, FamilyEvidence, current.ID, "proposal", current.Revision, current.Revision, nil)
	}
	if err := validateEvidenceEvaluationForConversion(evaluation); err != nil {
		return SupersessionProposal{}, hypothesisError(ErrInvalidSupersessionProposal, FamilyEvidence, current.ID, "proposal", current.Revision, current.Revision, err)
	}
	if evaluation.ChainID != current.Subject.ChainID || evaluation.TargetObservationID != current.Subject.ObservationID {
		return SupersessionProposal{}, hypothesisError(ErrSupersessionSubjectMismatch, FamilyEvidence, current.ID, "proposal", current.Revision, current.Revision, nil)
	}
	if evaluation.EvidenceFingerprint == current.Subject.EvidenceFingerprint {
		return SupersessionProposal{}, hypothesisError(ErrSupersessionNotRequired, FamilyEvidence, current.ID, "proposal", current.Revision, current.Revision, nil)
	}
	newSet, err := FromAmbiguousEvidence(evaluation, proposedAt, chains.MutationContext{At: proposedAt, Actor: "hypothesis-supersession", Reason: "supersession preparation", CorrelationID: "supersession-preparation"})
	if err != nil {
		return SupersessionProposal{}, hypothesisError(ErrInvalidSupersessionProposal, FamilyEvidence, current.ID, "proposal", current.Revision, current.Revision, err)
	}
	newSnapshot := newSet.Snapshot()
	newSnapshot.Lineage = Lineage{RootSetID: current.Lineage.RootSetID, PredecessorSetID: current.ID, Generation: current.Lineage.Generation + 1}
	if newSnapshot.ID == current.ID {
		return SupersessionProposal{}, hypothesisError(ErrInvalidSupersessionProposal, FamilyEvidence, current.ID, "proposal", current.Revision, current.Revision, fmt.Errorf("successor SetID is not distinct"))
	}
	if _, err := Restore(newSnapshot); err != nil {
		return SupersessionProposal{}, hypothesisError(ErrInvalidSupersessionProposal, FamilyEvidence, current.ID, "proposal", current.Revision, current.Revision, err)
	}
	return SupersessionProposal{PreviousSetID: current.ID, NewSetID: newSnapshot.ID, PreviousSourceRevision: current.Revision, PreviousStatus: current.Status, PreviousAssessmentVersion: current.CurrentAssessmentVersion, PreviousAssessmentID: current.Assessments[len(current.Assessments)-1].ID, PreviousSubject: current.Subject, NewSubject: newSnapshot.Subject, NewSet: newSnapshot, ProposedAt: proposedAt, ReasonCode: "evidence.superseded"}, nil
}

func (p SupersessionProposal) Command(mutation chains.MutationContext) (SupersedeCommand, error) {
	if err := mutation.Validate(); err != nil {
		return SupersedeCommand{}, hypothesisError(ErrInvalidContext, FamilyEvidence, p.PreviousSetID, "command", p.PreviousSourceRevision, 0, err)
	}
	if mutation.At.Before(p.ProposedAt) {
		return SupersedeCommand{}, hypothesisError(ErrInvalidContext, FamilyEvidence, p.PreviousSetID, "command", p.PreviousSourceRevision, 0, fmt.Errorf("mutation timestamp precedes proposal"))
	}
	snapshot, err := bindSuccessorOpening(p.NewSet, mutation, p.PreviousSetID)
	if err != nil {
		return SupersedeCommand{}, err
	}
	command := SupersedeCommand{PreviousSetID: p.PreviousSetID, NewSetID: p.NewSetID, PreviousSourceRevision: p.PreviousSourceRevision, PreviousAssessmentVersion: p.PreviousAssessmentVersion, PreviousAssessmentID: p.PreviousAssessmentID, NewSet: snapshot, Mutation: mutation}
	if err := command.Validate(); err != nil {
		return SupersedeCommand{}, err
	}
	return command, nil
}

func bindSuccessorOpening(snapshot Snapshot, mutation chains.MutationContext, predecessor SetID) (Snapshot, error) {
	set, err := Restore(snapshot)
	if err != nil {
		return Snapshot{}, err
	}
	if mutation.At.Before(set.createdAt) {
		return Snapshot{}, hypothesisError(ErrInvalidContext, FamilyEvidence, snapshot.ID, "successor_open", 0, 0, fmt.Errorf("mutation timestamp precedes creation"))
	}
	set.updatedAt = mutation.At
	set.history[0].At = mutation.At
	set.history[0].Actor = mutation.Actor
	set.history[0].CorrelationID = mutation.CorrelationID
	set.history[0].Reason = fmt.Sprintf("hypothesis.opened_from_supersession predecessor=%s", boundedSetToken(predecessor))
	if err := set.Validate(); err != nil {
		return Snapshot{}, err
	}
	return set.Snapshot(), nil
}

func boundedSetToken(id SetID) string {
	value := string(id)
	if len(value) > 24 {
		return value[:24]
	}
	return value
}

func (h *HypothesisSet) MarkSuperseded(successor Snapshot, mutation chains.MutationContext) error {
	if h == nil {
		return hypothesisError(ErrInvalidHypothesis, FamilyEvidence, successor.ID, "supersede", 0, 0, fmt.Errorf("hypothesis is nil"))
	}
	if h.family != FamilyEvidence || !CanSupersede(h.status) {
		return hypothesisError(ErrSupersessionNotAllowed, h.family, h.id, "supersede", h.revision, h.revision, nil)
	}
	if h.lineage.SuccessorSetID != "" {
		return hypothesisError(ErrHypothesisSupersessionCollision, h.family, h.id, "supersede", h.revision, h.revision, nil)
	}
	if err := mutation.Validate(); err != nil {
		return hypothesisError(ErrInvalidContext, h.family, h.id, "supersede", h.revision, h.revision, err)
	}
	if successor.ID == h.id || successor.Family != FamilyEvidence || successor.Status != StatusOpen || successor.Revision != 1 || successor.Lineage.PredecessorSetID != h.id || successor.Lineage.RootSetID != h.lineage.RootSetID || successor.Lineage.Generation != h.lineage.Generation+1 || successor.Lineage.SuccessorSetID != "" || successor.Subject.ChainID != h.subject.ChainID || successor.Subject.ObservationID != h.subject.ObservationID || successor.Subject.EvidenceFingerprint == h.subject.EvidenceFingerprint {
		return hypothesisError(ErrHypothesisLineageDivergence, h.family, h.id, "supersede", h.revision, h.revision, nil)
	}
	if _, err := Restore(successor); err != nil {
		return hypothesisError(ErrInvalidHypothesis, h.family, h.id, "supersede", h.revision, h.revision, err)
	}
	if mutation.At.Before(h.updatedAt) {
		return hypothesisError(ErrInvalidContext, h.family, h.id, "supersede", h.revision, h.revision, fmt.Errorf("mutation timestamp is older than update"))
	}
	candidate := *h
	candidate.status = StatusSuperseded
	candidate.lineage.SuccessorSetID = successor.ID
	candidate.updatedAt = mutation.At
	candidate.revision++
	candidate.history = append(cloneHistory(h.history), RevisionRecord{SetID: h.id, Operation: OperationHypothesisSuperseded, PreviousRevision: h.revision, NewRevision: candidate.revision, At: mutation.At, Actor: mutation.Actor, Reason: mutation.Reason, CorrelationID: mutation.CorrelationID, PreviousStatus: h.status, NewStatus: StatusSuperseded, PreviousSuccessorSetID: "", NewSuccessorSetID: successor.ID, SuccessorGeneration: successor.Lineage.Generation})
	if err := candidate.Validate(); err != nil {
		return err
	}
	*h = candidate
	return nil
}

type SupersessionApplyResult struct {
	PreviousBefore     Snapshot
	PreviousAfter      Snapshot
	NewAfter           Snapshot
	PreviousRevision   RevisionRecord
	NewOpeningRevision RevisionRecord
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	clone := snapshot
	clone.Subject = cloneSubject(snapshot.Subject)
	clone.Alternatives = cloneAlternatives(snapshot.Alternatives)
	clone.Assessments = cloneAssessments(snapshot.Assessments)
	clone.History = cloneHistory(snapshot.History)
	return clone
}
