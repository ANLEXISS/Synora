package deviation

import (
	"errors"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/context"
	"synora/internal/cge/routines"
)

var deviationTestBase = time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)

func testPresenceOccurrence(t *testing.T, subject routines.Subject, entry bool, id string, at time.Time) routines.Occurrence {
	t.Helper()
	pattern := routines.Pattern{Kind: routines.KindPresence, Presence: &routines.PresencePattern{ContextSchemaVersion: context.SchemaVersionV1, NodeID: "room", ZoneID: "ground", NodeKind: context.NodeRoom, EntryPoint: entry, Occupancy: context.OccupancyOccupied, HouseMode: context.HouseModeHome}}
	routineID, err := routines.DeriveRoutineID("deviation-test", subject, routines.KindPresence, pattern)
	if err != nil {
		t.Fatal(err)
	}
	occurrenceID, err := routines.DeriveOccurrenceID("deviation-test", routineID, routines.KindPresence, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	return routines.Occurrence{ID: occurrenceID, RoutineID: routineID, Kind: routines.KindPresence, Subject: subject, Pattern: pattern, ObservedAt: at, ObservationIDs: []string{id}, Weekday: at.Weekday(), MinuteOfDay: at.Hour()*60 + at.Minute(), TimeBucket: (at.Hour()*60 + at.Minute()) / 15, DayPart: context.DayPartMorning, LocalDate: at.Format("2006-01-02"), Timezone: "UTC", ContextQuality: context.QualityComplete, TopologyRevisions: []string{"topology-v1"}, ExtractionPolicyNamespace: "deviation-test", ExtractionPolicyVersion: "routine-extraction-v1"}
}

func testRoutine(t *testing.T, subject routines.Subject, entry bool) (routines.Snapshot, routines.Occurrence) {
	t.Helper()
	first := testPresenceOccurrence(t, subject, entry, "observation-1", deviationTestBase)
	routine, err := routines.NewFromOccurrence(first, chains.MutationContext{At: first.ObservedAt, Actor: "test", Reason: "routine create", CorrelationID: "routine-create"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 3; i++ {
		occurrence := testPresenceOccurrence(t, subject, entry, "observation-"+string(rune('0'+i)), deviationTestBase.Add(time.Duration(i-1)*7*24*time.Hour))
		if err := routine.AddOccurrence(routines.AddOccurrenceCommand{RoutineID: occurrence.RoutineID, SourceRevision: routine.Snapshot().Revision, Occurrence: occurrence, Mutation: chains.MutationContext{At: occurrence.ObservedAt, Actor: "test", Reason: "routine occurrence", CorrelationID: occurrenceIDs(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	return routine.Snapshot(), testPresenceOccurrence(t, subject, entry, "observation-new", deviationTestBase.Add(3*7*24*time.Hour))
}

func occurrenceIDs(i int) string { return "routine-occurrence-" + string(rune('0'+i)) }

func testPolicy() Policy {
	policy := DefaultPolicy()
	policy.Namespace = "deviation-test"
	policy.MinSpan = 6 * time.Hour
	return policy
}

func TestEvaluateOccurrenceBeforeLearningAndAlreadyEvaluated(t *testing.T) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "entity-a"}
	baseline, next := testRoutine(t, subject, false)
	assessment, err := EvaluateOccurrence(next, []routines.Snapshot{baseline}, deviationTestBase.Add(4*24*time.Hour), testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Status != StatusEvaluated || assessment.Score != 0 || assessment.Band != BandAligned || assessment.Fingerprint == "" {
		t.Fatalf("unexpected assessment: %+v", assessment)
	}
	if err := assessment.Validate(); err != nil {
		t.Fatal(err)
	}

	routine, err := routines.Restore(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := routine.AddOccurrence(routines.AddOccurrenceCommand{RoutineID: next.RoutineID, SourceRevision: routine.Snapshot().Revision, Occurrence: next, Mutation: chains.MutationContext{At: next.ObservedAt, Actor: "test", Reason: "routine occurrence", CorrelationID: "routine-new"}}); err != nil {
		t.Fatal(err)
	}
	assessment, err = EvaluateOccurrence(next, []routines.Snapshot{routine.Snapshot()}, deviationTestBase.Add(4*24*time.Hour), testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Status != StatusAlreadyEvaluated || assessment.Score != 0 || assessment.Band != BandUnknown {
		t.Fatalf("expected already evaluated, got %+v", assessment)
	}
}

func TestReadinessAndPartialContext(t *testing.T) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "entity-a"}
	baseline, occurrence := testRoutine(t, subject, false)
	policy := testPolicy()
	readiness, err := EvaluateRoutineReadiness(baseline, policy)
	if err != nil || !readiness.Eligible {
		t.Fatalf("expected ready routine: %+v, %v", readiness, err)
	}
	short := baseline
	short.Occurrences = short.Occurrences[:1]
	short.OccurrenceCount = 1
	if _, err := EvaluateRoutineReadiness(short, policy); err == nil {
		t.Fatal("expected invalid derived short snapshot")
	}
	occurrence.ContextQuality = context.QualityPartial
	assessment, err := EvaluateOccurrence(occurrence, []routines.Snapshot{baseline}, deviationTestBase.Add(4*24*time.Hour), policy)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Status != StatusPartial || assessment.Coverage >= MaxScore {
		t.Fatalf("expected partial assessment: %+v", assessment)
	}
}

func TestAmbiguityAndCandidateOrderAreDeterministic(t *testing.T) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "entity-a"}
	first, occurrence := testRoutine(t, subject, false)
	second, _ := testRoutine(t, subject, true)
	assessment, err := EvaluateOccurrence(occurrence, []routines.Snapshot{second, first}, deviationTestBase.Add(4*24*time.Hour), testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Status != StatusAmbiguous || len(assessment.Candidates) != 2 {
		t.Fatalf("expected ambiguity: %+v", assessment)
	}
	reversed, err := EvaluateOccurrence(occurrence, []routines.Snapshot{first, second}, deviationTestBase.Add(4*24*time.Hour), testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Fingerprint != reversed.Fingerprint {
		t.Fatalf("candidate order changed fingerprint: %s != %s", assessment.Fingerprint, reversed.Fingerprint)
	}
}

func TestDifferentRoutineIDStillUsesClosestBaseline(t *testing.T) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "entity-a"}
	baseline, occurrence := testRoutine(t, subject, false)
	otherPattern := occurrence.Pattern
	otherPattern.Presence.EntryPoint = true
	otherID, err := routines.DeriveRoutineID("deviation-test-other", subject, routines.KindPresence, otherPattern)
	if err != nil {
		t.Fatal(err)
	}
	occurrence.RoutineID = otherID
	occurrence.ID, err = routines.DeriveOccurrenceID("deviation-test-other", otherID, routines.KindPresence, occurrence.ObservationIDs)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := EvaluateOccurrence(occurrence, []routines.Snapshot{baseline}, deviationTestBase.Add(4*7*24*time.Hour), testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Status == StatusInsufficientHistory || assessment.BestMatch == nil || assessment.BestMatch.ExactRoutineID {
		t.Fatalf("expected closest non-exact baseline: %+v", assessment)
	}
}

func TestCollisionAndPolicyFingerprint(t *testing.T) {
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "entity-a"}
	baseline, occurrence := testRoutine(t, subject, false)
	routine, err := routines.Restore(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := routine.AddOccurrence(routines.AddOccurrenceCommand{RoutineID: occurrence.RoutineID, SourceRevision: routine.Snapshot().Revision, Occurrence: occurrence, Mutation: chains.MutationContext{At: occurrence.ObservedAt, Actor: "test", Reason: "routine occurrence", CorrelationID: "collision-seed"}}); err != nil {
		t.Fatal(err)
	}
	changed := occurrence
	changed.ObservedAt = changed.ObservedAt.Add(time.Minute)
	if _, err := EvaluateOccurrence(changed, []routines.Snapshot{routine.Snapshot()}, deviationTestBase.Add(4*7*24*time.Hour), testPolicy()); !errors.Is(err, ErrDeviationOccurrenceCollision) {
		t.Fatalf("expected occurrence collision, got %v", err)
	}
	first, err := EvaluateOccurrence(testPresenceOccurrence(t, subject, false, "policy-occurrence", deviationTestBase.Add(4*7*24*time.Hour)), []routines.Snapshot{baseline}, deviationTestBase.Add(5*7*24*time.Hour), testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	changedPolicy := testPolicy()
	changedPolicy.TemporalToleranceBuckets++
	second, err := EvaluateOccurrence(testPresenceOccurrence(t, subject, false, "policy-occurrence", deviationTestBase.Add(4*7*24*time.Hour)), []routines.Snapshot{baseline}, deviationTestBase.Add(5*7*24*time.Hour), changedPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint || first.PolicyFingerprint == second.PolicyFingerprint {
		t.Fatal("policy change did not change assessment identity")
	}
}

func TestInvalidInputsAndCollision(t *testing.T) {
	if _, err := NewScore(1001); !errors.Is(err, ErrInvalidDeviationScore) {
		t.Fatalf("unexpected score error: %v", err)
	}
	policy := testPolicy()
	policy.StructuralWeight = 500
	if err := policy.Validate(); !errors.Is(err, ErrInvalidDeviationPolicy) {
		t.Fatalf("unexpected policy error: %v", err)
	}
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "entity-a"}
	baseline, occurrence := testRoutine(t, subject, false)
	other := occurrence
	other.ID = "invalid"
	if _, err := EvaluateOccurrence(other, []routines.Snapshot{baseline}, deviationTestBase.Add(4*24*time.Hour), testPolicy()); !errors.Is(err, ErrInvalidDeviationOccurrence) {
		t.Fatalf("expected invalid occurrence, got %v", err)
	}
}
