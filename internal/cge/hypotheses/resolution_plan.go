package hypotheses

import (
	"fmt"
	"time"

	"synora/internal/cge/chains"
)

// ResolutionReadiness reports whether the current assessment contains enough
// immutable material for a future explicit resolution. It never selects an
// alternative and never consults another aggregate.
type ResolutionReadiness struct {
	Ready                      bool
	SchemaVersion              int
	AlternativeCount           int
	ResolvableAlternativeCount int
	ReasonCode                 string
}

// ResolutionReadiness returns a defensive, derived view of resolution
// material. Legacy assessments remain valid but are intentionally not ready.
func (s Snapshot) ResolutionReadiness() ResolutionReadiness {
	readiness := ResolutionReadiness{AlternativeCount: len(s.Alternatives)}
	if s.Status != StatusOpen && s.Status != StatusUnderReview {
		readiness.ReasonCode = string(ErrHypothesisResolutionNotAllowed.Error())
		return readiness
	}
	set, err := Restore(s)
	if err != nil || len(set.assessments) == 0 {
		readiness.ReasonCode = string(ErrHypothesisResolutionMaterialMissing.Error())
		return readiness
	}
	assessment := set.assessments[len(set.assessments)-1]
	readiness.SchemaVersion = assessment.ResolutionSchemaVersion
	readiness.AlternativeCount = len(assessment.Alternatives)
	for _, alternative := range assessment.Alternatives {
		if alternative.ResolutionEffect != nil {
			readiness.ResolvableAlternativeCount++
		}
	}
	if assessment.ResolutionSchemaVersion != ResolutionSchemaV1 {
		readiness.ReasonCode = string(ErrHypothesisResolutionMaterialMissing.Error())
		return readiness
	}
	readiness.Ready = readiness.ResolvableAlternativeCount == readiness.AlternativeCount && readiness.AlternativeCount >= 2
	if readiness.Ready {
		readiness.ReasonCode = "hypothesis_resolution_ready"
	} else {
		readiness.ReasonCode = string(ErrHypothesisResolutionMaterialMissing.Error())
	}
	return readiness
}

// ResolutionPlan is an explicit, immutable preparation for one named
// alternative. It contains optimistic preconditions for a future mutation.
type ResolutionPlan struct {
	SetID SetID

	SourceRevision uint64

	AssessmentVersion     uint64
	AssessmentID          string
	AssessmentFingerprint string

	AlternativeID   string
	AlternativeKind AlternativeKind

	Family  Family
	Subject Subject

	Effect ResolutionEffect

	PlannedAt  time.Time
	ReasonCode string
}

func (p ResolutionPlan) Clone() ResolutionPlan {
	p.Subject = cloneSubject(p.Subject)
	p.Effect = p.Effect.Clone()
	return p
}

func (p ResolutionPlan) Validate() error {
	if err := validSetID(p.SetID); err != nil {
		return fmt.Errorf("%w: set id: %v", ErrInvalidResolutionPlan, err)
	}
	if p.SourceRevision == 0 || p.AssessmentVersion == 0 || p.AssessmentID == "" || !validFingerprint(p.AssessmentFingerprint) || p.AlternativeID == "" || p.PlannedAt.IsZero() {
		return fmt.Errorf("%w: preconditions are incomplete", ErrInvalidResolutionPlan)
	}
	if err := p.Family.Validate(); err != nil {
		return fmt.Errorf("%w: family: %v", ErrInvalidResolutionPlan, err)
	}
	if err := p.Subject.validate(p.Family); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrInvalidResolutionPlan, err)
	}
	if err := p.AlternativeKind.Validate(); err != nil {
		return fmt.Errorf("%w: alternative: %v", ErrInvalidResolutionPlan, err)
	}
	if err := validResolutionText(p.ReasonCode, "reason code", 64, true); err != nil {
		return fmt.Errorf("%w: reason: %v", ErrInvalidResolutionPlan, err)
	}
	alternative := Alternative{ID: p.AlternativeID, Kind: p.AlternativeKind, ChainID: p.Subject.ChainID}
	switch p.Effect.Kind {
	case ResolutionEffectAttachObservation:
		if p.Effect.AttachObservation == nil {
			return fmt.Errorf("%w: attach effect payload is missing", ErrInvalidResolutionPlan)
		}
		alternative.ChainID = p.Effect.AttachObservation.ChainID
		alternative.SourceRevision = p.Effect.AttachObservation.SourceRevision
	case ResolutionEffectCreateCandidate:
		if p.Effect.CreateCandidate == nil {
			return fmt.Errorf("%w: candidate effect payload is missing", ErrInvalidResolutionPlan)
		}
		alternative.ChainID = p.Effect.CreateCandidate.ChainID
	case ResolutionEffectAddContribution:
		if p.Effect.AddContribution == nil {
			return fmt.Errorf("%w: contribution effect payload is missing", ErrInvalidResolutionPlan)
		}
		alternative.SourceRevision = p.Effect.AddContribution.SourceRevision
		alternative.ContributionID = p.Effect.AddContribution.ContributionTemplate.ID
		alternative.EvidenceFingerprint = p.Subject.EvidenceFingerprint
	}
	if err := p.Effect.ValidateFor(p.Family, p.Subject, alternative); err != nil {
		return fmt.Errorf("%w: effect: %v", ErrInvalidResolutionPlan, err)
	}
	return nil
}

// ResolveCommand is the explicit, optimistic command produced from one plan.
// It never contains a caller-supplied outcome: the coordinator must derive
// that value from the mutation actually applied to its candidate registry.
type ResolveCommand struct {
	SetID                 SetID
	SourceRevision        uint64
	AssessmentVersion     uint64
	AssessmentID          string
	AssessmentFingerprint string
	AlternativeID         string
	AlternativeKind       AlternativeKind
	Effect                ResolutionEffect
	Mutation              chains.MutationContext
}

func (c ResolveCommand) Clone() ResolveCommand {
	c.Effect = c.Effect.Clone()
	c.Mutation.ObservationIDs = append([]string(nil), c.Mutation.ObservationIDs...)
	return c
}

func (c ResolveCommand) Validate() error {
	if err := validSetID(c.SetID); err != nil || c.SourceRevision == 0 || c.AssessmentVersion == 0 || c.AssessmentID == "" || !validFingerprint(c.AssessmentFingerprint) || c.AlternativeID == "" {
		if err == nil {
			err = fmt.Errorf("resolve preconditions are incomplete")
		}
		return fmt.Errorf("%w: %v", ErrInvalidResolveCommand, err)
	}
	if err := c.AlternativeKind.Validate(); err != nil {
		return fmt.Errorf("%w: alternative kind: %v", ErrInvalidResolveCommand, err)
	}
	if err := c.Effect.Kind.Validate(); err != nil {
		return fmt.Errorf("%w: effect: %v", ErrInvalidResolveCommand, err)
	}
	if err := c.Mutation.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidContext, err)
	}
	if _, err := c.Effect.Fingerprint(); err != nil {
		return fmt.Errorf("%w: effect fingerprint: %v", ErrInvalidResolveCommand, err)
	}
	return nil
}

func (p ResolutionPlan) Command(mutation chains.MutationContext) (ResolveCommand, error) {
	if err := p.Validate(); err != nil {
		return ResolveCommand{}, err
	}
	if err := mutation.Validate(); err != nil {
		return ResolveCommand{}, fmt.Errorf("%w: %v", ErrInvalidContext, err)
	}
	command := ResolveCommand{SetID: p.SetID, SourceRevision: p.SourceRevision, AssessmentVersion: p.AssessmentVersion, AssessmentID: p.AssessmentID, AssessmentFingerprint: p.AssessmentFingerprint, AlternativeID: p.AlternativeID, AlternativeKind: p.AlternativeKind, Effect: p.Effect.Clone(), Mutation: mutation}
	if err := command.Validate(); err != nil {
		return ResolveCommand{}, err
	}
	return command.Clone(), nil
}

// PlanResolution prepares a resolution for the explicitly named alternative.
// It does not rank alternatives, mutate the snapshot, or inspect a registry.
func PlanResolution(snapshot Snapshot, alternativeID string, plannedAt time.Time) (ResolutionPlan, error) {
	if plannedAt.IsZero() {
		return ResolutionPlan{}, ErrInvalidTimestampResolution
	}
	set, err := Restore(snapshot)
	if err != nil {
		return ResolutionPlan{}, fmt.Errorf("%w: %v", ErrInvalidResolutionPlan, err)
	}
	if !CanResolve(set.status) {
		return ResolutionPlan{}, ErrHypothesisResolutionNotAllowed
	}
	readiness := snapshot.ResolutionReadiness()
	if !readiness.Ready {
		return ResolutionPlan{}, fmt.Errorf("%w: %s", ErrHypothesisResolutionMaterialMissing, readiness.ReasonCode)
	}
	assessment := set.assessments[len(set.assessments)-1]
	var selected *Alternative
	for i := range assessment.Alternatives {
		if assessment.Alternatives[i].ID == alternativeID {
			copy := assessment.Alternatives[i]
			selected = &copy
			break
		}
	}
	if selected == nil {
		return ResolutionPlan{}, fmt.Errorf("%w: %s", ErrAlternativeNotFound, alternativeID)
	}
	plan := ResolutionPlan{
		SetID: set.id, SourceRevision: set.revision,
		AssessmentVersion: assessment.Version, AssessmentID: assessment.ID, AssessmentFingerprint: assessment.Fingerprint,
		AlternativeID: selected.ID, AlternativeKind: selected.Kind,
		Family: set.family, Subject: cloneSubject(set.subject), Effect: selected.ResolutionEffect.Clone(),
		PlannedAt: plannedAt, ReasonCode: "resolution.explicit_alternative",
	}
	if err := plan.Validate(); err != nil {
		return ResolutionPlan{}, err
	}
	return plan.Clone(), nil
}

func CanResolve(status Status) bool { return status == StatusOpen || status == StatusUnderReview }
