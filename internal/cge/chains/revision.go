package chains

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxMutationReasonLength = 256

// MutationContext attributes one successful domain mutation to a component.
// At is mandatory so callers and tests control time explicitly.
type MutationContext struct {
	At             time.Time
	Actor          string
	Reason         string
	CorrelationID  string
	ObservationIDs []string
}

func (c MutationContext) validate() error {
	if c.At.IsZero() {
		return errors.New("mutation timestamp must not be zero")
	}
	if strings.TrimSpace(c.Actor) == "" {
		return errors.New("mutation actor must not be empty")
	}
	if strings.TrimSpace(c.Reason) == "" {
		return errors.New("mutation reason must not be empty")
	}
	if len([]rune(c.Reason)) > maxMutationReasonLength {
		return fmt.Errorf("mutation reason must not exceed %d characters", maxMutationReasonLength)
	}
	if strings.ContainsAny(c.Reason, "\r\n") {
		return errors.New("mutation reason must be a single line")
	}
	if strings.TrimSpace(c.CorrelationID) != c.CorrelationID {
		return errors.New("mutation correlation id must not have surrounding whitespace")
	}
	return validateIDList(c.ObservationIDs, "mutation observation id")
}

// Validate exposes the controlled mutation-context validation to persistence
// boundaries. It does not retain any caller-owned slice.
func (c MutationContext) Validate() error { return c.validate() }

// RevisionOperation identifies a supported or reserved chain mutation.
type RevisionOperation string

const (
	OperationChainCreated                 RevisionOperation = "chain.created"
	OperationObservationAdded             RevisionOperation = "observation.added"
	OperationContributionAdded            RevisionOperation = "contribution.added"
	OperationStatusChanged                RevisionOperation = "status.changed"
	OperationEntityAssigned               RevisionOperation = "entity.assigned"
	OperationHistoricalReliabilityUpdated RevisionOperation = "historical_reliability.updated"
	OperationConfidenceUpdated            RevisionOperation = "confidence.updated"

	// Merge and split remain reserved for future passes. Archival and
	// reactivation are emitted by the controlled lifecycle API.
	OperationChainMerged      RevisionOperation = "chain.merged"
	OperationChainSplit       RevisionOperation = "chain.split"
	OperationChainReactivated RevisionOperation = "chain.reactivated"
	OperationChainArchived    RevisionOperation = "chain.archived"
)

var validRevisionOperations = map[RevisionOperation]struct{}{
	OperationChainCreated:                 {},
	OperationObservationAdded:             {},
	OperationContributionAdded:            {},
	OperationStatusChanged:                {},
	OperationEntityAssigned:               {},
	OperationHistoricalReliabilityUpdated: {},
	OperationConfidenceUpdated:            {},
	OperationChainMerged:                  {},
	OperationChainSplit:                   {},
	OperationChainReactivated:             {},
	OperationChainArchived:                {},
}

// Validate reports whether the operation is known to this domain model.
func (o RevisionOperation) Validate() error {
	if _, ok := validRevisionOperations[o]; !ok {
		return fmt.Errorf("invalid revision operation %q", o)
	}
	return nil
}

func (o RevisionOperation) String() string { return string(o) }

// RevisionRecord describes one chain mutation without duplicating chain state.
// Values returned by History are defensive copies and must be treated as
// immutable audit data.
type RevisionRecord struct {
	ChainID          ChainID
	Operation        RevisionOperation
	PreviousRevision uint64
	NewRevision      uint64

	At            time.Time
	Actor         string
	Reason        string
	CorrelationID string

	ObservationIDs  []string
	ContributionIDs []string

	PreviousStatus Status
	NewStatus      Status

	PreviousEntityID              *string
	NewEntityID                   *string
	PreviousConfidence            *float64
	NewConfidence                 *float64
	PreviousHistoricalReliability *float64
	NewHistoricalReliability      *float64
}

func (r RevisionRecord) validate() error {
	if _, err := NewChainID(string(r.ChainID)); err != nil {
		return err
	}
	if err := r.Operation.Validate(); err != nil {
		return err
	}
	if r.NewRevision <= r.PreviousRevision {
		return errors.New("revision record must advance revision")
	}
	if r.At.IsZero() {
		return errors.New("revision timestamp must not be zero")
	}
	if strings.TrimSpace(r.Actor) == "" {
		return errors.New("revision actor must not be empty")
	}
	if strings.TrimSpace(r.Reason) == "" {
		return errors.New("revision reason must not be empty")
	}
	if err := validateIDList(r.ObservationIDs, "revision observation id"); err != nil {
		return err
	}
	if err := validateIDList(r.ContributionIDs, "revision contribution id"); err != nil {
		return err
	}
	if r.PreviousStatus != "" {
		if err := r.PreviousStatus.Validate(); err != nil {
			return err
		}
	}
	if r.NewStatus != "" {
		if err := r.NewStatus.Validate(); err != nil {
			return err
		}
	}
	if r.PreviousHistoricalReliability != nil {
		if err := validateConfidence(*r.PreviousHistoricalReliability, "previous historical reliability"); err != nil {
			return err
		}
	}
	if r.NewHistoricalReliability != nil {
		if err := validateConfidence(*r.NewHistoricalReliability, "new historical reliability"); err != nil {
			return err
		}
	}
	if r.PreviousConfidence != nil {
		if err := validateConfidence(*r.PreviousConfidence, "previous confidence"); err != nil {
			return err
		}
	}
	if r.NewConfidence != nil {
		if err := validateConfidence(*r.NewConfidence, "new confidence"); err != nil {
			return err
		}
	}
	return nil
}

// Validate exposes revision-record validation to journal and replay packages.
func (r RevisionRecord) Validate() error { return r.validate() }

func (c *Chain) validateMutationContext(context MutationContext) error {
	if err := context.validate(); err != nil {
		return err
	}
	if len(c.history) > 0 && context.At.Before(c.history[len(c.history)-1].At) {
		return errors.New("mutation timestamp must not precede the latest revision")
	}
	return nil
}

func newRevisionRecord(chainID ChainID, operation RevisionOperation, context MutationContext) RevisionRecord {
	return RevisionRecord{
		ChainID:        chainID,
		Operation:      operation,
		At:             context.At,
		Actor:          context.Actor,
		Reason:         context.Reason,
		CorrelationID:  context.CorrelationID,
		ObservationIDs: append([]string(nil), context.ObservationIDs...),
	}
}

func (c *Chain) commitRevision(record RevisionRecord) {
	record.PreviousRevision = c.revision
	record.NewRevision = c.revision + 1
	c.revision++
	c.history = append(c.history, cloneRevisionRecord(record))
}

// History returns the append-only local audit trail as defensive copies.
func (c *Chain) History() []RevisionRecord {
	if c == nil {
		return nil
	}
	return cloneRevisionRecords(c.history)
}

func cloneRevisionRecords(records []RevisionRecord) []RevisionRecord {
	cloned := make([]RevisionRecord, len(records))
	for i, record := range records {
		cloned[i] = cloneRevisionRecord(record)
	}
	return cloned
}

func cloneRevisionRecord(record RevisionRecord) RevisionRecord {
	record.ObservationIDs = append([]string(nil), record.ObservationIDs...)
	record.ContributionIDs = append([]string(nil), record.ContributionIDs...)
	record.PreviousEntityID = cloneStringPointer(record.PreviousEntityID)
	record.NewEntityID = cloneStringPointer(record.NewEntityID)
	record.PreviousConfidence = cloneFloatPointer(record.PreviousConfidence)
	record.NewConfidence = cloneFloatPointer(record.NewConfidence)
	record.PreviousHistoricalReliability = cloneFloatPointer(record.PreviousHistoricalReliability)
	record.NewHistoricalReliability = cloneFloatPointer(record.NewHistoricalReliability)
	return record
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloatPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func validateIDList(values []string, label string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must not be empty", label)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("duplicate %s %q", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func (c *Chain) validateObservationReferences(values []string) error {
	known := make(map[string]struct{}, len(c.observations))
	for _, observation := range c.observations {
		known[observation.ID] = struct{}{}
	}
	return validateObservationReferencesKnown(values, known)
}

func validateObservationReferencesKnown(values []string, known map[string]struct{}) error {
	for _, value := range values {
		if _, ok := known[value]; !ok {
			return fmt.Errorf("%w: %q is not part of chain", ErrUnknownObservationReference, value)
		}
	}
	return nil
}

func appendUniqueIDs(values ...[]string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, group := range values {
		for _, value := range group {
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

// Validate checks the aggregate and its complete local audit history without
// repairing any inconsistency.
func (c *Chain) Validate() error {
	if c == nil {
		return errors.New("chain is nil")
	}
	if _, err := NewChainID(string(c.id)); err != nil {
		return err
	}
	if err := c.status.Validate(); err != nil {
		return err
	}
	if err := validateConfidence(c.currentConfidence, "current confidence"); err != nil {
		return err
	}
	if err := validateConfidence(c.historicalReliability, "historical reliability"); err != nil {
		return err
	}
	if err := validateConfidence(c.maxHistoricalConfidence, "maximum historical confidence"); err != nil {
		return err
	}
	if c.maxHistoricalConfidence < c.currentConfidence {
		return errors.New("maximum historical confidence must not be below current confidence")
	}
	if c.occurrenceCount != uint64(len(c.observations)) {
		return errors.New("occurrence count does not match observations")
	}
	observationIDs := make(map[string]struct{}, len(c.observations))
	for i, observation := range c.observations {
		if err := observation.validate(); err != nil {
			return err
		}
		if _, exists := observationIDs[observation.ID]; exists {
			return fmt.Errorf("duplicate observation %q", observation.ID)
		}
		observationIDs[observation.ID] = struct{}{}
		if i > 0 {
			previous := c.observations[i-1]
			if observation.Timestamp.Before(previous.Timestamp) ||
				(observation.Timestamp.Equal(previous.Timestamp) && observation.ID < previous.ID) {
				return errors.New("observations are not deterministically ordered")
			}
		}
	}
	if len(c.observations) == 0 {
		if !c.firstSeenAt.IsZero() || !c.lastSeenAt.IsZero() || len(c.nodePath) != 0 {
			return errors.New("empty chain has observation projections")
		}
	} else {
		if !c.firstSeenAt.Equal(c.observations[0].Timestamp) || !c.lastSeenAt.Equal(c.observations[len(c.observations)-1].Timestamp) {
			return errors.New("seen timestamps do not match observations")
		}
		expectedPath := make([]string, 0, len(c.nodePath))
		for _, observation := range c.observations {
			if observation.NodeID != "" {
				expectedPath = append(expectedPath, observation.NodeID)
			}
		}
		if len(expectedPath) != len(c.nodePath) {
			return errors.New("node path length does not match observations")
		}
		for i := range expectedPath {
			if expectedPath[i] != c.nodePath[i] {
				return errors.New("node path does not match observation order")
			}
		}
	}
	contributionIDs := make(map[string]struct{}, len(c.contributions))
	knownObservationIDs := make(map[string]struct{}, len(c.observations))
	for _, observation := range c.observations {
		knownObservationIDs[observation.ID] = struct{}{}
	}

	supportCount := uint64(0)
	contradictionCount := uint64(0)
	for _, contribution := range c.contributions {
		if err := contribution.validate(); err != nil {
			return err
		}
		if _, exists := contributionIDs[contribution.ID]; exists {
			return fmt.Errorf("duplicate contribution %q", contribution.ID)
		}
		contributionIDs[contribution.ID] = struct{}{}
		switch contribution.Kind {
		case ContributionSupport:
			supportCount++
		case ContributionContradiction:
			contradictionCount++
		}
	}
	if c.confirmationCount != supportCount || c.contradictionCount != contradictionCount {
		return errors.New("contribution counters do not match contributions")
	}
	if len(c.history) == 0 {
		return errors.New("chain history must contain chain.created")
	}
	for index, record := range c.history {
		if err := record.validate(); err != nil {
			return fmt.Errorf("history revision %d: %w", index, err)
		}
		if record.ChainID != c.id {
			return fmt.Errorf("history revision %d has wrong chain id", index)
		}
		if err := validateObservationReferencesKnown(record.ObservationIDs, knownObservationIDs); err != nil {
			return fmt.Errorf("history revision %d: %w", index, err)
		}
		for _, contributionID := range record.ContributionIDs {
			if _, ok := contributionIDs[contributionID]; !ok {
				return fmt.Errorf("history revision %d references unknown contribution %q", index, contributionID)
			}
		}
		if record.PreviousRevision != uint64(index) || record.NewRevision != uint64(index+1) {
			return fmt.Errorf("history revision %d is not continuous", index)
		}
		if index == 0 {
			if record.Operation != OperationChainCreated {
				return errors.New("history must start with chain.created")
			}
			if record.PreviousRevision != 0 || record.NewRevision != 1 || record.PreviousStatus != "" || record.NewStatus != StatusCandidate {
				return errors.New("chain.created must create a candidate at revision 1")
			}
		} else if record.Operation == OperationChainCreated {
			return errors.New("chain.created may only be the first revision")
		} else if record.At.Before(c.history[index-1].At) {
			return fmt.Errorf("history revision %d timestamp precedes previous revision", index)
		}
	}
	if err := c.validateLifecycleHistory(); err != nil {
		return err
	}
	if c.revision != uint64(len(c.history)) {
		return errors.New("chain revision does not match history")
	}
	createdAt := c.history[0].At
	if createdAt.IsZero() || c.history[0].Operation != OperationChainCreated {
		return errors.New("chain creation anchor is invalid")
	}
	statusChangedAt := createdAt
	for index, record := range c.history[1:] {
		switch record.Operation {
		case OperationStatusChanged, OperationChainArchived, OperationChainReactivated:
			if record.At.Before(createdAt) {
				return fmt.Errorf("history revision %d status transition precedes creation", index+1)
			}
			statusChangedAt = record.At
		}
	}
	if len(c.observations) > 0 && c.observations[0].Timestamp.Before(createdAt) {
		return errors.New("observation precedes chain creation")
	}
	if statusChangedAt.Before(createdAt) {
		return errors.New("status change anchor precedes chain creation")
	}
	return nil
}

func (c *Chain) validateLifecycleHistory() error {
	status := StatusCandidate
	for index, record := range c.history {
		switch record.Operation {
		case OperationChainCreated:
			if index != 0 {
				return fmt.Errorf("history revision %d creates the chain again", index)
			}
		case OperationStatusChanged, OperationChainArchived, OperationChainReactivated:
			if record.PreviousStatus == "" || record.NewStatus == "" {
				return fmt.Errorf("history revision %d has incomplete status transition", index)
			}
			if record.PreviousStatus != status {
				return fmt.Errorf("history revision %d starts from %q, current historical status is %q", index, record.PreviousStatus, status)
			}
			if err := ValidateTransition(record.PreviousStatus, record.NewStatus); err != nil {
				return fmt.Errorf("history revision %d: %w", index, err)
			}
			expectedOperation := operationForTransition(record.PreviousStatus, record.NewStatus)
			if record.Operation != expectedOperation {
				return fmt.Errorf("history revision %d uses operation %q for %s -> %s, want %q", index, record.Operation, record.PreviousStatus, record.NewStatus, expectedOperation)
			}
			status = record.NewStatus
		case OperationChainMerged, OperationChainSplit:
			return fmt.Errorf("history revision %d uses reserved lifecycle operation %q", index, record.Operation)
		default:
			if record.PreviousStatus != "" || record.NewStatus != "" {
				return fmt.Errorf("history revision %d changes status without a lifecycle operation", index)
			}
		}
	}
	if status != c.status {
		return fmt.Errorf("current status %q does not match last lifecycle status %q", c.status, status)
	}
	return nil
}
