package routines

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"synora/internal/cge/chains"
	cgecontext "synora/internal/cge/context"
)

type ExtractionPolicy struct {
	Namespace                   string
	Version                     string
	TemporalBucketMinutes       int
	AllowPartialContext         bool
	MaxTransitionGap            time.Duration
	RequireSameTopologyRevision bool
}

func DefaultExtractionPolicy() ExtractionPolicy {
	return ExtractionPolicy{Namespace: "synora.cge.routines", Version: "routine-extraction-v1", TemporalBucketMinutes: 15, AllowPartialContext: true, MaxTransitionGap: 15 * time.Minute, RequireSameTopologyRevision: true}
}

func (p ExtractionPolicy) Validate() error {
	if !validText(p.Namespace, 128) || !validText(p.Version, 128) || p.TemporalBucketMinutes < 5 || p.TemporalBucketMinutes > 120 || 1440%p.TemporalBucketMinutes != 0 || p.MaxTransitionGap <= 0 || p.MaxTransitionGap > 24*time.Hour {
		return ErrInvalidPolicy
	}
	return nil
}

func ExtractPresenceOccurrence(chain chains.Snapshot, targetObservationID string, policy ExtractionPolicy) (Occurrence, error) {
	if err := policy.Validate(); err != nil {
		return Occurrence{}, err
	}
	observation, ok := findObservation(chain, targetObservationID)
	if !ok {
		return Occurrence{}, NotApplicableError{SkipTargetObservationMissing}
	}
	if observation.Context == nil {
		return Occurrence{}, NotApplicableError{SkipContextMissing}
	}
	frame := observation.Context
	if frame.Quality == cgecontext.QualityPartial && !policy.AllowPartialContext {
		return Occurrence{}, NotApplicableError{SkipPartialDisallowed}
	}
	if err := frame.Validate(); err != nil {
		return Occurrence{}, fmt.Errorf("%w: %v", ErrInvalidOccurrence, err)
	}
	subject, err := SubjectFromObservation(observation, chain.ID)
	if err != nil {
		return Occurrence{}, err
	}
	pattern := Pattern{Kind: KindPresence, Presence: &PresencePattern{ContextSchemaVersion: frame.SchemaVersion, NodeID: frame.NodeID, ZoneID: frame.ZoneID, NodeKind: frame.NodeKind, EntryPoint: frame.EntryPoint, Exterior: frame.Exterior, Occupancy: frame.Occupancy, HouseMode: frame.HouseMode}}
	if err := pattern.Validate(); err != nil {
		return Occurrence{}, err
	}
	routineID, err := DeriveRoutineID(policy.Namespace, subject, KindPresence, pattern)
	if err != nil {
		return Occurrence{}, err
	}
	return occurrenceFromFrame(policy, routineID, KindPresence, subject, pattern, frame, []string{observation.ID}, []string{frame.TopologyRevision})
}

func ExtractTransitionOccurrence(chain chains.Snapshot, targetObservationID string, topology cgecontext.TopologySnapshot, policy ExtractionPolicy) (Occurrence, error) {
	if err := policy.Validate(); err != nil {
		return Occurrence{}, err
	}
	target, ok := findObservation(chain, targetObservationID)
	if !ok {
		return Occurrence{}, NotApplicableError{SkipTargetObservationMissing}
	}
	if target.Context == nil {
		return Occurrence{}, NotApplicableError{SkipContextMissing}
	}
	if target.Context.Quality == cgecontext.QualityPartial && !policy.AllowPartialContext {
		return Occurrence{}, NotApplicableError{SkipPartialDisallowed}
	}
	previous, ok := previousObservation(chain, target)
	if !ok {
		return Occurrence{}, NotApplicableError{SkipPreviousContextMissing}
	}
	if previous.Context == nil {
		return Occurrence{}, NotApplicableError{SkipPreviousContextMissing}
	}
	if previous.Context.Quality == cgecontext.QualityPartial && !policy.AllowPartialContext {
		return Occurrence{}, NotApplicableError{SkipPartialDisallowed}
	}
	gap := target.Timestamp.Sub(previous.Timestamp)
	if gap > policy.MaxTransitionGap {
		return Occurrence{}, NotApplicableError{SkipTransitionGapExceeded}
	}
	if strings.TrimSpace(topology.Revision) == "" {
		return Occurrence{}, NotApplicableError{SkipTopologyMissing}
	}
	if err := topology.Validate(); err != nil {
		return Occurrence{}, NotApplicableError{SkipTopologyMissing}
	}
	if policy.RequireSameTopologyRevision && previous.Context.TopologyRevision != target.Context.TopologyRevision {
		return Occurrence{}, NotApplicableError{SkipTopologyRevisionMismatch}
	}
	assessment, err := cgecontext.EvaluateTransition(*previous.Context, *target.Context, topology)
	if err != nil {
		return Occurrence{}, err
	}
	if assessment.DistanceStatus == cgecontext.DistanceUnknown {
		return Occurrence{}, NotApplicableError{SkipTopologyMissing}
	}
	if assessment.DistanceStatus == cgecontext.DistanceUnreachable {
		return Occurrence{}, NotApplicableError{SkipTransitionUnreachable}
	}
	subject, err := SubjectFromObservation(target, chain.ID)
	if err != nil {
		return Occurrence{}, err
	}
	pattern := Pattern{Kind: KindTransition, Transition: &TransitionPattern{ContextSchemaVersion: target.Context.SchemaVersion, FromNodeID: previous.Context.NodeID, ToNodeID: target.Context.NodeID, FromZoneID: previous.Context.ZoneID, ToZoneID: target.Context.ZoneID, FromNodeKind: previous.Context.NodeKind, ToNodeKind: target.Context.NodeKind, EntryTransition: assessment.EntryTransition, ExitTransition: assessment.ExitTransition, ExteriorTransition: assessment.ExteriorTransition, Adjacent: assessment.Adjacent, GraphDistanceKnown: assessment.DistanceStatus == cgecontext.DistanceReachable, GraphDistance: assessment.GraphDistance, OccupancyBefore: previous.Context.Occupancy, OccupancyAfter: target.Context.Occupancy, HouseModeBefore: previous.Context.HouseMode, HouseModeAfter: target.Context.HouseMode}}
	if err := pattern.Validate(); err != nil {
		return Occurrence{}, err
	}
	routineID, err := DeriveRoutineID(policy.Namespace, subject, KindTransition, pattern)
	if err != nil {
		return Occurrence{}, err
	}
	revisions := []string{}
	if previous.Context.TopologyRevision != "" {
		revisions = append(revisions, previous.Context.TopologyRevision)
	}
	if target.Context.TopologyRevision != "" && (len(revisions) == 0 || revisions[len(revisions)-1] != target.Context.TopologyRevision) {
		revisions = append(revisions, target.Context.TopologyRevision)
	}
	return occurrenceFromFrame(policy, routineID, KindTransition, subject, pattern, target.Context, []string{previous.ID, target.ID}, revisions)
}

func occurrenceFromFrame(policy ExtractionPolicy, routineID RoutineID, kind Kind, subject Subject, pattern Pattern, frame *cgecontext.Frame, observationIDs []string, revisions []string) (Occurrence, error) {
	loc, err := time.LoadLocation(frame.Time.Timezone)
	if err != nil {
		return Occurrence{}, err
	}
	local := frame.ObservedAt.In(loc)
	bucket := frame.Time.MinuteOfDay / policy.TemporalBucketMinutes
	id, err := DeriveOccurrenceID(policy.Namespace, routineID, kind, observationIDs)
	if err != nil {
		return Occurrence{}, err
	}
	occ := Occurrence{ID: id, RoutineID: routineID, Kind: kind, Subject: subject, Pattern: pattern, ObservedAt: frame.ObservedAt, ObservationIDs: append([]string(nil), observationIDs...), Weekday: frame.Time.Weekday, MinuteOfDay: frame.Time.MinuteOfDay, TimeBucket: bucket, DayPart: frame.Time.DayPart, LocalDate: local.Format("2006-01-02"), Timezone: frame.Time.Timezone, ContextQuality: frame.Quality, TopologyRevisions: uniqueStrings(revisions), ExtractionPolicyNamespace: policy.Namespace, ExtractionPolicyVersion: policy.Version}
	return occ, occ.Validate()
}

func findObservation(snapshot chains.Snapshot, id string) (chains.ObservationRef, bool) {
	for _, observation := range snapshot.Observations {
		if observation.ID == id {
			return observation, true
		}
	}
	return chains.ObservationRef{}, false
}
func previousObservation(snapshot chains.Snapshot, target chains.ObservationRef) (*chains.ObservationRef, bool) {
	values := make([]chains.ObservationRef, 0)
	for _, candidate := range snapshot.Observations {
		if candidate.Timestamp.Before(target.Timestamp) && candidate.Context != nil {
			values = append(values, candidate)
		}
	}
	if len(values) == 0 {
		return nil, false
	}
	sort.Slice(values, func(i, j int) bool {
		if !values[i].Timestamp.Equal(values[j].Timestamp) {
			return values[i].Timestamp.After(values[j].Timestamp)
		}
		return values[i].ID < values[j].ID
	})
	value := values[0]
	return &value, true
}
func uniqueStrings(values []string) []string {
	out := []string{}
	for _, v := range values {
		if v == "" {
			continue
		}
		found := false
		for _, existing := range out {
			if existing == v {
				found = true
				break
			}
		}
		if !found {
			out = append(out, v)
		}
	}
	return out
}
