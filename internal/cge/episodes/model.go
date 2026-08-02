package episodes

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const maxReferenceText = 256

type EpisodeID string

type EpisodeStatus string

const (
	StatusOpen        EpisodeStatus = "open"
	StatusQuiescent   EpisodeStatus = "quiescent"
	StatusClosed      EpisodeStatus = "closed"
	StatusInvalidated EpisodeStatus = "invalidated"
)

type SubjectKind string

const (
	SubjectKnown     SubjectKind = "known"
	SubjectUnknown   SubjectKind = "unknown"
	SubjectUncertain SubjectKind = "uncertain"
	SubjectNone      SubjectKind = "none"
)

type SubjectRef struct {
	Kind               SubjectKind
	EntityID           string
	CandidateEntityIDs []string
}

type ChainRef struct{ ID string }
type RoutineRef struct{ ID string }
type NodeRef struct {
	ID     string
	ZoneID string
}

type DeviationRef struct {
	AssessmentID        string
	Status              string
	Band                string
	ScorePermille       int
	CoveragePermille    int
	StructuralAvailable bool
	TemporalAvailable   bool
	IntervalAvailable   bool
}

// ObservationRef is a redacted reference at the episode boundary. It never
// carries video, image, embedding, biometric, raw-event, or security data.
type ObservationRef struct {
	EventID    string
	ObservedAt time.Time
	ReceivedAt time.Time
	EventType  string

	Subject SubjectRef
	NodeID  string
	ZoneID  string

	HouseMode                  string
	Occupancy                  string
	ContextQuality             string
	ContextSnapshotFingerprint string
	ContextFreshness           string

	ActivationID string
	ClipID       string
	TrackID      string
	SequenceKey  string

	ChainID    string
	RoutineIDs []string
	Deviation  *DeviationRef
}

// Episode is a bounded working-memory aggregate, not an interpretation of a
// situation. Aggregate slices are canonicalized and are safe to expose only
// through defensive copies.
type Episode struct {
	ID EpisodeID

	Status EpisodeStatus

	CreatedAt       time.Time
	StartedAt       time.Time
	LastObservedAt  time.Time
	StatusChangedAt time.Time
	ClosedAt        *time.Time

	Observations []ObservationRef
	Subjects     []SubjectRef
	Nodes        []NodeRef

	ChainRefs   []ChainRef
	RoutineRefs []RoutineRef

	EventTypes       []string
	ContextQualities []string
	DurationObserved time.Duration
	Revision         uint64
}

type EpisodeSnapshot = Episode

func validText(value string, allowEmpty bool) bool {
	return (allowEmpty || value != "") && len([]rune(value)) <= maxReferenceText && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n")
}

func validSubjectKind(value SubjectKind) bool {
	return value == SubjectKnown || value == SubjectUnknown || value == SubjectUncertain || value == SubjectNone
}

func normalizeStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	result := out[:0]
	for _, value := range out {
		if value != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}

func normalizeSubject(subject SubjectRef) SubjectRef {
	subject.CandidateEntityIDs = normalizeStrings(subject.CandidateEntityIDs)
	return subject
}

func (s SubjectRef) Validate() error {
	if !validSubjectKind(s.Kind) || !validText(s.EntityID, true) {
		return fmt.Errorf("%w: subject", ErrInvalidObservation)
	}
	if s.Kind == SubjectKnown && s.EntityID == "" {
		return fmt.Errorf("%w: known subject has no entity", ErrInvalidObservation)
	}
	if s.Kind != SubjectKnown && s.EntityID != "" {
		return fmt.Errorf("%w: non-known subject has entity", ErrInvalidObservation)
	}
	for i, candidate := range s.CandidateEntityIDs {
		if !validText(candidate, false) || (i > 0 && s.CandidateEntityIDs[i-1] >= candidate) {
			return fmt.Errorf("%w: candidate entities", ErrInvalidObservation)
		}
	}
	return nil
}

func (o ObservationRef) Clone() ObservationRef {
	out := o
	out.Subject = normalizeSubject(o.Subject)
	out.RoutineIDs = normalizeStrings(o.RoutineIDs)
	if o.Deviation != nil {
		value := *o.Deviation
		out.Deviation = &value
	}
	return out
}

func (o ObservationRef) Validate() error {
	if o.EventID == "" {
		return ErrMissingEventID
	}
	if !validText(o.EventID, false) {
		return fmt.Errorf("%w: event id", ErrInvalidObservation)
	}
	if o.ObservedAt.IsZero() {
		return ErrMissingObservedAt
	}
	if !validText(o.EventType, true) || !validText(o.NodeID, true) || !validText(o.ZoneID, true) || !validText(o.HouseMode, true) || !validText(o.Occupancy, true) || !validText(o.ContextQuality, true) || !validText(o.ContextSnapshotFingerprint, true) || !validText(o.ContextFreshness, true) || !validText(o.ActivationID, true) || !validText(o.ClipID, true) || !validText(o.TrackID, true) || !validText(o.SequenceKey, true) || !validText(o.ChainID, true) {
		return fmt.Errorf("%w: bounded reference", ErrInvalidObservation)
	}
	if err := o.Subject.Validate(); err != nil {
		return err
	}
	for i, id := range o.RoutineIDs {
		if !validText(id, false) || (i > 0 && o.RoutineIDs[i-1] >= id) {
			return fmt.Errorf("%w: routine ids", ErrInvalidObservation)
		}
	}
	if o.Deviation != nil {
		if !validText(o.Deviation.AssessmentID, true) || !validText(o.Deviation.Status, true) || !validText(o.Deviation.Band, true) || o.Deviation.ScorePermille < 0 || o.Deviation.ScorePermille > 1000 || o.Deviation.CoveragePermille < 0 || o.Deviation.CoveragePermille > 1000 {
			return fmt.Errorf("%w: deviation reference", ErrInvalidObservation)
		}
	}
	return nil
}

func validStatus(value EpisodeStatus) bool {
	return value == StatusOpen || value == StatusQuiescent || value == StatusClosed || value == StatusInvalidated
}

func (e Episode) Clone() Episode {
	out := e
	out.Observations = make([]ObservationRef, len(e.Observations))
	for i, observation := range e.Observations {
		out.Observations[i] = observation.Clone()
	}
	out.Subjects = append([]SubjectRef(nil), e.Subjects...)
	for i := range out.Subjects {
		out.Subjects[i] = normalizeSubject(out.Subjects[i])
	}
	out.Nodes = append([]NodeRef(nil), e.Nodes...)
	out.ChainRefs = append([]ChainRef(nil), e.ChainRefs...)
	out.RoutineRefs = append([]RoutineRef(nil), e.RoutineRefs...)
	out.EventTypes = append([]string(nil), e.EventTypes...)
	out.ContextQualities = append([]string(nil), e.ContextQualities...)
	if e.ClosedAt != nil {
		value := *e.ClosedAt
		out.ClosedAt = &value
	}
	return out
}

func (e Episode) Validate() error {
	if e.ID == "" || !strings.HasPrefix(string(e.ID), "episode-") || !validText(string(e.ID), false) || !validStatus(e.Status) || e.CreatedAt.IsZero() || e.StartedAt.IsZero() || e.LastObservedAt.IsZero() || e.StatusChangedAt.IsZero() || e.Revision == 0 {
		return ErrInvalidEpisode
	}
	if e.StartedAt.After(e.LastObservedAt) || e.CreatedAt.After(e.StartedAt) || e.Status == StatusClosed && e.ClosedAt == nil || e.Status != StatusClosed && e.ClosedAt != nil {
		return ErrInvalidEpisode
	}
	if len(e.Observations) == 0 || len(e.Observations) > 100000 {
		return ErrInvalidEpisode
	}
	var previous ObservationRef
	for i, observation := range e.Observations {
		if err := observation.Validate(); err != nil {
			return err
		}
		if i > 0 && (observation.ObservedAt.Before(previous.ObservedAt) || observation.ObservedAt.Equal(previous.ObservedAt) && observation.EventID <= previous.EventID) {
			return ErrInvalidEpisode
		}
		previous = observation
	}
	return nil
}
