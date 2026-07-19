package cge

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/deviation"
	"synora/internal/cge/routines"
)

func TestShadowDeviationIsIndependentFromRoutineLearning(t *testing.T) {
	at := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	config := enabledShadowConfig(t.TempDir(), true)
	config.Context.Enabled = true
	config.Deviation.Enabled = true
	config.Routines.Enabled = false
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: at}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.contextProvider = completeContextProvider(at)
	if _, err := engine.Observe(context.Background(), shadowEvent("deviation-only", "vision.identity", at)); err != nil {
		t.Fatal(err)
	}
	if engine.coordinator.RoutineCount() != 0 {
		t.Fatalf("deviation-only mode mutated routines: %d", engine.coordinator.RoutineCount())
	}
	assessments := engine.RecentDeviationAssessments(10)
	if len(assessments) != 1 || assessments[0].Status != deviation.StatusInsufficientHistory {
		t.Fatalf("unexpected deviation-only assessment: %#v", assessments)
	}
	if got := engine.Metrics(); got.RoutineCreated != 0 || got.DeviationInsufficientHistory != 1 {
		t.Fatalf("unexpected independent metrics: %#v", got)
	}
}

func TestShadowDeviationConfigurationDefaultsAndBounds(t *testing.T) {
	config, err := LoadShadowConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if config.Deviation.Enabled || config.Deviation.RecentAssessmentLimit != 256 || config.Deviation.MaxAssessmentsPerObservation != 2 {
		t.Fatalf("unsafe or unexpected deviation defaults: %#v", config.Deviation)
	}
	values := map[string]string{
		ShadowEnabledEnv: "true", ShadowDeviationEnabledEnv: "true",
		ShadowDeviationRecentLimitEnv: "8", ShadowDeviationMaxAssessmentsEnv: "1",
	}
	configured, err := LoadShadowConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !configured.Deviation.Enabled || configured.Deviation.RecentAssessmentLimit != 8 || configured.Deviation.MaxAssessmentsPerObservation != 1 {
		t.Fatalf("deviation environment was not loaded: %#v", configured.Deviation)
	}
	configured.Deviation.RecentAssessmentLimit = MaxShadowDeviationRecentAssessments + 1
	if err := configured.Validate(); err == nil {
		t.Fatal("invalid recent assessment limit accepted")
	}
}

func TestShadowDeviationPrecedesLearningAndReloadClearsStore(t *testing.T) {
	at := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	root := t.TempDir()
	config := cognitiveShadowConfig(root)
	config.Context.Enabled = true
	config.Routines.Enabled = true
	config.Deviation.Enabled = true
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: at}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	engine.contextProvider = completeContextProvider(at)
	if _, err := engine.Observe(context.Background(), shadowEvent("deviation-first", "vision.identity", at)); err != nil {
		t.Fatal(err)
	}
	if engine.coordinator.RoutineCount() != 1 || len(engine.RecentDeviationAssessments(10)) != 1 {
		t.Fatalf("assessment was not completed before learning: routines=%d assessments=%d", engine.coordinator.RoutineCount(), len(engine.RecentDeviationAssessments(10)))
	}
	if _, err := engine.Observe(context.Background(), shadowEvent("deviation-first", "vision.identity", at)); err != nil {
		t.Fatal(err)
	}
	if got := engine.Metrics(); got.DeviationAlreadyEvaluated != 1 || got.RoutineOccurrenceIdempotent != 1 {
		t.Fatalf("retry was not independently idempotent: %#v", got)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	config.InitializeIfMissing = false
	reloaded, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: at}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if len(reloaded.RecentDeviationAssessments(10)) != 0 {
		t.Fatal("ephemeral assessments survived restart")
	}
	if reloaded.coordinator.RoutineCount() != 1 {
		t.Fatal("durable routine did not survive restart")
	}
}

func TestRecentDeviationStoreIsBoundedDefensiveAndConcurrent(t *testing.T) {
	store, err := NewRecentDeviationStore(2)
	if err != nil {
		t.Fatal(err)
	}
	assessment := deviation.Assessment{
		PolicyNamespace: "synora.cge.deviation", PolicyVersion: "deviation-v1",
		EvaluatedAt: atTestTime(), Status: deviation.StatusInsufficientHistory,
		Band: deviation.BandUnknown, ReasonCodes: []string{"baseline.empty"},
	}
	// Build a valid assessment through the pure evaluator instead of bypassing
	// its fingerprint invariants.
	assessment, err = deviation.EvaluateOccurrence(testDeviationOccurrence(t), nil, atTestTime(), deviation.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := store.Add(assessment); err != nil {
			t.Fatal(err)
		}
	}
	if store.Count() != 2 || len(store.List(0)) != 2 {
		t.Fatalf("store bound not enforced: count=%d", store.Count())
	}
	first := store.List(1)
	first[0].ReasonCodes[0] = "changed"
	if store.List(1)[0].ReasonCodes[0] == "changed" {
		t.Fatal("store returned shared mutable reason codes")
	}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_ = store.Add(assessment)
			_ = store.List(1)
		}()
	}
	group.Wait()
	if store.Count() != 2 {
		t.Fatal("concurrent store exceeded its bound")
	}
	if reflect.DeepEqual(store.List(0), nil) {
		t.Fatal("store unexpectedly empty")
	}
}

func atTestTime() time.Time { return time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC) }

func testDeviationOccurrence(t testing.TB) routines.Occurrence {
	t.Helper()
	at := atTestTime()
	subject := routines.Subject{Kind: routines.SubjectEntity, EntityID: "entity-a"}
	pattern := routines.Pattern{Kind: routines.KindPresence, Presence: &routines.PresencePattern{ContextSchemaVersion: cgecontext.SchemaVersionV1, NodeID: "entry", ZoneID: "ground", NodeKind: cgecontext.NodeEntrance, EntryPoint: true, Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome}}
	routineID, err := routines.DeriveRoutineID("shadow-test", subject, routines.KindPresence, pattern)
	if err != nil {
		t.Fatal(err)
	}
	occurrenceID, err := routines.DeriveOccurrenceID("shadow-test", routineID, routines.KindPresence, []string{"observation-a"})
	if err != nil {
		t.Fatal(err)
	}
	return routines.Occurrence{ID: occurrenceID, RoutineID: routineID, Kind: routines.KindPresence, Subject: subject, Pattern: pattern, ObservedAt: at, ObservationIDs: []string{"observation-a"}, Weekday: at.Weekday(), MinuteOfDay: 8 * 60, TimeBucket: 32, DayPart: cgecontext.DayPartMorning, LocalDate: at.Format("2006-01-02"), Timezone: "UTC", ContextQuality: cgecontext.QualityComplete, ExtractionPolicyNamespace: "shadow-test", ExtractionPolicyVersion: "routine-extraction-v1"}
}
