package durableworkflow

import (
	"sort"
	"sync"
	"time"

	"synora/internal/cge/advisoryrequests"
	"synora/internal/cge/authorizationboundary"
	"synora/internal/cge/capabilitymapping"
	"synora/internal/cge/episodes"
	"synora/internal/cge/evidencediscrimination"
	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

type EpisodeWorkflowState struct {
	Episode                  *episodes.EpisodeSnapshot
	Facts                    *situationfacts.FactSet
	Hypotheses               *situationhypotheses.CompetingHypothesisSet
	Discrimination           *evidencediscrimination.DiscriminationAssessment
	AdvisoryRequests         []advisoryrequests.AdvisoryEvidenceRequest
	CapabilityMappings       []capabilitymapping.CapabilityMappingAssessment
	AuthorizationAssessments []authorizationboundary.AuthorizationBoundaryAssessment

	EpisodeID   string
	Freshness   map[LayerKind]LayerFreshness
	Revision    uint64
	Fingerprint string
}

type WorkflowState struct {
	Revision          uint64
	LastSequence      uint64
	Episodes          []EpisodeWorkflowState
	SchemaFingerprint string
	PolicyFingerprint string
	Digest            string
}

type WorkflowMutation struct {
	EpisodeID string

	Episode        *episodes.EpisodeSnapshot
	Facts          *situationfacts.FactSet
	Hypotheses     *situationhypotheses.CompetingHypothesisSet
	Discrimination *evidencediscrimination.DiscriminationAssessment

	ReplaceAdvisoryRequests            []advisoryrequests.AdvisoryEvidenceRequest
	ReplaceAdvisoryRequestsSet         bool
	ReplaceCapabilityMappings          []capabilitymapping.CapabilityMappingAssessment
	ReplaceCapabilityMappingsSet       bool
	ReplaceAuthorizationAssessments    []authorizationboundary.AuthorizationBoundaryAssessment
	ReplaceAuthorizationAssessmentsSet bool

	ExplicitInvalidations []LayerKind
	ReasonCodes           []string

	SourceWorkflowRevision uint64
	SourceWorkflowDigest   string
}

type WorkflowTransactionID string

type WorkflowTransaction struct {
	ID                     WorkflowTransactionID
	Sequence               uint64
	CreatedAt              time.Time
	EpisodeID              string
	SourceWorkflowRevision uint64
	SourceWorkflowDigest   string
	Mutation               WorkflowMutation
	ResultWorkflowRevision uint64
	ResultWorkflowDigest   string
	SchemaFingerprint      string
	PolicyFingerprint      string
	Fingerprint            string
}

type Checkpoint struct {
	Sequence          uint64
	State             WorkflowState
	SchemaFingerprint string
	PolicyFingerprint string
	CreatedAt         time.Time
	Fingerprint       string
	Checksum          string
}

type RecordKind string

const (
	RecordGenesis          RecordKind = "genesis"
	RecordTransaction      RecordKind = "transaction"
	RecordCheckpointMarker RecordKind = "checkpoint_marker"
)

type Record struct {
	Version            uint16
	Sequence           uint64
	Kind               RecordKind
	PayloadLength      uint64
	Payload            []byte
	PayloadFingerprint string
	Checksum           string
}

type RecoveryInput struct {
	Records              []Record
	Checkpoint           *Checkpoint
	CheckpointError      error
	TruncatedFinalRecord bool
	Warnings             []string
}

type ReplayResult struct {
	State  WorkflowState
	Report RecoveryReport
}

type RecoveryReport struct {
	GenesisValidated             bool
	CheckpointFound              bool
	CheckpointUsed               bool
	CheckpointFallback           bool
	RecordsRead                  int
	TransactionsApplied          int
	DuplicateTransactionsIgnored int
	TruncatedFinalRecordIgnored  bool
	FinalSequence                uint64
	FinalRevision                uint64
	FinalDigest                  string
	Warnings                     []string
}

type CommitResult struct {
	Applied          bool
	Idempotent       bool
	WorkflowRevision uint64
	Sequence         uint64
	Digest           string
}

type CheckpointResult struct {
	Written     bool
	Sequence    uint64
	Fingerprint string
}

type Store interface {
	Append(record Record) error
	Load() (RecoveryInput, error)
	WriteCheckpoint(checkpoint Checkpoint) error
	Close() error
}

type SyncStore interface {
	Sync() error
}

type Coordinator struct {
	mu           sync.RWMutex
	store        Store
	policy       Policy
	state        WorkflowState
	walSequence  uint64
	closed       bool
	transactions map[WorkflowTransactionID]string
}

type Genesis struct {
	SchemaFingerprint string
	PolicyFingerprint string
	State             WorkflowState
	StateDigest       string
	CreatedAt         time.Time
}

type CheckpointMarker struct {
	Sequence              uint64
	CheckpointFingerprint string
}

func cloneEpisode(value *episodes.EpisodeSnapshot) *episodes.EpisodeSnapshot {
	if value == nil {
		return nil
	}
	copy := value.Clone()
	return &copy
}

func cloneFacts(value *situationfacts.FactSet) *situationfacts.FactSet {
	if value == nil {
		return nil
	}
	copy := value.Clone()
	return &copy
}

func cloneHypotheses(value *situationhypotheses.CompetingHypothesisSet) *situationhypotheses.CompetingHypothesisSet {
	if value == nil {
		return nil
	}
	copy := value.Clone()
	return &copy
}

func cloneDiscrimination(value *evidencediscrimination.DiscriminationAssessment) *evidencediscrimination.DiscriminationAssessment {
	if value == nil {
		return nil
	}
	copy := value.Clone()
	return &copy
}

func cloneRequests(values []advisoryrequests.AdvisoryEvidenceRequest) []advisoryrequests.AdvisoryEvidenceRequest {
	result := make([]advisoryrequests.AdvisoryEvidenceRequest, len(values))
	for i := range values {
		result[i] = values[i].Clone()
	}
	return result
}

func cloneMappings(values []capabilitymapping.CapabilityMappingAssessment) []capabilitymapping.CapabilityMappingAssessment {
	result := make([]capabilitymapping.CapabilityMappingAssessment, len(values))
	for i := range values {
		result[i] = values[i].Clone()
	}
	return result
}

func cloneAuthorizations(values []authorizationboundary.AuthorizationBoundaryAssessment) []authorizationboundary.AuthorizationBoundaryAssessment {
	result := make([]authorizationboundary.AuthorizationBoundaryAssessment, len(values))
	for i := range values {
		result[i] = values[i].Clone()
	}
	return result
}

func (s EpisodeWorkflowState) Clone() EpisodeWorkflowState {
	out := s
	out.Episode = cloneEpisode(s.Episode)
	out.Facts = cloneFacts(s.Facts)
	out.Hypotheses = cloneHypotheses(s.Hypotheses)
	out.Discrimination = cloneDiscrimination(s.Discrimination)
	out.AdvisoryRequests = cloneRequests(s.AdvisoryRequests)
	out.CapabilityMappings = cloneMappings(s.CapabilityMappings)
	out.AuthorizationAssessments = cloneAuthorizations(s.AuthorizationAssessments)
	out.Freshness = make(map[LayerKind]LayerFreshness, len(s.Freshness))
	for key, value := range s.Freshness {
		out.Freshness[key] = value
	}
	return out
}

func (s WorkflowState) Clone() WorkflowState {
	out := s
	out.Episodes = make([]EpisodeWorkflowState, len(s.Episodes))
	for i := range s.Episodes {
		out.Episodes[i] = s.Episodes[i].Clone()
	}
	sort.Slice(out.Episodes, func(i, j int) bool { return out.Episodes[i].EpisodeID < out.Episodes[j].EpisodeID })
	return out
}

func (m WorkflowMutation) Clone() WorkflowMutation {
	out := m
	out.Episode = cloneEpisode(m.Episode)
	out.Facts = cloneFacts(m.Facts)
	out.Hypotheses = cloneHypotheses(m.Hypotheses)
	out.Discrimination = cloneDiscrimination(m.Discrimination)
	out.ReplaceAdvisoryRequests = cloneRequests(m.ReplaceAdvisoryRequests)
	out.ReplaceCapabilityMappings = cloneMappings(m.ReplaceCapabilityMappings)
	out.ReplaceAuthorizationAssessments = cloneAuthorizations(m.ReplaceAuthorizationAssessments)
	out.ExplicitInvalidations = append([]LayerKind(nil), m.ExplicitInvalidations...)
	out.ReasonCodes = append([]string(nil), m.ReasonCodes...)
	return out
}

func (t WorkflowTransaction) Clone() WorkflowTransaction {
	out := t
	out.Mutation = t.Mutation.Clone()
	return out
}

func (c Checkpoint) Clone() Checkpoint {
	out := c
	out.State = c.State.Clone()
	return out
}

func (r Record) Clone() Record {
	out := r
	out.Payload = append([]byte(nil), r.Payload...)
	return out
}

func (r RecoveryInput) Clone() RecoveryInput {
	out := r
	out.Records = make([]Record, len(r.Records))
	for i := range r.Records {
		out.Records[i] = r.Records[i].Clone()
	}
	if r.Checkpoint != nil {
		copy := r.Checkpoint.Clone()
		out.Checkpoint = &copy
	}
	out.Warnings = append([]string(nil), r.Warnings...)
	return out
}
