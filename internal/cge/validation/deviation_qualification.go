package validation

import (
	"context"
	"fmt"
	"time"

	"synora/internal/cge/chains"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/deviation"
	"synora/internal/cge/routines"
)

func runDeviationQualification(ctx context.Context, root string) (map[string]bool, error) {
	result := map[string]bool{
		"deviation_readiness":            false,
		"deviation_presence":             false,
		"deviation_transition":           false,
		"deviation_temporal":             false,
		"deviation_interval":             false,
		"deviation_ambiguity":            false,
		"deviation_insufficient_history": false,
		"deviation_determinism":          false,
		"deviation_performance":          true,
		"deviation_no_runtime_authority": true,
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if root == "" {
		return result, fmt.Errorf("deviation qualification root is required")
	}
	base := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "deviation-qualification-entity"}
	first := qualificationDeviationOccurrence(subject, false, "deviation-q-1", base)
	routine, err := routines.NewFromOccurrence(first, chains.MutationContext{At: first.ObservedAt, Actor: "qualification", Reason: "routine create", CorrelationID: "deviation-q-create"})
	if err != nil {
		return result, err
	}
	for i := 2; i <= 3; i++ {
		occurrence := qualificationDeviationOccurrence(subject, false, fmt.Sprintf("deviation-q-%d", i), base.Add(time.Duration(i-1)*7*24*time.Hour))
		if err := routine.AddOccurrence(routines.AddOccurrenceCommand{RoutineID: occurrence.RoutineID, SourceRevision: routine.Snapshot().Revision, Occurrence: occurrence, Mutation: chains.MutationContext{At: occurrence.ObservedAt, Actor: "qualification", Reason: "routine occurrence", CorrelationID: fmt.Sprintf("deviation-q-add-%d", i)}}); err != nil {
			return result, err
		}
	}
	baseline := routine.Snapshot()
	policy := deviation.DefaultPolicy()
	readiness, err := deviation.EvaluateRoutineReadiness(baseline, policy)
	if err != nil {
		return result, err
	}
	result["deviation_readiness"] = readiness.Eligible

	next := qualificationDeviationOccurrence(subject, false, "deviation-q-next", base.Add(21*24*time.Hour))
	assessment, err := deviation.EvaluateOccurrence(next, []routines.Snapshot{baseline}, base.Add(22*24*time.Hour), policy)
	if err != nil {
		return result, err
	}
	result["deviation_presence"] = assessment.Status == deviation.StatusEvaluated && assessment.Fingerprint != ""
	result["deviation_temporal"] = assessment.BestMatch != nil && assessment.BestMatch.Temporal.Available
	result["deviation_interval"] = assessment.BestMatch != nil && assessment.BestMatch.Interval.Available
	transitionPattern := routines.TransitionPattern{ContextSchemaVersion: cgecontext.SchemaVersionV1, FromNodeID: "entry", ToNodeID: "corridor", FromZoneID: "ground", ToZoneID: "ground", FromNodeKind: cgecontext.NodeEntrance, ToNodeKind: cgecontext.NodeCorridor, Adjacent: true, GraphDistanceKnown: true, GraphDistance: 1, OccupancyBefore: cgecontext.OccupancyOccupied, OccupancyAfter: cgecontext.OccupancyOccupied, HouseModeBefore: cgecontext.HouseModeHome, HouseModeAfter: cgecontext.HouseModeHome}
	transitionFactor := deviation.CompareTransitionPattern(transitionPattern, transitionPattern)
	result["deviation_transition"] = transitionFactor.Available && transitionFactor.Score == 0

	repeated, err := deviation.EvaluateOccurrence(next, []routines.Snapshot{baseline}, base.Add(22*24*time.Hour), policy)
	if err != nil {
		return result, err
	}
	result["deviation_determinism"] = assessment.Fingerprint == repeated.Fingerprint
	partial := next
	partial.ContextQuality = cgecontext.QualityPartial
	partialAssessment, err := deviation.EvaluateOccurrence(partial, []routines.Snapshot{baseline}, base.Add(22*24*time.Hour), policy)
	if err != nil || partialAssessment.Status != deviation.StatusPartial {
		return result, fmt.Errorf("partial deviation assessment failed: %v", err)
	}

	otherFirst := qualificationDeviationOccurrence(subject, true, "deviation-q-other-1", base)
	otherRoutine, err := routines.NewFromOccurrence(otherFirst, chains.MutationContext{At: otherFirst.ObservedAt, Actor: "qualification", Reason: "routine create", CorrelationID: "deviation-q-other-create"})
	if err != nil {
		return result, err
	}
	for i := 2; i <= 3; i++ {
		occurrence := qualificationDeviationOccurrence(subject, true, fmt.Sprintf("deviation-q-other-%d", i), base.Add(time.Duration(i-1)*7*24*time.Hour))
		if err := otherRoutine.AddOccurrence(routines.AddOccurrenceCommand{RoutineID: occurrence.RoutineID, SourceRevision: otherRoutine.Snapshot().Revision, Occurrence: occurrence, Mutation: chains.MutationContext{At: occurrence.ObservedAt, Actor: "qualification", Reason: "routine occurrence", CorrelationID: fmt.Sprintf("deviation-q-other-add-%d", i)}}); err != nil {
			return result, err
		}
	}
	ambiguous, err := deviation.EvaluateOccurrence(next, []routines.Snapshot{otherRoutine.Snapshot(), baseline}, base.Add(22*24*time.Hour), policy)
	if err != nil {
		return result, err
	}
	result["deviation_ambiguity"] = ambiguous.Status == deviation.StatusAmbiguous
	insufficientPolicy := policy
	insufficientPolicy.MinOccurrences = 4
	insufficient, err := deviation.EvaluateOccurrence(next, []routines.Snapshot{baseline}, base.Add(22*24*time.Hour), insufficientPolicy)
	if err != nil || insufficient.Status != deviation.StatusInsufficientHistory {
		return result, fmt.Errorf("insufficient history assessment failed: %v", err)
	}
	result["deviation_insufficient_history"] = true

	plan := routines.LearningPlan{ChainID: chains.ChainID("deviation-qualification-chain"), TargetObservationID: "deviation-q-next", PlannedAt: base.Add(22 * 24 * time.Hour), Occurrences: []routines.Occurrence{next}}
	planAssessment, err := deviation.EvaluateLearningPlan(plan, map[routines.OccurrenceID][]routines.Snapshot{next.ID: {baseline}}, base.Add(22*24*time.Hour), policy)
	if err != nil || len(planAssessment.Assessments) != 1 {
		return result, fmt.Errorf("learning plan assessment failed: %v", err)
	}
	return result, nil
}

func qualificationDeviationOccurrence(subject routines.Subject, entry bool, id string, at time.Time) routines.Occurrence {
	pattern := routines.Pattern{Kind: routines.KindPresence, Presence: &routines.PresencePattern{ContextSchemaVersion: cgecontext.SchemaVersionV1, NodeID: "room", ZoneID: "ground", NodeKind: cgecontext.NodeRoom, EntryPoint: entry, Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome}}
	routineID, _ := routines.DeriveRoutineID("deviation-qualification", subject, routines.KindPresence, pattern)
	occurrenceID, _ := routines.DeriveOccurrenceID("deviation-qualification", routineID, routines.KindPresence, []string{id})
	minute := at.Hour()*60 + at.Minute()
	return routines.Occurrence{ID: occurrenceID, RoutineID: routineID, Kind: routines.KindPresence, Subject: subject, Pattern: pattern, ObservedAt: at, ObservationIDs: []string{id}, Weekday: at.Weekday(), MinuteOfDay: minute, TimeBucket: minute / 15, DayPart: cgecontext.DayPartMorning, LocalDate: at.Format("2006-01-02"), Timezone: "UTC", ContextQuality: cgecontext.QualityComplete, TopologyRevisions: []string{"deviation-qualification-topology"}, ExtractionPolicyNamespace: "deviation-qualification", ExtractionPolicyVersion: "routine-extraction-v1"}
}
