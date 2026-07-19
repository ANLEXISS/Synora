package chains

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	cgecontext "synora/internal/cge/context"
)

const (
	confidenceMin = 0.0
	confidenceMax = 1.0
)

// ChainID is the stable identity of a chain. It is supplied by the caller;
// this package deliberately does not derive it from an in-memory index.
type ChainID string

// NewChainID validates a caller-provided chain identity.
func NewChainID(value string) (ChainID, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("chain id must not be empty")
	}
	return ChainID(value), nil
}

// Status is the lifecycle label of a cognitive chain. The transition policy is
// centralized in lifecycle.go; no timing or automatic transition is applied.
type Status string

const (
	StatusCandidate   Status = "candidate"
	StatusActive      Status = "active"
	StatusConfirmed   Status = "confirmed"
	StatusDeclining   Status = "declining"
	StatusDormant     Status = "dormant"
	StatusArchived    Status = "archived"
	StatusReactivated Status = "reactivated"
	StatusMerged      Status = "merged"
	StatusSplit       Status = "split"
	StatusInvalidated Status = "invalidated"
)

var validStatuses = map[Status]struct{}{
	StatusCandidate:   {},
	StatusActive:      {},
	StatusConfirmed:   {},
	StatusDeclining:   {},
	StatusDormant:     {},
	StatusArchived:    {},
	StatusReactivated: {},
	StatusMerged:      {},
	StatusSplit:       {},
	StatusInvalidated: {},
}

var (
	ErrObservationNotAllowed       = errors.New("observation_not_allowed")
	ErrDuplicateObservation        = errors.New("duplicate_observation")
	ErrContributionNotAllowed      = errors.New("contribution_not_allowed")
	ErrDuplicateContribution       = errors.New("duplicate_contribution")
	ErrUnknownObservationReference = errors.New("unknown_observation_reference")
)

// Validate reports whether the status is one of the stable domain values.
func (s Status) Validate() error {
	if _, ok := validStatuses[s]; !ok {
		return fmt.Errorf("invalid chain status %q", s)
	}
	return nil
}

func (s Status) String() string { return string(s) }

// CanAcceptObservation is the explicit policy for this pass. Historical,
// replacement and terminal states are not implicitly reactivated.
func (s Status) CanAcceptObservation() bool {
	switch s {
	case StatusCandidate, StatusActive, StatusConfirmed, StatusDeclining, StatusReactivated:
		return true
	default:
		return false
	}
}

// ValidateObservationMutation rejects an observation on a state that requires
// an explicit future lifecycle operation first.
func (s Status) ValidateObservationMutation() error {
	if !s.CanAcceptObservation() {
		return fmt.Errorf("%w: status=%s", ErrObservationNotAllowed, s)
	}
	return nil
}

// CanAcceptContribution is intentionally broader than observation admission:
// dormant and archived chains may be explicitly re-evaluated, but replacement
// and invalidated states are immutable until a future explicit operation.
func (s Status) CanAcceptContribution() bool {
	switch s {
	case StatusCandidate, StatusActive, StatusConfirmed, StatusDeclining,
		StatusDormant, StatusArchived, StatusReactivated:
		return true
	default:
		return false
	}
}

// ValidateContributionMutation rejects contribution writes to replacement or
// invalidated chains without changing lifecycle status.
func (s Status) ValidateContributionMutation() error {
	if !s.CanAcceptContribution() {
		return fmt.Errorf("%w: status=%s", ErrContributionNotAllowed, s)
	}
	return nil
}

// ObservationRef is a detached, immutable reference to a source observation.
// It intentionally contains no arbitrary payload or mutable source object.
type ObservationRef struct {
	ID           string            `json:"id"`
	EventType    string            `json:"event_type"`
	Timestamp    time.Time         `json:"timestamp"`
	NodeID       string            `json:"node_id,omitempty"`
	DeviceID     string            `json:"device_id,omitempty"`
	EntityID     string            `json:"entity_id,omitempty"`
	ActivationID string            `json:"activation_id,omitempty"`
	ClipID       string            `json:"clip_id,omitempty"`
	ClipIndex    int               `json:"clip_index,omitempty"`
	TrackID      string            `json:"track_id,omitempty"`
	SequenceKey  string            `json:"sequence_key,omitempty"`
	Context      *cgecontext.Frame `json:"context,omitempty"`
}

func (r ObservationRef) validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return errors.New("observation id must not be empty")
	}
	if strings.TrimSpace(r.EventType) == "" {
		return fmt.Errorf("observation %q event type must not be empty", r.ID)
	}
	if r.Timestamp.IsZero() {
		return fmt.Errorf("observation %q timestamp must not be zero", r.ID)
	}
	if r.ClipIndex < 0 {
		return fmt.Errorf("observation %q clip index must not be negative", r.ID)
	}
	if r.Context != nil {
		if err := r.Context.Validate(); err != nil {
			return fmt.Errorf("observation %q context: %w", r.ID, err)
		}
		if r.Context.ObservationID != r.ID || !r.Context.ObservedAt.Equal(r.Timestamp) {
			return fmt.Errorf("observation %q context identity does not match observation", r.ID)
		}
		if r.NodeID != "" && r.Context.NodeID != "" && r.NodeID != r.Context.NodeID {
			return fmt.Errorf("observation %q context node does not match observation", r.ID)
		}
	}
	return nil
}

// Validate exposes the existing observation-reference validation to journal
// and replay packages without exposing mutable aggregate state.
func (r ObservationRef) Validate() error { return r.validate() }

// Clone returns a defensive copy. Frame is currently value-only, but keeping
// the operation explicit protects this boundary if it gains nested data.
func (r ObservationRef) Clone() ObservationRef {
	clone := r
	if r.Context != nil {
		frame := r.Context.Clone()
		clone.Context = &frame
	}
	return clone
}

// ContributionKind explains how a contribution affects current confidence.
type ContributionKind string

const (
	ContributionSupport       ContributionKind = "support"
	ContributionContradiction ContributionKind = "contradiction"
	ContributionNeutral       ContributionKind = "neutral"
)

func (k ContributionKind) Validate() error {
	switch k {
	case ContributionSupport, ContributionContradiction, ContributionNeutral:
		return nil
	default:
		return fmt.Errorf("invalid confidence contribution kind %q", k)
	}
}

// ConfidenceContribution is an explainable confidence delta. Value is a
// non-negative magnitude in [0,1]; Kind supplies its semantic direction.
type ConfidenceContribution struct {
	ID             string           `json:"id"`
	Source         string           `json:"source"`
	Kind           ContributionKind `json:"kind"`
	Value          float64          `json:"value"`
	ObservationIDs []string         `json:"observation_ids,omitempty"`
	Reason         string           `json:"reason"`
	CreatedAt      time.Time        `json:"created_at"`
}

func (c ConfidenceContribution) validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return errors.New("confidence contribution id must not be empty")
	}
	if strings.TrimSpace(c.Source) == "" {
		return errors.New("confidence contribution source must not be empty")
	}
	if err := c.Kind.Validate(); err != nil {
		return err
	}
	if err := validateConfidence(c.Value, "contribution value"); err != nil {
		return err
	}
	if strings.TrimSpace(c.Reason) == "" {
		return errors.New("confidence contribution reason must not be empty")
	}
	if c.CreatedAt.IsZero() {
		return errors.New("confidence contribution created_at must not be zero")
	}
	for _, id := range c.ObservationIDs {
		if strings.TrimSpace(id) == "" {
			return errors.New("confidence contribution observation id must not be empty")
		}
	}
	if err := validateIDList(c.ObservationIDs, "confidence contribution observation id"); err != nil {
		return err
	}
	return nil
}

// Validate exposes contribution validation to journal and replay boundaries.
func (c ConfidenceContribution) Validate() error { return c.validate() }

// Clone returns a detached contribution with no caller-owned reference slice.
func (c ConfidenceContribution) Clone() ConfidenceContribution {
	c.ObservationIDs = append([]string(nil), c.ObservationIDs...)
	return c
}

// ProjectedConfidence applies the existing contribution formula without
// mutating a chain. It is shared by transactional validation and journal
// validation so no second confidence formula can diverge.
func ProjectedConfidence(current float64, contribution ConfidenceContribution) (float64, error) {
	if err := validateConfidence(current, "current confidence"); err != nil {
		return 0, err
	}
	if err := contribution.validate(); err != nil {
		return 0, err
	}
	switch contribution.Kind {
	case ContributionSupport:
		current += contribution.Value
	case ContributionContradiction:
		current -= contribution.Value
	case ContributionNeutral:
	}
	return clampConfidence(current), nil
}

// Chain is a caller-owned domain aggregate. It is intentionally not safe for
// concurrent use; synchronize access in the owning registry or engine.
//
// State is private so callers cannot mutate slices or bypass validation. Use
// Snapshot for a defensive, read-only value and the controlled operations below
// for mutations.
type Chain struct {
	id                      ChainID
	entityID                string
	status                  Status
	observations            []ObservationRef
	nodePath                []string
	currentConfidence       float64
	historicalReliability   float64
	maxHistoricalConfidence float64
	firstSeenAt             time.Time
	lastSeenAt              time.Time
	occurrenceCount         uint64
	confirmationCount       uint64
	contradictionCount      uint64
	contributions           []ConfidenceContribution
	history                 []RevisionRecord
	revision                uint64
}

// New creates an empty candidate chain with a caller-supplied stable ID and
// records its creation as revision 1.
func New(id ChainID, mutation MutationContext) (*Chain, error) {
	if _, err := NewChainID(string(id)); err != nil {
		return nil, err
	}
	if err := mutation.validate(); err != nil {
		return nil, err
	}
	if len(mutation.ObservationIDs) != 0 {
		return nil, errors.New("chain creation cannot reference observations before they are added")
	}
	chain := &Chain{id: id, status: StatusCandidate}
	record := newRevisionRecord(id, OperationChainCreated, mutation)
	record.NewStatus = StatusCandidate
	chain.commitRevision(record)
	return chain, nil
}

// AddObservation adds a detached observation and keeps the aggregate ordered
// by timestamp, with ID as a deterministic tie-breaker.
func (c *Chain) AddObservation(observation ObservationRef, mutation MutationContext) error {
	if c == nil {
		return errors.New("chain is nil")
	}
	if err := c.validateMutationContext(mutation); err != nil {
		return err
	}
	if err := c.status.ValidateObservationMutation(); err != nil {
		return err
	}
	if err := observation.validate(); err != nil {
		return err
	}
	if len(c.history) > 0 && observation.Timestamp.Before(c.history[0].At) {
		return errors.New("observation timestamp must not precede chain creation")
	}
	for _, observationID := range mutation.ObservationIDs {
		if observationID != observation.ID {
			if err := c.validateObservationReferences([]string{observationID}); err != nil {
				return err
			}
		}
	}
	for _, existing := range c.observations {
		if existing.ID == observation.ID {
			return fmt.Errorf("%w: %q already exists in chain", ErrDuplicateObservation, observation.ID)
		}
	}

	record := newRevisionRecord(c.id, OperationObservationAdded, mutation)
	record.ObservationIDs = appendUniqueIDs(record.ObservationIDs, []string{observation.ID})
	c.observations = append(c.observations, observation.Clone())
	sort.SliceStable(c.observations, func(i, j int) bool {
		left, right := c.observations[i], c.observations[j]
		if left.Timestamp.Equal(right.Timestamp) {
			return left.ID < right.ID
		}
		return left.Timestamp.Before(right.Timestamp)
	})
	c.rebuildProjection()
	c.occurrenceCount = uint64(len(c.observations))
	c.commitRevision(record)
	return nil
}

// AddContribution records an explainable confidence contribution. Support
// adds Value, contradiction subtracts Value, and neutral leaves confidence
// unchanged. The result is clamped to [0,1].
func (c *Chain) AddContribution(contribution ConfidenceContribution, mutation MutationContext) error {
	if c == nil {
		return errors.New("chain is nil")
	}
	if err := c.validateMutationContext(mutation); err != nil {
		return err
	}
	if err := c.status.ValidateContributionMutation(); err != nil {
		return err
	}
	if err := contribution.validate(); err != nil {
		return err
	}
	if err := c.validateObservationReferences(appendUniqueIDs(mutation.ObservationIDs, contribution.ObservationIDs)); err != nil {
		return err
	}
	for _, existing := range c.contributions {
		if existing.ID == contribution.ID {
			return fmt.Errorf("%w: %q already exists in chain", ErrDuplicateContribution, contribution.ID)
		}
	}

	record := newRevisionRecord(c.id, OperationContributionAdded, mutation)
	record.ObservationIDs = appendUniqueIDs(record.ObservationIDs, contribution.ObservationIDs)
	record.ContributionIDs = []string{contribution.ID}
	contribution.ObservationIDs = append([]string(nil), contribution.ObservationIDs...)
	c.contributions = append(c.contributions, contribution)
	projectedConfidence, _ := ProjectedConfidence(c.currentConfidence, contribution)
	switch contribution.Kind {
	case ContributionSupport:
		c.confirmationCount++
	case ContributionContradiction:
		c.contradictionCount++
	}
	c.currentConfidence = projectedConfidence
	if c.currentConfidence > c.maxHistoricalConfidence {
		c.maxHistoricalConfidence = c.currentConfidence
	}
	c.commitRevision(record)
	return nil
}

// SetStatus validates and explicitly applies one allowed lifecycle transition.
func (c *Chain) SetStatus(status Status, mutation MutationContext) error {
	if c == nil {
		return errors.New("chain is nil")
	}
	if err := c.validateMutationContext(mutation); err != nil {
		return err
	}
	if err := status.Validate(); err != nil {
		return err
	}
	if err := c.validateObservationReferences(mutation.ObservationIDs); err != nil {
		return err
	}
	if err := ValidateTransition(c.status, status); err != nil {
		return err
	}
	record := newRevisionRecord(c.id, operationForTransition(c.status, status), mutation)
	record.PreviousStatus = c.status
	record.NewStatus = status
	c.status = status
	c.commitRevision(record)
	return nil
}

// AssignEntity assigns or clears the candidate entity identity.
func (c *Chain) AssignEntity(entityID string, mutation MutationContext) error {
	if c == nil {
		return errors.New("chain is nil")
	}
	if err := c.validateMutationContext(mutation); err != nil {
		return err
	}
	if strings.TrimSpace(entityID) != entityID {
		return errors.New("entity id must not have surrounding whitespace")
	}
	if err := c.validateObservationReferences(mutation.ObservationIDs); err != nil {
		return err
	}
	if c.entityID == entityID {
		return nil
	}
	record := newRevisionRecord(c.id, OperationEntityAssigned, mutation)
	record.PreviousEntityID = stringPointer(c.entityID)
	record.NewEntityID = stringPointer(entityID)
	c.entityID = entityID
	c.commitRevision(record)
	return nil
}

// SetConfidence sets the current confidence with explicit validation.
func (c *Chain) SetConfidence(value float64, mutation MutationContext) error {
	if c == nil {
		return errors.New("chain is nil")
	}
	if err := c.validateMutationContext(mutation); err != nil {
		return err
	}
	if err := validateConfidence(value, "current confidence"); err != nil {
		return err
	}
	if err := c.validateObservationReferences(mutation.ObservationIDs); err != nil {
		return err
	}
	normalized := normalizeConfidence(value)
	if c.currentConfidence == normalized {
		return nil
	}
	record := newRevisionRecord(c.id, OperationConfidenceUpdated, mutation)
	record.PreviousConfidence = floatPointer(c.currentConfidence)
	record.NewConfidence = floatPointer(normalized)
	c.currentConfidence = normalized
	if normalized > c.maxHistoricalConfidence {
		c.maxHistoricalConfidence = normalized
	}
	c.commitRevision(record)
	return nil
}

// SetHistoricalReliability records an explicitly supplied historical
// reliability. No decay, occurrence model, or automatic statistical update is
// performed in this pass.
func (c *Chain) SetHistoricalReliability(value float64, mutation MutationContext) error {
	if c == nil {
		return errors.New("chain is nil")
	}
	if err := c.validateMutationContext(mutation); err != nil {
		return err
	}
	if err := validateConfidence(value, "historical reliability"); err != nil {
		return err
	}
	if err := c.validateObservationReferences(mutation.ObservationIDs); err != nil {
		return err
	}
	if c.historicalReliability == value {
		return nil
	}
	record := newRevisionRecord(c.id, OperationHistoricalReliabilityUpdated, mutation)
	record.PreviousHistoricalReliability = floatPointer(c.historicalReliability)
	record.NewHistoricalReliability = floatPointer(value)
	c.historicalReliability = value
	c.commitRevision(record)
	return nil
}

// Snapshot is a defensive read model. All returned slices, including nested
// observation ID references, are independent from the chain.
type Snapshot struct {
	ID                      ChainID
	EntityID                string
	Status                  Status
	Observations            []ObservationRef
	NodePath                []string
	CurrentConfidence       float64
	HistoricalReliability   float64
	MaxHistoricalConfidence float64
	FirstSeenAt             time.Time
	LastSeenAt              time.Time
	OccurrenceCount         uint64
	ConfirmationCount       uint64
	ContradictionCount      uint64
	Contributions           []ConfidenceContribution
	History                 []RevisionRecord
	Revision                uint64
}

func cloneObservations(values []ObservationRef) []ObservationRef {
	if values == nil {
		return nil
	}
	result := make([]ObservationRef, len(values))
	for i, value := range values {
		result[i] = value.Clone()
	}
	return result
}

// Snapshot returns a defensive copy of the chain state.
func (c *Chain) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	contributions := make([]ConfidenceContribution, len(c.contributions))
	for i, contribution := range c.contributions {
		contributions[i] = contribution
		contributions[i].ObservationIDs = append([]string(nil), contribution.ObservationIDs...)
	}
	observations := make([]ObservationRef, len(c.observations))
	for i, observation := range c.observations {
		observations[i] = observation.Clone()
	}
	return Snapshot{
		ID:                      c.id,
		EntityID:                c.entityID,
		Status:                  c.status,
		Observations:            observations,
		NodePath:                append([]string(nil), c.nodePath...),
		CurrentConfidence:       c.currentConfidence,
		HistoricalReliability:   c.historicalReliability,
		MaxHistoricalConfidence: c.maxHistoricalConfidence,
		FirstSeenAt:             c.firstSeenAt,
		LastSeenAt:              c.lastSeenAt,
		OccurrenceCount:         c.occurrenceCount,
		ConfirmationCount:       c.confirmationCount,
		ContradictionCount:      c.contradictionCount,
		Contributions:           contributions,
		History:                 cloneRevisionRecords(c.history),
		Revision:                c.revision,
	}
}

// Clone returns an independently mutable, deeply copied aggregate without
// creating a revision. The clone is validated before it is returned and is
// intended for an explicit owner such as the cognitive-chain registry.
func (c *Chain) Clone() (*Chain, error) {
	if c == nil {
		return nil, errors.New("chain is nil")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	clone := &Chain{
		id:                      c.id,
		entityID:                c.entityID,
		status:                  c.status,
		observations:            cloneObservations(c.observations),
		nodePath:                append([]string(nil), c.nodePath...),
		currentConfidence:       c.currentConfidence,
		historicalReliability:   c.historicalReliability,
		maxHistoricalConfidence: c.maxHistoricalConfidence,
		firstSeenAt:             c.firstSeenAt,
		lastSeenAt:              c.lastSeenAt,
		occurrenceCount:         c.occurrenceCount,
		confirmationCount:       c.confirmationCount,
		contradictionCount:      c.contradictionCount,
		history:                 cloneRevisionRecords(c.history),
		revision:                c.revision,
	}
	clone.contributions = make([]ConfidenceContribution, len(c.contributions))
	for i, contribution := range c.contributions {
		clone.contributions[i] = contribution
		clone.contributions[i].ObservationIDs = append([]string(nil), contribution.ObservationIDs...)
	}
	return clone, nil
}

// Restore reconstructs an independently owned aggregate from a defensive
// snapshot. Restoration is a technical operation: it does not append a
// revision, change timestamps, or represent a domain mutation. The complete
// reconstructed aggregate is validated before it is returned.
func Restore(snapshot Snapshot) (*Chain, error) {
	if _, err := NewChainID(string(snapshot.ID)); err != nil {
		return nil, err
	}
	restored := &Chain{
		id:                      snapshot.ID,
		entityID:                snapshot.EntityID,
		status:                  snapshot.Status,
		observations:            cloneObservations(snapshot.Observations),
		nodePath:                append([]string(nil), snapshot.NodePath...),
		currentConfidence:       snapshot.CurrentConfidence,
		historicalReliability:   snapshot.HistoricalReliability,
		maxHistoricalConfidence: snapshot.MaxHistoricalConfidence,
		firstSeenAt:             snapshot.FirstSeenAt,
		lastSeenAt:              snapshot.LastSeenAt,
		occurrenceCount:         snapshot.OccurrenceCount,
		confirmationCount:       snapshot.ConfirmationCount,
		contradictionCount:      snapshot.ContradictionCount,
		history:                 cloneRevisionRecords(snapshot.History),
		revision:                snapshot.Revision,
	}
	if snapshot.Contributions != nil {
		restored.contributions = make([]ConfidenceContribution, len(snapshot.Contributions))
		for i, contribution := range snapshot.Contributions {
			restored.contributions[i] = contribution
			restored.contributions[i].ObservationIDs = append([]string(nil), contribution.ObservationIDs...)
		}
	}
	if err := restored.Validate(); err != nil {
		return nil, fmt.Errorf("restore chain: %w", err)
	}
	return restored, nil
}

// CreatedAt returns the timestamp of the chain.created revision. A zero value
// means that the snapshot history is absent or malformed.
func (s Snapshot) CreatedAt() time.Time {
	if len(s.History) == 0 || s.History[0].Operation != OperationChainCreated {
		return time.Time{}
	}
	return s.History[0].At
}

// StatusChangedAt returns the timestamp of the latest revision that changed
// lifecycle status. Before the first transition, creation is the status
// anchor. The value is derived from the defensive history, not stored as a
// second mutable source of truth.
func (s Snapshot) StatusChangedAt() time.Time {
	createdAt := s.CreatedAt()
	if createdAt.IsZero() {
		return time.Time{}
	}
	statusChangedAt := createdAt
	for _, record := range s.History[1:] {
		switch record.Operation {
		case OperationStatusChanged, OperationChainArchived, OperationChainReactivated:
			if record.At.IsZero() {
				return time.Time{}
			}
			statusChangedAt = record.At
		}
	}
	return statusChangedAt
}

// StatusSince is an explicit alias for the status-change temporal anchor.
func (s Snapshot) StatusSince() time.Time { return s.StatusChangedAt() }

func stringPointer(value string) *string { return &value }

func floatPointer(value float64) *float64 { return &value }

func (c *Chain) rebuildProjection() {
	c.nodePath = c.nodePath[:0]
	for _, observation := range c.observations {
		if observation.NodeID != "" {
			c.nodePath = append(c.nodePath, observation.NodeID)
		}
	}
	c.firstSeenAt = c.observations[0].Timestamp
	c.lastSeenAt = c.observations[len(c.observations)-1].Timestamp
}

func validateConfidence(value float64, name string) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < confidenceMin || value > confidenceMax {
		return fmt.Errorf("%s must be between 0 and 1", name)
	}
	return nil
}

func clampConfidence(value float64) float64 {
	if value < confidenceMin {
		return confidenceMin
	}
	if value > confidenceMax {
		return confidenceMax
	}
	return normalizeConfidence(value)
}

func normalizeConfidence(value float64) float64 {
	return math.Round(value*1e12) / 1e12
}
