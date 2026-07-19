package routines

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"synora/internal/cge/chains"
)

type Registry struct {
	mu              sync.RWMutex
	routines        map[RoutineID]*Routine
	bySubject       map[string]map[RoutineID]struct{}
	byKind          map[Kind]map[RoutineID]struct{}
	activeBySubject map[string]map[RoutineID]struct{}
}

func NewRegistry() *Registry {
	return &Registry{routines: make(map[RoutineID]*Routine), bySubject: make(map[string]map[RoutineID]struct{}), byKind: make(map[Kind]map[RoutineID]struct{}), activeBySubject: make(map[string]map[RoutineID]struct{})}
}
func (r *Registry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.routines)
}
func (r *Registry) Get(id RoutineID) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, ErrRoutineNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	routine, ok := r.routines[id]
	if !ok {
		return Snapshot{}, ErrRoutineNotFound
	}
	return routine.Snapshot(), nil
}
func (r *Registry) List() []Snapshot {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]RoutineID, 0, len(r.routines))
	for id := range r.routines {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]Snapshot, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.routines[id].Snapshot())
	}
	return out
}

func (r *Registry) ListBySubject(subject Subject) ([]Snapshot, error) {
	return r.listIndexed(subject, "", false)
}
func (r *Registry) ListBySubjectAndKind(subject Subject, kind Kind) ([]Snapshot, error) {
	if !validKind(kind) {
		return nil, ErrInvalidPattern
	}
	return r.listIndexed(subject, kind, false)
}

func (r *Registry) ListActiveBySubject(subject Subject) ([]Snapshot, error) {
	return r.listIndexed(subject, "", true)
}
func (r *Registry) listIndexed(subject Subject, kind Kind, active bool) ([]Snapshot, error) {
	if err := subject.Validate(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var source map[RoutineID]struct{}
	if active {
		source = r.activeBySubject[subject.key()]
	} else if kind != "" {
		source = r.byKindSubjectLocked(subject, kind)
	} else {
		source = r.bySubject[subject.key()]
	}
	ids := make([]RoutineID, 0, len(source))
	for id := range source {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]Snapshot, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.routines[id].Snapshot())
	}
	return out, nil
}
func (r *Registry) byKindSubjectLocked(subject Subject, kind Kind) map[RoutineID]struct{} {
	out := map[RoutineID]struct{}{}
	for id := range r.bySubject[subject.key()] {
		if r.routines[id].kind == kind {
			out[id] = struct{}{}
		}
	}
	return out
}

type ApplyResult struct {
	RoutineID  RoutineID
	Applied    bool
	Created    bool
	Idempotent bool
	Collision  bool
	Snapshot   Snapshot
}

// PreparedOccurrence is an internal transaction candidate. The candidate
// routine is deliberately private; callers can only publish it through the
// owning registry after the durable append succeeds.
type PreparedOccurrence struct {
	RoutineID  RoutineID
	Before     *Snapshot
	After      Snapshot
	Created    bool
	Idempotent bool

	candidate      *Routine
	sourceRevision uint64
}

type PreparedStatus struct {
	RoutineID RoutineID
	Before    Snapshot
	After     Snapshot

	candidate      *Routine
	sourceRevision uint64
}

// PrepareOccurrence builds a targeted candidate without changing the
// registry. It is used by the durable coordinator to keep publication after
// WAL durability while avoiding a deep clone of unrelated routines.
func (r *Registry) PrepareOccurrence(occurrence Occurrence, mutation chains.MutationContext) (PreparedOccurrence, error) {
	if r == nil {
		return PreparedOccurrence{}, ErrInvalidRoutine
	}
	if err := occurrence.Validate(); err != nil {
		return PreparedOccurrence{}, err
	}
	if err := validateMutation(mutation); err != nil {
		return PreparedOccurrence{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	existing, ok := r.routines[occurrence.RoutineID]
	if !ok {
		candidate, err := NewFromOccurrence(occurrence, mutation)
		if err != nil {
			return PreparedOccurrence{}, err
		}
		after := candidate.Snapshot()
		return PreparedOccurrence{RoutineID: occurrence.RoutineID, After: after, Created: true, candidate: candidate}, nil
	}
	if existing.status == StatusInvalidated {
		return PreparedOccurrence{}, ErrRoutineStatusTransition
	}
	if existing.kind != occurrence.Kind || existing.subject.key() != occurrence.Subject.key() || patternKey(existing.pattern) != patternKey(occurrence.Pattern) {
		return PreparedOccurrence{}, ErrRoutineMismatch
	}
	for _, ref := range existing.occurrences {
		if ref.ID == occurrence.ID {
			if occurrenceRefMatches(ref, occurrence.Ref()) {
				after := existing.Snapshot()
				return PreparedOccurrence{RoutineID: occurrence.RoutineID, Before: snapshotPointer(after), After: after, Idempotent: true, sourceRevision: existing.revision}, nil
			}
			return PreparedOccurrence{}, ErrRoutineOccurrenceCollision
		}
	}
	candidate, err := existing.Clone()
	if err != nil {
		return PreparedOccurrence{}, err
	}
	if err := candidate.AddOccurrence(AddOccurrenceCommand{RoutineID: occurrence.RoutineID, SourceRevision: existing.revision, Occurrence: occurrence, Mutation: mutation}); err != nil {
		return PreparedOccurrence{}, err
	}
	after := candidate.Snapshot()
	before := existing.Snapshot()
	return PreparedOccurrence{RoutineID: occurrence.RoutineID, Before: snapshotPointer(before), After: after, candidate: candidate, sourceRevision: existing.revision}, nil
}

// PublishPreparedOccurrence atomically replaces only the routine represented
// by a successful preparation. A concurrent source change is rejected as
// stale rather than overwriting it.
func (r *Registry) PublishPreparedOccurrence(prepared PreparedOccurrence) error {
	if r == nil {
		return ErrInvalidRoutine
	}
	if prepared.Idempotent {
		return nil
	}
	if prepared.candidate == nil {
		return ErrInvalidRoutine
	}
	if err := prepared.candidate.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.routines[prepared.RoutineID]
	if prepared.Created {
		if exists {
			return ErrRoutineRevisionStale
		}
	} else if !exists || current.revision != prepared.sourceRevision {
		return ErrRoutineRevisionStale
	}
	r.routines[prepared.RoutineID] = prepared.candidate
	if prepared.Created {
		r.addIndexesLocked(prepared.candidate)
	}
	return nil
}

// PrepareStatus builds an explicit status candidate without publishing it.
func (r *Registry) PrepareStatus(command SetStatusCommand) (PreparedStatus, error) {
	if r == nil {
		return PreparedStatus{}, ErrInvalidRoutine
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	current, ok := r.routines[command.RoutineID]
	if !ok {
		return PreparedStatus{}, ErrRoutineNotFound
	}
	candidate, err := current.Clone()
	if err != nil {
		return PreparedStatus{}, err
	}
	if err := candidate.SetStatus(command); err != nil {
		return PreparedStatus{}, err
	}
	return PreparedStatus{RoutineID: command.RoutineID, Before: current.Snapshot(), After: candidate.Snapshot(), candidate: candidate, sourceRevision: current.revision}, nil
}

func (r *Registry) PublishPreparedStatus(prepared PreparedStatus) error {
	if r == nil || prepared.candidate == nil {
		return ErrInvalidRoutine
	}
	if err := prepared.candidate.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.routines[prepared.RoutineID]
	if !ok || current.revision != prepared.sourceRevision {
		return ErrRoutineRevisionStale
	}
	r.routines[prepared.RoutineID] = prepared.candidate
	if current.status == StatusActive && prepared.candidate.status != StatusActive {
		delete(r.activeBySubject[current.subject.key()], prepared.RoutineID)
	}
	if current.status != StatusActive && prepared.candidate.status == StatusActive {
		if r.activeBySubject[prepared.candidate.subject.key()] == nil {
			r.activeBySubject[prepared.candidate.subject.key()] = map[RoutineID]struct{}{}
		}
		r.activeBySubject[prepared.candidate.subject.key()][prepared.RoutineID] = struct{}{}
	}
	return nil
}

func snapshotPointer(snapshot Snapshot) *Snapshot {
	copy := snapshot
	copy.Occurrences = cloneOccurrenceRefs(snapshot.Occurrences)
	copy.TemporalBins = append([]TemporalBin(nil), snapshot.TemporalBins...)
	copy.DayPartCounts = append([]DayPartCount(nil), snapshot.DayPartCounts...)
	copy.History = append([]RevisionRecord(nil), snapshot.History...)
	return &copy
}

// ReplayCreated restores one complete initial snapshot and is intentionally
// separate from normal mutation APIs: replay must not manufacture a new
// revision or timestamp.
func (r *Registry) ReplayCreated(snapshot Snapshot) error {
	if r == nil {
		return ErrInvalidRoutine
	}
	owned, err := Restore(snapshot)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.routines[snapshot.ID]; exists {
		return ErrRoutineOccurrenceCollision
	}
	r.routines[snapshot.ID] = owned
	r.addIndexesLocked(owned)
	return nil
}

// ReplayOccurrence applies the exact local revision carried by a journal
// delta and returns the resulting detached snapshot.
func (r *Registry) ReplayOccurrence(routineID RoutineID, sourceRevision uint64, occurrence Occurrence, revision RevisionRecord) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, ErrInvalidRoutine
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.routines[routineID]
	if !ok {
		return Snapshot{}, ErrRoutineNotFound
	}
	if current.revision != sourceRevision {
		return Snapshot{}, ErrRoutineRevisionStale
	}
	candidate, err := current.Clone()
	if err != nil {
		return Snapshot{}, err
	}
	mutation := chains.MutationContext{At: revision.At, Actor: revision.Actor, Reason: revision.Reason, CorrelationID: revision.CorrelationID}
	if err := candidate.AddOccurrence(AddOccurrenceCommand{RoutineID: routineID, SourceRevision: sourceRevision, Occurrence: occurrence, Mutation: mutation}); err != nil {
		return Snapshot{}, err
	}
	if !equalRevision(candidate.history[len(candidate.history)-1], revision) {
		return Snapshot{}, ErrRoutineRevisionStale
	}
	r.routines[routineID] = candidate
	return candidate.Snapshot(), nil
}

// ReplayStatus applies one explicit status revision during recovery.
func (r *Registry) ReplayStatus(command SetStatusCommand, expected RevisionRecord) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, ErrInvalidRoutine
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.routines[command.RoutineID]
	if !ok {
		return Snapshot{}, ErrRoutineNotFound
	}
	if current.revision != command.SourceRevision {
		return Snapshot{}, ErrRoutineRevisionStale
	}
	candidate, err := current.Clone()
	if err != nil {
		return Snapshot{}, err
	}
	if err := candidate.SetStatus(command); err != nil {
		return Snapshot{}, err
	}
	if !equalRevision(candidate.history[len(candidate.history)-1], expected) {
		return Snapshot{}, ErrRoutineRevisionStale
	}
	r.routines[command.RoutineID] = candidate
	if current.status == StatusActive && candidate.status != StatusActive {
		delete(r.activeBySubject[current.subject.key()], command.RoutineID)
	}
	if current.status != StatusActive && candidate.status == StatusActive {
		if r.activeBySubject[candidate.subject.key()] == nil {
			r.activeBySubject[candidate.subject.key()] = map[RoutineID]struct{}{}
		}
		r.activeBySubject[candidate.subject.key()][command.RoutineID] = struct{}{}
	}
	return candidate.Snapshot(), nil
}

func equalRevision(a, b RevisionRecord) bool {
	return a.RoutineID == b.RoutineID && a.Operation == b.Operation && a.PreviousRevision == b.PreviousRevision && a.NewRevision == b.NewRevision && a.At.Equal(b.At) && a.Actor == b.Actor && a.Reason == b.Reason && a.CorrelationID == b.CorrelationID && a.OccurrenceID == b.OccurrenceID && a.PreviousStatus == b.PreviousStatus && a.NewStatus == b.NewStatus
}

func (r *Registry) ApplyOccurrence(occurrence Occurrence, mutation chains.MutationContext) (ApplyResult, error) {
	if r == nil {
		return ApplyResult{}, ErrInvalidRoutine
	}
	if err := occurrence.Validate(); err != nil {
		return ApplyResult{}, err
	}
	if err := validateMutation(mutation); err != nil {
		return ApplyResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.routines[occurrence.RoutineID]; ok {
		if existing.status == StatusInvalidated {
			return ApplyResult{}, ErrRoutineStatusTransition
		}
		if existing.kind != occurrence.Kind || existing.subject.key() != occurrence.Subject.key() || patternKey(existing.pattern) != patternKey(occurrence.Pattern) {
			return ApplyResult{}, ErrRoutineMismatch
		}
		for _, ref := range existing.occurrences {
			if ref.ID == occurrence.ID {
				if occurrenceRefMatches(ref, occurrence.Ref()) {
					return ApplyResult{RoutineID: occurrence.RoutineID, Idempotent: true, Snapshot: existing.Snapshot()}, nil
				}
				return ApplyResult{}, ErrRoutineOccurrenceCollision
			}
		}
		candidate, err := existing.Clone()
		if err != nil {
			return ApplyResult{}, err
		}
		err = candidate.AddOccurrence(AddOccurrenceCommand{RoutineID: occurrence.RoutineID, SourceRevision: existing.revision, Occurrence: occurrence, Mutation: mutation})
		if err != nil {
			return ApplyResult{}, err
		}
		r.routines[occurrence.RoutineID] = candidate
		return ApplyResult{RoutineID: occurrence.RoutineID, Applied: true, Snapshot: candidate.Snapshot()}, nil
	}
	created, err := NewFromOccurrence(occurrence, mutation)
	if err != nil {
		return ApplyResult{}, err
	}
	r.routines[occurrence.RoutineID] = created
	r.addIndexesLocked(created)
	return ApplyResult{RoutineID: occurrence.RoutineID, Applied: true, Created: true, Snapshot: created.Snapshot()}, nil
}

func (r *Registry) SetStatus(command SetStatusCommand) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, ErrInvalidRoutine
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	routine, ok := r.routines[command.RoutineID]
	if !ok {
		return Snapshot{}, ErrRoutineNotFound
	}
	candidate, err := routine.Clone()
	if err != nil {
		return Snapshot{}, err
	}
	if err = candidate.SetStatus(command); err != nil {
		return Snapshot{}, err
	}
	r.routines[command.RoutineID] = candidate
	if routine.status == StatusActive && candidate.status != StatusActive {
		delete(r.activeBySubject[routine.subject.key()], command.RoutineID)
	}
	if routine.status != StatusActive && candidate.status == StatusActive {
		if r.activeBySubject[candidate.subject.key()] == nil {
			r.activeBySubject[candidate.subject.key()] = map[RoutineID]struct{}{}
		}
		r.activeBySubject[candidate.subject.key()][command.RoutineID] = struct{}{}
	}
	return candidate.Snapshot(), nil
}

type LearningOccurrenceResult struct {
	OccurrenceID OccurrenceID
	RoutineID    RoutineID
	Applied      bool
	Created      bool
	Idempotent   bool
	ErrorCode    string
}
type LearningApplyResult struct {
	ChainID         chains.ChainID
	Results         []LearningOccurrenceResult
	AppliedCount    int
	IdempotentCount int
	ErrorCount      int
}

func (r *Registry) ApplyLearningPlan(plan LearningPlan, actor, correlationID string) LearningApplyResult {
	result := LearningApplyResult{ChainID: plan.ChainID}
	for _, occurrence := range plan.Occurrences {
		mutation := chains.MutationContext{At: occurrence.ObservedAt, Actor: actor, Reason: "routine occurrence extracted", CorrelationID: boundedCorrelation(correlationID, occurrence.ID)}
		applied, err := r.ApplyOccurrence(occurrence, mutation)
		item := LearningOccurrenceResult{OccurrenceID: occurrence.ID, RoutineID: occurrence.RoutineID, Applied: applied.Applied, Created: applied.Created, Idempotent: applied.Idempotent}
		if err != nil {
			item.ErrorCode = errorCode(err)
			result.ErrorCount++
		} else if applied.Idempotent {
			result.IdempotentCount++
		} else if applied.Applied {
			result.AppliedCount++
		}
		result.Results = append(result.Results, item)
	}
	return result
}

func boundedCorrelation(base string, id OccurrenceID) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "cge-routines"
	}
	value := base + ":occ:" + string(id)
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}
func errorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case err == ErrDuplicateRoutineOccurrence:
		return "duplicate_routine_occurrence"
	case err == ErrRoutineOccurrenceCollision:
		return "routine_occurrence_collision"
	case err == ErrRoutineRevisionStale:
		return "routine_revision_stale"
	default:
		return "routine_error"
	}
}

func (r *Registry) Clone() (*Registry, error) {
	if r == nil {
		return nil, ErrInvalidRoutine
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	clone := NewRegistry()
	for id, routine := range r.routines {
		copy, err := routine.Clone()
		if err != nil {
			return nil, err
		}
		clone.routines[id] = copy
	}
	clone.rebuildIndexesLocked()
	return clone, nil
}
func (r *Registry) rebuildIndexesLocked() {
	r.bySubject = make(map[string]map[RoutineID]struct{})
	r.byKind = make(map[Kind]map[RoutineID]struct{})
	r.activeBySubject = make(map[string]map[RoutineID]struct{})
	for id, routine := range r.routines {
		subjectKey := routine.subject.key()
		if r.bySubject[subjectKey] == nil {
			r.bySubject[subjectKey] = map[RoutineID]struct{}{}
		}
		r.bySubject[subjectKey][id] = struct{}{}
		if r.byKind[routine.kind] == nil {
			r.byKind[routine.kind] = map[RoutineID]struct{}{}
		}
		r.byKind[routine.kind][id] = struct{}{}
		if routine.status == StatusActive {
			if r.activeBySubject[subjectKey] == nil {
				r.activeBySubject[subjectKey] = map[RoutineID]struct{}{}
			}
			r.activeBySubject[subjectKey][id] = struct{}{}
		}
	}
}

func (r *Registry) addIndexesLocked(routine *Routine) {
	if routine == nil {
		return
	}
	if r.bySubject == nil {
		r.bySubject = make(map[string]map[RoutineID]struct{})
	}
	if r.byKind == nil {
		r.byKind = make(map[Kind]map[RoutineID]struct{})
	}
	if r.activeBySubject == nil {
		r.activeBySubject = make(map[string]map[RoutineID]struct{})
	}
	subjectKey := routine.subject.key()
	if r.bySubject[subjectKey] == nil {
		r.bySubject[subjectKey] = make(map[RoutineID]struct{})
	}
	r.bySubject[subjectKey][routine.id] = struct{}{}
	if r.byKind[routine.kind] == nil {
		r.byKind[routine.kind] = make(map[RoutineID]struct{})
	}
	r.byKind[routine.kind][routine.id] = struct{}{}
	if routine.status == StatusActive {
		if r.activeBySubject[subjectKey] == nil {
			r.activeBySubject[subjectKey] = make(map[RoutineID]struct{})
		}
		r.activeBySubject[subjectKey][routine.id] = struct{}{}
	}
}
func (r *Registry) Validate() error {
	if r == nil {
		return ErrInvalidRoutine
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id, routine := range r.routines {
		if id != routine.id {
			return ErrInvalidRoutine
		}
		if err := routine.Validate(); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		subjectKey := routine.subject.key()
		if _, ok := r.bySubject[subjectKey][id]; !ok {
			return ErrInvalidRoutine
		}
		if _, ok := r.byKind[routine.kind][id]; !ok {
			return ErrInvalidRoutine
		}
		_, indexedActive := r.activeBySubject[subjectKey][id]
		if indexedActive != (routine.status == StatusActive) {
			return ErrInvalidRoutine
		}
	}
	return nil
}
