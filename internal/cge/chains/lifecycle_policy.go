package chains

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidLifecyclePolicy identifies a configuration that cannot be
// evaluated deterministically.
var ErrInvalidLifecyclePolicy = errors.New("invalid cognitive lifecycle policy")

// LifecyclePolicy contains explicit, cognitive-chain retention thresholds.
// It is an evaluation policy only: it never mutates a Chain or starts a
// background process.
type LifecyclePolicy struct {
	CandidateTTL time.Duration

	ActiveDeclineAfter    time.Duration
	ConfirmedDeclineAfter time.Duration

	DecliningDormantAfter time.Duration
	DormantArchiveAfter   time.Duration

	MinConfidenceToRemainActive    float64
	MinConfidenceToRemainConfirmed float64

	MaxCandidateContradictions uint64
}

// DefaultLifecyclePolicy returns a valid starting configuration. These values
// are domain defaults for deterministic evaluation, not product calibration.
func DefaultLifecyclePolicy() LifecyclePolicy {
	return LifecyclePolicy{
		CandidateTTL:                   time.Hour,
		ActiveDeclineAfter:             2 * time.Hour,
		ConfirmedDeclineAfter:          6 * time.Hour,
		DecliningDormantAfter:          24 * time.Hour,
		DormantArchiveAfter:            7 * 24 * time.Hour,
		MinConfidenceToRemainActive:    0.35,
		MinConfidenceToRemainConfirmed: 0.55,
		MaxCandidateContradictions:     3,
	}
}

// Validate checks every threshold without normalizing or repairing it.
// Durations are based on different temporal anchors. The only retained
// cross-state duration constraint is that confirmed chains must tolerate more
// observation inactivity than active chains.
func (p LifecyclePolicy) Validate() error {
	if p.CandidateTTL <= 0 {
		return fmt.Errorf("%w: candidate TTL must be positive", ErrInvalidLifecyclePolicy)
	}
	if p.ActiveDeclineAfter <= 0 {
		return fmt.Errorf("%w: active decline duration must be positive", ErrInvalidLifecyclePolicy)
	}
	if p.ConfirmedDeclineAfter <= 0 {
		return fmt.Errorf("%w: confirmed decline duration must be positive", ErrInvalidLifecyclePolicy)
	}
	if p.DecliningDormantAfter <= 0 {
		return fmt.Errorf("%w: declining dormant duration must be positive", ErrInvalidLifecyclePolicy)
	}
	if p.DormantArchiveAfter <= 0 {
		return fmt.Errorf("%w: dormant archive duration must be positive", ErrInvalidLifecyclePolicy)
	}
	if p.ConfirmedDeclineAfter <= p.ActiveDeclineAfter {
		return fmt.Errorf("%w: confirmed decline duration must exceed active decline duration", ErrInvalidLifecyclePolicy)
	}
	if err := validateConfidence(p.MinConfidenceToRemainActive, "minimum active confidence"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidLifecyclePolicy, err)
	}
	if err := validateConfidence(p.MinConfidenceToRemainConfirmed, "minimum confirmed confidence"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidLifecyclePolicy, err)
	}
	if p.MinConfidenceToRemainConfirmed <= p.MinConfidenceToRemainActive {
		return fmt.Errorf("%w: minimum confirmed confidence must exceed minimum active confidence", ErrInvalidLifecyclePolicy)
	}
	if p.MaxCandidateContradictions == 0 {
		return fmt.Errorf("%w: maximum candidate contradictions must be positive", ErrInvalidLifecyclePolicy)
	}
	return nil
}

// LifecycleReasonCode is a stable machine-readable reason for a proposal.
type LifecycleReasonCode string

const (
	ReasonCandidateTTLExpired               LifecycleReasonCode = "candidate.ttl_expired"
	ReasonCandidateTooManyContradictions    LifecycleReasonCode = "candidate.too_many_contradictions"
	ReasonActiveConfidenceBelowThreshold    LifecycleReasonCode = "active.confidence_below_threshold"
	ReasonActiveInactive                    LifecycleReasonCode = "active.inactive"
	ReasonConfirmedConfidenceBelowThreshold LifecycleReasonCode = "confirmed.confidence_below_threshold"
	ReasonConfirmedInactive                 LifecycleReasonCode = "confirmed.inactive"
	ReasonDecliningInactive                 LifecycleReasonCode = "declining.inactive"
	ReasonDormantRetentionExpired           LifecycleReasonCode = "dormant.retention_expired"
	ReasonReactivatedInactive               LifecycleReasonCode = "reactivated.inactive"
)

func (c LifecycleReasonCode) Validate() error {
	switch c {
	case ReasonCandidateTTLExpired,
		ReasonCandidateTooManyContradictions,
		ReasonActiveConfidenceBelowThreshold,
		ReasonActiveInactive,
		ReasonConfirmedConfidenceBelowThreshold,
		ReasonConfirmedInactive,
		ReasonDecliningInactive,
		ReasonDormantRetentionExpired,
		ReasonReactivatedInactive:
		return nil
	default:
		return fmt.Errorf("invalid lifecycle reason code %q", c)
	}
}

const (
	LifecycleEvaluationNoTransition   = "no_lifecycle_transition_required"
	LifecycleEvaluationStatusExcluded = "status_has_no_temporal_transition"
)

// LifecycleFact is a small, non-sensitive fact supporting a transition
// proposal. It deliberately carries values as deterministic text rather than
// source event payloads.
type LifecycleFact struct {
	Name  string
	Value string
}

// TransitionProposal is an immutable-by-convention suggestion. It contains
// no reference to the source Snapshot and is never applied by this package.
type TransitionProposal struct {
	ChainID        ChainID
	SourceRevision uint64

	From Status
	To   Status

	EvaluatedAt time.Time

	ReasonCode LifecycleReasonCode
	Reason     string

	Age                time.Duration
	InactiveFor        time.Duration
	InCurrentStatusFor time.Duration
	StatusChangedAt    time.Time

	CurrentConfidence     float64
	HistoricalReliability float64

	SupportingFacts []LifecycleFact
}

// MutationContext prepares explicit future application of this proposal. It
// never calls SetStatus.
func (p TransitionProposal) MutationContext(actor, correlationID string) (MutationContext, error) {
	if _, err := NewChainID(string(p.ChainID)); err != nil {
		return MutationContext{}, err
	}
	if p.SourceRevision == 0 {
		return MutationContext{}, errors.New("proposal source revision must be positive")
	}
	if err := ValidateTransition(p.From, p.To); err != nil {
		return MutationContext{}, err
	}
	if p.EvaluatedAt.IsZero() {
		return MutationContext{}, errors.New("proposal evaluation timestamp must not be zero")
	}
	if err := p.ReasonCode.Validate(); err != nil {
		return MutationContext{}, err
	}
	if strings.TrimSpace(actor) == "" {
		return MutationContext{}, errors.New("mutation actor must not be empty")
	}
	if strings.TrimSpace(correlationID) != correlationID {
		return MutationContext{}, errors.New("mutation correlation id must not have surrounding whitespace")
	}
	reason := string(p.ReasonCode) + ": " + p.Reason
	context := MutationContext{
		At:            p.EvaluatedAt,
		Actor:         actor,
		Reason:        reason,
		CorrelationID: correlationID,
	}
	if err := context.validate(); err != nil {
		return MutationContext{}, err
	}
	return context, nil
}

// LifecycleEvaluation is the deterministic result of evaluating one
// Snapshot. Proposal is nil when no transition should be suggested.
type LifecycleEvaluation struct {
	ChainID     ChainID
	EvaluatedAt time.Time

	CreatedAt          time.Time
	LastSeenAt         time.Time
	StatusChangedAt    time.Time
	Age                time.Duration
	InactiveFor        time.Duration
	InCurrentStatusFor time.Duration

	Proposal *TransitionProposal
	Reason   string
}

// EvaluateLifecycle evaluates a snapshot at an explicit time. It does not
// mutate the source chain, append a revision, call SetStatus, or use time.Now.
func EvaluateLifecycle(snapshot Snapshot, evaluatedAt time.Time, policy LifecyclePolicy) (LifecycleEvaluation, error) {
	if err := policy.Validate(); err != nil {
		return LifecycleEvaluation{}, err
	}
	if evaluatedAt.IsZero() {
		return LifecycleEvaluation{}, errors.New("lifecycle evaluation timestamp must not be zero")
	}
	if _, err := NewChainID(string(snapshot.ID)); err != nil {
		return LifecycleEvaluation{}, fmt.Errorf("lifecycle snapshot: %w", err)
	}
	if snapshot.Revision == 0 {
		return LifecycleEvaluation{}, errors.New("lifecycle snapshot revision must be positive")
	}
	if err := snapshot.Status.Validate(); err != nil {
		return LifecycleEvaluation{}, err
	}
	if err := validateConfidence(snapshot.CurrentConfidence, "snapshot current confidence"); err != nil {
		return LifecycleEvaluation{}, err
	}
	if err := validateConfidence(snapshot.HistoricalReliability, "snapshot historical reliability"); err != nil {
		return LifecycleEvaluation{}, err
	}
	createdAt := snapshot.CreatedAt()
	if createdAt.IsZero() {
		return LifecycleEvaluation{}, errors.New("lifecycle snapshot has no valid creation anchor")
	}
	lastSeenAt, err := lifecycleLastSeenAt(snapshot, createdAt)
	if err != nil {
		return LifecycleEvaluation{}, err
	}
	statusChangedAt := snapshot.StatusChangedAt()
	if statusChangedAt.IsZero() {
		return LifecycleEvaluation{}, errors.New("lifecycle snapshot has no valid status anchor")
	}
	if lastSeenAt.Before(createdAt) {
		return LifecycleEvaluation{}, errors.New("last observation precedes chain creation")
	}
	if statusChangedAt.Before(createdAt) {
		return LifecycleEvaluation{}, errors.New("status change precedes chain creation")
	}
	if evaluatedAt.Before(createdAt) {
		return LifecycleEvaluation{}, errors.New("lifecycle evaluation timestamp must not precede chain creation")
	}
	if evaluatedAt.Before(lastSeenAt) {
		return LifecycleEvaluation{}, errors.New("lifecycle evaluation timestamp must not precede last observation")
	}
	if evaluatedAt.Before(statusChangedAt) {
		return LifecycleEvaluation{}, errors.New("lifecycle evaluation timestamp must not precede status change")
	}

	evaluation := LifecycleEvaluation{
		ChainID:            snapshot.ID,
		EvaluatedAt:        evaluatedAt,
		CreatedAt:          createdAt,
		LastSeenAt:         lastSeenAt,
		StatusChangedAt:    statusChangedAt,
		Age:                evaluatedAt.Sub(createdAt),
		InactiveFor:        evaluatedAt.Sub(lastSeenAt),
		InCurrentStatusFor: evaluatedAt.Sub(statusChangedAt),
		Reason:             LifecycleEvaluationNoTransition,
	}
	inactiveFor := evaluation.InactiveFor
	inCurrentStatusFor := evaluation.InCurrentStatusFor
	propose := func(to Status, code LifecycleReasonCode, reason string, facts []LifecycleFact) error {
		proposal, proposalErr := newTransitionProposal(snapshot, evaluatedAt, evaluation.Age, inactiveFor, inCurrentStatusFor, statusChangedAt, to, code, reason, facts)
		if proposalErr != nil {
			return proposalErr
		}
		evaluation.Proposal = proposal
		evaluation.Reason = reason
		return nil
	}

	switch snapshot.Status {
	case StatusCandidate:
		if snapshot.ContradictionCount >= policy.MaxCandidateContradictions {
			return evaluation, propose(StatusInvalidated, ReasonCandidateTooManyContradictions, "candidate contradiction threshold reached", []LifecycleFact{
				lifecycleFact("contradiction_count", strconv.FormatUint(snapshot.ContradictionCount, 10)),
				lifecycleFact("max_candidate_contradictions", strconv.FormatUint(policy.MaxCandidateContradictions, 10)),
			})
		}
		if inactiveFor >= policy.CandidateTTL {
			return evaluation, propose(StatusInvalidated, ReasonCandidateTTLExpired, "candidate inactivity reached configured TTL", []LifecycleFact{
				lifecycleFact("time_basis", "observation_inactivity"),
				lifecycleFact("inactive_for", inactiveFor.String()),
				lifecycleFact("threshold", policy.CandidateTTL.String()),
				lifecycleFact("candidate_ttl", policy.CandidateTTL.String()),
			})
		}
	case StatusActive:
		if snapshot.CurrentConfidence < policy.MinConfidenceToRemainActive {
			return evaluation, propose(StatusDeclining, ReasonActiveConfidenceBelowThreshold, "active confidence is below configured threshold", []LifecycleFact{
				lifecycleFact("current_confidence", formatLifecycleFloat(snapshot.CurrentConfidence)),
				lifecycleFact("threshold", formatLifecycleFloat(policy.MinConfidenceToRemainActive)),
				lifecycleFact("minimum_active_confidence", formatLifecycleFloat(policy.MinConfidenceToRemainActive)),
			})
		}
		if inactiveFor >= policy.ActiveDeclineAfter {
			return evaluation, propose(StatusDeclining, ReasonActiveInactive, "active inactivity reached configured threshold", []LifecycleFact{
				lifecycleFact("time_basis", "observation_inactivity"),
				lifecycleFact("inactive_for", inactiveFor.String()),
				lifecycleFact("threshold", policy.ActiveDeclineAfter.String()),
				lifecycleFact("active_decline_after", policy.ActiveDeclineAfter.String()),
			})
		}
	case StatusConfirmed:
		if snapshot.CurrentConfidence < policy.MinConfidenceToRemainConfirmed {
			return evaluation, propose(StatusDeclining, ReasonConfirmedConfidenceBelowThreshold, "confirmed confidence is below configured threshold", []LifecycleFact{
				lifecycleFact("current_confidence", formatLifecycleFloat(snapshot.CurrentConfidence)),
				lifecycleFact("threshold", formatLifecycleFloat(policy.MinConfidenceToRemainConfirmed)),
				lifecycleFact("minimum_confirmed_confidence", formatLifecycleFloat(policy.MinConfidenceToRemainConfirmed)),
				lifecycleFact("historical_reliability", formatLifecycleFloat(snapshot.HistoricalReliability)),
			})
		}
		if inactiveFor >= policy.ConfirmedDeclineAfter {
			return evaluation, propose(StatusDeclining, ReasonConfirmedInactive, "confirmed inactivity reached configured threshold", []LifecycleFact{
				lifecycleFact("time_basis", "observation_inactivity"),
				lifecycleFact("inactive_for", inactiveFor.String()),
				lifecycleFact("threshold", policy.ConfirmedDeclineAfter.String()),
				lifecycleFact("confirmed_decline_after", policy.ConfirmedDeclineAfter.String()),
				lifecycleFact("historical_reliability", formatLifecycleFloat(snapshot.HistoricalReliability)),
			})
		}
	case StatusDeclining:
		if inCurrentStatusFor >= policy.DecliningDormantAfter {
			return evaluation, propose(StatusDormant, ReasonDecliningInactive, "declining status duration reached configured threshold", []LifecycleFact{
				lifecycleFact("time_basis", "current_status"),
				lifecycleFact("in_current_status_for", inCurrentStatusFor.String()),
				lifecycleFact("threshold", policy.DecliningDormantAfter.String()),
				lifecycleFact("declining_dormant_after", policy.DecliningDormantAfter.String()),
			})
		}
	case StatusDormant:
		if inCurrentStatusFor >= policy.DormantArchiveAfter {
			return evaluation, propose(StatusArchived, ReasonDormantRetentionExpired, "dormant status duration reached configured threshold", []LifecycleFact{
				lifecycleFact("time_basis", "current_status"),
				lifecycleFact("in_current_status_for", inCurrentStatusFor.String()),
				lifecycleFact("threshold", policy.DormantArchiveAfter.String()),
				lifecycleFact("dormant_archive_after", policy.DormantArchiveAfter.String()),
			})
		}
	case StatusReactivated:
		// Reactivation is deliberately conservative: it is never promoted
		// automatically. If it becomes inactive again, declining is the only
		// temporal proposal made by this pass.
		if inCurrentStatusFor >= policy.ActiveDeclineAfter {
			return evaluation, propose(StatusDeclining, ReasonReactivatedInactive, "reactivated status duration reached conservative threshold", []LifecycleFact{
				lifecycleFact("time_basis", "current_status"),
				lifecycleFact("in_current_status_for", inCurrentStatusFor.String()),
				lifecycleFact("threshold", policy.ActiveDeclineAfter.String()),
				lifecycleFact("reactivated_decline_after", policy.ActiveDeclineAfter.String()),
			})
		}
	case StatusArchived, StatusMerged, StatusSplit, StatusInvalidated:
		evaluation.Reason = LifecycleEvaluationStatusExcluded
	}
	return evaluation, nil
}

func lifecycleLastSeenAt(snapshot Snapshot, createdAt time.Time) (time.Time, error) {
	if !snapshot.LastSeenAt.IsZero() {
		return snapshot.LastSeenAt, nil
	}
	return createdAt, nil
}

func newTransitionProposal(snapshot Snapshot, evaluatedAt time.Time, age, inactiveFor, inCurrentStatusFor time.Duration, statusChangedAt time.Time, to Status, code LifecycleReasonCode, reason string, facts []LifecycleFact) (*TransitionProposal, error) {
	if err := ValidateTransition(snapshot.Status, to); err != nil {
		return nil, fmt.Errorf("lifecycle policy proposed invalid transition: %w", err)
	}
	if err := code.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(reason) == "" {
		return nil, errors.New("lifecycle proposal reason must not be empty")
	}
	return &TransitionProposal{
		ChainID:               snapshot.ID,
		SourceRevision:        snapshot.Revision,
		From:                  snapshot.Status,
		To:                    to,
		EvaluatedAt:           evaluatedAt,
		ReasonCode:            code,
		Reason:                reason,
		Age:                   age,
		InactiveFor:           inactiveFor,
		InCurrentStatusFor:    inCurrentStatusFor,
		StatusChangedAt:       statusChangedAt,
		CurrentConfidence:     snapshot.CurrentConfidence,
		HistoricalReliability: snapshot.HistoricalReliability,
		SupportingFacts:       append([]LifecycleFact(nil), facts...),
	}, nil
}

func lifecycleFact(name, value string) LifecycleFact {
	return LifecycleFact{Name: name, Value: value}
}

func formatLifecycleFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
