package deviation

import (
	"fmt"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/routines"
)

func BenchmarkEvaluateRoutineReadiness(b *testing.B) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "bench-entity"}
	baseline, _ := testRoutineForBenchmark(subject, false)
	policy := DefaultPolicy()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EvaluateRoutineReadiness(baseline, policy)
	}
}

func BenchmarkComparePresencePattern(b *testing.B) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "bench-entity"}
	baseline, occurrence := testRoutineForBenchmark(subject, false)
	pattern := *occurrence.Pattern.Presence
	other := *baseline.Pattern.Presence
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ComparePresencePattern(pattern, other)
	}
}

func BenchmarkCompareTransitionPattern(b *testing.B) {
	pattern := routines.TransitionPattern{ContextSchemaVersion: 1, FromNodeID: "entry", ToNodeID: "corridor", FromZoneID: "ground", ToZoneID: "ground", FromNodeKind: "entrance", ToNodeKind: "corridor", Adjacent: true, GraphDistanceKnown: true, GraphDistance: 1, OccupancyBefore: "occupied", OccupancyAfter: "occupied", HouseModeBefore: "home", HouseModeAfter: "home"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = CompareTransitionPattern(pattern, pattern)
	}
}

func BenchmarkEvaluateTemporal(b *testing.B) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "bench-entity"}
	baseline, occurrence := testRoutineForBenchmark(subject, false)
	policy := DefaultPolicy()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = EvaluateTemporal(occurrence, baseline, policy)
	}
}

func BenchmarkEvaluateInterval(b *testing.B) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "bench-entity"}
	baseline, occurrence := testRoutineForBenchmark(subject, false)
	policy := DefaultPolicy()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = EvaluateInterval(occurrence, baseline, policy)
	}
}

func BenchmarkEvaluateLearningPlan(b *testing.B) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "bench-entity"}
	baseline, occurrence := testRoutineForBenchmark(subject, false)
	plan := routines.LearningPlan{ChainID: "benchmark-chain", TargetObservationID: "bench-new", PlannedAt: occurrence.ObservedAt, Occurrences: []routines.Occurrence{occurrence}}
	candidates := map[routines.OccurrenceID][]routines.Snapshot{occurrence.ID: {baseline}}
	policy := DefaultPolicy()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = EvaluateLearningPlan(plan, candidates, occurrence.ObservedAt.Add(time.Hour), policy)
	}
}

func BenchmarkEvaluateOccurrence(b *testing.B) {
	for _, candidates := range []int{1, 5, 10, 32, 64} {
		b.Run(fmt.Sprintf("candidates_%d", candidates), func(b *testing.B) {
			subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "bench-entity"}
			_, occurrence := testRoutineForBenchmark(subject, false)
			values := make([]routines.Snapshot, 0, candidates)
			for i := 0; i < candidates; i++ {
				candidate, _ := testRoutineForBenchmarkAtNode(subject, false, fmt.Sprintf("room-%d", i))
				values = append(values, candidate)
			}
			policy := DefaultPolicy()
			policy.MaxCandidateRoutines = 128
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = EvaluateOccurrence(occurrence, values, occurrence.ObservedAt.Add(time.Hour), policy)
			}
		})
	}
}

func BenchmarkEvaluateOccurrenceHistory(b *testing.B) {
	for _, size := range []int{3, 10, 100, 1000, 5000} {
		b.Run(fmt.Sprintf("occurrences_%d", size), func(b *testing.B) {
			subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "bench-history"}
			baseline, occurrence := syntheticRoutineSnapshot(subject, size)
			policy := DefaultPolicy()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = EvaluateOccurrence(occurrence, []routines.Snapshot{baseline}, occurrence.ObservedAt.Add(time.Hour), policy)
			}
		})
	}
}

func BenchmarkAssessmentValidate(b *testing.B) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "bench-entity"}
	baseline, occurrence := testRoutineForBenchmark(subject, false)
	assessment, err := EvaluateOccurrence(occurrence, []routines.Snapshot{baseline}, occurrence.ObservedAt.Add(time.Hour), DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = assessment.Validate()
	}
}

func testRoutineForBenchmark(subject routines.Subject, entry bool) (routines.Snapshot, routines.Occurrence) {
	return testRoutineForBenchmarkAtNode(subject, entry, "room")
}

func testRoutineForBenchmarkAtNode(subject routines.Subject, entry bool, node string) (routines.Snapshot, routines.Occurrence) {
	first := benchmarkOccurrenceAtNode(subject, entry, node, "bench-1", deviationTestBase)
	routine, _ := routines.NewFromOccurrence(first, benchmarkMutation(first.ObservedAt, "bench-create"))
	for i := 2; i <= 3; i++ {
		occurrence := benchmarkOccurrenceAtNode(subject, entry, node, fmt.Sprintf("bench-%d", i), deviationTestBase.Add(time.Duration(i-1)*24*time.Hour))
		_ = routine.AddOccurrence(routines.AddOccurrenceCommand{RoutineID: occurrence.RoutineID, SourceRevision: routine.Snapshot().Revision, Occurrence: occurrence, Mutation: benchmarkMutation(occurrence.ObservedAt, fmt.Sprintf("bench-add-%d", i))})
	}
	return routine.Snapshot(), benchmarkOccurrenceAtNode(subject, entry, node, "bench-new", deviationTestBase.Add(3*24*time.Hour))
}

func benchmarkOccurrence(subject routines.Subject, entry bool, id string, at time.Time) routines.Occurrence {
	return benchmarkOccurrenceAtNode(subject, entry, "room", id, at)
}

func benchmarkOccurrenceAtNode(subject routines.Subject, entry bool, node, id string, at time.Time) routines.Occurrence {
	pattern := routines.Pattern{Kind: routines.KindPresence, Presence: &routines.PresencePattern{ContextSchemaVersion: 1, NodeID: node, ZoneID: "ground", NodeKind: "room", EntryPoint: entry, Occupancy: "occupied", HouseMode: "home"}}
	routineID, _ := routines.DeriveRoutineID("deviation-benchmark", subject, routines.KindPresence, pattern)
	occurrenceID, _ := routines.DeriveOccurrenceID("deviation-benchmark", routineID, routines.KindPresence, []string{id})
	return routines.Occurrence{ID: occurrenceID, RoutineID: routineID, Kind: routines.KindPresence, Subject: subject, Pattern: pattern, ObservedAt: at, ObservationIDs: []string{id}, Weekday: at.Weekday(), MinuteOfDay: at.Hour() * 60, TimeBucket: at.Hour() * 4, DayPart: "morning", LocalDate: at.Format("2006-01-02"), Timezone: "UTC", ContextQuality: "complete", TopologyRevisions: []string{"benchmark"}, ExtractionPolicyNamespace: "deviation-benchmark", ExtractionPolicyVersion: "routine-extraction-v1"}
}

func syntheticRoutineSnapshot(subject routines.Subject, count int) (routines.Snapshot, routines.Occurrence) {
	pattern := routines.Pattern{Kind: routines.KindPresence, Presence: &routines.PresencePattern{ContextSchemaVersion: 1, NodeID: "room", ZoneID: "ground", NodeKind: "room", Occupancy: "occupied", HouseMode: "home"}}
	routineID, _ := routines.DeriveRoutineID("deviation-history-benchmark", subject, routines.KindPresence, pattern)
	refs := make([]routines.OccurrenceRef, 0, count)
	history := make([]routines.RevisionRecord, 0, count)
	base := deviationTestBase
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("history-%d", i)
		at := base.Add(time.Duration(i) * 7 * 24 * time.Hour)
		occurrenceID, _ := routines.DeriveOccurrenceID("deviation-history-benchmark", routineID, routines.KindPresence, []string{id})
		ref := routines.OccurrenceRef{ID: occurrenceID, ObservedAt: at, ObservationIDs: []string{id}, Weekday: at.Weekday(), TimeBucket: at.Hour() * 4, DayPart: "morning", LocalDate: at.Format("2006-01-02"), ContextQuality: "complete", TopologyRevisions: []string{"benchmark"}}
		refs = append(refs, ref)
		if i == 0 {
			history = append(history, routines.RevisionRecord{RoutineID: routineID, Operation: routines.OperationRoutineCreated, PreviousRevision: 0, NewRevision: 1, At: at, Actor: "benchmark", Reason: "create", CorrelationID: "create"})
		} else {
			history = append(history, routines.RevisionRecord{RoutineID: routineID, Operation: routines.OperationOccurrenceAdded, PreviousRevision: uint64(i), NewRevision: uint64(i + 1), At: at, Actor: "benchmark", Reason: "add", CorrelationID: fmt.Sprintf("add-%d", i), OccurrenceID: occurrenceID})
		}
	}
	last := refs[len(refs)-1].ObservedAt
	interval := 7 * 24 * time.Hour
	snapshot := routines.Snapshot{ID: routineID, Kind: routines.KindPresence, Subject: subject, Pattern: pattern, Status: routines.StatusCandidate, Occurrences: refs, OccurrenceCount: uint64(count), FirstSeenAt: refs[0].ObservedAt, LastSeenAt: last, DistinctLocalDays: uint64(count), DistinctLocalWeeks: uint64(count), TemporalBins: []routines.TemporalBin{{Weekday: refs[0].Weekday, TimeBucket: refs[0].TimeBucket, Count: uint64(count)}}, DayPartCounts: []routines.DayPartCount{{DayPart: "morning", Count: uint64(count)}}, IntervalStatistics: routines.IntervalStatistics{Count: uint64(count - 1), Minimum: interval, Maximum: interval, Total: time.Duration(count-1) * interval, Mean: interval}, CompleteContextCount: uint64(count), CreatedAt: refs[0].ObservedAt, UpdatedAt: last, Revision: uint64(count), History: history}
	newOccurrence := benchmarkOccurrenceAtNode(subject, false, "room", "history-new", last.Add(interval))
	return snapshot, newOccurrence
}

func benchmarkMutation(at time.Time, correlation string) chains.MutationContext {
	return chains.MutationContext{At: at, Actor: "benchmark", Reason: "benchmark routine", CorrelationID: correlation}
}
