package hypotheses

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"synora/internal/cge/chains"
)

// Registry owns hypothesis aggregates and is the only concurrent access
// boundary in this pass. Stored aggregates are never returned directly.
type Registry struct {
	mu   sync.RWMutex
	sets map[SetID]*HypothesisSet

	// These indexes are derived acceleration structures. They are deliberately
	// not part of snapshots or journal records and are rebuilt after every
	// registry mutation/recovery.
	currentBySubject    map[string]SetID
	openEvidenceByChain map[chains.ChainID]map[SetID]struct{}
}

func NewRegistry() *Registry {
	return &Registry{sets: make(map[SetID]*HypothesisSet), currentBySubject: make(map[string]SetID), openEvidenceByChain: make(map[chains.ChainID]map[SetID]struct{})}
}

// CloneShallow creates a transaction candidate by copying only the ownership
// table. Mutation methods clone the affected aggregate before replacing its
// entry, so unchanged hypotheses remain immutable and safely shareable between
// the published and prepared candidates.
func (r *Registry) CloneShallow() (*Registry, error) {
	if r == nil {
		return nil, hypothesisError(ErrInvalidHypothesis, "", "", "registry_clone", 0, 0, errors.New("registry is nil"))
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	clone := NewRegistry()
	clone.sets = make(map[SetID]*HypothesisSet, len(r.sets))
	for id, set := range r.sets {
		clone.sets[id] = set
	}
	clone.rebuildIndexesLocked()
	return clone, nil
}

// Clone returns a validated, independently owned registry snapshot.
func (r *Registry) Clone() (*Registry, error) {
	if r == nil {
		return nil, hypothesisError(ErrInvalidHypothesis, "", "", "registry_clone", 0, 0, errors.New("registry is nil"))
	}
	clone := NewRegistry()
	for _, snapshot := range r.List() {
		set, err := Restore(snapshot)
		if err != nil {
			return nil, hypothesisError(ErrInvalidHypothesis, snapshot.Family, snapshot.ID, "registry_clone", snapshot.Revision, snapshot.Revision, err)
		}
		clone.sets[set.ID()] = set
	}
	if err := clone.validateLineageLocked(); err != nil {
		return nil, err
	}
	clone.rebuildIndexesLocked()
	return clone, nil
}

func (r *Registry) cloneLocked() (*Registry, error) {
	clone := NewRegistry()
	ids := make([]SetID, 0, len(r.sets))
	for id := range r.sets {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		set, err := r.sets[id].Clone()
		if err != nil {
			return nil, err
		}
		clone.sets[id] = set
	}
	clone.rebuildIndexesLocked()
	return clone, nil
}

func (r *Registry) Add(set *HypothesisSet) error {
	if r == nil {
		return hypothesisError(ErrInvalidHypothesis, "", "", "add", 0, 0, errors.New("registry is nil"))
	}
	if set == nil {
		return hypothesisError(ErrInvalidHypothesis, "", "", "add", 0, 0, errors.New("hypothesis is nil"))
	}
	if err := set.Validate(); err != nil {
		return err
	}
	clone, err := set.Clone()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sets == nil {
		r.sets = make(map[SetID]*HypothesisSet)
	}
	if existing, ok := r.sets[clone.id]; ok {
		if reflect.DeepEqual(existing.Snapshot(), clone.Snapshot()) {
			return &AlreadyExistsError{SetID: clone.id, Identical: true}
		}
		return &CollisionError{SetID: clone.id}
	}
	r.sets[clone.id] = clone
	if clone.lineage.PredecessorSetID == "" && clone.lineage.SuccessorSetID == "" {
		// Opening a root cannot alter any existing lineage. The existing
		// registry was validated at recovery/publication time, so avoid a
		// full historical scan on every runtime hypothesis opening.
		if err := clone.Snapshot().Lineage.Validate(clone.id); err != nil {
			delete(r.sets, clone.id)
			return err
		}
	} else if err := r.validateLineageLocked(); err != nil {
		delete(r.sets, clone.id)
		return err
	}
	r.rebuildIndexesLocked()
	return nil
}

func (r *Registry) Get(id SetID) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, hypothesisError(ErrHypothesisNotFound, "", id, "get", 0, 0, errors.New("registry is nil"))
	}
	r.mu.RLock()
	set, ok := r.sets[id]
	if !ok {
		r.mu.RUnlock()
		return Snapshot{}, hypothesisError(ErrHypothesisNotFound, "", id, "get", 0, 0, nil)
	}
	snapshot := set.Snapshot()
	r.mu.RUnlock()
	return snapshot, nil
}

func (r *Registry) List() []Snapshot {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	ids := make([]SetID, 0, len(r.sets))
	for id := range r.sets {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]Snapshot, 0, len(ids))
	for _, id := range ids {
		result = append(result, r.sets[id].Snapshot())
	}
	r.mu.RUnlock()
	return result
}

func (r *Registry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	count := len(r.sets)
	r.mu.RUnlock()
	return count
}

func (r *Registry) SetStatus(id SetID, sourceRevision uint64, target Status, mutation chains.MutationContext) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, hypothesisError(ErrHypothesisNotFound, "", id, "status", sourceRevision, 0, errors.New("registry is nil"))
	}
	if sourceRevision == 0 {
		return Snapshot{}, hypothesisError(ErrStaleHypothesisCommand, "", id, "status", sourceRevision, 0, fmt.Errorf("source revision must be positive"))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	set, ok := r.sets[id]
	if !ok {
		return Snapshot{}, hypothesisError(ErrHypothesisNotFound, "", id, "status", sourceRevision, 0, nil)
	}
	if set.revision != sourceRevision {
		return Snapshot{}, hypothesisError(ErrStaleHypothesisCommand, set.family, id, "status", sourceRevision, set.revision, nil)
	}
	clone, err := set.Clone()
	if err != nil {
		return Snapshot{}, err
	}
	if err := clone.SetStatus(target, mutation); err != nil {
		return Snapshot{}, err
	}
	if err := clone.Validate(); err != nil {
		return Snapshot{}, err
	}
	r.sets[id] = clone
	r.rebuildIndexesLocked()
	return clone.Snapshot(), nil
}

// Resolve applies the specialized terminal resolution operation to one
// aggregate in a cloned registry. It does not touch any chain registry.
func (r *Registry) Resolve(command ResolveCommand, outcome ResolutionOutcome) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, ErrHypothesisNotFound
	}
	command = command.Clone()
	if err := command.Validate(); err != nil {
		return Snapshot{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	set, ok := r.sets[command.SetID]
	if !ok {
		return Snapshot{}, hypothesisError(ErrHypothesisNotFound, "", command.SetID, "resolve", command.SourceRevision, 0, nil)
	}
	if set.revision != command.SourceRevision {
		return Snapshot{}, hypothesisError(ErrStaleHypothesisResolution, set.family, command.SetID, "resolve", command.SourceRevision, set.revision, nil)
	}
	clone, err := set.Clone()
	if err != nil {
		return Snapshot{}, err
	}
	if err := clone.MarkResolved(command, outcome); err != nil {
		return Snapshot{}, err
	}
	if err := clone.Validate(); err != nil {
		return Snapshot{}, err
	}
	r.sets[command.SetID] = clone
	r.rebuildIndexesLocked()
	return clone.Snapshot(), nil
}

// Rebase applies one optimistic append-only assessment version transactionally.
func (r *Registry) Rebase(command RebaseCommand) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, hypothesisError(ErrHypothesisNotFound, command.Family, command.SetID, "rebase", command.SourceRevision, 0, errors.New("registry is nil"))
	}
	if err := command.Validate(); err != nil {
		return Snapshot{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	set, ok := r.sets[command.SetID]
	if !ok {
		return Snapshot{}, hypothesisError(ErrHypothesisNotFound, command.Family, command.SetID, "rebase", command.SourceRevision, 0, nil)
	}
	if set.revision != command.SourceRevision {
		return Snapshot{}, hypothesisError(ErrStaleHypothesisRebase, set.family, command.SetID, "rebase", command.SourceRevision, set.revision, nil)
	}
	clone, err := set.Clone()
	if err != nil {
		return Snapshot{}, err
	}
	if err := clone.Rebase(command); err != nil {
		return Snapshot{}, err
	}
	if err := clone.Validate(); err != nil {
		return Snapshot{}, err
	}
	r.sets[command.SetID] = clone
	r.rebuildIndexesLocked()
	return clone.Snapshot(), nil
}

func (r *Registry) Supersede(command SupersedeCommand) (SupersessionApplyResult, error) {
	if r == nil {
		return SupersessionApplyResult{}, hypothesisError(ErrHypothesisNotFound, FamilyEvidence, command.PreviousSetID, "supersede", command.PreviousSourceRevision, 0, errors.New("registry is nil"))
	}
	if err := command.Validate(); err != nil {
		return SupersessionApplyResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	previous, ok := r.sets[command.PreviousSetID]
	if !ok {
		return SupersessionApplyResult{}, hypothesisError(ErrHypothesisNotFound, FamilyEvidence, command.PreviousSetID, "supersede", command.PreviousSourceRevision, 0, nil)
	}
	previousBefore := previous.Snapshot()
	if existing, exists := r.sets[command.NewSetID]; exists {
		if previous.status == StatusSuperseded && previous.lineage.SuccessorSetID == command.NewSetID && reflect.DeepEqual(existing.Snapshot(), command.NewSet) {
			return SupersessionApplyResult{PreviousBefore: previousBefore, PreviousAfter: previousBefore, NewAfter: existing.Snapshot()}, nil
		}
		if previous.lineage.SuccessorSetID != "" && previous.lineage.SuccessorSetID != command.NewSetID {
			return SupersessionApplyResult{}, hypothesisError(ErrHypothesisSupersessionCollision, FamilyEvidence, command.PreviousSetID, "supersede", command.PreviousSourceRevision, previous.revision, nil)
		}
		if !reflect.DeepEqual(existing.Snapshot(), command.NewSet) {
			return SupersessionApplyResult{}, hypothesisError(ErrHypothesisSuccessorCollision, FamilyEvidence, command.NewSetID, "supersede", command.PreviousSourceRevision, previous.revision, nil)
		}
		return SupersessionApplyResult{}, hypothesisError(ErrHypothesisLineageDivergence, FamilyEvidence, command.PreviousSetID, "supersede", command.PreviousSourceRevision, previous.revision, nil)
	}
	if previous.revision != command.PreviousSourceRevision {
		return SupersessionApplyResult{}, hypothesisError(ErrStaleHypothesisSupersession, previous.family, command.PreviousSetID, "supersede", command.PreviousSourceRevision, previous.revision, nil)
	}
	if len(previous.assessments) == 0 || previous.currentAssessmentVersion != command.PreviousAssessmentVersion || previous.assessments[len(previous.assessments)-1].ID != command.PreviousAssessmentID {
		return SupersessionApplyResult{}, hypothesisError(ErrStaleHypothesisSupersession, previous.family, command.PreviousSetID, "supersede_assessment", command.PreviousSourceRevision, previous.revision, nil)
	}
	candidate, err := r.cloneShallowLocked()
	if err != nil {
		return SupersessionApplyResult{}, err
	}
	old, err := candidate.sets[command.PreviousSetID].Clone()
	if err != nil {
		return SupersessionApplyResult{}, err
	}
	newSet, err := Restore(command.NewSet)
	if err != nil {
		return SupersessionApplyResult{}, err
	}
	if err := old.MarkSuperseded(newSet.Snapshot(), command.Mutation); err != nil {
		return SupersessionApplyResult{}, err
	}
	candidate.sets[command.PreviousSetID] = old
	candidate.sets[command.NewSetID] = newSet
	if err := candidate.validateLineageDeltaLocked(command.PreviousSetID, command.NewSetID); err != nil {
		return SupersessionApplyResult{}, err
	}
	result := SupersessionApplyResult{PreviousBefore: previousBefore, PreviousAfter: old.Snapshot(), NewAfter: newSet.Snapshot(), PreviousRevision: old.history[len(old.history)-1], NewOpeningRevision: newSet.history[0]}
	r.sets = candidate.sets
	r.rebuildIndexesLocked()
	return result, nil
}

func (r *Registry) cloneShallowLocked() (*Registry, error) {
	clone := NewRegistry()
	clone.sets = make(map[SetID]*HypothesisSet, len(r.sets))
	for id, set := range r.sets {
		clone.sets[id] = set
	}
	clone.rebuildIndexesLocked()
	return clone, nil
}

func subjectIndexKey(family Family, subject Subject) string {
	if family == FamilyAssociation {
		return string(family) + "\x00" + subject.ObservationID
	}
	return string(family) + "\x00" + string(subject.ChainID) + "\x00" + subject.ObservationID
}

func (r *Registry) rebuildIndexesLocked() {
	r.currentBySubject = make(map[string]SetID)
	r.openEvidenceByChain = make(map[chains.ChainID]map[SetID]struct{})
	for id, set := range r.sets {
		snapshot := set.Snapshot()
		key := subjectIndexKey(snapshot.Family, snapshot.Subject)
		if current, ok := r.currentBySubject[key]; !ok || preferCurrent(snapshot, r.sets[current].Snapshot()) {
			r.currentBySubject[key] = id
		}
		if snapshot.Family == FamilyEvidence && (snapshot.Status == StatusOpen || snapshot.Status == StatusUnderReview) {
			if r.openEvidenceByChain[snapshot.Subject.ChainID] == nil {
				r.openEvidenceByChain[snapshot.Subject.ChainID] = make(map[SetID]struct{})
			}
			r.openEvidenceByChain[snapshot.Subject.ChainID][id] = struct{}{}
		}
	}
}

func (r *Registry) validateLineageDeltaLocked(previousID, successorID SetID) error {
	previous, previousOK := r.sets[previousID]
	successor, successorOK := r.sets[successorID]
	if !previousOK || !successorOK {
		return ErrHypothesisLineageDivergence
	}
	previousSnapshot, successorSnapshot := previous.Snapshot(), successor.Snapshot()
	if err := previousSnapshot.Validate(); err != nil {
		return err
	}
	if err := successorSnapshot.Validate(); err != nil {
		return err
	}
	if successorSnapshot.Lineage.PredecessorSetID != previousID || previousSnapshot.Lineage.SuccessorSetID != successorID {
		return ErrHypothesisLineageDivergence
	}
	if successorSnapshot.Lineage.RootSetID != previousSnapshot.Lineage.RootSetID || successorSnapshot.Lineage.Generation != previousSnapshot.Lineage.Generation+1 || previousSnapshot.Family != FamilyEvidence || successorSnapshot.Family != FamilyEvidence || previousSnapshot.Subject.ChainID != successorSnapshot.Subject.ChainID || previousSnapshot.Subject.ObservationID != successorSnapshot.Subject.ObservationID || previousSnapshot.Status != StatusSuperseded || successorSnapshot.Status == StatusSuperseded || previousSnapshot.Subject.EvidenceFingerprint == successorSnapshot.Subject.EvidenceFingerprint {
		return ErrHypothesisLineageDivergence
	}
	return nil
}

func preferCurrent(candidate, current Snapshot) bool {
	if candidate.Lineage.Generation != current.Lineage.Generation {
		return candidate.Lineage.Generation > current.Lineage.Generation
	}
	if candidate.Revision != current.Revision {
		return candidate.Revision > current.Revision
	}
	return candidate.ID > current.ID
}

// FindCurrentSubject returns the latest non-superseded dossier for a derived
// subject key, including terminal dossiers that must remain preserved.
func (r *Registry) FindCurrentSubject(family Family, subject Subject) (Snapshot, bool, error) {
	if r == nil {
		return Snapshot{}, false, ErrInvalidHypothesis
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.currentBySubject[subjectIndexKey(family, subject)]
	if !ok {
		return Snapshot{}, false, nil
	}
	set, ok := r.sets[id]
	if !ok {
		return Snapshot{}, false, ErrInvalidHypothesis
	}
	return set.Snapshot(), true, nil
}

// FindCurrentEvidenceSubject finds one current evidence dossier without
// scanning all hypothesis aggregates.
func (r *Registry) FindCurrentEvidenceSubject(chainID chains.ChainID, observationID string) (Snapshot, bool, error) {
	return r.FindCurrentSubject(FamilyEvidence, Subject{ChainID: chainID, ObservationID: observationID})
}

// FindCurrentAssociationSubject finds one current association dossier.
func (r *Registry) FindCurrentAssociationSubject(observationID string) (Snapshot, bool, error) {
	return r.FindCurrentSubject(FamilyAssociation, Subject{ObservationID: observationID})
}

// ListOpenEvidenceForChain returns only active evidence dossiers in a stable
// target/generation/SetID order.
func (r *Registry) ListOpenEvidenceForChain(chainID chains.ChainID) ([]Snapshot, error) {
	if r == nil {
		return nil, ErrInvalidHypothesis
	}
	r.mu.RLock()
	ids := make([]SetID, 0, len(r.openEvidenceByChain[chainID]))
	for id := range r.openEvidenceByChain[chainID] {
		ids = append(ids, id)
	}
	result := make([]Snapshot, 0, len(ids))
	for _, id := range ids {
		if set, ok := r.sets[id]; ok {
			result = append(result, set.Snapshot())
		}
	}
	r.mu.RUnlock()
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Subject.ObservationID != result[j].Subject.ObservationID {
			return result[i].Subject.ObservationID < result[j].Subject.ObservationID
		}
		if result[i].Lineage.Generation != result[j].Lineage.Generation {
			return result[i].Lineage.Generation < result[j].Lineage.Generation
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (r *Registry) validateLineageLocked() error {
	byID := make(map[SetID]Snapshot, len(r.sets))
	for id, set := range r.sets {
		byID[id] = set.Snapshot()
	}
	for start := range byID {
		seen := make(map[SetID]struct{})
		current := start
		for current != "" {
			if _, exists := seen[current]; exists {
				return ErrHypothesisLineageCycle
			}
			seen[current] = struct{}{}
			snapshot, exists := byID[current]
			if !exists {
				break
			}
			current = snapshot.Lineage.SuccessorSetID
		}
	}
	for id, snapshot := range byID {
		if err := snapshot.Lineage.Validate(id); err != nil {
			return hypothesisError(ErrInvalidHypothesis, snapshot.Family, id, "lineage", snapshot.Revision, snapshot.Revision, err)
		}
		lineage := snapshot.Lineage
		if lineage.PredecessorSetID != "" {
			predecessor, exists := byID[lineage.PredecessorSetID]
			if !exists || predecessor.Lineage.SuccessorSetID != id || predecessor.Lineage.RootSetID != lineage.RootSetID || predecessor.Lineage.Generation+1 != lineage.Generation || predecessor.Family != FamilyEvidence || snapshot.Family != FamilyEvidence || predecessor.Subject.ChainID != snapshot.Subject.ChainID || predecessor.Subject.ObservationID != snapshot.Subject.ObservationID || predecessor.Status != StatusSuperseded || predecessor.Subject.EvidenceFingerprint == snapshot.Subject.EvidenceFingerprint {
				return ErrHypothesisLineageDivergence
			}
		}
		if lineage.SuccessorSetID != "" {
			successor, exists := byID[lineage.SuccessorSetID]
			if !exists || successor.Lineage.PredecessorSetID != id || successor.Lineage.RootSetID != lineage.RootSetID || successor.Lineage.Generation != lineage.Generation+1 || snapshot.Status != StatusSuperseded {
				return ErrHypothesisLineageDivergence
			}
		}
		if snapshot.Status == StatusSuperseded && lineage.SuccessorSetID == "" {
			return ErrHypothesisLineageDivergence
		}
		if snapshot.Status != StatusSuperseded && lineage.SuccessorSetID != "" {
			return ErrHypothesisLineageDivergence
		}
	}
	return nil
}

func (r *Registry) ValidateLineage() error {
	if r == nil {
		return ErrInvalidHypothesis
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.validateLineageLocked()
}

func (r *Registry) Lineage(id SetID) ([]Snapshot, error) {
	if r == nil {
		return nil, hypothesisError(ErrHypothesisNotFound, "", id, "lineage", 0, 0, nil)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.sets[id]; !ok {
		return nil, hypothesisError(ErrHypothesisNotFound, "", id, "lineage", 0, 0, nil)
	}
	if err := r.validateLineageLocked(); err != nil {
		return nil, err
	}
	current := r.sets[id].Snapshot()
	result := []Snapshot{current}
	for current.Lineage.PredecessorSetID != "" {
		predecessor := r.sets[current.Lineage.PredecessorSetID].Snapshot()
		result = append(result, predecessor)
		current = predecessor
		if len(result) > len(r.sets) {
			return nil, ErrHypothesisLineageCycle
		}
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
}

type AlreadyExistsError struct {
	SetID     SetID
	Identical bool
}

func (e *AlreadyExistsError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s: set=%s identical=%t", ErrHypothesisAlreadyExists, e.SetID, e.Identical)
}
func (e *AlreadyExistsError) Unwrap() error { return ErrHypothesisAlreadyExists }

type CollisionError struct{ SetID SetID }

func (e *CollisionError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s: set=%s", ErrHypothesisCollision, e.SetID)
}
func (e *CollisionError) Unwrap() error { return ErrHypothesisCollision }
