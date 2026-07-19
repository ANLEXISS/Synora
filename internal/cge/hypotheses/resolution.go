package hypotheses

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"synora/internal/cge/chains"
)

const (
	ResolutionSchemaLegacy = 0
	ResolutionSchemaV1     = 1
)

type ResolutionEffectKind string

const (
	ResolutionEffectAttachObservation ResolutionEffectKind = "association_attach_observation"
	ResolutionEffectCreateCandidate   ResolutionEffectKind = "association_create_candidate"
	ResolutionEffectAddContribution   ResolutionEffectKind = "evidence_add_contribution"
	ResolutionEffectNoChain           ResolutionEffectKind = "no_chain_effect"
)

type AttachObservationEffect struct {
	ChainID        chains.ChainID
	SourceRevision uint64
	Observation    chains.ObservationRef
}

type CreateCandidateEffect struct {
	ChainID           chains.ChainID
	Observation       chains.ObservationRef
	InitialStatus     chains.Status
	InitialConfidence float64
}

type ContributionTemplate struct {
	ID             string
	Source         string
	Kind           chains.ContributionKind
	Value          float64
	ObservationIDs []string
	ReasonCode     string
}

type AddContributionEffect struct {
	ChainID              chains.ChainID
	SourceRevision       uint64
	ContributionTemplate ContributionTemplate
}

type NoChainEffect struct {
	ReasonCode string
}

type ResolutionEffect struct {
	Kind              ResolutionEffectKind
	AttachObservation *AttachObservationEffect
	CreateCandidate   *CreateCandidateEffect
	AddContribution   *AddContributionEffect
	NoChainEffect     *NoChainEffect
}

// ResolutionOutcome is the detached result produced by applying exactly one
// immutable resolution effect. It contains no intent or raw payload.
type ResolutionOutcome struct {
	Kind              ResolutionEffectKind
	AttachObservation *AttachObservationOutcome
	CreateCandidate   *CreateCandidateOutcome
	AddContribution   *AddContributionOutcome
	NoChainEffect     *NoChainEffectOutcome
}

type AttachObservationOutcome struct {
	ChainID          chains.ChainID
	PreviousRevision uint64
	NewRevision      uint64
	ObservationID    string
}

type CreateCandidateOutcome struct {
	ChainID     chains.ChainID
	NewRevision uint64
	Status      chains.Status
}

type AddContributionOutcome struct {
	ChainID            chains.ChainID
	PreviousRevision   uint64
	NewRevision        uint64
	ContributionID     string
	PreviousConfidence float64
	NewConfidence      float64
}

type NoChainEffectOutcome struct{ ReasonCode string }

func (o ResolutionOutcome) Clone() ResolutionOutcome {
	if o.AttachObservation != nil {
		v := *o.AttachObservation
		o.AttachObservation = &v
	}
	if o.CreateCandidate != nil {
		v := *o.CreateCandidate
		o.CreateCandidate = &v
	}
	if o.AddContribution != nil {
		v := *o.AddContribution
		o.AddContribution = &v
	}
	if o.NoChainEffect != nil {
		v := *o.NoChainEffect
		o.NoChainEffect = &v
	}
	return o
}

func (o ResolutionOutcome) Validate() error {
	if err := o.Kind.Validate(); err != nil {
		return err
	}
	count := 0
	if o.AttachObservation != nil {
		count++
	}
	if o.CreateCandidate != nil {
		count++
	}
	if o.AddContribution != nil {
		count++
	}
	if o.NoChainEffect != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("%w: exactly one outcome payload is required", ErrInvalidResolutionEffect)
	}
	switch o.Kind {
	case ResolutionEffectAttachObservation:
		v := o.AttachObservation
		if v == nil {
			return ErrResolutionOutcomeMismatch
		}
		if _, err := chains.NewChainID(string(v.ChainID)); err != nil || v.PreviousRevision == 0 || v.NewRevision != v.PreviousRevision+1 || strings.TrimSpace(v.ObservationID) == "" {
			return ErrResolutionOutcomeMismatch
		}
	case ResolutionEffectCreateCandidate:
		v := o.CreateCandidate
		if v == nil {
			return ErrResolutionOutcomeMismatch
		}
		if _, err := chains.NewChainID(string(v.ChainID)); err != nil || v.NewRevision != 2 || v.Status != chains.StatusCandidate {
			return ErrResolutionOutcomeMismatch
		}
	case ResolutionEffectAddContribution:
		v := o.AddContribution
		if v == nil {
			return ErrResolutionOutcomeMismatch
		}
		if _, err := chains.NewChainID(string(v.ChainID)); err != nil || v.PreviousRevision == 0 || v.NewRevision != v.PreviousRevision+1 || strings.TrimSpace(v.ContributionID) == "" || !validConfidenceValue(v.PreviousConfidence) || !validConfidenceValue(v.NewConfidence) {
			return ErrResolutionOutcomeMismatch
		}
	case ResolutionEffectNoChain:
		if o.NoChainEffect == nil {
			return ErrResolutionOutcomeMismatch
		}
		if o.NoChainEffect == nil || validResolutionText(o.NoChainEffect.ReasonCode, "reason code", 64, true) != nil {
			return ErrResolutionOutcomeMismatch
		}
	}
	return nil
}

func (o ResolutionOutcome) validateFor(effect ResolutionEffect) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if o.Kind != effect.Kind {
		return ErrResolutionOutcomeMismatch
	}
	switch effect.Kind {
	case ResolutionEffectAttachObservation:
		v, e := o.AttachObservation, effect.AttachObservation
		if e == nil || v.ChainID != e.ChainID || v.PreviousRevision != e.SourceRevision || v.NewRevision != e.SourceRevision+1 || v.ObservationID != e.Observation.ID {
			return ErrResolutionOutcomeMismatch
		}
	case ResolutionEffectCreateCandidate:
		v, e := o.CreateCandidate, effect.CreateCandidate
		if e == nil || v.ChainID != e.ChainID || v.NewRevision != 2 || v.Status != e.InitialStatus {
			return ErrResolutionOutcomeMismatch
		}
	case ResolutionEffectAddContribution:
		v, e := o.AddContribution, effect.AddContribution
		if e == nil || v.ChainID != e.ChainID || v.PreviousRevision != e.SourceRevision || v.NewRevision != e.SourceRevision+1 || v.ContributionID != e.ContributionTemplate.ID {
			return ErrResolutionOutcomeMismatch
		}
	case ResolutionEffectNoChain:
		if effect.NoChainEffect == nil || o.NoChainEffect.ReasonCode != effect.NoChainEffect.ReasonCode {
			return ErrResolutionOutcomeMismatch
		}
	}
	return nil
}

// Fingerprint is the canonical, policy-independent identity of an effect.
func (e ResolutionEffect) Fingerprint() (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	count := 0
	if e.AttachObservation != nil {
		count++
	}
	if e.CreateCandidate != nil {
		count++
	}
	if e.AddContribution != nil {
		count++
	}
	if e.NoChainEffect != nil {
		count++
	}
	if count != 1 {
		return "", ErrInvalidResolutionEffect
	}
	canonical := resolutionEffectCanonical{Kind: e.Kind}
	switch e.Kind {
	case ResolutionEffectAttachObservation:
		if e.AttachObservation == nil {
			return "", ErrInvalidResolutionEffect
		}
		canonical.Attach = &attachEffectCanonical{ChainID: e.AttachObservation.ChainID, SourceRevision: e.AttachObservation.SourceRevision, Observation: e.AttachObservation.Observation}
	case ResolutionEffectCreateCandidate:
		if e.CreateCandidate == nil {
			return "", ErrInvalidResolutionEffect
		}
		canonical.Create = &createEffectCanonical{ChainID: e.CreateCandidate.ChainID, Observation: e.CreateCandidate.Observation, InitialStatus: e.CreateCandidate.InitialStatus, InitialConfidence: e.CreateCandidate.InitialConfidence}
	case ResolutionEffectAddContribution:
		if e.AddContribution == nil {
			return "", ErrInvalidResolutionEffect
		}
		t := e.AddContribution.ContributionTemplate
		canonical.Contribution = &contributionEffectCanonical{ChainID: e.AddContribution.ChainID, SourceRevision: e.AddContribution.SourceRevision, ID: t.ID, Source: t.Source, Kind: t.Kind, Value: t.Value, ObservationIDs: append([]string(nil), t.ObservationIDs...), ReasonCode: t.ReasonCode}
	case ResolutionEffectNoChain:
		if e.NoChainEffect == nil {
			return "", ErrInvalidResolutionEffect
		}
		canonical.NoChain = &noChainEffectCanonical{ReasonCode: e.NoChainEffect.ReasonCode}
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal resolution effect: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

type resolutionEffectCanonical struct {
	Kind         ResolutionEffectKind         `json:"kind"`
	Attach       *attachEffectCanonical       `json:"attach_observation,omitempty"`
	Create       *createEffectCanonical       `json:"create_candidate,omitempty"`
	Contribution *contributionEffectCanonical `json:"add_contribution,omitempty"`
	NoChain      *noChainEffectCanonical      `json:"no_chain_effect,omitempty"`
}
type attachEffectCanonical struct {
	ChainID        chains.ChainID        `json:"chain_id"`
	SourceRevision uint64                `json:"source_revision"`
	Observation    chains.ObservationRef `json:"observation"`
}
type createEffectCanonical struct {
	ChainID           chains.ChainID        `json:"chain_id"`
	Observation       chains.ObservationRef `json:"observation"`
	InitialStatus     chains.Status         `json:"initial_status"`
	InitialConfidence float64               `json:"initial_confidence"`
}
type contributionEffectCanonical struct {
	ChainID        chains.ChainID          `json:"chain_id"`
	SourceRevision uint64                  `json:"source_revision"`
	ID             string                  `json:"id"`
	Source         string                  `json:"source"`
	Kind           chains.ContributionKind `json:"kind"`
	Value          float64                 `json:"value"`
	ObservationIDs []string                `json:"observation_ids"`
	ReasonCode     string                  `json:"reason_code"`
}
type noChainEffectCanonical struct {
	ReasonCode string `json:"reason_code"`
}

type ResolutionSnapshot struct {
	AssessmentVersion     uint64
	AssessmentID          string
	AssessmentFingerprint string
	AlternativeID         string
	AlternativeKind       AlternativeKind
	EffectKind            ResolutionEffectKind
	EffectFingerprint     string
	Outcome               ResolutionOutcome
	ResolvedAt            time.Time
	Actor                 string
	Reason                string
	CorrelationID         string
}

// Validate checks the immutable effect envelope without requiring an owning
// hypothesis. Family/subject/alternative coherence is checked at resolution
// time against the current assessment.
func (e ResolutionEffect) Validate() error {
	if err := e.Kind.Validate(); err != nil {
		return err
	}
	count := 0
	if e.AttachObservation != nil {
		count++
	}
	if e.CreateCandidate != nil {
		count++
	}
	if e.AddContribution != nil {
		count++
	}
	if e.NoChainEffect != nil {
		count++
	}
	if count != 1 {
		return ErrInvalidResolutionEffect
	}
	switch e.Kind {
	case ResolutionEffectAttachObservation:
		if e.AttachObservation == nil {
			return ErrInvalidResolutionEffect
		}
		if e.AttachObservation.SourceRevision == 0 {
			return ErrInvalidResolutionEffect
		}
		if _, err := chains.NewChainID(string(e.AttachObservation.ChainID)); err != nil {
			return err
		}
		return e.AttachObservation.Observation.Validate()
	case ResolutionEffectCreateCandidate:
		if e.CreateCandidate == nil {
			return ErrInvalidResolutionEffect
		}
		if _, err := chains.NewChainID(string(e.CreateCandidate.ChainID)); err != nil {
			return err
		}
		if e.CreateCandidate.InitialStatus != chains.StatusCandidate || !validConfidenceValue(e.CreateCandidate.InitialConfidence) || e.CreateCandidate.InitialConfidence != 0 {
			return ErrInvalidResolutionEffect
		}
		return e.CreateCandidate.Observation.Validate()
	case ResolutionEffectAddContribution:
		if e.AddContribution == nil {
			return ErrInvalidResolutionEffect
		}
		if e.AddContribution.SourceRevision == 0 {
			return ErrInvalidResolutionEffect
		}
		if _, err := chains.NewChainID(string(e.AddContribution.ChainID)); err != nil {
			return err
		}
		t := e.AddContribution.ContributionTemplate
		if err := validResolutionText(t.ID, "contribution id", 256, true); err != nil {
			return err
		}
		if err := validResolutionText(t.Source, "contribution source", 128, true); err != nil {
			return err
		}
		if err := t.Kind.Validate(); err != nil {
			return err
		}
		if !validConfidenceValue(t.Value) || (t.Kind == chains.ContributionNeutral && t.Value != 0) {
			return ErrInvalidResolutionEffect
		}
		if err := validResolutionText(t.ReasonCode, "contribution reason code", 64, true); err != nil {
			return err
		}
		if len(t.ObservationIDs) == 0 {
			return ErrInvalidResolutionEffect
		}
		for _, id := range t.ObservationIDs {
			if err := validResolutionText(id, "observation id", 256, true); err != nil {
				return err
			}
		}
		return nil
	case ResolutionEffectNoChain:
		if e.NoChainEffect == nil {
			return ErrInvalidResolutionEffect
		}
		return validResolutionText(e.NoChainEffect.ReasonCode, "no-chain reason", 64, true)
	}
	return ErrInvalidResolutionEffect
}

func (s *ResolutionSnapshot) Clone() *ResolutionSnapshot {
	if s == nil {
		return nil
	}
	c := *s
	c.Outcome = s.Outcome.Clone()
	return &c
}

func (k ResolutionEffectKind) Validate() error {
	switch k {
	case ResolutionEffectAttachObservation, ResolutionEffectCreateCandidate,
		ResolutionEffectAddContribution, ResolutionEffectNoChain:
		return nil
	default:
		return fmt.Errorf("%w: unknown effect kind %q", ErrInvalidResolutionEffect, k)
	}
}

func (e ResolutionEffect) Clone() ResolutionEffect {
	if e.AttachObservation != nil {
		copy := *e.AttachObservation
		copy.Observation = copy.Observation.Clone()
		e.AttachObservation = &copy
	}
	if e.CreateCandidate != nil {
		copy := *e.CreateCandidate
		copy.Observation = copy.Observation.Clone()
		e.CreateCandidate = &copy
	}
	if e.AddContribution != nil {
		copy := *e.AddContribution
		copy.ContributionTemplate.ObservationIDs = append([]string(nil), e.AddContribution.ContributionTemplate.ObservationIDs...)
		e.AddContribution = &copy
	}
	if e.NoChainEffect != nil {
		copy := *e.NoChainEffect
		e.NoChainEffect = &copy
	}
	return e
}

func (e *ResolutionEffect) ValidateFor(family Family, subject Subject, alternative Alternative) error {
	if e == nil {
		return ErrInvalidResolutionEffect
	}
	if err := e.Kind.Validate(); err != nil {
		return err
	}
	count := 0
	if e.AttachObservation != nil {
		count++
	}
	if e.CreateCandidate != nil {
		count++
	}
	if e.AddContribution != nil {
		count++
	}
	if e.NoChainEffect != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("%w: exactly one effect payload is required", ErrInvalidResolutionEffect)
	}
	switch e.Kind {
	case ResolutionEffectAttachObservation:
		if family != FamilyAssociation || alternative.Kind != AlternativeAttachExisting || e.AttachObservation == nil || e.CreateCandidate != nil || e.AddContribution != nil || e.NoChainEffect != nil {
			return fmt.Errorf("%w: attach effect does not match alternative", ErrResolutionEffectMismatch)
		}
		if _, err := chains.NewChainID(string(e.AttachObservation.ChainID)); err != nil || e.AttachObservation.SourceRevision == 0 || e.AttachObservation.ChainID != alternative.ChainID || e.AttachObservation.SourceRevision != alternative.SourceRevision || e.AttachObservation.Observation.ID != subject.ObservationID {
			return fmt.Errorf("%w: attach effect identity is inconsistent", ErrResolutionEffectMismatch)
		}
		if err := e.AttachObservation.Observation.Validate(); err != nil {
			return fmt.Errorf("%w: attach observation: %v", ErrInvalidResolutionEffect, err)
		}
	case ResolutionEffectCreateCandidate:
		if family != FamilyAssociation || alternative.Kind != AlternativeCreateCandidate || e.CreateCandidate == nil || e.AttachObservation != nil || e.AddContribution != nil || e.NoChainEffect != nil {
			return fmt.Errorf("%w: candidate effect does not match alternative", ErrResolutionEffectMismatch)
		}
		if _, err := chains.NewChainID(string(e.CreateCandidate.ChainID)); err != nil || e.CreateCandidate.ChainID != alternative.ChainID || e.CreateCandidate.Observation.ID != subject.ObservationID || e.CreateCandidate.InitialStatus != chains.StatusCandidate || !validConfidenceValue(e.CreateCandidate.InitialConfidence) || e.CreateCandidate.InitialConfidence != 0 {
			return fmt.Errorf("%w: candidate effect is inconsistent with the current chain constructor", ErrResolutionEffectMismatch)
		}
		if err := e.CreateCandidate.Observation.Validate(); err != nil {
			return fmt.Errorf("%w: candidate observation: %v", ErrInvalidResolutionEffect, err)
		}
	case ResolutionEffectAddContribution:
		if family != FamilyEvidence || (alternative.Kind != AlternativeSupport && alternative.Kind != AlternativeContradiction && alternative.Kind != AlternativeNeutral) || e.AddContribution == nil || e.AttachObservation != nil || e.CreateCandidate != nil || e.NoChainEffect != nil {
			return fmt.Errorf("%w: contribution effect does not match alternative", ErrResolutionEffectMismatch)
		}
		if _, err := chains.NewChainID(string(e.AddContribution.ChainID)); err != nil || e.AddContribution.ChainID != subject.ChainID || e.AddContribution.SourceRevision == 0 || e.AddContribution.SourceRevision != alternative.SourceRevision {
			return fmt.Errorf("%w: contribution effect identity is inconsistent", ErrResolutionEffectMismatch)
		}
		if err := e.AddContribution.ContributionTemplate.validate(subject.ObservationID, alternative, subject); err != nil {
			return err
		}
	case ResolutionEffectNoChain:
		if family != FamilyEvidence || alternative.Kind != AlternativeInsufficient || e.NoChainEffect == nil || e.AttachObservation != nil || e.CreateCandidate != nil || e.AddContribution != nil {
			return fmt.Errorf("%w: no-chain effect does not match alternative", ErrResolutionEffectMismatch)
		}
		if err := validResolutionText(e.NoChainEffect.ReasonCode, "no-chain reason", 64, true); err != nil {
			return err
		}
	}
	return nil
}

func (t ContributionTemplate) validate(targetID string, alternative Alternative, subject Subject) error {
	if err := validResolutionText(t.ID, "contribution id", 256, true); err != nil {
		return err
	}
	if err := validResolutionText(t.Source, "contribution source", 128, true); err != nil {
		return err
	}
	if err := t.Kind.Validate(); err != nil {
		return fmt.Errorf("%w: contribution kind: %v", ErrInvalidResolutionEffect, err)
	}
	if !validConfidenceValue(t.Value) || (t.Kind == chains.ContributionNeutral && t.Value != 0) {
		return fmt.Errorf("%w: contribution value is invalid", ErrResolutionContributionMismatch)
	}
	if alternative.Kind == AlternativeSupport && t.Kind != chains.ContributionSupport || alternative.Kind == AlternativeContradiction && t.Kind != chains.ContributionContradiction || alternative.Kind == AlternativeNeutral && t.Kind != chains.ContributionNeutral {
		return fmt.Errorf("%w: contribution kind does not match alternative", ErrResolutionContributionMismatch)
	}
	if alternative.ContributionID != t.ID {
		return fmt.Errorf("%w: contribution id does not match alternative", ErrResolutionContributionMismatch)
	}
	expectedID := "cge-evidence-" + strings.TrimPrefix(subject.EvidenceFingerprint, "sha256:")
	if t.ID != expectedID {
		return fmt.Errorf("%w: contribution id is not deterministic", ErrResolutionContributionMismatch)
	}
	if !strings.HasPrefix(t.Source, "cge-evidence/") {
		return fmt.Errorf("%w: contribution source is not an evidence source", ErrResolutionContributionMismatch)
	}
	if err := validResolutionText(t.ReasonCode, "contribution reason code", 64, true); err != nil {
		return err
	}
	if alternative.EvidenceFingerprint != subject.EvidenceFingerprint {
		return fmt.Errorf("%w: evidence fingerprint does not match subject", ErrResolutionContributionMismatch)
	}
	if len(t.ObservationIDs) == 0 {
		return fmt.Errorf("%w: contribution observations are empty", ErrResolutionContributionMismatch)
	}
	seen := make(map[string]struct{}, len(t.ObservationIDs))
	for i, id := range t.ObservationIDs {
		if err := validResolutionText(id, "contribution observation id", 256, true); err != nil {
			return err
		}
		if _, ok := seen[id]; ok || (i > 0 && t.ObservationIDs[i-1] >= id) {
			return fmt.Errorf("%w: contribution observations are not unique and sorted", ErrResolutionContributionMismatch)
		}
		seen[id] = struct{}{}
	}
	if _, ok := seen[targetID]; !ok {
		return fmt.Errorf("%w: target observation is not referenced", ErrResolutionObservationMismatch)
	}
	return nil
}

func validConfidenceValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validResolutionText(value, name string, limit int, required bool) error {
	if required && strings.TrimSpace(value) == "" || len([]rune(value)) > limit || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%w: %s is invalid", ErrInvalidResolutionEffect, name)
	}
	return nil
}

func (e AttachObservationEffect) Command(mutation chains.MutationContext) (chains.AddObservationCommand, error) {
	if err := mutation.Validate(); err != nil {
		return chains.AddObservationCommand{}, fmt.Errorf("%w: %v", ErrInvalidContext, err)
	}
	command := chains.AddObservationCommand{ChainID: e.ChainID, SourceRevision: e.SourceRevision, Observation: e.Observation, Mutation: mutation}
	if len(command.Mutation.ObservationIDs) == 0 {
		command.Mutation.ObservationIDs = []string{e.Observation.ID}
	}
	if err := command.Validate(); err != nil {
		return chains.AddObservationCommand{}, err
	}
	return command.Clone(), nil
}

func (e AddContributionEffect) Command(mutation chains.MutationContext) (chains.AddContributionCommand, error) {
	if err := mutation.Validate(); err != nil {
		return chains.AddContributionCommand{}, fmt.Errorf("%w: %v", ErrInvalidContext, err)
	}
	template := e.ContributionTemplate
	contribution := chains.ConfidenceContribution{ID: template.ID, Source: template.Source, Kind: template.Kind, Value: template.Value, ObservationIDs: append([]string(nil), template.ObservationIDs...), Reason: template.ReasonCode, CreatedAt: mutation.At}
	command := chains.AddContributionCommand{ChainID: e.ChainID, SourceRevision: e.SourceRevision, Contribution: contribution, Mutation: mutation}
	if err := command.Validate(); err != nil {
		return chains.AddContributionCommand{}, err
	}
	return command.Clone(), nil
}

func (e CreateCandidateEffect) BuildChain(mutation chains.MutationContext) (*chains.Chain, error) {
	if err := mutation.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidContext, err)
	}
	if e.InitialStatus != chains.StatusCandidate || e.InitialConfidence != 0 {
		return nil, fmt.Errorf("%w: candidate profile is not supported by the current constructor", ErrResolutionEffectMismatch)
	}
	creationAt := mutation.At
	if e.Observation.Timestamp.Before(creationAt) {
		creationAt = e.Observation.Timestamp
	}
	creationMutation := mutation
	creationMutation.At = creationAt
	chain, err := chains.New(e.ChainID, creationMutation)
	if err != nil {
		return nil, err
	}
	if err := chain.AddObservation(e.Observation, mutation); err != nil {
		return nil, err
	}
	return chain, nil
}

func cloneResolutionEffect(effect *ResolutionEffect) *ResolutionEffect {
	if effect == nil {
		return nil
	}
	clone := effect.Clone()
	return &clone
}

func cloneResolutionEffectList(alternatives []Alternative) {
	for i := range alternatives {
		alternatives[i].ResolutionEffect = cloneResolutionEffect(alternatives[i].ResolutionEffect)
	}
}

func sortObservationIDs(ids []string) []string {
	result := append([]string(nil), ids...)
	sort.Strings(result)
	return result
}
