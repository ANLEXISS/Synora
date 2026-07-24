package cognitiverecommendation

import "synora/internal/cge/cognitivesituation"

type RecommendationKind string

const (
	RecommendationContinueObservation    RecommendationKind = "continue_observation"
	RecommendationMaintainInterpretation RecommendationKind = "maintain_current_interpretation"
	RecommendationAdditionalEvidence     RecommendationKind = "request_additional_evidence"
	RecommendationReassessContext        RecommendationKind = "reassess_after_context_change"
	RecommendationReassessObservation    RecommendationKind = "reassess_after_new_observation"
	RecommendationPreserveAmbiguity      RecommendationKind = "preserve_ambiguity"
	RecommendationCognitiveTransition    RecommendationKind = "flag_cognitive_transition"
	RecommendationNone                   RecommendationKind = "no_recommendation"
)

type RecommendationTargetKind string

const (
	TargetSituation         RecommendationTargetKind = "situation"
	TargetHypothesis        RecommendationTargetKind = "hypothesis"
	TargetEvidenceRequest   RecommendationTargetKind = "evidence_request"
	TargetContext           RecommendationTargetKind = "context"
	TargetFutureObservation RecommendationTargetKind = "future_observation"
)

type RecommendationTarget struct {
	Kind RecommendationTargetKind

	SituationID       string
	HypothesisID      string
	AdvisoryRequestID string

	ReferenceCode string

	Fingerprint string
}

type RecommendationStatus string

const (
	RecommendationCandidate               RecommendationStatus = "candidate"
	RecommendationApplicable              RecommendationStatus = "applicable"
	RecommendationBlocked                 RecommendationStatus = "blocked"
	RecommendationInsufficientInformation RecommendationStatus = "insufficient_information"
	RecommendationSuperseded              RecommendationStatus = "superseded"
	RecommendationWithdrawn               RecommendationStatus = "withdrawn"
	RecommendationInvalidated             RecommendationStatus = "invalidated"
)

type RecommendationMarkers struct {
	NotADecision                      bool
	NotAProbability                   bool
	NotAuthorization                  bool
	NotACommand                       bool
	NotAnAction                       bool
	NotAnAlert                        bool
	NoSecurityMeaning                 bool
	RequiresSeparateDecisionAuthority bool
}

type CognitiveRecommendation struct {
	ID string

	SituationID string
	EpisodeID   string

	Kind   RecommendationKind
	Target RecommendationTarget
	Status RecommendationStatus

	Rank int

	ApplicabilityPermille    int
	InformationValuePermille int
	StabilityPermille        int
	UrgencyPermille          int

	SupportingReasonCodes []string
	BlockingReasonCodes   []string

	SourceSituationFingerprint string
	SourceSituationRevision    uint64

	PreviousRecommendationID string

	Fingerprint string
	Markers     RecommendationMarkers
}

type RecommendationSetMarkers struct {
	NotADecision      bool
	NotAuthorization  bool
	NotACommand       bool
	NotAnAction       bool
	NoSecurityMeaning bool
}

type CognitiveRecommendationSet struct {
	ID string

	SituationID string
	EpisodeID   string

	SourceSituationFingerprint string
	SourceSituationRevision    uint64

	Recommendations []CognitiveRecommendation

	PrimaryRecommendationID string
	PrimaryMarginPermille   int

	Ambiguous                    bool
	HasApplicableRecommendation  bool
	HasObservationRecommendation bool
	HasCognitiveTransition       bool

	Revision    uint64
	Fingerprint string

	Markers RecommendationSetMarkers
}

type PlanInput struct {
	Situation cognitivesituation.CognitiveSituation

	SituationDiff *cognitivesituation.CognitiveSituationDiff

	Previous *CognitiveRecommendationSet
}

type CognitiveRecommendationDiff struct {
	EpisodeID string

	PreviousSetFingerprint string
	CurrentSetFingerprint  string

	PrimaryChanged    bool
	PreviousPrimaryID string
	CurrentPrimaryID  string

	AddedRecommendationIDs         []string
	RemovedRecommendationIDs       []string
	StatusChangedRecommendationIDs []string

	AmbiguityChanged     bool
	ApplicabilityChanged bool

	ReasonCodes []string

	Fingerprint string
}

type CognitiveRecommendationExplanation struct {
	RecommendationID string
	SituationID      string
	EpisodeID        string

	Kind   RecommendationKind
	Status RecommendationStatus

	SummaryCode string

	Target RecommendationTarget

	ApplicabilityPermille    int
	InformationValuePermille int
	StabilityPermille        int
	ReviewPriorityPermille   int

	SupportingReasonCodes []string
	BlockingReasonCodes   []string

	NotADecision                      bool
	NotAProbability                   bool
	NotAuthorization                  bool
	NotACommand                       bool
	NotAnAction                       bool
	NotAnAlert                        bool
	NoSecurityMeaning                 bool
	RequiresSeparateDecisionAuthority bool
}

type CognitiveRecommendationSnapshot struct {
	WorkflowRevision        uint64
	SituationSnapshotDigest string
	RecommendationSets      []CognitiveRecommendationSet
	EpisodeIndex            map[string]int
	Digest                  string
}

func (r CognitiveRecommendation) Clone() CognitiveRecommendation {
	out := r
	out.Target = r.Target
	out.SupportingReasonCodes = append([]string(nil), r.SupportingReasonCodes...)
	out.BlockingReasonCodes = append([]string(nil), r.BlockingReasonCodes...)
	out.Markers = r.Markers
	return out
}

func (s CognitiveRecommendationSet) Clone() CognitiveRecommendationSet {
	out := s
	out.Recommendations = make([]CognitiveRecommendation, len(s.Recommendations))
	for i, value := range s.Recommendations {
		out.Recommendations[i] = value.Clone()
	}
	out.Markers = s.Markers
	return out
}

func (s CognitiveRecommendationSnapshot) Clone() CognitiveRecommendationSnapshot {
	out := s
	out.RecommendationSets = make([]CognitiveRecommendationSet, len(s.RecommendationSets))
	for i, value := range s.RecommendationSets {
		out.RecommendationSets[i] = value.Clone()
	}
	out.EpisodeIndex = make(map[string]int, len(s.EpisodeIndex))
	for key, value := range s.EpisodeIndex {
		out.EpisodeIndex[key] = value
	}
	return out
}
