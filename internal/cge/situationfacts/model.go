package situationfacts

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"synora/internal/cge/episodes"
)

type FactID string
type FactCode string
type FactKey string

type FactScope string

const (
	ScopeEpisode     FactScope = "episode"
	ScopeEntity      FactScope = "entity"
	ScopeObservation FactScope = "observation"
	ScopeTransition  FactScope = "transition"
	ScopeContext     FactScope = "context"
	ScopeMemory      FactScope = "memory"
)

type FactOrigin string

const (
	OriginObserved FactOrigin = "observed"
	OriginDerived  FactOrigin = "derived"
	OriginCarried  FactOrigin = "carried"
)

type FactStatus string

const (
	StatusAsserted    FactStatus = "asserted"
	StatusUnknown     FactStatus = "unknown"
	StatusConflicting FactStatus = "conflicting"
	StatusRetracted   FactStatus = "retracted"
)

type FactSubject struct {
	Kind string
	ID   string
	Role string
}

type FactQuality struct {
	CompletenessPermille int
	ReliabilityPermille  int
	SourceCount          int
	Partial              bool
}

type Fact struct {
	ID   FactID
	Key  FactKey
	Code FactCode

	Scope     FactScope
	Subject   FactSubject
	Predicate string
	Value     FactValue

	Origin FactOrigin
	Status FactStatus

	ValidFrom time.Time
	ValidTo   *time.Time

	Quality    FactQuality
	Provenance []ProvenanceRef
}

type ConflictSet struct {
	ID         string
	Key        FactKey
	FactIDs    []FactID
	Code       string
	Provenance []ProvenanceRef
}

type FactSet struct {
	EpisodeID       episodes.EpisodeID
	EpisodeRevision uint64
	ExtractedAt     time.Time

	SchemaFingerprint string
	PolicyFingerprint string

	Facts     []Fact
	Conflicts []ConflictSet

	Fingerprint string
}

type FactSetDiff struct {
	EpisodeID episodes.EpisodeID

	BeforeEpisodeRevision uint64
	AfterEpisodeRevision  uint64

	Added   []Fact
	Removed []Fact
	Changed []FactChange

	ConflictsAdded   []ConflictSet
	ConflictsRemoved []ConflictSet

	BeforeFingerprint string
	AfterFingerprint  string
}

type FactChange struct {
	Key    FactKey
	Before Fact
	After  Fact
}

type ExtractionReport struct {
	EpisodeID       episodes.EpisodeID
	EpisodeRevision uint64

	FactCount     int
	ConflictCount int
	UnknownCount  int

	ObservedCount int
	DerivedCount  int
	CarriedCount  int

	Codes []FactCode

	SchemaFingerprint  string
	PolicyFingerprint  string
	FactSetFingerprint string
}

func validText(value string, allowEmpty bool, max int) bool {
	return (allowEmpty || value != "") && len([]rune(value)) <= max && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n")
}

func validScope(value FactScope) bool {
	return value == ScopeEpisode || value == ScopeEntity || value == ScopeObservation || value == ScopeTransition || value == ScopeContext || value == ScopeMemory
}

func validOrigin(value FactOrigin) bool {
	return value == OriginObserved || value == OriginDerived || value == OriginCarried
}

func validStatus(value FactStatus) bool {
	return value == StatusAsserted || value == StatusUnknown || value == StatusConflicting || value == StatusRetracted
}

func (s FactSubject) Validate(max int) error {
	if !validText(s.Kind, false, max) || !validText(s.ID, false, max) || !validText(s.Role, true, max) {
		return fmt.Errorf("%w: subject", ErrInvalidFact)
	}
	return nil
}

func (q FactQuality) Validate() error {
	if q.CompletenessPermille < 0 || q.CompletenessPermille > 1000 || q.ReliabilityPermille < 0 || q.ReliabilityPermille > 1000 || q.SourceCount < 0 {
		return fmt.Errorf("%w: quality", ErrInvalidFact)
	}
	return nil
}

func (f Fact) Clone() Fact {
	out := f
	out.Value = f.Value.Clone()
	out.Provenance = cloneProvenance(f.Provenance)
	if f.ValidTo != nil {
		value := *f.ValidTo
		out.ValidTo = &value
	}
	return out
}

func (f Fact) Validate(schema FactSchema, policy Policy) error {
	if f.ID == "" || !strings.HasPrefix(string(f.ID), "fact-") || f.Key == "" || !strings.HasPrefix(string(f.Key), "fact-key-") || f.Predicate == "" || !validScope(f.Scope) || !validOrigin(f.Origin) || !validStatus(f.Status) || f.ValidFrom.IsZero() || f.ValidFrom.Location() != time.UTC || f.ValidTo != nil && (f.ValidTo.Before(f.ValidFrom) || f.ValidTo.Location() != time.UTC) {
		return ErrInvalidFact
	}
	if err := f.Subject.Validate(policy.MaxStringLength); err != nil {
		return err
	}
	if err := f.Quality.Validate(); err != nil {
		return err
	}
	definition, ok := schema.Definition(f.Code)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownFactCode, f.Code)
	}
	if definition.Scope != f.Scope || definition.ValueKind != f.Value.Kind {
		return fmt.Errorf("%w: schema shape", ErrInvalidFact)
	}
	maxValues := policy.MaxSetValues
	if f.Value.Kind == ValueStringList {
		maxValues = policy.MaxSequenceValues
	}
	if err := f.Value.Validate(policy.MaxStringLength, maxValues); err != nil {
		return err
	}
	if len(f.Provenance) == 0 || len(f.Provenance) > policy.MaxProvenancePerFact {
		return ErrProvenanceLimitReached
	}
	for i, provenance := range f.Provenance {
		if err := provenance.Validate(policy.MaxStringLength); err != nil || i > 0 && provenance.Compare(f.Provenance[i-1]) <= 0 {
			return ErrInvalidFact
		}
	}
	return nil
}

func cloneFacts(values []Fact) []Fact {
	out := make([]Fact, len(values))
	for i, value := range values {
		out[i] = value.Clone()
	}
	return out
}

func cloneConflicts(values []ConflictSet) []ConflictSet {
	out := make([]ConflictSet, len(values))
	for i, value := range values {
		out[i] = value
		out[i].FactIDs = append([]FactID(nil), value.FactIDs...)
		out[i].Provenance = cloneProvenance(value.Provenance)
	}
	return out
}

func (s FactSet) Clone() FactSet {
	out := s
	out.Facts = cloneFacts(s.Facts)
	out.Conflicts = cloneConflicts(s.Conflicts)
	return out
}

func (s FactSet) Validate(schema FactSchema, policy Policy) error {
	if s.EpisodeID == "" || s.EpisodeRevision == 0 || s.ExtractedAt.IsZero() || s.ExtractedAt.Location() != time.UTC || s.SchemaFingerprint == "" || s.PolicyFingerprint == "" || s.Fingerprint == "" {
		return ErrInvalidFactSet
	}
	if s.SchemaFingerprint != SchemaFingerprint() || s.PolicyFingerprint != policy.Fingerprint() {
		return ErrFingerprintMismatch
	}
	if len(s.Facts) > policy.MaxFactsPerEpisode {
		return ErrFactLimitReached
	}
	seenIDs := map[FactID]struct{}{}
	seenKeys := map[FactKey][]FactID{}
	lastKey := FactKey("")
	for _, fact := range s.Facts {
		if err := fact.Validate(schema, policy); err != nil {
			return err
		}
		if fact.ID != factIDFor(fact) {
			return ErrFingerprintMismatch
		}
		if _, exists := seenIDs[fact.ID]; exists {
			return ErrFactIDCollision
		}
		seenIDs[fact.ID] = struct{}{}
		if lastKey != "" && (fact.Key < lastKey || fact.Key == lastKey && len(seenKeys[fact.Key]) > 0 && fact.ID <= seenKeys[fact.Key][len(seenKeys[fact.Key])-1]) {
			return ErrInvalidFactSet
		}
		lastKey = fact.Key
		seenKeys[fact.Key] = append(seenKeys[fact.Key], fact.ID)
	}
	conflictKeys := map[FactKey]map[FactID]struct{}{}
	for _, conflict := range s.Conflicts {
		if err := conflict.Validate(policy); err != nil {
			return err
		}
		if conflict.ID != conflictIDFor(conflict) {
			return ErrFingerprintMismatch
		}
		for _, id := range conflict.FactIDs {
			fact, ok := factByID(s.Facts, id)
			if !ok || fact.Key != conflict.Key || fact.Code != FactCode(conflict.Code) {
				return ErrInvalidConflict
			}
			if conflictKeys[conflict.Key] == nil {
				conflictKeys[conflict.Key] = map[FactID]struct{}{}
			}
			conflictKeys[conflict.Key][id] = struct{}{}
		}
	}
	lastConflict := ""
	for _, conflict := range s.Conflicts {
		if lastConflict != "" && conflict.ID <= lastConflict {
			return ErrInvalidFactSet
		}
		lastConflict = conflict.ID
	}
	for key, ids := range seenKeys {
		definition, _ := schema.Definition(factByKey(s.Facts, key).Code)
		if len(ids) > 1 && !definition.AllowsMultiple {
			conflictIDs := conflictKeys[key]
			if len(conflictIDs) != len(ids) {
				return ErrFactKeyCollision
			}
		}
	}
	if s.Fingerprint != FactSetFingerprint(s) {
		return ErrFingerprintMismatch
	}
	return nil
}

func factByID(values []Fact, id FactID) (Fact, bool) {
	for _, value := range values {
		if value.ID == id {
			return value, true
		}
	}
	return Fact{}, false
}
func factByKey(values []Fact, key FactKey) Fact {
	for _, value := range values {
		if value.Key == key {
			return value
		}
	}
	return Fact{}
}

func (c ConflictSet) Validate(policy Policy) error {
	if c.ID == "" || c.Key == "" || c.Code == "" || len(c.FactIDs) < 2 || len(c.Provenance) > policy.MaxProvenancePerFact {
		return ErrInvalidConflict
	}
	for i, id := range c.FactIDs {
		if id == "" || i > 0 && c.FactIDs[i-1] >= id {
			return ErrInvalidConflict
		}
	}
	for i, provenance := range c.Provenance {
		if err := provenance.Validate(policy.MaxStringLength); err != nil || i > 0 && provenance.Compare(c.Provenance[i-1]) <= 0 {
			return ErrInvalidConflict
		}
	}
	if len(c.Provenance) == 0 {
		return ErrInvalidConflict
	}
	return nil
}

func sortFacts(values []Fact) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Key != values[j].Key {
			return values[i].Key < values[j].Key
		}
		return values[i].ID < values[j].ID
	})
}

func factsSorted(values []Fact) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1].Key > values[i].Key || values[i-1].Key == values[i].Key && values[i-1].ID > values[i].ID {
			return false
		}
	}
	return true
}

func conflictsSorted(values []ConflictSet) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1].ID > values[i].ID {
			return false
		}
	}
	return true
}

func factSetsSorted(values []FactSet) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1].EpisodeID > values[i].EpisodeID {
			return false
		}
	}
	return true
}
