package cge

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"synora/pkg/contract"
)

const (
	ExecutionPlanSchemaVersion           = "synora.cge.execution-plan.v1"
	ExecutionPlanComparisonSchemaVersion = "synora.cge.execution-plan-comparison.v1"
	maxExecutionActions                  = 16
	maxExecutionLine                     = 2 * 1024 * 1024
)

// CGEExecutionMode is deliberately separate from AuthorityMode. Authority
// controls who may publish a decision; this mode controls whether a plan may
// be built, and never grants a dispatcher capability.
type CGEExecutionMode string

const (
	CGEExecutionDisabled CGEExecutionMode = "disabled"
	CGEExecutionDryRun   CGEExecutionMode = "dry_run"
	CGEExecutionLive     CGEExecutionMode = "live"
)

var (
	ErrInvalidCGEExecutionMode          = errors.New("invalid_cge_execution_mode")
	ErrExecutionDisabled                = errors.New("execution_disabled")
	ErrLiveExecutionUnavailable         = errors.New("live_execution_unavailable")
	ErrUnsupportedIntent                = errors.New("unsupported_intent")
	ErrAmbiguousExecutionTarget         = errors.New("ambiguous_target")
	ErrPolicyUnavailable                = errors.New("policy_unavailable")
	ErrAuthorizationUnknown             = errors.New("authorization_unknown")
	ErrPhysicalLimitUnknown             = errors.New("physical_limit_unknown")
	ErrInvalidExecutionParameters       = errors.New("invalid_parameters")
	ErrOperationalCapabilityUnavailable = errors.New("capability_unavailable")
	ErrExecutionPlanStore               = errors.New("execution_plan_store_error")
)

func (m CGEExecutionMode) Validate() error {
	switch m {
	case CGEExecutionDisabled, CGEExecutionDryRun, CGEExecutionLive:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidCGEExecutionMode, m)
	}
}

func ParseCGEExecutionMode(value string) (CGEExecutionMode, error) {
	mode := CGEExecutionMode(strings.TrimSpace(value))
	if mode == "" {
		return CGEExecutionDisabled, nil
	}
	if err := mode.Validate(); err != nil {
		return "", err
	}
	return mode, nil
}

type ExecutionPlanStatus string

const (
	ExecutionPlanPlanned     ExecutionPlanStatus = "planned"
	ExecutionPlanDenied      ExecutionPlanStatus = "denied"
	ExecutionPlanUnsupported ExecutionPlanStatus = "unsupported"
	ExecutionPlanAmbiguous   ExecutionPlanStatus = "ambiguous"
	ExecutionPlanStale       ExecutionPlanStatus = "stale"
	ExecutionPlanInvalid     ExecutionPlanStatus = "invalid"
)

func (s ExecutionPlanStatus) Validate() error {
	switch s {
	case ExecutionPlanPlanned, ExecutionPlanDenied, ExecutionPlanUnsupported, ExecutionPlanAmbiguous, ExecutionPlanStale, ExecutionPlanInvalid:
		return nil
	default:
		return fmt.Errorf("invalid execution plan status %q", s)
	}
}

type PlannedActionRequirement string

const (
	PlannedActionRequired PlannedActionRequirement = "required"
	PlannedActionOptional PlannedActionRequirement = "optional"
)

func (r PlannedActionRequirement) Validate() error {
	if r != PlannedActionRequired && r != PlannedActionOptional {
		return fmt.Errorf("invalid planned action requirement %q", r)
	}
	return nil
}

type PlannedAction struct {
	PlannedActionID    string                   `json:"planned_action_id" yaml:"planned_action_id"`
	IntentID           string                   `json:"intent_id" yaml:"intent_id"`
	ActionType         string                   `json:"action_type" yaml:"action_type"`
	Target             DecisionTarget           `json:"target" yaml:"target"`
	Priority           int                      `json:"priority" yaml:"priority"`
	RequestFingerprint string                   `json:"request_fingerprint" yaml:"request_fingerprint"`
	Requirement        PlannedActionRequirement `json:"requirement" yaml:"requirement"`
}

func (a PlannedAction) Validate() error {
	for name, value := range map[string]string{"planned action id": a.PlannedActionID, "intent id": a.IntentID, "action type": a.ActionType, "request fingerprint": a.RequestFingerprint} {
		if err := validateAuthorityText(value, name, 256, true); err != nil {
			return err
		}
	}
	if err := a.Target.Validate(); err != nil {
		return err
	}
	if a.Priority < 0 || a.Priority > 100 {
		return fmt.Errorf("planned action priority is out of bounds")
	}
	return a.Requirement.Validate()
}

type ExecutionPlan struct {
	SchemaVersion    string              `json:"schema_version" yaml:"schema_version"`
	PlanID           string              `json:"plan_id" yaml:"plan_id"`
	DecisionID       string              `json:"decision_id" yaml:"decision_id"`
	SituationID      string              `json:"situation_id" yaml:"situation_id"`
	StateRevision    uint64              `json:"state_revision" yaml:"state_revision"`
	PolicyRevision   uint64              `json:"policy_revision" yaml:"policy_revision"`
	Actions          []PlannedAction     `json:"actions" yaml:"actions"`
	Status           ExecutionPlanStatus `json:"status" yaml:"status"`
	DryRun           bool                `json:"dry_run" yaml:"dry_run"`
	DispatchEligible bool                `json:"dispatch_eligible" yaml:"dispatch_eligible"`
	CreatedAt        time.Time           `json:"created_at" yaml:"created_at"`
	ValidUntil       time.Time           `json:"valid_until" yaml:"valid_until"`
	IdempotencyKey   string              `json:"idempotency_key" yaml:"idempotency_key"`
	FailureCodes     []string            `json:"failure_codes,omitempty" yaml:"failure_codes,omitempty"`
}

func (p ExecutionPlan) Validate() error {
	if p.SchemaVersion != ExecutionPlanSchemaVersion {
		return fmt.Errorf("invalid execution plan schema version %q", p.SchemaVersion)
	}
	for name, value := range map[string]string{"plan id": p.PlanID, "decision id": p.DecisionID, "situation id": p.SituationID, "idempotency key": p.IdempotencyKey} {
		if err := validateAuthorityText(value, name, 256, true); err != nil {
			return err
		}
	}
	if p.StateRevision == 0 || p.CreatedAt.IsZero() || !p.ValidUntil.After(p.CreatedAt) {
		return fmt.Errorf("invalid execution plan revision or validity")
	}
	if err := p.Status.Validate(); err != nil {
		return err
	}
	if len(p.Actions) > maxExecutionActions {
		return fmt.Errorf("too many planned actions")
	}
	if !p.DryRun || p.DispatchEligible {
		return fmt.Errorf("execution plan is not fail-closed dry-run")
	}
	seen := make(map[string]struct{}, len(p.Actions))
	for _, action := range p.Actions {
		if err := action.Validate(); err != nil {
			return err
		}
		switch action.ActionType {
		case "record.clip", "device.command", "notify", "light.on", "siren", "mark_security_degraded":
		default:
			return fmt.Errorf("action type is not allowlisted: %s", action.ActionType)
		}
		if _, ok := seen[action.PlannedActionID]; ok {
			return fmt.Errorf("duplicate planned action")
		}
		seen[action.PlannedActionID] = struct{}{}
	}
	for _, code := range p.FailureCodes {
		if err := validateAuthorityText(code, "execution plan failure code", 128, true); err != nil {
			return err
		}
	}
	return nil
}

type GovernedExecutionPlanner interface {
	BuildPlan(context.Context, DecisionEnvelope, OperationalSnapshot) (ExecutionPlan, error)
}

type ExecutionPlanSafetyKernel interface {
	ValidatePlan(context.Context, DecisionEnvelope, ExecutionPlan, OperationalSnapshot) ExecutionPlanVerdict
}

type ExecutionPlanVerdict struct {
	PlanID      string               `json:"plan_id" yaml:"plan_id"`
	Allowed     bool                 `json:"allowed" yaml:"allowed"`
	Violations  []InvariantViolation `json:"violations,omitempty" yaml:"violations,omitempty"`
	EvaluatedAt time.Time            `json:"evaluated_at" yaml:"evaluated_at"`
}

// HistoricalActionRequest is the redacted pre-dispatch observation copied
// from automation.EvaluateRequests. It intentionally has no dynamic payload.
type HistoricalActionRequest struct {
	ID                 string    `json:"id" yaml:"id"`
	ActionType         string    `json:"action_type" yaml:"action_type"`
	Target             string    `json:"target" yaml:"target"`
	Priority           int       `json:"priority" yaml:"priority"`
	RequestFingerprint string    `json:"request_fingerprint" yaml:"request_fingerprint"`
	CreatedAt          time.Time `json:"created_at" yaml:"created_at"`
	PolicyResult       string    `json:"policy_result" yaml:"policy_result"`
}

// HistoricalActionRequestFromContract takes a copy before dispatch and keeps
// only the fields needed for order-independent comparison. The dynamic legacy
// payload is hashed and never crosses or enters the diagnostic store.
func HistoricalActionRequestFromContract(request contract.ActionRequest, priority int, policyResult string) HistoricalActionRequest {
	actionType := strings.TrimSpace(request.Type)
	if actionType == "" {
		actionType = strings.TrimSpace(request.Action.Type)
	}
	target := strings.TrimSpace(request.Target)
	if target == "" {
		target = strings.TrimSpace(request.Action.Device)
		if target == "" {
			target = strings.TrimSpace(request.Action.Channel)
		}
	}
	parameters, _ := json.Marshal(struct {
		Data   map[string]any  `json:"data,omitempty"`
		Action contract.Action `json:"action"`
	}{Data: request.Data, Action: request.Action})
	return HistoricalActionRequest{ID: request.ID, ActionType: actionType, Target: target, Priority: priority, RequestFingerprint: digest("historical-request", actionType, target, fmt.Sprint(priority), string(parameters)), CreatedAt: request.CreatedAt, PolicyResult: policyResult}
}

type ExecutionPlanComparison struct {
	SchemaVersion           string    `json:"schema_version" yaml:"schema_version"`
	EventID                 string    `json:"event_id" yaml:"event_id"`
	DecisionID              string    `json:"decision_id" yaml:"decision_id"`
	PlanID                  string    `json:"plan_id" yaml:"plan_id"`
	HistoricalActionIDs     []string  `json:"historical_action_ids" yaml:"historical_action_ids"`
	CognitiveActionIDs      []string  `json:"cognitive_action_ids" yaml:"cognitive_action_ids"`
	ExactMatch              bool      `json:"exact_match" yaml:"exact_match"`
	SameActionTypes         bool      `json:"same_action_types" yaml:"same_action_types"`
	SameTargets             bool      `json:"same_targets" yaml:"same_targets"`
	SamePriorities          bool      `json:"same_priorities" yaml:"same_priorities"`
	SameParameters          bool      `json:"same_parameters" yaml:"same_parameters"`
	MissingCognitiveActions []string  `json:"missing_cognitive_actions,omitempty" yaml:"missing_cognitive_actions,omitempty"`
	ExtraCognitiveActions   []string  `json:"extra_cognitive_actions,omitempty" yaml:"extra_cognitive_actions,omitempty"`
	DivergenceCodes         []string  `json:"divergence_codes,omitempty" yaml:"divergence_codes,omitempty"`
	ComparedAt              time.Time `json:"compared_at" yaml:"compared_at"`
}

func (c ExecutionPlanComparison) Validate() error {
	if c.SchemaVersion != ExecutionPlanComparisonSchemaVersion || c.EventID == "" || c.DecisionID == "" || c.PlanID == "" || c.ComparedAt.IsZero() {
		return ErrExecutionPlanStore
	}
	if len(c.HistoricalActionIDs) > maxExecutionActions || len(c.CognitiveActionIDs) > maxExecutionActions || len(c.DivergenceCodes) > 32 {
		return ErrExecutionPlanStore
	}
	return nil
}

// DefaultGovernedExecutionPlanner performs only closed normalization and
// fingerprinting. It never imports automation or the action dispatcher.
type DefaultGovernedExecutionPlanner struct {
	Now func() time.Time
}

func (p DefaultGovernedExecutionPlanner) BuildPlan(ctx context.Context, decision DecisionEnvelope, snapshot OperationalSnapshot) (ExecutionPlan, error) {
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	base := ExecutionPlan{SchemaVersion: ExecutionPlanSchemaVersion, DecisionID: decision.DecisionID, SituationID: decision.SituationID, StateRevision: snapshot.Revision, PolicyRevision: snapshot.PolicyRevision, Status: ExecutionPlanInvalid, DryRun: true, DispatchEligible: false, CreatedAt: decision.CreatedAt, ValidUntil: decision.ValidUntil}
	fail := func(status ExecutionPlanStatus, code error) (ExecutionPlan, error) {
		base.Status = status
		base.FailureCodes = []string{code.Error()}
		base.PlanID = digest("plan", decision.DecisionID, decision.SituationID, fmt.Sprint(snapshot.Revision), fmt.Sprint(snapshot.PolicyRevision), code.Error())
		base.IdempotencyKey = digest("plan-idempotency", base.PlanID)
		return base, code
	}
	if err := decision.ValidateAt(now); err != nil {
		return fail(ExecutionPlanInvalid, err)
	}
	if snapshot.CapturedAt.IsZero() || snapshot.FreshUntil.IsZero() || now.After(snapshot.FreshUntil) || snapshot.Revision == 0 {
		return fail(ExecutionPlanStale, fmt.Errorf("%w: operational snapshot is stale", ErrExecutionPlanStore))
	}
	if snapshot.PolicyRevision == 0 {
		return fail(ExecutionPlanDenied, ErrPolicyUnavailable)
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return fail(ExecutionPlanInvalid, ctx.Err())
		default:
		}
	}
	for _, intent := range decision.Constraints.ProposedActions {
		if forbiddenIntent(decision.Constraints.ForbiddenActions, intent) {
			return fail(ExecutionPlanDenied, fmt.Errorf("forbidden_action"))
		}
		actions, err := normalizePlannedActions(decision, snapshot, intent)
		if err != nil {
			return fail(planStatusForError(err), err)
		}
		if len(base.Actions)+len(actions) > maxExecutionActions {
			return fail(ExecutionPlanInvalid, fmt.Errorf("too many planned actions"))
		}
		base.Actions = append(base.Actions, actions...)
	}
	sort.Slice(base.Actions, func(i, j int) bool {
		if base.Actions[i].IntentID != base.Actions[j].IntentID {
			return base.Actions[i].IntentID < base.Actions[j].IntentID
		}
		if base.Actions[i].Target.Kind != base.Actions[j].Target.Kind {
			return base.Actions[i].Target.Kind < base.Actions[j].Target.Kind
		}
		return base.Actions[i].Target.ID < base.Actions[j].Target.ID
	})
	base.Status = ExecutionPlanPlanned
	base.PlanID = digest("plan", decision.DecisionID, decision.SituationID, fmt.Sprint(snapshot.Revision), fmt.Sprint(snapshot.PolicyRevision), actionDigest(base.Actions))
	base.IdempotencyKey = digest("plan-idempotency", decision.IdempotencyKey, fmt.Sprint(snapshot.Revision), fmt.Sprint(snapshot.PolicyRevision), actionDigest(base.Actions))
	return base, nil
}

func planStatusForError(err error) ExecutionPlanStatus {
	switch {
	case errors.Is(err, ErrUnsupportedIntent):
		return ExecutionPlanUnsupported
	case errors.Is(err, ErrOperationalCapabilityUnavailable):
		return ExecutionPlanUnsupported
	case errors.Is(err, ErrAmbiguousExecutionTarget):
		return ExecutionPlanAmbiguous
	case errors.Is(err, ErrPhysicalLimitUnknown), errors.Is(err, ErrAuthorizationUnknown):
		return ExecutionPlanDenied
	default:
		return ExecutionPlanInvalid
	}
}

func normalizeIntent(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, ".", "_")
	return value
}

func forbiddenIntent(forbidden []string, intent string) bool {
	want := normalizeIntent(intent)
	for _, value := range forbidden {
		if normalizeIntent(value) == want {
			return true
		}
	}
	return false
}

type normalizedHistoricalAction struct{ Type, Target, Parameters string }

func normalizePlannedActions(decision DecisionEnvelope, snapshot OperationalSnapshot, intent string) ([]PlannedAction, error) {
	normalized := normalizeIntent(intent)
	candidates, err := operationalTargetsForIntent(decision, snapshot, normalized)
	if err != nil {
		return nil, err
	}
	result := make([]PlannedAction, 0, len(candidates))
	for _, candidate := range candidates {
		action, err := normalizePlannedAction(decision, candidate, normalized)
		if err != nil {
			return nil, err
		}
		result = append(result, action)
	}
	return result, nil
}

func normalizePlannedAction(decision DecisionEnvelope, operational OperationalTarget, normalized string) (PlannedAction, error) {
	target := operational.Target
	requireAuth, requirePhysical := false, false
	var historical normalizedHistoricalAction
	switch normalized {
	case "record_clip":
		if target.Kind != DecisionTargetDevice || !hasOperationalCapability(operational, normalized) {
			return PlannedAction{}, ErrOperationalCapabilityUnavailable
		}
		historical = normalizedHistoricalAction{Type: "record.clip", Target: target.ID}
		requireAuth = true
	case "increase_tracking_frequency":
		if !hasOperationalCapability(operational, normalized) {
			return PlannedAction{}, ErrOperationalCapabilityUnavailable
		}
		historical = normalizedHistoricalAction{Type: "device.command", Target: target.ID, Parameters: "command=increase_tracking_frequency"}
		requireAuth, requirePhysical = true, true
	case "notify_user", "notify_user_high_priority", "notify_user_critical":
		if target.Kind != DecisionTargetSystem && target.Kind != DecisionTargetResident {
			return PlannedAction{}, fmt.Errorf("%w: notification recipient required", ErrAmbiguousExecutionTarget)
		}
		historical = normalizedHistoricalAction{Type: "notify", Target: target.ID, Parameters: "intent=" + normalized}
		requireAuth = true
	case "turn_on_relevant_lights", "turn_on_security_lights":
		if !hasOperationalCapability(operational, normalized) {
			return PlannedAction{}, ErrOperationalCapabilityUnavailable
		}
		historical = normalizedHistoricalAction{Type: "light.on", Target: target.ID, Parameters: "command=on;intent=" + normalized}
		requireAuth, requirePhysical = true, true
	case "trigger_approved_alarm_policy":
		if !operational.Authorization.Known || operational.Authorization.PolicyID == "" {
			return PlannedAction{}, ErrPolicyUnavailable
		}
		historical = normalizedHistoricalAction{Type: "siren", Target: target.ID, Parameters: "policy_id=" + operational.Authorization.PolicyID}
		requireAuth, requirePhysical = true, true
	case "mark_security_degraded":
		if target.Kind != DecisionTargetSystem {
			return PlannedAction{}, fmt.Errorf("%w: system target required", ErrAmbiguousExecutionTarget)
		}
		historical = normalizedHistoricalAction{Type: "mark_security_degraded", Target: target.ID}
		requireAuth = true
	default:
		return PlannedAction{}, fmt.Errorf("%w: %s", ErrUnsupportedIntent, normalized)
	}
	if decision.Constraints.RequiresAuthorization || requireAuth {
		if !operational.Authorization.Known {
			return PlannedAction{}, ErrAuthorizationUnknown
		}
		if !operational.Authorization.Authorized {
			return PlannedAction{}, fmt.Errorf("%w: authorization denied", ErrAuthorizationUnknown)
		}
	}
	if decision.Constraints.RequiresPhysicalLimit || requirePhysical {
		if !operational.PhysicalLimits.Known {
			return PlannedAction{}, ErrPhysicalLimitUnknown
		}
		if operational.PhysicalLimits.MaxValue > 0 && decision.Priority > operational.PhysicalLimits.MaxValue {
			return PlannedAction{}, ErrInvalidExecutionParameters
		}
	}
	if decision.Constraints.MaxPriority > 0 && decision.Priority > decision.Constraints.MaxPriority {
		return PlannedAction{}, ErrInvalidExecutionParameters
	}
	fingerprint := digest("request", historical.Type, historical.Target, fmt.Sprint(decision.Priority), historical.Parameters)
	return PlannedAction{PlannedActionID: digest("planned-action", decision.DecisionID, normalized, fingerprint), IntentID: normalized, ActionType: historical.Type, Target: target, Priority: decision.Priority, RequestFingerprint: fingerprint, Requirement: PlannedActionRequired}, nil
}

func operationalTargetsForIntent(decision DecisionEnvelope, snapshot OperationalSnapshot, intent string) ([]OperationalTarget, error) {
	normalized := normalizeIntent(intent)
	switch normalized {
	case "record_clip", "increase_tracking_frequency", "notify_user", "notify_user_high_priority", "notify_user_critical", "turn_on_relevant_lights", "turn_on_security_lights", "trigger_approved_alarm_policy", "mark_security_degraded":
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedIntent, normalized)
	}
	if normalized == "notify_user" || normalized == "notify_user_high_priority" || normalized == "notify_user_critical" || normalized == "mark_security_degraded" {
		target, ok := operationalTarget(snapshot, decision.Target)
		if !ok || !target.Exists {
			return nil, fmt.Errorf("%w: target is not resolved", ErrAmbiguousExecutionTarget)
		}
		if normalized == "mark_security_degraded" && target.Target.Kind != DecisionTargetSystem {
			return nil, fmt.Errorf("%w: system target required", ErrAmbiguousExecutionTarget)
		}
		if normalized != "mark_security_degraded" && target.Target.Kind != DecisionTargetSystem && target.Target.Kind != DecisionTargetResident {
			return nil, fmt.Errorf("%w: notification recipient required", ErrAmbiguousExecutionTarget)
		}
		return []OperationalTarget{target}, nil
	}
	if normalized == "trigger_approved_alarm_policy" {
		if decision.Target.Kind != DecisionTargetSystem {
			return nil, fmt.Errorf("%w: alarm system target required", ErrAmbiguousExecutionTarget)
		}
		policyTarget, ok := operationalTarget(snapshot, decision.Target)
		if !ok || !policyTarget.Exists {
			return nil, fmt.Errorf("%w: target is not resolved", ErrAmbiguousExecutionTarget)
		}
		if !policyTarget.Authorization.Known || policyTarget.Authorization.PolicyID == "" {
			return nil, ErrPolicyUnavailable
		}
		// The approved system policy is the authority for the alarm action; the
		// physical siren targets are selected from the operational capability
		// inventory when one is available.
		candidates := capabilityCandidates(snapshot, decision.Target, normalized)
		if len(candidates) == 0 {
			return nil, ErrOperationalCapabilityUnavailable
		}
		return candidates, nil
	}
	if normalized == "record_clip" && decision.Target.Kind != DecisionTargetDevice && decision.Target.Kind != DecisionTargetNode && decision.Target.Kind != DecisionTargetZone && decision.Target.Kind != DecisionTargetSystem {
		return nil, fmt.Errorf("%w: camera scope required", ErrAmbiguousExecutionTarget)
	}
	if (normalized == "turn_on_relevant_lights" || normalized == "turn_on_security_lights") && decision.Target.Kind != DecisionTargetNode && decision.Target.Kind != DecisionTargetZone && decision.Target.Kind != DecisionTargetSystem {
		return nil, fmt.Errorf("%w: light zone required", ErrAmbiguousExecutionTarget)
	}
	if normalized == "increase_tracking_frequency" && decision.Target.Kind != DecisionTargetDevice && decision.Target.Kind != DecisionTargetNode && decision.Target.Kind != DecisionTargetZone && decision.Target.Kind != DecisionTargetSystem {
		return nil, fmt.Errorf("%w: tracking scope required", ErrAmbiguousExecutionTarget)
	}
	if decision.Target.Kind != DecisionTargetSystem {
		if target, ok := operationalTarget(snapshot, decision.Target); !ok || !target.Exists {
			return nil, fmt.Errorf("%w: target is not resolved", ErrAmbiguousExecutionTarget)
		}
	}
	candidates := capabilityCandidates(snapshot, decision.Target, normalized)
	if len(candidates) == 0 {
		return nil, ErrOperationalCapabilityUnavailable
	}
	return candidates, nil
}

func capabilityCandidates(snapshot OperationalSnapshot, requested DecisionTarget, intent string) []OperationalTarget {
	result := make([]OperationalTarget, 0)
	seen := make(map[string]struct{})
	for _, candidate := range snapshot.Targets {
		if !candidate.Exists || candidate.Target.Kind != DecisionTargetDevice || !candidateMatchesScope(candidate, requested) || !hasOperationalCapability(candidate, intent) {
			continue
		}
		key := string(candidate.Target.Kind) + "\x00" + candidate.Target.ID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Target.ID < result[j].Target.ID })
	return result
}

func candidateMatchesScope(candidate OperationalTarget, requested DecisionTarget) bool {
	switch requested.Kind {
	case DecisionTargetSystem:
		return true
	case DecisionTargetDevice:
		return candidate.Target.equal(requested)
	case DecisionTargetNode:
		return candidate.NodeID == requested.ID
	case DecisionTargetZone:
		return candidate.ZoneID == requested.ID || candidate.NodeID == requested.ID
	default:
		return false
	}
}

func hasOperationalCapability(target OperationalTarget, intent string) bool {
	want := normalizeIntent(intent)
	for _, value := range target.Capabilities {
		capability := normalizeIntent(value)
		switch want {
		case "record_clip":
			if capability == "record_clip" || capability == "recording" || capability == "video_recording" || capability == "record" {
				return true
			}
		case "increase_tracking_frequency":
			if capability == "tracking" || capability == "tracking_frequency" || capability == "motion_detection" {
				return true
			}
		case "turn_on_relevant_lights", "turn_on_security_lights":
			if capability == "light" || capability == "lighting" || capability == "light_on" {
				return true
			}
		case "trigger_approved_alarm_policy":
			if capability == "siren" || capability == "alarm" || capability == "alarm_siren" {
				return true
			}
		}
	}
	return false
}

func operationalTarget(snapshot OperationalSnapshot, target DecisionTarget) (OperationalTarget, bool) {
	for _, item := range snapshot.Targets {
		if item.Target.equal(target) {
			return item, true
		}
	}
	return OperationalTarget{}, false
}

func digest(prefix string, values ...string) string {
	joined := prefix
	for _, value := range values {
		joined += "\x00" + value
	}
	sum := sha256.Sum256([]byte(joined))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func actionDigest(actions []PlannedAction) string {
	copyActions := append([]PlannedAction(nil), actions...)
	sort.Slice(copyActions, func(i, j int) bool { return copyActions[i].IntentID < copyActions[j].IntentID })
	payload, _ := json.Marshal(copyActions)
	return string(payload)
}

type DefaultExecutionPlanSafetyKernel struct{ Now func() time.Time }

func (k DefaultExecutionPlanSafetyKernel) ValidatePlan(ctx context.Context, decision DecisionEnvelope, plan ExecutionPlan, snapshot OperationalSnapshot) ExecutionPlanVerdict {
	now := time.Now().UTC()
	if k.Now != nil {
		now = k.Now().UTC()
	}
	verdict := ExecutionPlanVerdict{PlanID: plan.PlanID, Allowed: false, EvaluatedAt: now}
	violate := func(code, detail string) ExecutionPlanVerdict {
		verdict.Violations = []InvariantViolation{{Code: code, Detail: detail}}
		return verdict
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return violate("context_cancelled", "execution plan context cancelled")
		default:
		}
	}
	if err := decision.ValidateAt(now); err != nil {
		return violate("decision_invalid", err.Error())
	}
	if err := plan.Validate(); err != nil {
		return violate("plan_invalid", err.Error())
	}
	if plan.DecisionID != decision.DecisionID || plan.SituationID != decision.SituationID {
		return violate("decision_mismatch", "plan does not belong to decision")
	}
	if plan.StateRevision != snapshot.Revision {
		return violate("state_revision_mismatch", "plan state revision is stale")
	}
	if plan.PolicyRevision != snapshot.PolicyRevision {
		return violate("policy_revision_mismatch", "plan policy revision is stale")
	}
	if now.After(plan.ValidUntil) {
		return violate("plan_expired", "plan validity has elapsed")
	}
	if now.Before(plan.CreatedAt) {
		return violate("plan_time_invalid", "plan was created in the future")
	}
	if snapshot.CapturedAt.IsZero() || snapshot.FreshUntil.IsZero() || now.After(snapshot.FreshUntil) {
		return violate("stale_context", "operational snapshot is stale")
	}
	if len(snapshot.ConflictingDecisionIDs) > 0 {
		return violate("authoritative_conflict", "authoritative execution conflict exists")
	}
	seen := make(map[string]struct{}, len(plan.Actions))
	for _, action := range plan.Actions {
		if _, ok := seen[action.RequestFingerprint]; ok {
			return violate("duplicate_action", "duplicate action fingerprint")
		}
		seen[action.RequestFingerprint] = struct{}{}
		if forbiddenIntent(decision.Constraints.ForbiddenActions, action.IntentID) {
			return violate("forbidden_action", action.IntentID)
		}
		if _, ok := operationalTarget(snapshot, action.Target); !ok {
			return violate("target_unresolved", action.IntentID)
		}
		operational, _ := operationalTarget(snapshot, action.Target)
		if intentRequiresOperationalCapability(action.IntentID) && !hasOperationalCapability(operational, action.IntentID) {
			return violate("capability_unavailable", action.IntentID)
		}
		if intentRequiresAuthorization(action.IntentID) && (!operational.Authorization.Known || !operational.Authorization.Authorized) {
			return violate("authorization_unknown", action.IntentID)
		}
		if intentRequiresPhysicalLimit(action.IntentID) && !operational.PhysicalLimits.Known {
			return violate("physical_limit_unknown", action.IntentID)
		}
		if action.IntentID == "trigger_approved_alarm_policy" && operational.Authorization.PolicyID == "" {
			return violate("policy_unavailable", action.IntentID)
		}
		if action.Priority > 100 || (decision.Constraints.MaxPriority > 0 && action.Priority > decision.Constraints.MaxPriority) {
			return violate("priority_invalid", action.IntentID)
		}
	}
	verdict.Allowed = true
	return verdict
}

func intentRequiresAuthorization(intent string) bool {
	switch normalizeIntent(intent) {
	case "record_clip", "increase_tracking_frequency", "notify_user", "notify_user_high_priority", "notify_user_critical", "turn_on_relevant_lights", "turn_on_security_lights", "trigger_approved_alarm_policy", "mark_security_degraded":
		return true
	default:
		return false
	}
}

func intentRequiresPhysicalLimit(intent string) bool {
	switch normalizeIntent(intent) {
	case "increase_tracking_frequency", "turn_on_relevant_lights", "turn_on_security_lights", "trigger_approved_alarm_policy":
		return true
	default:
		return false
	}
}

func intentRequiresOperationalCapability(intent string) bool {
	switch normalizeIntent(intent) {
	case "record_clip", "increase_tracking_frequency", "turn_on_relevant_lights", "turn_on_security_lights", "trigger_approved_alarm_policy":
		return true
	default:
		return false
	}
}

func NormalizedHistoricalRequests(plan ExecutionPlan) []HistoricalActionRequest {
	result := make([]HistoricalActionRequest, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		result = append(result, HistoricalActionRequest{ID: action.PlannedActionID, ActionType: action.ActionType, Target: action.Target.ID, Priority: action.Priority, RequestFingerprint: action.RequestFingerprint, CreatedAt: plan.CreatedAt, PolicyResult: "allowlisted"})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RequestFingerprint < result[j].RequestFingerprint })
	return result
}

func CompareExecutionPlan(eventID string, plan ExecutionPlan, historical []HistoricalActionRequest, comparedAt time.Time) (ExecutionPlanComparison, error) {
	if err := plan.Validate(); err != nil {
		return ExecutionPlanComparison{}, err
	}
	c := ExecutionPlanComparison{SchemaVersion: ExecutionPlanComparisonSchemaVersion, EventID: eventID, DecisionID: plan.DecisionID, PlanID: plan.PlanID, ComparedAt: comparedAt.UTC()}
	cognitive := NormalizedHistoricalRequests(plan)
	c.HistoricalActionIDs = make([]string, 0, len(historical))
	c.CognitiveActionIDs = make([]string, 0, len(cognitive))
	for _, item := range historical {
		c.HistoricalActionIDs = append(c.HistoricalActionIDs, item.ID)
	}
	for _, item := range cognitive {
		c.CognitiveActionIDs = append(c.CognitiveActionIDs, item.ID)
	}
	hist := make(map[string]int)
	cog := make(map[string]int)
	histTypes := make(map[string]int)
	cogTypes := make(map[string]int)
	histTargets := make(map[string]int)
	cogTargets := make(map[string]int)
	histPriorities := make(map[int]int)
	cogPriorities := make(map[int]int)
	for _, item := range historical {
		hist[item.RequestFingerprint]++
		histTypes[item.ActionType]++
		histTargets[item.Target]++
		histPriorities[item.Priority]++
	}
	for _, item := range cognitive {
		cog[item.RequestFingerprint]++
		cogTypes[item.ActionType]++
		cogTargets[item.Target]++
		cogPriorities[item.Priority]++
	}
	c.SameActionTypes = mapsEqual(histTypes, cogTypes)
	c.SameTargets = mapsEqual(histTargets, cogTargets)
	c.SamePriorities = mapsEqual(histPriorities, cogPriorities)
	c.SameParameters = mapsEqual(hist, cog)
	c.ExactMatch = c.SameActionTypes && c.SameTargets && c.SamePriorities && c.SameParameters && len(historical) == len(cognitive)
	for fingerprint, count := range cog {
		for i := 0; i < count-hist[fingerprint]; i++ {
			c.MissingCognitiveActions = append(c.MissingCognitiveActions, fingerprint)
		}
	}
	for fingerprint, count := range hist {
		for i := 0; i < count-cog[fingerprint]; i++ {
			c.ExtraCognitiveActions = append(c.ExtraCognitiveActions, fingerprint)
		}
	}
	if plan.Status != ExecutionPlanPlanned {
		c.DivergenceCodes = append(c.DivergenceCodes, "planner_denied")
	}
	if len(c.MissingCognitiveActions) > 0 {
		c.DivergenceCodes = append(c.DivergenceCodes, "cognitive_only_action")
	}
	if len(c.ExtraCognitiveActions) > 0 {
		c.DivergenceCodes = append(c.DivergenceCodes, "historical_only_action")
	}
	if !c.SameActionTypes {
		c.DivergenceCodes = append(c.DivergenceCodes, "action_type_mismatch")
	}
	if !c.SameTargets {
		c.DivergenceCodes = append(c.DivergenceCodes, "target_mismatch")
	}
	if !c.SamePriorities {
		c.DivergenceCodes = append(c.DivergenceCodes, "priority_mismatch")
	}
	if !c.SameParameters {
		c.DivergenceCodes = append(c.DivergenceCodes, "parameter_mismatch")
	}
	sort.Strings(c.MissingCognitiveActions)
	sort.Strings(c.ExtraCognitiveActions)
	sort.Strings(c.DivergenceCodes)
	return c, c.Validate()
}

func mapsEqual[K comparable](left, right map[K]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

type ExecutionPlanStore interface {
	PersistExecutionPlan(context.Context, ExecutionPlan) error
	ExecutionPlans(context.Context) ([]ExecutionPlan, error)
	PersistExecutionPlanComparison(context.Context, ExecutionPlanComparison) error
	ExecutionPlanComparisons(context.Context) ([]ExecutionPlanComparison, error)
}

type MemoryExecutionPlanStore struct {
	mu          sync.RWMutex
	plans       []ExecutionPlan
	comparisons []ExecutionPlanComparison
}

func (s *MemoryExecutionPlanStore) PersistExecutionPlan(ctx context.Context, plan ExecutionPlan) error {
	if s == nil {
		return ErrExecutionPlanStore
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plans = append(s.plans, plan)
	return nil
}
func (s *MemoryExecutionPlanStore) ExecutionPlans(ctx context.Context) ([]ExecutionPlan, error) {
	if s == nil {
		return nil, ErrExecutionPlanStore
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ExecutionPlan(nil), s.plans...), nil
}
func (s *MemoryExecutionPlanStore) PersistExecutionPlanComparison(ctx context.Context, comparison ExecutionPlanComparison) error {
	if s == nil {
		return ErrExecutionPlanStore
	}
	if err := comparison.Validate(); err != nil {
		return err
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comparisons = append(s.comparisons, comparison)
	return nil
}
func (s *MemoryExecutionPlanStore) ExecutionPlanComparisons(ctx context.Context) ([]ExecutionPlanComparison, error) {
	if s == nil {
		return nil, ErrExecutionPlanStore
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ExecutionPlanComparison(nil), s.comparisons...), nil
}

type FileExecutionPlanStore struct {
	mu   sync.Mutex
	path string
}

func NewFileExecutionPlanStore(dataDir string) (*FileExecutionPlanStore, error) {
	if strings.TrimSpace(dataDir) == "" || !filepath.IsAbs(dataDir) {
		return nil, ErrExecutionPlanStore
	}
	return &FileExecutionPlanStore{path: dataDir}, nil
}
func (s *FileExecutionPlanStore) plansPath() string {
	return filepath.Join(s.path, "execution-plans.ndjson")
}
func (s *FileExecutionPlanStore) comparisonsPath() string {
	return filepath.Join(s.path, "execution-plan-comparisons.ndjson")
}
func (s *FileExecutionPlanStore) append(ctx context.Context, path string, value any) error {
	if s == nil {
		return ErrExecutionPlanStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := ValidateStoreWrite(value); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ErrExecutionPlanStore
	}
	if err := os.MkdirAll(s.path, 0o700); err != nil {
		return ErrExecutionPlanStore
	}
	_ = os.Chmod(s.path, 0o700)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return ErrExecutionPlanStore
	}
	defer file.Close()
	_ = file.Chmod(0o600)
	if _, err := file.Write(append(data, '\n')); err != nil {
		return ErrExecutionPlanStore
	}
	return file.Sync()
}
func (s *FileExecutionPlanStore) PersistExecutionPlan(ctx context.Context, plan ExecutionPlan) error {
	return s.append(ctx, s.plansPath(), plan)
}
func (s *FileExecutionPlanStore) PersistExecutionPlanComparison(ctx context.Context, comparison ExecutionPlanComparison) error {
	return s.append(ctx, s.comparisonsPath(), comparison)
}
func (s *FileExecutionPlanStore) read(ctx context.Context, path string, decode func([]byte) error) error {
	if s == nil {
		return ErrExecutionPlanStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := contextErr(ctx); err != nil {
		return err
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return ErrExecutionPlanStore
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Mode().Perm() != 0o600 {
		return ErrExecutionPlanStore
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), maxExecutionLine)
	for scanner.Scan() {
		if err := decode(scanner.Bytes()); err != nil {
			return ErrExecutionPlanStore
		}
	}
	if err := scanner.Err(); err != nil {
		return ErrExecutionPlanStore
	}
	return nil
}
func (s *FileExecutionPlanStore) ExecutionPlans(ctx context.Context) ([]ExecutionPlan, error) {
	var out []ExecutionPlan
	err := s.read(ctx, s.plansPath(), func(data []byte) error {
		var value ExecutionPlan
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		out = append(out, value)
		return nil
	})
	return out, err
}
func (s *FileExecutionPlanStore) ExecutionPlanComparisons(ctx context.Context) ([]ExecutionPlanComparison, error) {
	var out []ExecutionPlanComparison
	err := s.read(ctx, s.comparisonsPath(), func(data []byte) error {
		var value ExecutionPlanComparison
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if err := value.Validate(); err != nil {
			return err
		}
		out = append(out, value)
		return nil
	})
	return out, err
}
