package routines

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/chains"
)

type Routine struct {
	id                   RoutineID
	kind                 Kind
	subject              Subject
	pattern              Pattern
	status               Status
	occurrences          []OccurrenceRef
	temporalBins         []TemporalBin
	dayPartCounts        []DayPartCount
	intervalStatistics   IntervalStatistics
	firstSeenAt          time.Time
	lastSeenAt           time.Time
	distinctLocalDays    uint64
	distinctLocalWeeks   uint64
	completeContextCount uint64
	partialContextCount  uint64
	createdAt            time.Time
	updatedAt            time.Time
	revision             uint64
	history              []RevisionRecord
}

type Snapshot struct {
	ID                   RoutineID          `json:"id"`
	Kind                 Kind               `json:"kind"`
	Subject              Subject            `json:"subject"`
	Pattern              Pattern            `json:"pattern"`
	Status               Status             `json:"status"`
	Occurrences          []OccurrenceRef    `json:"occurrences"`
	OccurrenceCount      uint64             `json:"occurrence_count"`
	FirstSeenAt          time.Time          `json:"first_seen_at"`
	LastSeenAt           time.Time          `json:"last_seen_at"`
	DistinctLocalDays    uint64             `json:"distinct_local_days"`
	DistinctLocalWeeks   uint64             `json:"distinct_local_weeks"`
	TemporalBins         []TemporalBin      `json:"temporal_bins"`
	DayPartCounts        []DayPartCount     `json:"day_part_counts"`
	IntervalStatistics   IntervalStatistics `json:"interval_statistics"`
	CompleteContextCount uint64             `json:"complete_context_count"`
	PartialContextCount  uint64             `json:"partial_context_count"`
	CreatedAt            time.Time          `json:"created_at"`
	UpdatedAt            time.Time          `json:"updated_at"`
	Revision             uint64             `json:"revision"`
	History              []RevisionRecord   `json:"history"`
}

// MutationOutcome is the compact deterministic projection written beside an
// occurrence delta. It lets replay verify the derived statistics without
// duplicating the complete occurrence history in every journal record.
type MutationOutcome struct {
	OccurrenceCount      uint64
	FirstSeenAt          time.Time
	LastSeenAt           time.Time
	DistinctLocalDays    uint64
	DistinctLocalWeeks   uint64
	IntervalStatistics   IntervalStatistics
	CompleteContextCount uint64
	PartialContextCount  uint64
}

// MutationOutcome returns the derived values that an occurrence mutation is
// expected to produce.
func (s Snapshot) MutationOutcome() MutationOutcome {
	return MutationOutcome{
		OccurrenceCount:      s.OccurrenceCount,
		FirstSeenAt:          s.FirstSeenAt,
		LastSeenAt:           s.LastSeenAt,
		DistinctLocalDays:    s.DistinctLocalDays,
		DistinctLocalWeeks:   s.DistinctLocalWeeks,
		IntervalStatistics:   s.IntervalStatistics,
		CompleteContextCount: s.CompleteContextCount,
		PartialContextCount:  s.PartialContextCount,
	}
}

// Fingerprint returns the stable SHA-256 identity of the complete detached
// snapshot. Snapshot slices are already kept in canonical order by the
// domain, and the JSON representation contains no maps or external data.
func (s Snapshot) Fingerprint() (string, error) {
	if _, err := Restore(s); err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(canonicalJSON(s)))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func NewFromOccurrence(occurrence Occurrence, mutation chains.MutationContext) (*Routine, error) {
	if err := occurrence.Validate(); err != nil {
		return nil, err
	}
	if err := validateMutation(mutation); err != nil {
		return nil, err
	}
	if !mutation.At.Equal(occurrence.ObservedAt) {
		return nil, fmt.Errorf("%w: mutation time must equal occurrence time", ErrInvalidRoutine)
	}
	r := &Routine{id: occurrence.RoutineID, kind: occurrence.Kind, subject: occurrence.Subject, pattern: occurrence.Pattern, status: StatusCandidate, createdAt: mutation.At, updatedAt: mutation.At, revision: 1}
	r.occurrences = []OccurrenceRef{occurrence.Ref()}
	if err := r.rebuild(); err != nil {
		return nil, err
	}
	r.history = []RevisionRecord{{RoutineID: r.id, Operation: OperationRoutineCreated, PreviousRevision: 0, NewRevision: 1, At: mutation.At, Actor: mutation.Actor, Reason: mutation.Reason, CorrelationID: mutation.CorrelationID}}
	return r, nil
}

type AddOccurrenceCommand struct {
	RoutineID      RoutineID
	SourceRevision uint64
	Occurrence     Occurrence
	Mutation       chains.MutationContext
}
type SetStatusCommand struct {
	RoutineID      RoutineID
	SourceRevision uint64
	Target         Status
	Mutation       chains.MutationContext
}

func (r *Routine) AddOccurrence(command AddOccurrenceCommand) error {
	if r == nil {
		return ErrInvalidRoutine
	}
	if command.RoutineID != r.id || command.SourceRevision != r.revision {
		return fmt.Errorf("%w: source revision", ErrRoutineRevisionStale)
	}
	if err := command.Occurrence.Validate(); err != nil {
		return err
	}
	if err := validateMutation(command.Mutation); err != nil {
		return err
	}
	if command.Mutation.At.Before(r.updatedAt) || r.status == StatusInvalidated {
		return ErrRoutineStatusTransition
	}
	if command.Occurrence.RoutineID != r.id || command.Occurrence.Kind != r.kind || command.Occurrence.Subject.key() != r.subject.key() || patternKey(command.Occurrence.Pattern) != patternKey(r.pattern) {
		return ErrRoutineMismatch
	}
	for _, existing := range r.occurrences {
		if existing.ID == command.Occurrence.ID {
			if occurrenceRefMatches(existing, command.Occurrence.Ref()) {
				return ErrDuplicateRoutineOccurrence
			}
			return ErrRoutineOccurrenceCollision
		}
	}
	candidate, err := r.Clone()
	if err != nil {
		return err
	}
	candidate.occurrences = append(candidate.occurrences, command.Occurrence.Ref())
	if err = candidate.rebuild(); err != nil {
		return err
	}
	candidate.revision++
	candidate.updatedAt = command.Mutation.At
	candidate.history = append(candidate.history, RevisionRecord{RoutineID: r.id, Operation: OperationOccurrenceAdded, PreviousRevision: r.revision, NewRevision: candidate.revision, At: command.Mutation.At, Actor: command.Mutation.Actor, Reason: command.Mutation.Reason, CorrelationID: command.Mutation.CorrelationID, OccurrenceID: command.Occurrence.ID})
	*r = *candidate
	return nil
}

func (r *Routine) SetStatus(command SetStatusCommand) error {
	if r == nil {
		return ErrInvalidRoutine
	}
	if command.RoutineID != r.id || command.SourceRevision != r.revision {
		return ErrRoutineRevisionStale
	}
	if err := validateMutation(command.Mutation); err != nil {
		return err
	}
	if !validStatus(command.Target) || !canStatusTransition(r.status, command.Target) {
		return ErrRoutineStatusTransition
	}
	candidate, err := r.Clone()
	if err != nil {
		return err
	}
	candidate.status = command.Target
	candidate.updatedAt = command.Mutation.At
	candidate.revision++
	candidate.history = append(candidate.history, RevisionRecord{RoutineID: r.id, Operation: OperationStatusChanged, PreviousRevision: r.revision, NewRevision: candidate.revision, At: command.Mutation.At, Actor: command.Mutation.Actor, Reason: command.Mutation.Reason, CorrelationID: command.Mutation.CorrelationID, PreviousStatus: r.status, NewStatus: command.Target})
	if err = candidate.Validate(); err != nil {
		return err
	}
	*r = *candidate
	return nil
}

func occurrenceRefMatches(a, b OccurrenceRef) bool {
	return a.ID == b.ID && a.ObservedAt.Equal(b.ObservedAt) && equalStrings(a.ObservationIDs, b.ObservationIDs) && a.Weekday == b.Weekday && a.TimeBucket == b.TimeBucket && a.DayPart == b.DayPart && a.LocalDate == b.LocalDate && a.ContextQuality == b.ContextQuality && equalStrings(a.TopologyRevisions, b.TopologyRevisions)
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func (r *Routine) rebuild() error {
	sort.SliceStable(r.occurrences, func(i, j int) bool { return occurrenceRefLess(r.occurrences[i], r.occurrences[j]) })
	for i := 1; i < len(r.occurrences); i++ {
		if r.occurrences[i].ID == r.occurrences[i-1].ID {
			return ErrRoutineOccurrenceCollision
		}
	}
	bins, parts, intervals, days, weeks, first, last, err := deriveStatistics(r.occurrences)
	if err != nil {
		return err
	}
	r.temporalBins = bins
	r.dayPartCounts = parts
	r.intervalStatistics = intervals
	r.distinctLocalDays = days
	r.distinctLocalWeeks = weeks
	r.firstSeenAt = first
	r.lastSeenAt = last
	r.completeContextCount = 0
	r.partialContextCount = 0
	for _, o := range r.occurrences {
		if o.ContextQuality == "complete" {
			r.completeContextCount++
		} else if o.ContextQuality == "partial" {
			r.partialContextCount++
		}
	}
	return nil
}

func (r *Routine) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	return Snapshot{ID: r.id, Kind: r.kind, Subject: r.subject, Pattern: r.pattern, Status: r.status, Occurrences: cloneOccurrenceRefs(r.occurrences), OccurrenceCount: uint64(len(r.occurrences)), FirstSeenAt: r.firstSeenAt, LastSeenAt: r.lastSeenAt, DistinctLocalDays: r.distinctLocalDays, DistinctLocalWeeks: r.distinctLocalWeeks, TemporalBins: append([]TemporalBin(nil), r.temporalBins...), DayPartCounts: append([]DayPartCount(nil), r.dayPartCounts...), IntervalStatistics: r.intervalStatistics, CompleteContextCount: r.completeContextCount, PartialContextCount: r.partialContextCount, CreatedAt: r.createdAt, UpdatedAt: r.updatedAt, Revision: r.revision, History: append([]RevisionRecord(nil), r.history...)}
}
func (r *Routine) Clone() (*Routine, error) {
	if r == nil {
		return nil, ErrInvalidRoutine
	}
	clone, err := Restore(r.Snapshot())
	if err != nil {
		return nil, err
	}
	return clone, nil
}
func Restore(s Snapshot) (*Routine, error) {
	r := &Routine{id: s.ID, kind: s.Kind, subject: s.Subject, pattern: s.Pattern, status: s.Status, occurrences: cloneOccurrenceRefs(s.Occurrences), temporalBins: append([]TemporalBin(nil), s.TemporalBins...), dayPartCounts: append([]DayPartCount(nil), s.DayPartCounts...), intervalStatistics: s.IntervalStatistics, firstSeenAt: s.FirstSeenAt, lastSeenAt: s.LastSeenAt, distinctLocalDays: s.DistinctLocalDays, distinctLocalWeeks: s.DistinctLocalWeeks, completeContextCount: s.CompleteContextCount, partialContextCount: s.PartialContextCount, createdAt: s.CreatedAt, updatedAt: s.UpdatedAt, revision: s.Revision, history: append([]RevisionRecord(nil), s.History...)}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Routine) Validate() error {
	if r == nil {
		return ErrInvalidRoutine
	}
	if !validRoutineID(r.id) || !validKind(r.kind) || r.subject.Validate() != nil || r.pattern.Validate() != nil || r.pattern.Kind != r.kind || !validStatus(r.status) || r.revision == 0 || r.createdAt.IsZero() || r.updatedAt.IsZero() || r.updatedAt.Before(r.createdAt) || len(r.occurrences) == 0 {
		return ErrInvalidRoutine
	}
	for i, o := range r.occurrences {
		if !validOccurrenceID(o.ID) || o.ObservedAt.IsZero() || i > 0 && occurrenceRefLess(o, r.occurrences[i-1]) {
			return ErrInvalidRoutine
		}
		if i > 0 && o.ID == r.occurrences[i-1].ID {
			return ErrInvalidRoutine
		}
	}
	bins, parts, intervals, days, weeks, first, last, _ := deriveStatistics(r.occurrences)
	expectedComplete, expectedPartial := uint64(0), uint64(0)
	for _, occurrence := range r.occurrences {
		if occurrence.ContextQuality == "complete" {
			expectedComplete++
		}
		if occurrence.ContextQuality == "partial" {
			expectedPartial++
		}
	}
	if !equalBins(bins, r.temporalBins) || !equalParts(parts, r.dayPartCounts) || intervals != r.intervalStatistics || days != r.distinctLocalDays || weeks != r.distinctLocalWeeks || !first.Equal(r.firstSeenAt) || !last.Equal(r.lastSeenAt) || r.completeContextCount != expectedComplete || r.partialContextCount != expectedPartial {
		return ErrInvalidRoutine
	}
	if len(r.history) != int(r.revision) {
		return ErrInvalidRoutine
	}
	currentStatus := StatusCandidate
	lastAt := time.Time{}
	for i, h := range r.history {
		if h.NewRevision != uint64(i+1) || h.RoutineID != r.id || h.At.Before(r.createdAt) {
			return ErrInvalidRoutine
		}
		if err := h.validate(r.id); err != nil || (!lastAt.IsZero() && h.At.Before(lastAt)) {
			return ErrInvalidRoutine
		}
		if h.Operation == OperationStatusChanged {
			if h.PreviousStatus != currentStatus || !canStatusTransition(h.PreviousStatus, h.NewStatus) {
				return ErrInvalidRoutine
			}
			currentStatus = h.NewStatus
		}
		lastAt = h.At
	}
	if r.history[0].Operation != OperationRoutineCreated || currentStatus != r.status || !lastAt.Equal(r.updatedAt) {
		return ErrInvalidRoutine
	}
	return nil
}
func equalBins(a, b []TemporalBin) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func equalParts(a, b []DayPartCount) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
