package situationhypotheses

import (
	"sort"
	"strings"

	"synora/internal/cge/situationfacts"
)

type SituationHypothesis struct {
	ID HypothesisID

	EpisodeID string
	Kind      HypothesisKind
	Status    HypothesisStatus

	CreatedFromFactRevision uint64
	EvaluatedFactRevision   uint64

	Support       []Contribution
	Contradiction []Contribution
	Missing       []MissingRequirement

	SupportPermille       int
	ContradictionPermille int
	CoveragePermille      int
	PlausibilityPermille  int

	Revision    uint64
	Fingerprint string
}

type CompetingHypothesisSet struct {
	EpisodeID string

	FactSetFingerprint   string
	FactRegistryRevision uint64
	SchemaFingerprint    string
	PolicyFingerprint    string

	Hypotheses []SituationHypothesis

	LeadingHypothesisID   HypothesisID
	LeadingMarginPermille int

	Ambiguous               bool
	InsufficientInformation bool

	Revision    uint64
	Fingerprint string
}

type Contribution struct {
	ID string

	Role       ContributionRole
	RuleID     string
	ReasonCode string

	FactIDs []situationfacts.FactID

	WeightPermille int

	FactSetFingerprint string
}

type MissingRequirement struct {
	RuleID             string
	RequiredFactCode   situationfacts.FactCode
	ReasonCode         string
	ImportancePermille int
}

type EvaluationInput struct {
	FactSet     situationfacts.FactSet
	PreviousSet *CompetingHypothesisSet
}

type EvaluationResult struct {
	Set           CompetingHypothesisSet
	CompetingSet  CompetingHypothesisSet
	HypothesisSet CompetingHypothesisSet
	Mode          IncrementalEvaluationMode
	ReasonCodes   []string
}

type HypothesisEvaluation = SituationHypothesis

type HypothesisUpdate struct {
	Before SituationHypothesis
	After  SituationHypothesis
}

type HypothesisInvalidation struct {
	Before     SituationHypothesis
	After      SituationHypothesis
	ReasonCode string
}

type HypothesisPlan struct {
	EpisodeID string

	SourceFactSetFingerprint string
	SourceRegistryRevision   uint64
	SourceFactSet            situationfacts.FactSet

	Creates     []SituationHypothesis
	Updates     []HypothesisUpdate
	Invalidates []HypothesisInvalidation

	ResultingSet CompetingHypothesisSet
	ReasonCodes  []string
}

type PlanResult = HypothesisPlan

func (h SituationHypothesis) Clone() SituationHypothesis {
	out := h
	out.Support = cloneContributions(h.Support)
	out.Contradiction = cloneContributions(h.Contradiction)
	out.Missing = append([]MissingRequirement(nil), h.Missing...)
	return out
}

func (s CompetingHypothesisSet) Clone() CompetingHypothesisSet {
	out := s
	out.Hypotheses = make([]SituationHypothesis, len(s.Hypotheses))
	for i, hypothesis := range s.Hypotheses {
		out.Hypotheses[i] = hypothesis.Clone()
	}
	return out
}

func cloneContributions(values []Contribution) []Contribution {
	out := make([]Contribution, len(values))
	for i, value := range values {
		out[i] = value
		out[i].FactIDs = append([]situationfacts.FactID(nil), value.FactIDs...)
	}
	return out
}

func canonicalContributions(values []Contribution) []Contribution {
	out := cloneContributions(values)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func canonicalMissing(values []MissingRequirement) []MissingRequirement {
	out := append([]MissingRequirement(nil), values...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].RuleID != out[j].RuleID {
			return out[i].RuleID < out[j].RuleID
		}
		return out[i].RequiredFactCode < out[j].RequiredFactCode
	})
	return out
}

func validText(value string, allowEmpty bool) bool {
	return (allowEmpty || value != "") && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n")
}

func bounded(value int) bool { return value >= 0 && value <= 1000 }
