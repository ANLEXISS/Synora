package cognitivesituation

import "synora/internal/cge/durableworkflow"

type ExpectedPipelineDepth string

const (
	DepthEpisode                ExpectedPipelineDepth = "episode"
	DepthSituationFacts         ExpectedPipelineDepth = "situation_facts"
	DepthSituationHypotheses    ExpectedPipelineDepth = "situation_hypotheses"
	DepthEvidenceDiscrimination ExpectedPipelineDepth = "evidence_discrimination"
	DepthAdvisoryRequests       ExpectedPipelineDepth = "advisory_requests"
	DepthCapabilityMapping      ExpectedPipelineDepth = "capability_mapping"
	DepthAuthorizationBoundary  ExpectedPipelineDepth = "authorization_boundary"
)

type CognitivePhase string

const (
	PhaseObserving                CognitivePhase = "observing"
	PhaseBuilding                 CognitivePhase = "building"
	PhaseCoherent                 CognitivePhase = "coherent"
	PhaseAmbiguous                CognitivePhase = "ambiguous"
	PhaseIncomplete               CognitivePhase = "incomplete"
	PhaseAwaitingEvidence         CognitivePhase = "awaiting_evidence"
	PhaseCapabilityUnavailable    CognitivePhase = "capability_unavailable"
	PhaseAuthorizationConstrained CognitivePhase = "authorization_constrained"
	PhaseStale                    CognitivePhase = "stale"
	PhaseInvalidated              CognitivePhase = "invalidated"
)

type BuildInput struct {
	Workflow      durableworkflow.WorkflowState
	EpisodeID     string
	ExpectedDepth ExpectedPipelineDepth
	Previous      *CognitiveSituation
}

type CognitiveSituation struct {
	ID        string
	EpisodeID string

	Phase         CognitivePhase
	ExpectedDepth ExpectedPipelineDepth

	WorkflowRevision uint64
	WorkflowSequence uint64
	WorkflowDigest   string

	SourceFingerprints      SourceFingerprints
	Knowledge               KnowledgeSummary
	Hypotheses              HypothesisSummary
	Evidence                EvidenceSummary
	Advisory                AdvisorySummary
	Capability              CapabilitySummary
	Authorization           AuthorizationSummary
	RecommendationReadiness RecommendationReadiness

	PreviousSituationID string
	PreviousFingerprint string

	Revision    uint64
	Fingerprint string
	Markers     CognitiveSituationMarkers
}

type CognitiveSituationMarkers struct {
	NotADecision              bool
	NotAProbability           bool
	NotAuthorization          bool
	NotACommand               bool
	NotAnAction               bool
	NoSecurityMeaning         bool
	DerivedFromCommittedState bool
}

type SourceFingerprints struct {
	Episode                  string
	Facts                    string
	Hypotheses               string
	Discrimination           string
	AdvisoryRequests         []string
	CapabilityMappings       []string
	AuthorizationAssessments []string
}

type KnowledgeSummary struct {
	ExpectedLayers          int
	FreshLayers             int
	StaleLayers             int
	AbsentExpectedLayers    int
	InvalidatedLayers       int
	OverallCoveragePermille int
	LayerStates             []LayerKnowledgeState
	PartialContext          bool
	ConflictingFacts        bool
	UnknownFacts            int
	AssertedFacts           int
	ConflictingFactCount    int
}

type LayerKnowledgeState struct {
	Layer             durableworkflow.LayerKind
	Expected          bool
	Freshness         durableworkflow.LayerFreshness
	Present           bool
	SourceFingerprint string
	ReasonCodes       []string
}

type HypothesisSummary struct {
	Available                   bool
	CandidateCount              int
	SupportedCount              int
	WeakenedCount               int
	ContradictedCount           int
	InsufficientCount           int
	Ambiguous                   bool
	LeadingHypothesisKind       string
	LeadingHypothesisID         string
	LeadingPlausibilityPermille int
	LeadingCoveragePermille     int
	LeadingMarginPermille       int
	Alternatives                []HypothesisAlternative
}

type HypothesisAlternative struct {
	ID                   string
	Kind                 string
	Status               string
	PlausibilityPermille int
	CoveragePermille     int
	Rank                 int
}

type EvidenceSummary struct {
	Available                    bool
	CandidateCount               int
	DiscriminatingCandidateCount int
	RedundantCandidateCount      int
	BestCandidateID              string
	BestCandidateKind            string
	BestUtilityPermille          int
	BestDiscriminationPermille   int
	BestCoverageGainPermille     int
	Ambiguous                    bool
	MissingRequirementCodes      []string
}

type AdvisorySummary struct {
	Available                     bool
	Total                         int
	Active                        int
	Proposed                      int
	Acknowledged                  int
	Deferred                      int
	Suppressed                    int
	Cancelled                     int
	Satisfied                     int
	Expired                       int
	Invalidated                   int
	PreferredRequestID            string
	PreferredCandidateKind        string
	ExternalMappingRequired       bool
	ExternalAuthorizationRequired bool
}

type CapabilitySummary struct {
	Configured              bool
	Available               bool
	Ambiguous               bool
	Unavailable             bool
	AssessmentCount         int
	CompatibleCount         int
	DegradedCount           int
	IncompatibleCount       int
	PreferredMappingID      string
	PreferredCapabilityKind string
}

type AuthorizationSummary struct {
	Configured                   bool
	AssessmentCount              int
	EligibleCandidateCount       int
	DeniedCandidateCount         int
	ConfirmationRequiredCount    int
	DeferredCount                int
	DefaultDeniedCount           int
	AuthorizationEligible        bool
	AuthorizationAmbiguous       bool
	ExternalConfirmationRequired bool
	DeniedByDefault              bool
	PreferredEligibleCandidateID string
}

type RecommendationReadinessStatus string

const (
	ReadinessNotReady                       RecommendationReadinessStatus = "not_ready"
	ReadinessObservationRecommendation      RecommendationReadinessStatus = "ready_for_observation_recommendation"
	ReadinessCognitiveRecommendation        RecommendationReadinessStatus = "ready_for_cognitive_recommendation"
	ReadinessBlockedStaleness               RecommendationReadinessStatus = "blocked_by_staleness"
	ReadinessBlockedInvalidState            RecommendationReadinessStatus = "blocked_by_invalid_state"
	ReadinessBlockedInsufficientInformation RecommendationReadinessStatus = "blocked_by_insufficient_information"
	ReadinessBlockedAmbiguity               RecommendationReadinessStatus = "blocked_by_ambiguity"
	ReadinessBlockedMissingCapability       RecommendationReadinessStatus = "blocked_by_missing_capability"
	ReadinessBlockedAuthorization           RecommendationReadinessStatus = "blocked_by_authorization_constraint"
)

type RecommendationReadiness struct {
	Status                RecommendationReadinessStatus
	Ready                 bool
	BlockingReasonCodes   []string
	SupportingReasonCodes []string
	RequiredFreshLayers   []durableworkflow.LayerKind
	MissingFreshLayers    []durableworkflow.LayerKind
	Fingerprint           string
}

type CognitiveSituationDiff struct {
	EpisodeID                string
	PreviousFingerprint      string
	CurrentFingerprint       string
	PhaseChanged             bool
	PreviousPhase            CognitivePhase
	CurrentPhase             CognitivePhase
	LeadingHypothesisChanged bool
	KnowledgeCoverageChanged bool
	AdvisoryChanged          bool
	CapabilityChanged        bool
	AuthorizationChanged     bool
	ReadinessChanged         bool
	ReasonCodes              []string
	Fingerprint              string
}

type CognitiveSituationExplanation struct {
	SituationID                string
	EpisodeID                  string
	Phase                      CognitivePhase
	SummaryCode                string
	ReasonCodes                []string
	LayerStates                []LayerKnowledgeState
	LeadingHypothesisKind      string
	AlternativeHypothesisKinds []string
	MissingInformationCodes    []string
	ActiveAdvisoryCount        int
	CapabilityAvailable        bool
	AuthorizationEligible      bool
	RecommendationReadiness    RecommendationReadinessStatus
	NotADecision               bool
	NotAProbability            bool
	NotAuthorization           bool
	NotACommand                bool
	NotAnAction                bool
	NoSecurityMeaning          bool
}

type CognitiveSituationSnapshot struct {
	WorkflowRevision uint64
	Situations       []CognitiveSituation
	EpisodeIndex     map[string]int
	Digest           string
}

func (s CognitiveSituationSnapshot) Clone() CognitiveSituationSnapshot {
	out := s
	out.Situations = make([]CognitiveSituation, len(s.Situations))
	for i := range s.Situations {
		out.Situations[i] = cloneSituation(s.Situations[i])
	}
	out.EpisodeIndex = make(map[string]int, len(s.EpisodeIndex))
	for key, value := range s.EpisodeIndex {
		out.EpisodeIndex[key] = value
	}
	out.Digest = s.Digest
	return out
}

func cloneSituation(in CognitiveSituation) CognitiveSituation {
	out := in
	out.SourceFingerprints = cloneFingerprints(in.SourceFingerprints)
	out.Knowledge = cloneKnowledge(in.Knowledge)
	out.Hypotheses = in.Hypotheses
	out.Hypotheses.Alternatives = append([]HypothesisAlternative(nil), in.Hypotheses.Alternatives...)
	out.Evidence.MissingRequirementCodes = append([]string(nil), in.Evidence.MissingRequirementCodes...)
	out.Advisory = in.Advisory
	out.RecommendationReadiness = cloneReadiness(in.RecommendationReadiness)
	out.Markers = in.Markers
	return out
}

func (s CognitiveSituation) Clone() CognitiveSituation { return cloneSituation(s) }

func cloneFingerprints(in SourceFingerprints) SourceFingerprints {
	out := in
	out.AdvisoryRequests = append([]string(nil), in.AdvisoryRequests...)
	out.CapabilityMappings = append([]string(nil), in.CapabilityMappings...)
	out.AuthorizationAssessments = append([]string(nil), in.AuthorizationAssessments...)
	return out
}

func cloneKnowledge(in KnowledgeSummary) KnowledgeSummary {
	out := in
	out.LayerStates = make([]LayerKnowledgeState, len(in.LayerStates))
	for i, value := range in.LayerStates {
		out.LayerStates[i] = value
		out.LayerStates[i].ReasonCodes = append([]string(nil), value.ReasonCodes...)
	}
	return out
}

func cloneReadiness(in RecommendationReadiness) RecommendationReadiness {
	out := in
	out.BlockingReasonCodes = append([]string(nil), in.BlockingReasonCodes...)
	out.SupportingReasonCodes = append([]string(nil), in.SupportingReasonCodes...)
	out.RequiredFreshLayers = append([]durableworkflow.LayerKind(nil), in.RequiredFreshLayers...)
	out.MissingFreshLayers = append([]durableworkflow.LayerKind(nil), in.MissingFreshLayers...)
	return out
}
