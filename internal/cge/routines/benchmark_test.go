package routines

import (
	"fmt"
	"testing"
	"time"

	"synora/internal/cge/chains"
	cgecontext "synora/internal/cge/context"
)

func BenchmarkPlanLearning(b *testing.B) {
	for _, size := range []int{10, 100, 1000, 5000} {
		b.Run(fmt.Sprintf("occurrences_%d", size), func(b *testing.B) {
			observations := make([]chains.ObservationRef, 0, size)
			for i := 0; i < size; i++ {
				observations = append(observations, routineObservationForBenchmark(fmt.Sprintf("bench-%d", i), routineTestBase.Add(time.Duration(i)*time.Hour), "room", "entity-a"))
			}
			chain := snapshotForBenchmark("bench-plan-chain", observations)
			policy := DefaultExtractionPolicy()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = PlanLearning(chain, observations[len(observations)-1].ID, routineTopology(), routineTestBase.Add(24*time.Hour), policy)
			}
		})
	}
}

func BenchmarkExtractPresenceOccurrence(b *testing.B) {
	for _, size := range []int{10, 100, 1000, 5000} {
		b.Run(fmt.Sprintf("occurrences_%d", size), func(b *testing.B) {
			observations := make([]chains.ObservationRef, 0, size)
			for i := 0; i < size; i++ {
				observations = append(observations, routineObservationForBenchmark(fmt.Sprintf("presence-%d", i), routineTestBase.Add(time.Duration(i)*time.Minute), "room", "entity-a"))
			}
			chain := snapshotForBenchmark("bench-presence", observations)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = ExtractPresenceOccurrence(chain, observations[len(observations)-1].ID, DefaultExtractionPolicy())
			}
		})
	}
}

func BenchmarkExtractTransitionOccurrence(b *testing.B) {
	for _, size := range []int{10, 100, 1000, 5000} {
		b.Run(fmt.Sprintf("occurrences_%d", size), func(b *testing.B) {
			observations := make([]chains.ObservationRef, 0, size)
			for i := 0; i < size; i++ {
				observations = append(observations, routineObservationForBenchmark(fmt.Sprintf("transition-%d", i), routineTestBase.Add(time.Duration(i)*time.Minute), "room", "entity-a"))
			}
			chain := snapshotForBenchmark("bench-transition", observations)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = ExtractTransitionOccurrence(chain, observations[len(observations)-1].ID, routineTopology(), DefaultExtractionPolicy())
			}
		})
	}
}

func BenchmarkRoutineAddOccurrence(b *testing.B) {
	for _, size := range []int{10, 100, 1000, 5000} {
		b.Run(fmt.Sprintf("occurrences_%d", size), func(b *testing.B) {
			observations := make([]chains.ObservationRef, 0, size)
			for i := 0; i < size; i++ {
				observations = append(observations, routineObservationForBenchmark(fmt.Sprintf("add-%d", i), routineTestBase.Add(time.Duration(i)*time.Minute), "room", "entity-a"))
			}
			chain := snapshotForBenchmark("bench-add", observations)
			occurrence, _ := ExtractPresenceOccurrence(chain, observations[len(observations)-1].ID, DefaultExtractionPolicy())
			routine, err := NewFromOccurrence(occurrence, chains.MutationContext{At: occurrence.ObservedAt, Actor: "bench", Reason: "create", CorrelationID: "create"})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				candidate, _ := routine.Clone()
				extra := occurrence
				extra.ID, _ = DeriveOccurrenceID("bench", routine.id, KindPresence, []string{fmt.Sprintf("extra-%d", i)})
				extra.ObservationIDs = []string{fmt.Sprintf("extra-%d", i)}
				extra.ObservedAt = occurrence.ObservedAt.Add(time.Duration(i+1) * time.Minute)
				extra.LocalDate = extra.ObservedAt.UTC().Format("2006-01-02")
				_ = candidate.AddOccurrence(AddOccurrenceCommand{RoutineID: candidate.id, SourceRevision: candidate.revision, Occurrence: extra, Mutation: chains.MutationContext{At: extra.ObservedAt, Actor: "bench", Reason: "add", CorrelationID: "add"}})
			}
		})
	}
}

func BenchmarkRegistryApplyOccurrence(b *testing.B) {
	for _, size := range []int{50, 200, 500, 1000} {
		b.Run(fmt.Sprintf("routines_%d", size), func(b *testing.B) {
			registry := NewRegistry()
			var base Occurrence
			for i := 0; i < size; i++ {
				occurrence := occurrenceForRegistry(b, fmt.Sprintf("apply-%d", i), routineTestBase.Add(time.Duration(i)*time.Minute), fmt.Sprintf("apply-chain-%d", i), "")
				if i == 0 {
					base = occurrence
				}
				_, err := registry.ApplyOccurrence(occurrence, chains.MutationContext{At: occurrence.ObservedAt, Actor: "bench", Reason: "seed", CorrelationID: "seed"})
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				occurrence := base
				occurrence.ID, _ = DeriveOccurrenceID("bench", base.RoutineID, KindPresence, []string{fmt.Sprintf("apply-extra-%d", i)})
				occurrence.ObservationIDs = []string{fmt.Sprintf("apply-extra-%d", i)}
				occurrence.ObservedAt = base.ObservedAt.Add(time.Duration(i+1) * time.Minute)
				occurrence.LocalDate = occurrence.ObservedAt.UTC().Format("2006-01-02")
				_, _ = registry.ApplyOccurrence(occurrence, chains.MutationContext{At: occurrence.ObservedAt, Actor: "bench", Reason: "apply", CorrelationID: "apply"})
			}
		})
	}
}

func BenchmarkRegistryListBySubject(b *testing.B) {
	for _, size := range []int{50, 200, 500, 1000} {
		b.Run(fmt.Sprintf("routines_%d", size), func(b *testing.B) {
			registry := NewRegistry()
			var subject Subject
			for i := 0; i < size; i++ {
				occurrence := occurrenceForRegistry(b, fmt.Sprintf("list-%d", i), routineTestBase.Add(time.Duration(i)*time.Minute), fmt.Sprintf("list-chain-%d", i), "")
				if i == 0 {
					subject = occurrence.Subject
				}
				_, err := registry.ApplyOccurrence(occurrence, chains.MutationContext{At: occurrence.ObservedAt, Actor: "bench", Reason: "seed", CorrelationID: "seed"})
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = registry.ListBySubject(subject)
			}
		})
	}
}

func routineObservationForBenchmark(id string, at time.Time, node, entity string) chains.ObservationRef {
	frame, _ := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: id, ObservedAt: at, NodeID: node, Timezone: "UTC", Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome, Topology: routineTopology()})
	return chains.ObservationRef{ID: id, EventType: "vision.identity", Timestamp: at, NodeID: node, EntityID: entity, Context: &frame}
}
func snapshotForBenchmark(id string, observations []chains.ObservationRef) chains.Snapshot {
	chain, _ := chains.New(chains.ChainID(id), chains.MutationContext{At: routineTestBase, Actor: "bench", Reason: "create", CorrelationID: "bench-create"})
	for _, observation := range observations {
		_ = chain.AddObservation(observation, chains.MutationContext{At: observation.Timestamp, Actor: "bench", Reason: "add", CorrelationID: "bench-" + observation.ID})
	}
	return chain.Snapshot()
}
