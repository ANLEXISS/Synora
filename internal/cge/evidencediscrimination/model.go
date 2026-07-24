package evidencediscrimination

import (
	"sort"

	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

type PotentialOutcome struct {
	ID                string
	FactCode          situationfacts.FactCode
	Operator          OutcomeOperator
	Value             *situationfacts.FactValue
	DescriptionCode   string
	Supports          []situationhypotheses.HypothesisKind
	Contradicts       []situationhypotheses.HypothesisKind
	ReducesMissingFor []situationhypotheses.HypothesisKind
	Fingerprint       string
}

type EvidenceCandidate struct {
	ID                             EvidenceCandidateID
	EpisodeID                      string
	Kind                           EvidenceCandidateKind
	Dimension                      EvidenceDimension
	RequiredFactCodes              []situationfacts.FactCode
	Outcomes                       []PotentialOutcome
	Discriminates                  []HypothesisPair
	SupportingHypothesisIDs        []situationhypotheses.HypothesisID
	WeakeningHypothesisIDs         []situationhypotheses.HypothesisID
	DiscriminationPermille         int
	CoverageGainPermille           int
	RedundancyPermille             int
	UtilityPermille                int
	CostClass                      EvidenceCostClass
	LatencyClass                   EvidenceLatencyClass
	SensitivityClass               EvidenceSensitivityClass
	ReasonCodes                    []string
	SourceFactSetFingerprint       string
	SourceHypothesisSetFingerprint string
	Fingerprint                    string
}

type DiscriminationScore struct {
	PairSeparationPermille  int
	CoverageGainPermille    int
	OutcomeContrastPermille int
	RedundancyPermille      int
	UtilityPermille         int
}

type AnalysisInput struct {
	FactSet            situationfacts.FactSet
	HypothesisSet      situationhypotheses.CompetingHypothesisSet
	HypothesisSchema   situationhypotheses.HypothesisSchema
	PreviousAssessment *DiscriminationAssessment
}

type DiscriminationAssessment struct {
	EpisodeID                      string
	SourceFactSetFingerprint       string
	SourceHypothesisSetFingerprint string
	CatalogFingerprint             string
	PolicyFingerprint              string
	Candidates                     []EvidenceCandidate
	BestCandidateID                EvidenceCandidateID
	AmbiguityRelevant              bool
	EvidenceUseful                 bool
	UnresolvedPairCount            int
	CoveredPairCount               int
	Revision                       uint64
	Fingerprint                    string
}

type EvidenceExplanation struct {
	CandidateID            EvidenceCandidateID
	Kind                   EvidenceCandidateKind
	Dimension              EvidenceDimension
	SummaryCode            string
	HypothesisPairs        []HypothesisPair
	RequiredFactCodes      []situationfacts.FactCode
	OutcomeExplanations    []OutcomeExplanation
	DiscriminationPermille int
	CoverageGainPermille   int
	RedundancyPermille     int
	UtilityPermille        int
	CostClass              EvidenceCostClass
	LatencyClass           EvidenceLatencyClass
	SensitivityClass       EvidenceSensitivityClass
	NotACommand            bool
	NotAProbability        bool
	NoSecurityMeaning      bool
}

type OutcomeExplanation struct {
	OutcomeID         string
	DescriptionCode   string
	FactCode          situationfacts.FactCode
	Supports          []situationhypotheses.HypothesisKind
	Contradicts       []situationhypotheses.HypothesisKind
	ReducesMissingFor []situationhypotheses.HypothesisKind
}

type EvidenceCandidateUpdate struct{ Before, After EvidenceCandidate }
type EvidenceCandidateRemoval struct {
	Candidate  EvidenceCandidate
	ReasonCode string
}

type EvidencePlan struct {
	EpisodeID                   string
	SourceAssessmentFingerprint string
	SourceRegistryRevision      uint64
	SourceAssessment            DiscriminationAssessment
	Creates                     []EvidenceCandidate
	Updates                     []EvidenceCandidateUpdate
	Removes                     []EvidenceCandidateRemoval
	ResultingAssessment         DiscriminationAssessment
	ReasonCodes                 []string
}

type DiscriminationAssessmentDiff struct {
	EpisodeID         string
	BeforeFingerprint string
	AfterFingerprint  string
	Added             []EvidenceCandidate
	Removed           []EvidenceCandidate
	Changed           []EvidenceCandidateUpdate
}

func (o PotentialOutcome) Clone() PotentialOutcome {
	out := o
	if o.Value != nil {
		v := o.Value.Clone()
		out.Value = &v
	}
	out.Supports = append([]situationhypotheses.HypothesisKind(nil), o.Supports...)
	out.Contradicts = append([]situationhypotheses.HypothesisKind(nil), o.Contradicts...)
	out.ReducesMissingFor = append([]situationhypotheses.HypothesisKind(nil), o.ReducesMissingFor...)
	return out
}
func (c EvidenceCandidate) Clone() EvidenceCandidate {
	out := c
	out.RequiredFactCodes = append([]situationfacts.FactCode(nil), c.RequiredFactCodes...)
	out.Outcomes = make([]PotentialOutcome, len(c.Outcomes))
	for i, o := range c.Outcomes {
		out.Outcomes[i] = o.Clone()
	}
	out.Discriminates = append([]HypothesisPair(nil), c.Discriminates...)
	out.SupportingHypothesisIDs = append([]situationhypotheses.HypothesisID(nil), c.SupportingHypothesisIDs...)
	out.WeakeningHypothesisIDs = append([]situationhypotheses.HypothesisID(nil), c.WeakeningHypothesisIDs...)
	out.ReasonCodes = append([]string(nil), c.ReasonCodes...)
	return out
}
func (a DiscriminationAssessment) Clone() DiscriminationAssessment {
	out := a
	out.Candidates = make([]EvidenceCandidate, len(a.Candidates))
	for i, c := range a.Candidates {
		out.Candidates[i] = c.Clone()
	}
	return out
}

func sortStrings[T ~string](values []T) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}
