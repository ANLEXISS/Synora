package validation

import (
	"context"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/hypotheses"
)

type StepKind string

const (
	StepOpenCoordinator     StepKind = "open_coordinator"
	StepAddChain            StepKind = "add_chain"
	StepAddObservation      StepKind = "add_observation"
	StepPlanAssociation     StepKind = "plan_association"
	StepApplyAssociation    StepKind = "apply_association"
	StepEvaluateEvidence    StepKind = "evaluate_evidence"
	StepApplyEvidence       StepKind = "apply_evidence"
	StepOpenHypothesis      StepKind = "open_hypothesis"
	StepRebaseHypothesis    StepKind = "rebase_hypothesis"
	StepSupersedeHypothesis StepKind = "supersede_hypothesis"
	StepPlanResolution      StepKind = "plan_resolution"
	StepResolveHypothesis   StepKind = "resolve_hypothesis"
	StepSetHypothesisStatus StepKind = "set_hypothesis_status"
	StepCreateGeneration    StepKind = "create_generation"
	StepRestartFromJournal  StepKind = "restart_from_journal"
	StepRestartFromManifest StepKind = "restart_from_manifest"
	StepAssertState         StepKind = "assert_state"
)

type Step struct {
	ID          string
	Kind        StepKind
	At          time.Time
	Mutation    chains.MutationContext
	Input       StepInput
	ExpectError bool
}

// StepInput is a typed union. A scenario never uses an untyped map as its
// internal command model; the concrete value is selected by Step.Kind.
type StepInput interface{ validationStepInput() }

type OpenCoordinatorInput struct {
	Purpose              string
	JournalMaxSize       int64
	JournalMaxRecordSize int
	AppendHook           func(stage string) error
	PublicationHook      func() error
}

func (OpenCoordinatorInput) validationStepInput() {}

type AddChainInput struct {
	ChainID             chains.ChainID
	InitialObservations []chains.ObservationRef
}

func (AddChainInput) validationStepInput() {}

type AddObservationInput struct {
	ChainID        chains.ChainID
	SourceRevision uint64
	Observation    chains.ObservationRef
}

func (AddObservationInput) validationStepInput() {}

type PlanAssociationInput struct {
	Observation   chains.ObservationRef
	SituationKind string
	Policy        association.Policy
}

func (PlanAssociationInput) validationStepInput() {}

type ApplyAssociationInput struct{ PlanStepID string }

func (ApplyAssociationInput) validationStepInput() {}

type EvaluateEvidenceInput struct {
	ChainID             chains.ChainID
	TargetObservationID string
	Policy              evidence.Policy
	ForceAmbiguous      bool
}

func (EvaluateEvidenceInput) validationStepInput() {}

type ApplyEvidenceInput struct{ EvaluationStepID string }

func (ApplyEvidenceInput) validationStepInput() {}

type OpenHypothesisInput struct {
	AssociationPlanStepID    string
	EvidenceEvaluationStepID string
	CreateCandidate          bool
}

func (OpenHypothesisInput) validationStepInput() {}

type RebaseHypothesisInput struct {
	SetID                 hypotheses.SetID
	EvaluationStepID      string
	AssociationPlanStepID string
}

func (RebaseHypothesisInput) validationStepInput() {}

type SupersedeHypothesisInput struct {
	SetID            hypotheses.SetID
	Successor        hypotheses.SetID
	SourceRevision   uint64
	EvaluationStepID string
}

func (SupersedeHypothesisInput) validationStepInput() {}

type PlanResolutionInput struct {
	SetID              hypotheses.SetID
	AlternativeID      string
	AlternativeKind    hypotheses.AlternativeKind
	AlternativeChainID chains.ChainID
}

func (PlanResolutionInput) validationStepInput() {}

type ResolveHypothesisInput struct {
	PlanStepID   string
	Mutation     chains.MutationContext
	CancelBefore bool
	CancelDuring bool
}

func (ResolveHypothesisInput) validationStepInput() {}

type SetHypothesisStatusInput struct {
	SetID          hypotheses.SetID
	Target         hypotheses.Status
	SourceRevision uint64
}

func (SetHypothesisStatusInput) validationStepInput() {}

type CreateGenerationInput struct{ Actor, CorrelationID string }

func (CreateGenerationInput) validationStepInput() {}

type RestartInput struct{}

func (RestartInput) validationStepInput() {}

type AssertStateInput struct {
	ChainCount                    int
	HypothesisCount               int
	SupersededCount               int
	ResolvedSetID                 hypotheses.SetID
	ResolvedEffect                hypotheses.ResolutionEffectKind
	RequireSingleResolutionRecord bool
}

func (AssertStateInput) validationStepInput() {}

type ExpectedOutcome struct {
	RequiredEffects       []hypotheses.ResolutionEffectKind
	MinimumGenerations    int
	RequireReplayEquality bool
	AllowJournalFailure   bool
}

type Scenario struct {
	ID          string
	Description string
	StartAt     time.Time
	Steps       []Step
	Expected    ExpectedOutcome
}

type StepResult struct {
	StepID          string             `json:"step_id"`
	StepKind        StepKind           `json:"step_kind"`
	StartedAt       time.Time          `json:"started_at"`
	CompletedAt     time.Time          `json:"completed_at"`
	Success         bool               `json:"success"`
	ChainIDs        []chains.ChainID   `json:"chain_ids,omitempty"`
	HypothesisIDs   []hypotheses.SetID `json:"hypothesis_ids,omitempty"`
	JournalSequence uint64             `json:"journal_sequence,omitempty"`
	JournalHeadHash string             `json:"journal_head_hash,omitempty"`
	ErrorCode       string             `json:"error_code,omitempty"`
	Error           string             `json:"error,omitempty"`
}

type StateDigest struct {
	ChainCount       int    `json:"chain_count"`
	HypothesisCount  int    `json:"hypothesis_count"`
	ChainsSHA256     string `json:"chains_sha256"`
	HypothesesSHA256 string `json:"hypotheses_sha256"`
	JournalSequence  uint64 `json:"journal_sequence"`
	JournalHeadHash  string `json:"journal_head_hash"`
}

type ScenarioMetrics struct {
	ObservationsAdded              int `json:"observations_added"`
	ChainsCreated                  int `json:"chains_created"`
	ChainsUpdated                  int `json:"chains_updated"`
	ContributionsAdded             int `json:"contributions_added"`
	HypothesesOpened               int `json:"hypotheses_opened"`
	HypothesesRebased              int `json:"hypotheses_rebased"`
	HypothesesSuperseded           int `json:"hypotheses_superseded"`
	HypothesesResolved             int `json:"hypotheses_resolved"`
	ResolutionAttachEffects        int `json:"resolution_attach_effects"`
	ResolutionCreateEffects        int `json:"resolution_create_effects"`
	ResolutionSupportEffects       int `json:"resolution_support_effects"`
	ResolutionContradictionEffects int `json:"resolution_contradiction_effects"`
	ResolutionNeutralEffects       int `json:"resolution_neutral_effects"`
	ResolutionNoChainEffects       int `json:"resolution_no_chain_effects"`
	ReplaysPerformed               int `json:"replays_performed"`
	GenerationsCreated             int `json:"generations_created"`
	IdempotentOperations           int `json:"idempotent_operations"`
	StaleOperations                int `json:"stale_operations"`
	CollisionOperations            int `json:"collision_operations"`
	InvariantFailures              int `json:"invariant_failures"`
}

type InvariantFailure struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ScenarioReport struct {
	ScenarioID   string             `json:"scenario_id"`
	StartedAt    time.Time          `json:"started_at"`
	CompletedAt  time.Time          `json:"completed_at"`
	Success      bool               `json:"success"`
	Steps        []StepResult       `json:"steps"`
	InitialState StateDigest        `json:"initial_state"`
	FinalState   StateDigest        `json:"final_state"`
	Metrics      ScenarioMetrics    `json:"metrics"`
	Failures     []InvariantFailure `json:"failures,omitempty"`
}

type ScenarioContext interface {
	Run(context.Context, Scenario) (ScenarioReport, error)
}

type JournalSummary struct {
	Sequence uint64
	HeadHash string
	Kinds    []journal.RecordKind
}
