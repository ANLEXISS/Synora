package cge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DecisionEnvelopeSchemaVersion = "synora.cge.decision-envelope.v1"
	DecisionRecommendationSchema  = "synora.cge.recommendation.v1"
	DecisionExecutionSchema       = "synora.cge.execution-request.v1"
	DecisionActionResultSchema    = "synora.cge.action-result.v1"
	AuthorityModeEnv              = "SYNORA_CGE_AUTHORITY_MODE"
	maxDecisionEvidenceRefs       = 32
	maxDecisionInvariantRefs      = 16
	maxDecisionValidity           = 24 * time.Hour
)

var (
	ErrInvalidAuthorityMode        = errors.New("invalid_cge_authority_mode")
	ErrInvalidDecisionEnvelope     = errors.New("invalid_cge_decision_envelope")
	ErrInvalidDecisionTarget       = errors.New("invalid_cge_decision_target")
	ErrInvalidChainReference       = errors.New("invalid_cge_chain_reference")
	ErrDecisionExpired             = errors.New("cge_decision_expired")
	ErrDecisionStore               = errors.New("cge_decision_store_error")
	ErrUnknownDecision             = errors.New("unknown_cge_decision")
	ErrActionResultUnauthorized    = errors.New("action_result_not_authorized")
	ErrActionResultConflict        = errors.New("action_result_conflict")
	ErrExecutionPlannerUnavailable = errors.New("execution_planner_unavailable")
	ErrInvalidPromotion            = errors.New("invalid_learned_chain_promotion")
)

// AuthorityMode controls the publication boundary between CGE and Core.
// Shadow is deliberately the zero-risk default at configuration boundaries.
type AuthorityMode string

const (
	AuthorityModeShadow        AuthorityMode = "shadow"
	AuthorityModeAdvisory      AuthorityMode = "advisory"
	AuthorityModeAuthoritative AuthorityMode = "authoritative"
)

func (m AuthorityMode) Validate() error {
	switch m {
	case AuthorityModeShadow, AuthorityModeAdvisory, AuthorityModeAuthoritative:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidAuthorityMode, m)
	}
}

func ParseAuthorityMode(value string) (AuthorityMode, error) {
	mode := AuthorityMode(strings.TrimSpace(value))
	if mode == "" {
		return AuthorityModeShadow, nil
	}
	if err := mode.Validate(); err != nil {
		return "", err
	}
	return mode, nil
}

type DecisionType string

const (
	DecisionTypeObserve         DecisionType = "observe"
	DecisionTypeNotify          DecisionType = "notify"
	DecisionTypeRecordEvidence  DecisionType = "record_evidence"
	DecisionTypeRequestValidate DecisionType = "request_validation"
	DecisionTypeChangeMode      DecisionType = "change_mode"
)

func (t DecisionType) Validate() error {
	switch t {
	case DecisionTypeObserve, DecisionTypeNotify, DecisionTypeRecordEvidence, DecisionTypeRequestValidate, DecisionTypeChangeMode:
		return nil
	default:
		return fmt.Errorf("%w: decision type %q", ErrInvalidDecisionEnvelope, t)
	}
}

type DecisionTargetKind string

const (
	DecisionTargetDevice   DecisionTargetKind = "device"
	DecisionTargetNode     DecisionTargetKind = "node"
	DecisionTargetZone     DecisionTargetKind = "zone"
	DecisionTargetResident DecisionTargetKind = "resident"
	DecisionTargetSystem   DecisionTargetKind = "system"
)

type DecisionTarget struct {
	Kind         DecisionTargetKind `json:"kind" yaml:"kind"`
	ID           string             `json:"id" yaml:"id"`
	Scope        string             `json:"scope,omitempty" yaml:"scope,omitempty"`
	RevisionHash string             `json:"revision_hash,omitempty" yaml:"revision_hash,omitempty"`
}

func (t DecisionTarget) Validate() error {
	switch t.Kind {
	case DecisionTargetDevice, DecisionTargetNode, DecisionTargetZone, DecisionTargetResident, DecisionTargetSystem:
	default:
		return fmt.Errorf("%w: kind %q", ErrInvalidDecisionTarget, t.Kind)
	}
	if err := validateAuthorityText(t.ID, "target id", 256, true); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDecisionTarget, err)
	}
	if err := validateAuthorityText(t.Scope, "target scope", 256, false); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDecisionTarget, err)
	}
	return validateAuthorityText(t.RevisionHash, "target revision hash", 128, false)
}

func (t DecisionTarget) equal(other DecisionTarget) bool {
	return t.Kind == other.Kind && t.ID == other.ID
}

type ChainClass string

const (
	ChainClassInvariant ChainClass = "invariant"
	ChainClassCritical  ChainClass = "critical"
	ChainClassLearned   ChainClass = "learned"
)

func (c ChainClass) Validate() error {
	switch c {
	case ChainClassInvariant, ChainClassCritical, ChainClassLearned:
		return nil
	default:
		return fmt.Errorf("%w: class %q", ErrInvalidChainReference, c)
	}
}

type ChainReference struct {
	ChainID      string     `json:"chain_id" yaml:"chain_id"`
	Version      uint64     `json:"version" yaml:"version"`
	Class        ChainClass `json:"class" yaml:"class"`
	RevisionHash string     `json:"revision_hash" yaml:"revision_hash"`
}

func (r ChainReference) Validate() error {
	if err := validateAuthorityText(r.ChainID, "chain id", 256, true); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChainReference, err)
	}
	if r.Version == 0 {
		return fmt.Errorf("%w: chain version is zero", ErrInvalidChainReference)
	}
	if err := r.Class.Validate(); err != nil {
		return err
	}
	if err := validateAuthorityText(r.RevisionHash, "chain revision hash", 128, true); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChainReference, err)
	}
	return nil
}

type DecisionConstraints struct {
	RequiresAuthorization bool     `json:"requires_authorization" yaml:"requires_authorization"`
	RequiresPhysicalLimit bool     `json:"requires_physical_limit" yaml:"requires_physical_limit"`
	MaxPriority           int      `json:"max_priority,omitempty" yaml:"max_priority,omitempty"`
	RequiredStateRevision uint64   `json:"required_state_revision,omitempty" yaml:"required_state_revision,omitempty"`
	RequiredInvariantRefs []string `json:"required_invariant_refs,omitempty" yaml:"required_invariant_refs,omitempty"`
	ProposedActions       []string `json:"proposed_actions,omitempty" yaml:"proposed_actions,omitempty"`
	ForbiddenActions      []string `json:"forbidden_actions,omitempty" yaml:"forbidden_actions,omitempty"`
}

func (c DecisionConstraints) Validate() error {
	if c.MaxPriority < 0 || c.MaxPriority > 100 {
		return fmt.Errorf("%w: max priority is out of bounds", ErrInvalidDecisionEnvelope)
	}
	if len(c.RequiredInvariantRefs) > maxDecisionInvariantRefs {
		return fmt.Errorf("%w: too many invariant references", ErrInvalidDecisionEnvelope)
	}
	if len(c.ProposedActions) > 16 || len(c.ForbiddenActions) > 16 {
		return fmt.Errorf("%w: action intent is out of bounds", ErrInvalidDecisionEnvelope)
	}
	if err := validateActionIntent(c.ProposedActions); err != nil {
		return err
	}
	if err := validateActionIntent(c.ForbiddenActions); err != nil {
		return err
	}
	proposed := make(map[string]struct{}, len(c.ProposedActions))
	for _, action := range c.ProposedActions {
		proposed[action] = struct{}{}
	}
	for _, action := range c.ForbiddenActions {
		if _, ok := proposed[action]; ok {
			return fmt.Errorf("%w: action is both proposed and forbidden", ErrInvalidDecisionEnvelope)
		}
	}
	seen := make(map[string]struct{}, len(c.RequiredInvariantRefs))
	for _, ref := range c.RequiredInvariantRefs {
		if err := validateAuthorityText(ref, "invariant reference", 128, true); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDecisionEnvelope, err)
		}
		if _, ok := seen[ref]; ok {
			return fmt.Errorf("%w: duplicate invariant reference", ErrInvalidDecisionEnvelope)
		}
		seen[ref] = struct{}{}
	}
	return nil
}

func validateActionIntent(values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateAuthorityText(value, "action intent", 128, true); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDecisionEnvelope, err)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%w: duplicate action intent", ErrInvalidDecisionEnvelope)
		}
		seen[value] = struct{}{}
	}
	return nil
}

type DecisionEnvelope struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	DecisionID   string       `json:"decision_id" yaml:"decision_id"`
	SituationID  string       `json:"situation_id" yaml:"situation_id"`
	DecisionType DecisionType `json:"decision_type" yaml:"decision_type"`
	DesiredState string       `json:"desired_state,omitempty" yaml:"desired_state,omitempty"`

	Target DecisionTarget `json:"target" yaml:"target"`

	Confidence float64 `json:"confidence" yaml:"confidence"`
	Priority   int     `json:"priority" yaml:"priority"`

	EvidenceRefs []string `json:"evidence_refs,omitempty" yaml:"evidence_refs,omitempty"`

	CriticalChainRef *ChainReference `json:"critical_chain_ref,omitempty" yaml:"critical_chain_ref,omitempty"`
	LearnedChainRef  *ChainReference `json:"learned_chain_ref,omitempty" yaml:"learned_chain_ref,omitempty"`

	Constraints DecisionConstraints `json:"constraints" yaml:"constraints"`

	CreatedAt  time.Time `json:"created_at" yaml:"created_at"`
	ValidUntil time.Time `json:"valid_until" yaml:"valid_until"`

	IdempotencyKey string `json:"idempotency_key" yaml:"idempotency_key"`
}

// Decision is the CGE's selected cognitive choice. The envelope is the
// transport/persistence contract; this wrapper makes the decision boundary
// explicit and keeps recommendations distinct from decisions.
type Decision struct {
	Envelope   DecisionEnvelope `json:"envelope" yaml:"envelope"`
	SelectedAt time.Time        `json:"selected_at" yaml:"selected_at"`
}

func (d Decision) Validate() error {
	if err := d.Envelope.Validate(); err != nil {
		return err
	}
	if d.SelectedAt.IsZero() {
		return fmt.Errorf("%w: decision selection timestamp is required", ErrInvalidDecisionEnvelope)
	}
	return nil
}

func (d DecisionEnvelope) Validate() error {
	return d.validateAt(time.Time{}, false)
}

func (d DecisionEnvelope) ValidateAt(now time.Time) error {
	return d.validateAt(now.UTC(), true)
}

func (d DecisionEnvelope) validateAt(now time.Time, checkExpiry bool) error {
	if d.SchemaVersion != DecisionEnvelopeSchemaVersion {
		return fmt.Errorf("%w: schema version %q", ErrInvalidDecisionEnvelope, d.SchemaVersion)
	}
	for name, value := range map[string]string{"decision id": d.DecisionID, "situation id": d.SituationID, "idempotency key": d.IdempotencyKey} {
		if err := validateAuthorityText(value, name, 256, true); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDecisionEnvelope, err)
		}
	}
	if err := d.DecisionType.Validate(); err != nil {
		return err
	}
	if err := validateAuthorityText(d.DesiredState, "desired state", 64, d.DecisionType == DecisionTypeChangeMode); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDecisionEnvelope, err)
	}
	if d.DecisionType == DecisionTypeChangeMode && !validCognitiveExpectedState(d.DesiredState) {
		return fmt.Errorf("%w: desired state is not allowlisted", ErrInvalidDecisionEnvelope)
	}
	if err := d.Target.Validate(); err != nil {
		return err
	}
	if math.IsNaN(d.Confidence) || math.IsInf(d.Confidence, 0) || d.Confidence < 0 || d.Confidence > 1 {
		return fmt.Errorf("%w: confidence is out of bounds", ErrInvalidDecisionEnvelope)
	}
	if d.Priority < 0 || d.Priority > 100 {
		return fmt.Errorf("%w: priority is out of bounds", ErrInvalidDecisionEnvelope)
	}
	if len(d.EvidenceRefs) == 0 || len(d.EvidenceRefs) > maxDecisionEvidenceRefs {
		return fmt.Errorf("%w: evidence references must contain between 1 and %d entries", ErrInvalidDecisionEnvelope, maxDecisionEvidenceRefs)
	}
	seenEvidence := make(map[string]struct{}, len(d.EvidenceRefs))
	for _, ref := range d.EvidenceRefs {
		if err := validateAuthorityText(ref, "evidence reference", 256, true); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDecisionEnvelope, err)
		}
		if _, ok := seenEvidence[ref]; ok {
			return fmt.Errorf("%w: duplicate evidence reference", ErrInvalidDecisionEnvelope)
		}
		seenEvidence[ref] = struct{}{}
	}
	if (d.CriticalChainRef == nil) == (d.LearnedChainRef == nil) {
		return fmt.Errorf("%w: exactly one chain reference is required", ErrInvalidDecisionEnvelope)
	}
	if d.CriticalChainRef != nil {
		if err := d.CriticalChainRef.Validate(); err != nil {
			return err
		}
		if d.CriticalChainRef.Class != ChainClassCritical {
			return fmt.Errorf("%w: invariant references belong in constraints, not as business chains", ErrInvalidDecisionEnvelope)
		}
	}
	if d.LearnedChainRef != nil {
		if err := d.LearnedChainRef.Validate(); err != nil {
			return err
		}
		if d.LearnedChainRef.Class != ChainClassLearned {
			return fmt.Errorf("%w: learned reference has non-learned class", ErrInvalidDecisionEnvelope)
		}
	}
	if err := d.Constraints.Validate(); err != nil {
		return err
	}
	if d.CreatedAt.IsZero() || d.ValidUntil.IsZero() || !d.ValidUntil.After(d.CreatedAt) {
		return fmt.Errorf("%w: invalid decision validity interval", ErrInvalidDecisionEnvelope)
	}
	if d.ValidUntil.Sub(d.CreatedAt) > maxDecisionValidity {
		return fmt.Errorf("%w: validity interval is too long", ErrInvalidDecisionEnvelope)
	}
	if checkExpiry && !now.IsZero() && now.After(d.ValidUntil) {
		return ErrDecisionExpired
	}
	return nil
}

type RecommendationMarkers struct {
	DescriptiveOnly          bool `json:"descriptive_only" yaml:"descriptive_only"`
	NotADecision             bool `json:"not_a_decision" yaml:"not_a_decision"`
	NotAnExecutionRequest    bool `json:"not_an_execution_request" yaml:"not_an_execution_request"`
	RequiresDecisionBoundary bool `json:"requires_decision_boundary" yaml:"requires_decision_boundary"`
}

type Recommendation struct {
	SchemaVersion    string                `json:"schema_version" yaml:"schema_version"`
	RecommendationID string                `json:"recommendation_id" yaml:"recommendation_id"`
	SituationID      string                `json:"situation_id" yaml:"situation_id"`
	DecisionType     DecisionType          `json:"decision_type" yaml:"decision_type"`
	Target           DecisionTarget        `json:"target" yaml:"target"`
	Confidence       float64               `json:"confidence" yaml:"confidence"`
	EvidenceRefs     []string              `json:"evidence_refs" yaml:"evidence_refs"`
	CreatedAt        time.Time             `json:"created_at" yaml:"created_at"`
	Markers          RecommendationMarkers `json:"markers" yaml:"markers"`
}

func (r Recommendation) Validate() error {
	if r.SchemaVersion != DecisionRecommendationSchema || r.RecommendationID == "" || r.SituationID == "" || r.CreatedAt.IsZero() {
		return fmt.Errorf("%w: recommendation identity", ErrInvalidDecisionEnvelope)
	}
	if err := r.DecisionType.Validate(); err != nil {
		return err
	}
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if r.Confidence < 0 || r.Confidence > 1 || math.IsNaN(r.Confidence) || math.IsInf(r.Confidence, 0) {
		return fmt.Errorf("%w: recommendation confidence", ErrInvalidDecisionEnvelope)
	}
	if len(r.EvidenceRefs) == 0 || len(r.EvidenceRefs) > maxDecisionEvidenceRefs {
		return fmt.Errorf("%w: recommendation evidence", ErrInvalidDecisionEnvelope)
	}
	if !r.Markers.DescriptiveOnly || !r.Markers.NotADecision || !r.Markers.NotAnExecutionRequest || !r.Markers.RequiresDecisionBoundary {
		return fmt.Errorf("%w: recommendation authority markers", ErrInvalidDecisionEnvelope)
	}
	return nil
}

type ExecutionRequest struct {
	SchemaVersion   string         `json:"schema_version" yaml:"schema_version"`
	DecisionID      string         `json:"decision_id" yaml:"decision_id"`
	ActionRequestID string         `json:"action_request_id" yaml:"action_request_id"`
	ExecutionType   string         `json:"execution_type" yaml:"execution_type"`
	Target          DecisionTarget `json:"target" yaml:"target"`
	IdempotencyKey  string         `json:"idempotency_key" yaml:"idempotency_key"`
	CreatedAt       time.Time      `json:"created_at" yaml:"created_at"`
	ValidUntil      time.Time      `json:"valid_until" yaml:"valid_until"`
}

func (r ExecutionRequest) Validate() error {
	if r.SchemaVersion != DecisionExecutionSchema || r.DecisionID == "" || r.ActionRequestID == "" || r.IdempotencyKey == "" || r.CreatedAt.IsZero() || !r.ValidUntil.After(r.CreatedAt) {
		return fmt.Errorf("%w: invalid execution request", ErrInvalidDecisionEnvelope)
	}
	if err := r.Target.Validate(); err != nil {
		return err
	}
	return validateAuthorityText(r.ExecutionType, "execution type", 128, true)
}

type ActionResultStatus string

const (
	ActionResultSucceeded    ActionResultStatus = "succeeded"
	ActionResultFailed       ActionResultStatus = "failed"
	ActionResultRejected     ActionResultStatus = "rejected"
	ActionResultImpossible   ActionResultStatus = "impossible"
	ActionResultContradicted ActionResultStatus = "contradicted"
)

type ActionResult struct {
	SchemaVersion          string             `json:"schema_version" yaml:"schema_version"`
	DecisionID             string             `json:"decision_id" yaml:"decision_id"`
	ActionRequestID        string             `json:"action_request_id" yaml:"action_request_id"`
	Status                 ActionResultStatus `json:"status" yaml:"status"`
	Error                  string             `json:"error,omitempty" yaml:"error,omitempty"`
	Duration               time.Duration      `json:"duration" yaml:"duration"`
	BeforeStateFingerprint string             `json:"before_state_fingerprint" yaml:"before_state_fingerprint"`
	AfterStateFingerprint  string             `json:"after_state_fingerprint" yaml:"after_state_fingerprint"`
	Timestamp              time.Time          `json:"timestamp" yaml:"timestamp"`
}

func (r ActionResult) Validate() error {
	if r.SchemaVersion != DecisionActionResultSchema || r.DecisionID == "" || r.ActionRequestID == "" || r.Timestamp.IsZero() || r.Duration < 0 {
		return fmt.Errorf("%w: invalid action result", ErrInvalidDecisionEnvelope)
	}
	switch r.Status {
	case ActionResultSucceeded, ActionResultFailed, ActionResultRejected, ActionResultImpossible, ActionResultContradicted:
	default:
		return fmt.Errorf("%w: invalid action result status", ErrInvalidDecisionEnvelope)
	}
	if err := validateAuthorityText(r.BeforeStateFingerprint, "before state fingerprint", 128, true); err != nil {
		return err
	}
	if err := validateAuthorityText(r.AfterStateFingerprint, "after state fingerprint", 128, true); err != nil {
		return err
	}
	return validateAuthorityText(r.Error, "action error", 2048, false)
}

type SafetyStatus string

const (
	SafetyAllowed                   SafetyStatus = "allowed"
	SafetyDenied                    SafetyStatus = "denied"
	SafetyExpired                   SafetyStatus = "expired"
	SafetyStaleContext              SafetyStatus = "stale_context"
	SafetyInvalidTarget             SafetyStatus = "invalid_target"
	SafetyInsufficientAuthorization SafetyStatus = "insufficient_authorization"
	SafetyInvariantViolation        SafetyStatus = "invariant_violation"
)

type InvariantViolation struct {
	Code   string `json:"code" yaml:"code"`
	Detail string `json:"detail" yaml:"detail"`
}

type SafetyVerdict struct {
	DecisionID  string               `json:"decision_id" yaml:"decision_id"`
	Status      SafetyStatus         `json:"status" yaml:"status"`
	Violations  []InvariantViolation `json:"violations,omitempty" yaml:"violations,omitempty"`
	EvaluatedAt time.Time            `json:"evaluated_at" yaml:"evaluated_at"`
}

type OperationalTarget struct {
	Target          DecisionTarget            `json:"target" yaml:"target"`
	Exists          bool                      `json:"exists" yaml:"exists"`
	Authorized      bool                      `json:"authorized" yaml:"authorized"`
	PhysicalLimit   int                       `json:"physical_limit,omitempty" yaml:"physical_limit,omitempty"`
	CurrentRevision uint64                    `json:"current_revision,omitempty" yaml:"current_revision,omitempty"`
	Authorization   OperationalAuthorization  `json:"authorization" yaml:"authorization"`
	PhysicalLimits  OperationalPhysicalLimits `json:"physical_limits" yaml:"physical_limits"`
	Capabilities    []string                  `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	NodeID          string                    `json:"node_id,omitempty" yaml:"node_id,omitempty"`
	ZoneID          string                    `json:"zone_id,omitempty" yaml:"zone_id,omitempty"`
}

type OperationalAuthorization struct {
	Known      bool   `json:"known" yaml:"known"`
	Authorized bool   `json:"authorized" yaml:"authorized"`
	PolicyID   string `json:"policy_id,omitempty" yaml:"policy_id,omitempty"`
	Revision   uint64 `json:"revision,omitempty" yaml:"revision,omitempty"`
}

type OperationalPhysicalLimits struct {
	Known    bool   `json:"known" yaml:"known"`
	MaxValue int    `json:"max_value,omitempty" yaml:"max_value,omitempty"`
	Unit     string `json:"unit,omitempty" yaml:"unit,omitempty"`
	Source   string `json:"source,omitempty" yaml:"source,omitempty"`
}

type OperationalSnapshot struct {
	CapturedAt             time.Time           `json:"captured_at" yaml:"captured_at"`
	FreshUntil             time.Time           `json:"fresh_until" yaml:"fresh_until"`
	Revision               uint64              `json:"revision" yaml:"revision"`
	PolicyRevision         uint64              `json:"policy_revision" yaml:"policy_revision"`
	AuthorityMode          AuthorityMode       `json:"authority_mode" yaml:"authority_mode"`
	Targets                []OperationalTarget `json:"targets,omitempty" yaml:"targets,omitempty"`
	UsedIdempotencyKeys    []string            `json:"used_idempotency_keys,omitempty" yaml:"used_idempotency_keys,omitempty"`
	ConflictingDecisionIDs []string            `json:"conflicting_decision_ids,omitempty" yaml:"conflicting_decision_ids,omitempty"`
	CurrentSystemState     string              `json:"current_system_state,omitempty" yaml:"current_system_state,omitempty"`
	SecurityMode           string              `json:"security_mode,omitempty" yaml:"security_mode,omitempty"`
}

type SafetyKernel interface {
	ValidateDecision(context.Context, DecisionEnvelope, OperationalSnapshot) SafetyVerdict
}

type DefaultSafetyKernel struct {
	Mode AuthorityMode
	Now  func() time.Time
}

func NewSafetyKernel(mode AuthorityMode, now func() time.Time) (DefaultSafetyKernel, error) {
	if err := mode.Validate(); err != nil {
		return DefaultSafetyKernel{}, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return DefaultSafetyKernel{Mode: mode, Now: now}, nil
}

func (k DefaultSafetyKernel) ValidateDecision(ctx context.Context, decision DecisionEnvelope, snapshot OperationalSnapshot) SafetyVerdict {
	now := time.Now().UTC()
	if k.Now != nil {
		now = k.Now().UTC()
	}
	verdict := SafetyVerdict{DecisionID: decision.DecisionID, Status: SafetyAllowed, EvaluatedAt: now}
	violate := func(status SafetyStatus, code, detail string) SafetyVerdict {
		verdict.Status = status
		verdict.Violations = []InvariantViolation{{Code: code, Detail: detail}}
		return verdict
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return violate(SafetyInvariantViolation, "context_cancelled", "safety evaluation context cancelled")
		default:
		}
	}
	if err := decision.Validate(); err != nil {
		if errors.Is(err, ErrDecisionExpired) {
			return violate(SafetyExpired, "decision_expired", "decision validity has elapsed")
		}
		return violate(SafetyInvariantViolation, "decision_invalid", err.Error())
	}
	if now.After(decision.ValidUntil) {
		return violate(SafetyExpired, "decision_expired", "decision validity has elapsed")
	}
	if snapshot.AuthorityMode != "" && snapshot.AuthorityMode != k.Mode {
		return violate(SafetyInvariantViolation, "authority_mode_mismatch", "operational snapshot mode differs from kernel mode")
	}
	if snapshot.CapturedAt.IsZero() || snapshot.FreshUntil.IsZero() || now.After(snapshot.FreshUntil) {
		return violate(SafetyStaleContext, "stale_context", "operational snapshot is not fresh")
	}
	for _, ref := range decision.Constraints.RequiredInvariantRefs {
		if !knownInvariantReference(ref) {
			return violate(SafetyInvariantViolation, "unknown_invariant", ref)
		}
	}
	var target *OperationalTarget
	for i := range snapshot.Targets {
		if snapshot.Targets[i].Target.equal(decision.Target) {
			target = &snapshot.Targets[i]
			break
		}
	}
	if target == nil || !target.Exists {
		return violate(SafetyInvalidTarget, "target_missing", "decision target does not exist")
	}
	if decision.Constraints.RequiresAuthorization && (!target.Authorization.Known || !target.Authorization.Authorized) {
		return violate(SafetyInsufficientAuthorization, "authorization_missing", "decision target is not authorized")
	}
	if decision.Constraints.RequiresPhysicalLimit && !target.PhysicalLimits.Known {
		return violate(SafetyInvariantViolation, "physical_limits_unknown", "physical limits are not known for the decision target")
	}
	physicalLimit := target.PhysicalLimits.MaxValue
	if decision.Constraints.RequiresPhysicalLimit && decision.Priority > physicalLimit {
		return violate(SafetyInvariantViolation, "physical_limit_exceeded", "decision priority exceeds target physical limit")
	}
	if decision.Constraints.RequiredStateRevision != 0 && target.CurrentRevision != decision.Constraints.RequiredStateRevision {
		return violate(SafetyStaleContext, "state_revision_mismatch", "target state revision differs from decision constraint")
	}
	for _, key := range snapshot.UsedIdempotencyKeys {
		if key == decision.IdempotencyKey {
			return violate(SafetyInvariantViolation, "idempotency_key_replayed", "decision idempotency key was already used")
		}
	}
	if len(snapshot.ConflictingDecisionIDs) > 0 {
		return violate(SafetyInvariantViolation, "decision_conflict", "an active decision conflict exists")
	}
	return verdict
}

type DecisionPublicationStatus string

const (
	DecisionPublishedShadow              DecisionPublicationStatus = "shadow"
	DecisionPublishedShadowDryRun        DecisionPublicationStatus = "shadow_dry_run"
	DecisionPublishedAdvisory            DecisionPublicationStatus = "advisory"
	DecisionPublishedAdvisoryDryRun      DecisionPublicationStatus = "advisory_dry_run"
	DecisionPublishedAuthoritative       DecisionPublicationStatus = "authoritative"
	DecisionPublishedAuthoritativeDryRun DecisionPublicationStatus = "authoritative_dry_run"
	DecisionPublicationDenied            DecisionPublicationStatus = "denied"
)

type DecisionRecord struct {
	Envelope         DecisionEnvelope          `json:"envelope" yaml:"envelope"`
	Mode             AuthorityMode             `json:"mode" yaml:"mode"`
	Status           DecisionPublicationStatus `json:"status" yaml:"status"`
	Verdict          SafetyVerdict             `json:"verdict" yaml:"verdict"`
	PersistedAt      time.Time                 `json:"persisted_at" yaml:"persisted_at"`
	ExecutionRequest *ExecutionRequest         `json:"execution_request,omitempty" yaml:"execution_request,omitempty"`
	ExecutionLease   *ActiveExecutionLease     `json:"execution_lease,omitempty" yaml:"execution_lease,omitempty"`
	ExecutionPlan    *ExecutionPlan            `json:"execution_plan,omitempty" yaml:"execution_plan,omitempty"`
}

type ExecutionLeaseStatus string

const (
	ExecutionLeasePlanned    ExecutionLeaseStatus = "planned"
	ExecutionLeaseDispatched ExecutionLeaseStatus = "dispatched"
	ExecutionLeaseRunning    ExecutionLeaseStatus = "running"
	ExecutionLeaseCompleted  ExecutionLeaseStatus = "completed"
	ExecutionLeaseFailed     ExecutionLeaseStatus = "failed"
	ExecutionLeaseExpired    ExecutionLeaseStatus = "expired"
	ExecutionLeaseCancelled  ExecutionLeaseStatus = "cancelled"
)

type ActiveExecutionLease struct {
	DecisionID      string               `json:"decision_id" yaml:"decision_id"`
	ActionRequestID string               `json:"action_request_id" yaml:"action_request_id"`
	Target          DecisionTarget       `json:"target" yaml:"target"`
	ExecutionType   string               `json:"execution_type" yaml:"execution_type"`
	CreatedAt       time.Time            `json:"created_at" yaml:"created_at"`
	ValidUntil      time.Time            `json:"valid_until" yaml:"valid_until"`
	Status          ExecutionLeaseStatus `json:"status" yaml:"status"`
}

func (l ActiveExecutionLease) Validate() error {
	if l.DecisionID == "" || l.ActionRequestID == "" || l.CreatedAt.IsZero() || !l.ValidUntil.After(l.CreatedAt) {
		return ErrInvalidDecisionEnvelope
	}
	if err := l.Target.Validate(); err != nil {
		return err
	}
	if err := validateAuthorityText(l.ExecutionType, "execution type", 128, true); err != nil {
		return err
	}
	switch l.Status {
	case ExecutionLeasePlanned, ExecutionLeaseDispatched, ExecutionLeaseRunning, ExecutionLeaseCompleted, ExecutionLeaseFailed, ExecutionLeaseExpired, ExecutionLeaseCancelled:
	default:
		return ErrInvalidDecisionEnvelope
	}
	return nil
}

type DecisionPublication struct {
	DecisionID       string                    `json:"decision_id" yaml:"decision_id"`
	Mode             AuthorityMode             `json:"mode" yaml:"mode"`
	Status           DecisionPublicationStatus `json:"status" yaml:"status"`
	Verdict          SafetyVerdict             `json:"verdict" yaml:"verdict"`
	ExecutionRequest *ExecutionRequest         `json:"execution_request,omitempty" yaml:"execution_request,omitempty"`
	ExecutionPlan    *ExecutionPlan            `json:"execution_plan,omitempty" yaml:"execution_plan,omitempty"`
}

type ExecutionPlanner interface {
	PlanExecution(context.Context, DecisionEnvelope, OperationalSnapshot) (ExecutionRequest, error)
}

// DecisionPublisher is the narrow Core↔CGE publication boundary. It does not
// expose a free-form command channel.
type DecisionPublisher interface {
	PublishDecision(context.Context, DecisionEnvelope, OperationalSnapshot) (DecisionPublication, error)
}

// ActionFeedbackReceiver is the narrow execution-feedback boundary used by
// the CGE; recording feedback never performs learning or promotion implicitly.
type ActionFeedbackReceiver interface {
	RecordActionResult(context.Context, ActionResult) error
}

type DecisionStore interface {
	PersistDecision(context.Context, DecisionRecord) error
	Decisions(context.Context) ([]DecisionRecord, error)
	PersistActionResult(context.Context, ActionResult) error
	ActionResults(context.Context) ([]ActionResult, error)
}

type MemoryDecisionStore struct {
	mu            sync.RWMutex
	records       []DecisionRecord
	actionResults []ActionResult
}

func (s *MemoryDecisionStore) PersistDecision(ctx context.Context, record DecisionRecord) error {
	if s == nil {
		return ErrDecisionStore
	}
	if err := validatePersistedDecisionRecord(record); err != nil {
		return err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, cloneDecisionRecord(record))
	return nil
}

func (s *MemoryDecisionStore) Decisions(ctx context.Context) ([]DecisionRecord, error) {
	if s == nil {
		return nil, ErrDecisionStore
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]DecisionRecord, len(s.records))
	for i := range s.records {
		result[i] = cloneDecisionRecord(s.records[i])
	}
	return result, nil
}

func (s *MemoryDecisionStore) PersistActionResult(ctx context.Context, result ActionResult) error {
	if s == nil {
		return ErrDecisionStore
	}
	if err := result.Validate(); err != nil {
		return err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actionResults = append(s.actionResults, result)
	return nil
}

func (s *MemoryDecisionStore) ActionResults(ctx context.Context) ([]ActionResult, error) {
	if s == nil {
		return nil, ErrDecisionStore
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ActionResult(nil), s.actionResults...), nil
}

type FileDecisionStore struct {
	mu   sync.Mutex
	path string
}

func (s *FileDecisionStore) actionResultsPath() string {
	return filepath.Join(filepath.Dir(s.path), "action-results.ndjson")
}

// ValidateStoreWrite is the common durable-write guard used by the contract
// inventory. It accepts only the two closed records owned by this store.
func ValidateStoreWrite(value any) error {
	switch typed := value.(type) {
	case ChainGovernanceRecord:
		return typed.Validate()
	case DecisionRecord:
		return validatePersistedDecisionRecord(typed)
	case ActionResult:
		return typed.Validate()
	case ExecutionPlan:
		return typed.Validate()
	case ExecutionPlanComparison:
		return typed.Validate()
	default:
		return fmt.Errorf("%w: unsupported durable record %T", ErrDecisionStore, value)
	}
}

func NewFileDecisionStore(path string) (*FileDecisionStore, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrDecisionStore
	}
	return &FileDecisionStore{path: path}, nil
}

func (s *FileDecisionStore) PersistDecision(ctx context.Context, record DecisionRecord) error {
	if s == nil {
		return ErrDecisionStore
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if err := ValidateStoreWrite(record); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	_ = os.Chmod(filepath.Dir(s.path), 0o700)
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	defer file.Close()
	_ = file.Chmod(0o600)
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	return nil
}

func (s *FileDecisionStore) Decisions(ctx context.Context) ([]DecisionRecord, error) {
	if s == nil {
		return nil, ErrDecisionStore
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	file, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr != nil || info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("%w: decision store permissions", ErrDecisionStore)
	}
	var result []DecisionRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 2*1024*1024)
	for scanner.Scan() {
		var record DecisionRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDecisionStore, err)
		}
		if err := validatePersistedDecisionRecord(record); err != nil {
			return nil, fmt.Errorf("%w: invalid persisted decision: %v", ErrDecisionStore, err)
		}
		result = append(result, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	return result, nil
}

func (s *FileDecisionStore) PersistActionResult(ctx context.Context, result ActionResult) error {
	if s == nil {
		return ErrDecisionStore
	}
	if err := result.Validate(); err != nil {
		return err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if err := ValidateStoreWrite(result); err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.actionResultsPath()), 0o700); err != nil {
		return fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	_ = os.Chmod(filepath.Dir(s.actionResultsPath()), 0o700)
	file, err := os.OpenFile(s.actionResultsPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	defer file.Close()
	_ = file.Chmod(0o600)
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	return nil
}

func (s *FileDecisionStore) ActionResults(ctx context.Context) ([]ActionResult, error) {
	if s == nil {
		return nil, ErrDecisionStore
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	file, err := os.Open(s.actionResultsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr != nil || info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("%w: action result store permissions", ErrDecisionStore)
	}
	var result []ActionResult
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 2*1024*1024)
	for scanner.Scan() {
		var actionResult ActionResult
		if err := json.Unmarshal(scanner.Bytes(), &actionResult); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDecisionStore, err)
		}
		if err := validatePersistedActionResult(actionResult); err != nil {
			return nil, fmt.Errorf("%w: invalid persisted action result: %v", ErrDecisionStore, err)
		}
		result = append(result, actionResult)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecisionStore, err)
	}
	return result, nil
}

func validatePersistedDecisionRecord(record DecisionRecord) error {
	if err := record.Envelope.Validate(); err != nil {
		return err
	}
	if record.Verdict.Status == "" || record.PersistedAt.IsZero() {
		return fmt.Errorf("%w: incomplete decision record", ErrDecisionStore)
	}
	if err := record.Mode.Validate(); err != nil {
		return err
	}
	switch record.Status {
	case DecisionPublishedShadow, DecisionPublishedShadowDryRun, DecisionPublishedAdvisory, DecisionPublishedAdvisoryDryRun, DecisionPublishedAuthoritative, DecisionPublishedAuthoritativeDryRun, DecisionPublicationDenied:
	default:
		return fmt.Errorf("%w: invalid decision publication status", ErrDecisionStore)
	}
	if record.Status == DecisionPublishedAuthoritative && record.ExecutionRequest == nil {
		return fmt.Errorf("%w: authoritative record has no execution request", ErrDecisionStore)
	}
	if record.Status != DecisionPublishedAuthoritative && record.ExecutionRequest != nil {
		return fmt.Errorf("%w: non-authoritative record contains an execution request", ErrDecisionStore)
	}
	if (record.Status == DecisionPublishedAdvisoryDryRun || record.Status == DecisionPublishedAuthoritativeDryRun) && record.ExecutionPlan == nil {
		return fmt.Errorf("%w: dry-run record has no execution plan", ErrDecisionStore)
	}
	if (record.Status == DecisionPublishedAdvisoryDryRun || record.Status == DecisionPublishedAuthoritativeDryRun) && record.ExecutionLease != nil {
		return fmt.Errorf("%w: dry-run record contains an execution lease", ErrDecisionStore)
	}
	if record.ExecutionPlan != nil {
		if err := record.ExecutionPlan.Validate(); err != nil {
			return err
		}
		if record.ExecutionPlan.DecisionID != record.Envelope.DecisionID {
			return fmt.Errorf("%w: execution plan does not belong to decision", ErrDecisionStore)
		}
	}
	if record.ExecutionRequest != nil {
		if err := record.ExecutionRequest.Validate(); err != nil {
			return err
		}
		if record.ExecutionLease == nil {
			return fmt.Errorf("%w: authoritative record has no execution lease", ErrDecisionStore)
		}
		if err := record.ExecutionLease.Validate(); err != nil {
			return err
		}
		if record.ExecutionLease.DecisionID != record.ExecutionRequest.DecisionID || record.ExecutionLease.ActionRequestID != record.ExecutionRequest.ActionRequestID || !record.ExecutionLease.Target.equal(record.ExecutionRequest.Target) {
			return fmt.Errorf("%w: execution lease does not belong to request", ErrDecisionStore)
		}
	} else if record.ExecutionLease != nil {
		return fmt.Errorf("%w: non-authoritative record contains an execution lease", ErrDecisionStore)
	}
	return nil
}

func validatePersistedActionResult(result ActionResult) error {
	return result.Validate()
}

type DecisionAuthority struct {
	mode                 AuthorityMode
	kernel               SafetyKernel
	planner              ExecutionPlanner
	store                DecisionStore
	now                  func() time.Time
	executionMode        CGEExecutionMode
	governedPlanner      GovernedExecutionPlanner
	planKernel           ExecutionPlanSafetyKernel
	planStore            ExecutionPlanStore
	executionDiagnostics bool
}

// ConfigureExecution installs the dry-run-only planning boundary. It does not
// accept a dispatcher and therefore cannot grant CGE a physical side effect.
func (a *DecisionAuthority) ConfigureExecution(mode CGEExecutionMode, planner GovernedExecutionPlanner, kernel ExecutionPlanSafetyKernel, store ExecutionPlanStore, diagnostics bool) error {
	if a == nil {
		return ErrInvalidAuthorityMode
	}
	if err := mode.Validate(); err != nil {
		return err
	}
	if mode == CGEExecutionDryRun && (planner == nil || kernel == nil || store == nil) {
		return ErrExecutionPlannerUnavailable
	}
	a.executionMode, a.governedPlanner, a.planKernel, a.planStore, a.executionDiagnostics = mode, planner, kernel, store, diagnostics
	return nil
}

func NewGovernedDecisionAuthority(mode AuthorityMode, executionMode CGEExecutionMode, kernel SafetyKernel, planner GovernedExecutionPlanner, planKernel ExecutionPlanSafetyKernel, store DecisionStore, planStore ExecutionPlanStore, diagnostics bool) (*DecisionAuthority, error) {
	authority, err := NewDecisionAuthority(mode, kernel, nil, store)
	if err != nil {
		return nil, err
	}
	if err := authority.ConfigureExecution(executionMode, planner, planKernel, planStore, diagnostics); err != nil {
		return nil, err
	}
	return authority, nil
}

func NewDecisionAuthority(mode AuthorityMode, kernel SafetyKernel, planner ExecutionPlanner, store DecisionStore) (*DecisionAuthority, error) {
	if err := mode.Validate(); err != nil {
		return nil, err
	}
	if kernel == nil {
		defaultKernel, err := NewSafetyKernel(mode, nil)
		if err != nil {
			return nil, err
		}
		kernel = defaultKernel
	}
	if store == nil {
		store = &MemoryDecisionStore{}
	}
	return &DecisionAuthority{mode: mode, kernel: kernel, planner: planner, store: store, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (a *DecisionAuthority) Mode() AuthorityMode {
	if a == nil {
		return AuthorityModeShadow
	}
	return a.mode
}

func (a *DecisionAuthority) PublishDecision(ctx context.Context, decision DecisionEnvelope, snapshot OperationalSnapshot) (DecisionPublication, error) {
	if a == nil {
		return DecisionPublication{Status: DecisionPublicationDenied, Verdict: SafetyVerdict{Status: SafetyInvariantViolation}}, ErrInvalidAuthorityMode
	}
	if err := decision.Validate(); err != nil {
		return DecisionPublication{DecisionID: decision.DecisionID, Mode: a.mode, Status: DecisionPublicationDenied, Verdict: SafetyVerdict{DecisionID: decision.DecisionID, Status: SafetyInvariantViolation, EvaluatedAt: a.now()}}, err
	}
	records, err := a.store.Decisions(ctx)
	if err != nil {
		return DecisionPublication{DecisionID: decision.DecisionID, Mode: a.mode, Status: DecisionPublicationDenied, Verdict: SafetyVerdict{DecisionID: decision.DecisionID, Status: SafetyInvariantViolation, EvaluatedAt: a.now()}}, err
	}
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Envelope.DecisionID == decision.DecisionID {
			if records[i].Envelope.IdempotencyKey != decision.IdempotencyKey {
				return DecisionPublication{DecisionID: decision.DecisionID, Mode: a.mode, Status: DecisionPublicationDenied, Verdict: SafetyVerdict{DecisionID: decision.DecisionID, Status: SafetyInvariantViolation, EvaluatedAt: a.now()}}, ErrActionResultConflict
			}
			return publicationFromRecord(records[i]), nil
		}
	}
	// The Core snapshot provider cannot know the new decision's intention from
	// its target-only boundary. Recompute conflicts here against the exact
	// candidate being published so compatible authoritative executions do not
	// block one another and shadow/advisory records never become leases.
	snapshot.ConflictingDecisionIDs = activeDecisionConflicts(records, decision, a.now())
	verdict := a.kernel.ValidateDecision(ctx, decision, snapshot)
	publication := DecisionPublication{DecisionID: decision.DecisionID, Mode: a.mode, Status: DecisionPublicationDenied, Verdict: verdict}
	if verdict.Status == SafetyAllowed {
		if a.executionMode != "" {
			if a.executionMode == CGEExecutionDisabled && a.mode == AuthorityModeAuthoritative {
				publication.Verdict.Status = SafetyDenied
				publication.Verdict.Violations = []InvariantViolation{{Code: ErrExecutionDisabled.Error(), Detail: "execution is disabled"}}
			} else if a.executionMode == CGEExecutionLive {
				publication.Verdict.Status = SafetyDenied
				publication.Verdict.Violations = []InvariantViolation{{Code: ErrLiveExecutionUnavailable.Error(), Detail: "live execution is unavailable in this pass"}}
			} else {
				shouldPlan := a.executionMode == CGEExecutionDryRun && (a.mode == AuthorityModeAdvisory || a.mode == AuthorityModeAuthoritative || (a.mode == AuthorityModeShadow && a.executionDiagnostics))
				if shouldPlan {
					if a.governedPlanner == nil || a.planKernel == nil || a.planStore == nil {
						publication.Verdict.Status = SafetyDenied
						publication.Verdict.Violations = []InvariantViolation{{Code: ErrExecutionPlannerUnavailable.Error(), Detail: "dry-run planning boundary is not configured"}}
					} else {
						plan, planErr := a.governedPlanner.BuildPlan(ctx, decision, snapshot)
						publication.ExecutionPlan = cloneExecutionPlanPtr(&plan)
						var planPersistErr error
						if validateErr := plan.Validate(); validateErr == nil {
							// Denied/unsupported plans are diagnostic records too; they
							// remain dry-run and never create a lease.
							planPersistErr = a.planStore.PersistExecutionPlan(ctx, plan)
						}
						if planErr != nil {
							publication.Verdict.Status = SafetyDenied
							publication.Verdict.Violations = []InvariantViolation{{Code: planFailureCode(planErr), Detail: "execution plan was refused"}}
						} else if planVerdict := a.planKernel.ValidatePlan(ctx, decision, plan, snapshot); !planVerdict.Allowed {
							publication.Verdict.Status = SafetyDenied
							publication.Verdict.Violations = append([]InvariantViolation(nil), planVerdict.Violations...)
						} else if planPersistErr != nil {
							publication.Verdict.Status = SafetyDenied
							publication.Verdict.Violations = []InvariantViolation{{Code: "execution_plan_persist_failed", Detail: "execution plan persistence failed"}}
						} else if a.mode == AuthorityModeAuthoritative {
							publication.Status = DecisionPublishedAuthoritativeDryRun
						} else if a.mode == AuthorityModeAdvisory {
							publication.Status = DecisionPublishedAdvisoryDryRun
						} else if a.mode == AuthorityModeShadow {
							publication.Status = DecisionPublishedShadowDryRun
						}
					}
				} else if a.mode == AuthorityModeShadow {
					publication.Status = DecisionPublishedShadow
				} else if a.mode == AuthorityModeAdvisory {
					publication.Status = DecisionPublishedAdvisory
				}
			}
		} else {
			// Compatibility path for the pre-planner authority tests and callers.
			switch a.mode {
			case AuthorityModeShadow:
				publication.Status = DecisionPublishedShadow
			case AuthorityModeAdvisory:
				publication.Status = DecisionPublishedAdvisory
			case AuthorityModeAuthoritative:
				if a.planner == nil {
					publication.Verdict.Status = SafetyDenied
					publication.Verdict.Violations = []InvariantViolation{{Code: ErrExecutionPlannerUnavailable.Error(), Detail: "authoritative publication requires an explicit execution planner"}}
				} else {
					request, planErr := a.planner.PlanExecution(ctx, decision, snapshot)
					if planErr != nil {
						publication.Verdict.Status = SafetyDenied
						publication.Verdict.Violations = []InvariantViolation{{Code: "execution_planner_failed", Detail: planErr.Error()}}
					} else if err := request.Validate(); err != nil {
						publication.Verdict.Status = SafetyDenied
						publication.Verdict.Violations = []InvariantViolation{{Code: "execution_request_invalid", Detail: err.Error()}}
					} else if request.DecisionID != decision.DecisionID || request.IdempotencyKey != decision.IdempotencyKey {
						publication.Verdict.Status = SafetyDenied
						publication.Verdict.Violations = []InvariantViolation{{Code: "execution_request_identity_mismatch", Detail: "execution request identity does not match the decision"}}
					} else {
						publication.Status = DecisionPublishedAuthoritative
						publication.ExecutionRequest = &request
					}
				}
			}
		}
	}
	record := DecisionRecord{Envelope: decision, Mode: a.mode, Status: publication.Status, Verdict: publication.Verdict, PersistedAt: a.now(), ExecutionRequest: cloneExecutionRequest(publication.ExecutionRequest), ExecutionPlan: cloneExecutionPlanPtr(publication.ExecutionPlan)}
	if publication.ExecutionRequest != nil {
		request := publication.ExecutionRequest
		record.ExecutionLease = &ActiveExecutionLease{DecisionID: request.DecisionID, ActionRequestID: request.ActionRequestID, Target: request.Target, ExecutionType: request.ExecutionType, CreatedAt: request.CreatedAt, ValidUntil: request.ValidUntil, Status: ExecutionLeasePlanned}
	}
	if err := a.store.PersistDecision(ctx, record); err != nil {
		return publication, err
	}
	return publication, nil
}

func activeDecisionConflicts(records []DecisionRecord, decision DecisionEnvelope, now time.Time) []string {
	latest := make(map[string]DecisionRecord, len(records))
	for _, record := range records {
		latest[record.Envelope.DecisionID] = record
	}
	conflicts := make([]string, 0)
	for _, record := range latest {
		if record.Envelope.DecisionID == decision.DecisionID || record.Status != DecisionPublishedAuthoritative || record.ExecutionRequest == nil || record.ExecutionLease == nil {
			continue
		}
		lease := record.ExecutionLease
		if !now.Before(record.Envelope.ValidUntil) || !now.Before(lease.ValidUntil) || (lease.Status != ExecutionLeasePlanned && lease.Status != ExecutionLeaseDispatched && lease.Status != ExecutionLeaseRunning) {
			continue
		}
		if !decisionTargetsOverlap(lease.Target, decision.Target) || compatibleDecisionIntent(record.Envelope, decision) {
			continue
		}
		conflicts = append(conflicts, record.Envelope.DecisionID)
	}
	sort.Strings(conflicts)
	if len(conflicts) > 32 {
		conflicts = conflicts[:32]
	}
	return conflicts
}

func decisionTargetsOverlap(left, right DecisionTarget) bool {
	return left.Kind == DecisionTargetSystem || right.Kind == DecisionTargetSystem || left.equal(right)
}

func compatibleDecisionIntent(left, right DecisionEnvelope) bool {
	return left.DecisionType == right.DecisionType && left.DesiredState == right.DesiredState && equalStringSet(left.Constraints.ProposedActions, right.Constraints.ProposedActions)
}

func equalStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy, rightCopy := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	return reflect.DeepEqual(leftCopy, rightCopy)
}
func publicationFromRecord(record DecisionRecord) DecisionPublication {
	return DecisionPublication{DecisionID: record.Envelope.DecisionID, Mode: record.Mode, Status: record.Status, Verdict: record.Verdict, ExecutionRequest: cloneExecutionRequest(record.ExecutionRequest), ExecutionPlan: cloneExecutionPlanPtr(record.ExecutionPlan)}
}

func (a *DecisionAuthority) Decisions(ctx context.Context) ([]DecisionRecord, error) {
	if a == nil {
		return nil, ErrDecisionStore
	}
	return a.store.Decisions(ctx)
}

func (a *DecisionAuthority) ExecutionPlans(ctx context.Context) ([]ExecutionPlan, error) {
	if a == nil || a.planStore == nil {
		return nil, ErrExecutionPlanStore
	}
	return a.planStore.ExecutionPlans(ctx)
}

func (a *DecisionAuthority) ExecutionPlanComparisons(ctx context.Context) ([]ExecutionPlanComparison, error) {
	if a == nil || a.planStore == nil {
		return nil, ErrExecutionPlanStore
	}
	return a.planStore.ExecutionPlanComparisons(ctx)
}

func (a *DecisionAuthority) persistExecutionPlanComparison(ctx context.Context, comparison ExecutionPlanComparison) error {
	if a == nil || a.planStore == nil {
		return ErrExecutionPlanStore
	}
	return a.planStore.PersistExecutionPlanComparison(ctx, comparison)
}

// RecordActionResult accepts only feedback tied to a persisted decision. It
// records facts about an actually attempted execution; it never promotes or
// learns a chain by itself.
func (a *DecisionAuthority) RecordActionResult(ctx context.Context, result ActionResult) error {
	if a == nil {
		return ErrDecisionStore
	}
	if err := result.Validate(); err != nil {
		return err
	}
	records, err := a.store.Decisions(ctx)
	if err != nil {
		return err
	}
	var matched *DecisionRecord
	for index := len(records) - 1; index >= 0; index-- {
		record := records[index]
		if record.Envelope.DecisionID == result.DecisionID {
			copy := cloneDecisionRecord(record)
			matched = &copy
			break
		}
	}
	if matched == nil {
		return fmt.Errorf("%w: %s", ErrUnknownDecision, result.DecisionID)
	}
	if matched.Mode != AuthorityModeAuthoritative || matched.Status != DecisionPublishedAuthoritative || matched.ExecutionRequest == nil {
		return fmt.Errorf("%w: decision is not an executable authoritative record", ErrActionResultUnauthorized)
	}
	if matched.ExecutionRequest.ActionRequestID != result.ActionRequestID {
		return fmt.Errorf("%w: action request does not belong to decision", ErrActionResultUnauthorized)
	}
	if matched.ExecutionRequest.DecisionID != matched.Envelope.DecisionID || matched.ExecutionRequest.IdempotencyKey != matched.Envelope.IdempotencyKey {
		return fmt.Errorf("%w: persisted execution request does not belong to decision", ErrActionResultUnauthorized)
	}
	if result.Timestamp.Before(matched.ExecutionRequest.CreatedAt) || result.Timestamp.After(matched.ExecutionRequest.ValidUntil) || result.Timestamp.After(matched.Envelope.ValidUntil) {
		return fmt.Errorf("%w: action result outside request validity", ErrActionResultUnauthorized)
	}
	previous, err := a.store.ActionResults(ctx)
	if err != nil {
		return err
	}
	for _, existing := range previous {
		if existing.DecisionID == result.DecisionID && existing.ActionRequestID == result.ActionRequestID {
			if reflect.DeepEqual(existing, result) {
				return nil
			}
			return ErrActionResultConflict
		}
	}
	if err := a.store.PersistActionResult(ctx, result); err != nil {
		return err
	}
	if matched.ExecutionLease != nil {
		updated := cloneDecisionRecord(*matched)
		if result.Status == ActionResultSucceeded {
			updated.ExecutionLease.Status = ExecutionLeaseCompleted
		} else {
			updated.ExecutionLease.Status = ExecutionLeaseFailed
		}
		// Lease transitions are append-only records. The prior immutable
		// publication remains available for audit, while the latest record
		// closes its conflict lease after feedback.
		if err := a.store.PersistDecision(ctx, updated); err != nil {
			return err
		}
	}
	return nil
}

func (a *DecisionAuthority) ActionResults(ctx context.Context) ([]ActionResult, error) {
	if a == nil {
		return nil, ErrDecisionStore
	}
	return a.store.ActionResults(ctx)
}

func cloneDecisionRecord(record DecisionRecord) DecisionRecord {
	result := record
	result.Envelope.EvidenceRefs = append([]string(nil), record.Envelope.EvidenceRefs...)
	result.Envelope.Constraints.RequiredInvariantRefs = append([]string(nil), record.Envelope.Constraints.RequiredInvariantRefs...)
	result.Envelope.Constraints.ProposedActions = append([]string(nil), record.Envelope.Constraints.ProposedActions...)
	result.Envelope.Constraints.ForbiddenActions = append([]string(nil), record.Envelope.Constraints.ForbiddenActions...)
	result.Verdict.Violations = append([]InvariantViolation(nil), record.Verdict.Violations...)
	if record.Envelope.CriticalChainRef != nil {
		value := *record.Envelope.CriticalChainRef
		result.Envelope.CriticalChainRef = &value
	}
	if record.Envelope.LearnedChainRef != nil {
		value := *record.Envelope.LearnedChainRef
		result.Envelope.LearnedChainRef = &value
	}
	result.ExecutionRequest = cloneExecutionRequest(record.ExecutionRequest)
	result.ExecutionPlan = cloneExecutionPlanPtr(record.ExecutionPlan)
	if record.ExecutionLease != nil {
		lease := *record.ExecutionLease
		result.ExecutionLease = &lease
	}
	return result
}

func cloneExecutionRequest(request *ExecutionRequest) *ExecutionRequest {
	if request == nil {
		return nil
	}
	copy := *request
	return &copy
}

func cloneExecutionPlanPtr(plan *ExecutionPlan) *ExecutionPlan {
	if plan == nil {
		return nil
	}
	copy := *plan
	copy.Actions = append([]PlannedAction(nil), plan.Actions...)
	copy.FailureCodes = append([]string(nil), plan.FailureCodes...)
	return &copy
}

func planFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrUnsupportedIntent):
		return ErrUnsupportedIntent.Error()
	case errors.Is(err, ErrAmbiguousExecutionTarget):
		return ErrAmbiguousExecutionTarget.Error()
	case errors.Is(err, ErrPolicyUnavailable):
		return ErrPolicyUnavailable.Error()
	case errors.Is(err, ErrAuthorizationUnknown):
		return ErrAuthorizationUnknown.Error()
	case errors.Is(err, ErrPhysicalLimitUnknown):
		return ErrPhysicalLimitUnknown.Error()
	case errors.Is(err, ErrInvalidExecutionParameters):
		return ErrInvalidExecutionParameters.Error()
	default:
		return "execution_plan_invalid"
	}
}

func validateAuthorityText(value, field string, max int, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if strings.TrimSpace(value) != value || len([]rune(value)) > max || strings.ContainsAny(value, "\r\n") || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s is invalid", field)
	}
	return nil
}

func knownInvariantReference(value string) bool {
	switch value {
	case "safety.contract_valid", "safety.fresh_context", "safety.target_exists", "safety.authorization", "safety.physical_limits", "safety.idempotence", "safety.no_conflict", "safety.expiration", "safety.authority_mode":
		return true
	default:
		return false
	}
}
