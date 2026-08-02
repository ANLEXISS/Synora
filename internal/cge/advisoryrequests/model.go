package advisoryrequests

import (
	"sort"
	"time"

	"synora/internal/cge/evidencediscrimination"
)

type AdvisoryEvidenceRequest struct {
	ID                          AdvisoryRequestID
	Key                         AdvisoryRequestKey
	Generation                  uint64
	EpisodeID                   string
	Status                      AdvisoryRequestStatus
	CandidateID                 evidencediscrimination.EvidenceCandidateID
	Kind                        evidencediscrimination.EvidenceCandidateKind
	Dimension                   evidencediscrimination.EvidenceDimension
	RequiredFactCodes           []string
	HypothesisPairs             []AdvisoryHypothesisPair
	DiscriminationPermille      int
	CoverageGainPermille        int
	RedundancyPermille          int
	UtilityPermille             int
	CostClass                   string
	LatencyClass                string
	SensitivityClass            string
	AdvisoryRank                int
	ReasonCodes                 []string
	FirstProposedAt             time.Time
	LastEvaluatedAt             time.Time
	StatusChangedAt             time.Time
	ExpiresAt                   time.Time
	DeferredUntil               *time.Time
	SourceAssessmentFingerprint string
	SourceCandidateFingerprint  string
	Revision                    uint64
	Fingerprint                 string
	Flags                       AdvisoryRequestFlags
}

type AdvisoryDisposition struct {
	RequestID      AdvisoryRequestID
	Kind           AdvisoryDispositionKind
	Actor          string
	At             time.Time
	DeferUntil     *time.Time
	ReasonCode     string
	SourceRevision uint64
}

type AdvisoryPlan struct {
	EpisodeID                   string
	SourceAssessmentFingerprint string
	SourceRegistryRevision      uint64
	PolicyFingerprint           string
	Creates                     []AdvisoryEvidenceRequest
	Updates                     []AdvisoryRequestUpdate
	Transitions                 []AdvisoryRequestTransition
	ResultingRequests           []AdvisoryEvidenceRequest
	PreferredRequestID          AdvisoryRequestID
	PreferredMarginPermille     int
	ReasonCodes                 []string
	Fingerprint                 string
}

type AdvisoryRequestUpdate struct{ Before, After AdvisoryEvidenceRequest }

type AdvisoryRequestChange = AdvisoryRequestUpdate

type AdvisoryRequestTransition struct {
	RequestID  AdvisoryRequestID
	Before     AdvisoryRequestStatus
	After      AdvisoryRequestStatus
	ReasonCode string
}

type AdvisoryRequestDiff struct {
	EpisodeID         string
	Added             []AdvisoryEvidenceRequest
	Updated           []AdvisoryRequestUpdate
	Transitioned      []AdvisoryRequestTransition
	Removed           []AdvisoryRequestID
	BeforeFingerprint string
	AfterFingerprint  string
}

type AdvisoryExplanation struct {
	RequestID                     AdvisoryRequestID
	RequestKey                    AdvisoryRequestKey
	Status                        AdvisoryRequestStatus
	Kind                          string
	Dimension                     string
	SummaryCode                   string
	CandidateID                   string
	ReasonCodes                   []string
	RequiredFactCodes             []string
	HypothesisPairs               []AdvisoryHypothesisPair
	DiscriminationPermille        int
	CoverageGainPermille          int
	RedundancyPermille            int
	UtilityPermille               int
	CostClass                     string
	LatencyClass                  string
	SensitivityClass              string
	NotACommand                   bool
	NotAProbability               bool
	NoSecurityMeaning             bool
	RequiresExternalMapping       bool
	RequiresExternalAuthorization bool
}

func (r AdvisoryEvidenceRequest) Clone() AdvisoryEvidenceRequest {
	out := r
	out.RequiredFactCodes = append([]string(nil), r.RequiredFactCodes...)
	out.HypothesisPairs = append([]AdvisoryHypothesisPair(nil), r.HypothesisPairs...)
	out.ReasonCodes = append([]string(nil), r.ReasonCodes...)
	if r.DeferredUntil != nil {
		value := *r.DeferredUntil
		out.DeferredUntil = &value
	}
	return out
}

func cloneRequests(values []AdvisoryEvidenceRequest) []AdvisoryEvidenceRequest {
	out := make([]AdvisoryEvidenceRequest, len(values))
	for i, v := range values {
		out[i] = v.Clone()
	}
	return out
}
func sortRequests(values []AdvisoryEvidenceRequest) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].EpisodeID != values[j].EpisodeID {
			return values[i].EpisodeID < values[j].EpisodeID
		}
		if values[i].Key != values[j].Key {
			return values[i].Key < values[j].Key
		}
		if values[i].Generation != values[j].Generation {
			return values[i].Generation < values[j].Generation
		}
		return values[i].ID < values[j].ID
	})
}
