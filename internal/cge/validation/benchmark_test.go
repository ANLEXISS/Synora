package validation

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/durable"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/chains/generations"
	"synora/internal/cge/chains/registry"
	"synora/internal/cge/hypotheses"
)

var benchmarkBase = time.Date(2026, 7, 18, 20, 0, 0, 0, time.UTC)

func benchmarkSnapshots(n int) []chains.Snapshot {
	result := make([]chains.Snapshot, 0, n)
	for i := 0; i < n; i++ {
		at := benchmarkBase.Add(time.Duration(i+1) * time.Second)
		chain, err := chains.New(chains.ChainID(fmt.Sprintf("cge-bench-%04d", i)), chains.MutationContext{At: at, Actor: "benchmark", Reason: "fixture", CorrelationID: fmt.Sprintf("chain-%d", i)})
		if err != nil {
			panic(err)
		}
		observation := chains.ObservationRef{ID: fmt.Sprintf("bench-observation-%04d", i), EventType: "vision.identity", Timestamp: at, EntityID: "benchmark-entity", SequenceKey: "benchmark-sequence"}
		if err := chain.AddObservation(observation, chains.MutationContext{At: at, Actor: "benchmark", Reason: "fixture observation", CorrelationID: observation.ID}); err != nil {
			panic(err)
		}
		result = append(result, chain.Snapshot())
	}
	return result
}

func benchmarkObservationChain(n int) chains.Snapshot {
	chain, err := chains.New("cge-benchmark-observation", chains.MutationContext{At: benchmarkBase, Actor: "benchmark", Reason: "fixture", CorrelationID: "observation-chain"})
	if err != nil {
		panic(err)
	}
	for i := 0; i < n; i++ {
		at := benchmarkBase.Add(time.Duration(i+1) * time.Second)
		observation := chains.ObservationRef{ID: fmt.Sprintf("bench-context-%05d", i), EventType: "vision.identity", Timestamp: at, EntityID: "benchmark-entity", SequenceKey: "benchmark-sequence"}
		if err := chain.AddObservation(observation, chains.MutationContext{At: at, Actor: "benchmark", Reason: "fixture observation", CorrelationID: observation.ID}); err != nil {
			panic(err)
		}
	}
	return chain.Snapshot()
}

func benchmarkRegistry(b *testing.B, contribution bool) {
	b.Helper()
	for _, size := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				reg := registry.New()
				chain, err := chains.New("cge-benchmark-registry", chains.MutationContext{At: benchmarkBase, Actor: "benchmark", Reason: "fixture", CorrelationID: "registry"})
				if err != nil {
					b.Fatal(err)
				}
				for j := 0; j < size; j++ {
					at := benchmarkBase.Add(time.Duration(j+1) * time.Second)
					observation := chains.ObservationRef{ID: fmt.Sprintf("registry-%d-%05d", i, j), EventType: "vision.identity", Timestamp: at, EntityID: "registry-entity", SequenceKey: "registry-sequence"}
					if err := chain.AddObservation(observation, chains.MutationContext{At: at, Actor: "benchmark", Reason: "fixture observation", CorrelationID: observation.ID}); err != nil {
						b.Fatal(err)
					}
				}
				if err := reg.Add(chain); err != nil {
					b.Fatal(err)
				}
				at := benchmarkBase.Add(time.Duration(size+2) * time.Second)
				var observation chains.ObservationRef
				var observationCommand chains.AddObservationCommand
				var contributionCommand chains.AddContributionCommand
				if contribution {
					contributionCommand = chains.AddContributionCommand{ChainID: "cge-benchmark-registry", SourceRevision: uint64(size + 1), Contribution: chains.ConfidenceContribution{ID: fmt.Sprintf("bench-contribution-%d", i), Source: "benchmark", Kind: chains.ContributionSupport, Value: 0.1, ObservationIDs: []string{fmt.Sprintf("registry-%d-%05d", i, size-1)}, Reason: "benchmark contribution", CreatedAt: at}, Mutation: chains.MutationContext{At: at, Actor: "benchmark", Reason: "benchmark contribution", CorrelationID: fmt.Sprintf("contribution-%d", i)}}
				} else {
					observation = chains.ObservationRef{ID: fmt.Sprintf("registry-new-%d", i), EventType: "vision.identity", Timestamp: at, EntityID: "registry-entity", SequenceKey: "registry-sequence"}
					observationCommand = chains.AddObservationCommand{ChainID: "cge-benchmark-registry", SourceRevision: uint64(size + 1), Observation: observation, Mutation: chains.MutationContext{At: at, Actor: "benchmark", Reason: "benchmark observation", CorrelationID: observation.ID}}
				}
				b.StartTimer()
				if contribution {
					_, err = reg.AddContribution(contributionCommand)
				} else {
					_, err = reg.AddObservation(observationCommand)
				}
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPlanAssociation(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500, 1000, 5000} {
		b.Run(fmt.Sprintf("chains-%d", size), func(b *testing.B) {
			snapshots := benchmarkSnapshots(size)
			input := association.Input{Observation: chains.ObservationRef{ID: "bench-target", EventType: "vision.identity", Timestamp: benchmarkBase.Add(time.Hour), EntityID: "benchmark-entity", SequenceKey: "benchmark-sequence"}}
			b.ReportMetric(float64(size), "chains")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := association.PlanAssociation(snapshots, input, benchmarkBase.Add(time.Hour), association.DefaultPolicy()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEvaluateObservation(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500, 1000, 5000} {
		b.Run(fmt.Sprintf("observations-%d", size), func(b *testing.B) {
			snapshot := benchmarkObservationChain(size)
			target := snapshot.Observations[len(snapshot.Observations)-1].ID
			b.ReportMetric(float64(size), "observations")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := evidence.EvaluateObservation(snapshot, target, benchmarkBase.Add(time.Duration(size+1)*time.Second), evidence.DefaultPolicy()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEvaluateBatch(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500, 1000, 5000} {
		b.Run(fmt.Sprintf("chains-%d", size), func(b *testing.B) {
			snapshots := benchmarkSnapshots(size)
			options := evidence.DefaultBatchOptions()
			options.MaxChains = size
			options.MaxObservationsPerChain = 1
			b.ReportMetric(float64(size), "chains")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := evidence.EvaluateBatch(snapshots, benchmarkBase.Add(time.Hour), evidence.DefaultPolicy(), options); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkShadowFlow is a synthetic future-shadow workload only. It exercises
// planning, explicit association application, and a bounded evidence batch;
// it never resolves a hypothesis and is not wired to runtime orchestration.
func BenchmarkShadowFlow(b *testing.B) {
	for _, size := range []int{50, 200, 500, 1000} {
		for _, operations := range []int{1, 5, 10} {
			b.Run(fmt.Sprintf("chains-%d/events-%d", size, operations), func(b *testing.B) {
				if operations > size {
					b.Skip("operation count exceeds fixture size")
				}
				for iteration := 0; iteration < b.N; iteration++ {
					b.StopTimer()
					coordinator, _, err := performanceCoordinator(context.Background(), filepath.Join(b.TempDir(), fmt.Sprintf("shadow-%d", iteration)), size)
					if err != nil {
						b.Fatal(err)
					}
					snapshots := benchmarkSnapshots(size)
					for index := range snapshots {
						snapshots[index].Observations[0].EntityID = fmt.Sprintf("shadow-entity-%d", index)
						chain, err := chains.New(chains.ChainID(fmt.Sprintf("cge-performance-chain-%05d", index)), chains.MutationContext{At: benchmarkBase, Actor: "benchmark", Reason: "shadow fixture", CorrelationID: fmt.Sprintf("shadow-chain-%d", index)})
						if err != nil {
							b.Fatal(err)
						}
						observation := snapshots[index].Observations[0]
						if err := chain.AddObservation(observation, chains.MutationContext{At: observation.Timestamp, Actor: "benchmark", Reason: "shadow fixture observation", CorrelationID: observation.ID}); err != nil {
							b.Fatal(err)
						}
						snapshots[index] = chain.Snapshot()
					}
					for index, snapshot := range snapshots {
						observation := snapshot.Observations[0]
						if _, err := coordinator.AddObservation(context.Background(), chains.AddObservationCommand{ChainID: snapshot.ID, SourceRevision: 1, Observation: observation, Mutation: chains.MutationContext{At: observation.Timestamp, Actor: "benchmark", Reason: "shadow fixture observation", CorrelationID: fmt.Sprintf("shadow-seed-%d", index)}}, observation.Timestamp.Add(time.Second)); err != nil {
							b.Fatal(err)
						}
					}
					options := evidence.DefaultBatchOptions()
					options.MaxChains = size
					options.MaxObservationsPerChain = 1
					b.StartTimer()
					for index := 0; index < operations; index++ {
						at := benchmarkBase.Add(time.Duration(index+2) * time.Second)
						plan, err := association.PlanAssociation(snapshots, association.Input{Observation: chains.ObservationRef{ID: fmt.Sprintf("shadow-event-%d", index), EventType: "vision.identity", Timestamp: at, EntityID: fmt.Sprintf("shadow-entity-%d", index), SequenceKey: "shadow-sequence"}}, at, association.DefaultPolicy())
						if err != nil {
							b.Fatal(err)
						}
						if plan.Decision != association.DecisionAttachExisting {
							b.Fatalf("shadow plan decision = %s, want attach_existing", plan.Decision)
						}
						if _, err := coordinator.ApplyAssociationPlan(context.Background(), plan, "benchmark", fmt.Sprintf("shadow-%d", index), at, at); err != nil {
							b.Fatal(err)
						}
						if _, err := evidence.EvaluateBatch(snapshots, at, evidence.DefaultPolicy(), options); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(operations), "simulated_events_per_second")
					_ = coordinator.Close()
				}
			})
		}
	}
}

func BenchmarkRegistryAddObservation(b *testing.B)  { benchmarkRegistry(b, false) }
func BenchmarkRegistryAddContribution(b *testing.B) { benchmarkRegistry(b, true) }

func benchmarkDurableFixture(b *testing.B) concurrencyFixture {
	b.Helper()
	fixture, err := newConcurrencyFixture(filepath.Join(b.TempDir(), "fixture"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = fixture.coordinator.Close() })
	return fixture
}

func BenchmarkCoordinatorResolveHypothesis(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		fixture := benchmarkDurableFixture(b)
		b.StartTimer()
		if _, err := fixture.coordinator.ResolveHypothesis(context.Background(), fixture.commands[0], fixture.commands[0].Mutation.At.Add(time.Second)); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
	}
}

func BenchmarkCoordinatorAddObservation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		fixture := benchmarkDurableFixture(b)
		at := fixture.commands[0].Mutation.At.Add(time.Second)
		command := chains.AddObservationCommand{ChainID: fixture.chainID, SourceRevision: 2, Observation: chains.ObservationRef{ID: fmt.Sprintf("bench-runtime-observation-%d", i), EventType: "vision.identity", Timestamp: at, EntityID: "concurrency-entity", SequenceKey: "concurrency-sequence"}, Mutation: chains.MutationContext{At: at, Actor: "benchmark", Reason: "benchmark observation", CorrelationID: fmt.Sprintf("runtime-observation-%d", i)}}
		b.StartTimer()
		if _, err := fixture.coordinator.AddObservation(context.Background(), command, at); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
	}
}

func BenchmarkCoordinatorAddObservationByChainCount(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("chains-%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				coordinator, _, err := performanceCoordinator(context.Background(), filepath.Join(b.TempDir(), fmt.Sprintf("observation-%d", i)), size)
				if err != nil {
					b.Fatal(err)
				}
				at := benchmarkBase.Add(time.Duration(size+2) * time.Second)
				command := chains.AddObservationCommand{ChainID: "cge-performance-chain-00000", SourceRevision: 1, Observation: chains.ObservationRef{ID: fmt.Sprintf("bench-size-observation-%d", i), EventType: "vision.identity", Timestamp: at, EntityID: "performance-entity", SequenceKey: "performance-sequence"}, Mutation: chains.MutationContext{At: at, Actor: "benchmark", Reason: "benchmark observation", CorrelationID: fmt.Sprintf("benchmark-size-observation-%d", i)}}
				b.StartTimer()
				if _, err := coordinator.AddObservation(context.Background(), command, at); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				_ = coordinator.Close()
			}
			b.ReportMetric(float64(size), "chains")
		})
	}
}

func BenchmarkCoordinatorAddContribution(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		coordinator, command, _, err := newEvidenceConcurrencyFixture(filepath.Join(b.TempDir(), "fixture"))
		if err != nil {
			b.Fatal(err)
		}
		template := command.Effect.AddContribution.ContributionTemplate
		at := command.Mutation.At.Add(time.Second)
		contribution := chains.AddContributionCommand{ChainID: command.Effect.AddContribution.ChainID, SourceRevision: command.Effect.AddContribution.SourceRevision, Contribution: chains.ConfidenceContribution{ID: fmt.Sprintf("bench-runtime-contribution-%d", i), Source: template.Source, Kind: template.Kind, Value: template.Value, ObservationIDs: append([]string(nil), template.ObservationIDs...), Reason: template.ReasonCode, CreatedAt: at}, Mutation: chains.MutationContext{At: at, Actor: "benchmark", Reason: "benchmark contribution", CorrelationID: fmt.Sprintf("runtime-contribution-%d", i)}}
		b.StartTimer()
		if _, err := coordinator.AddContribution(context.Background(), contribution, at); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		_ = coordinator.Close()
	}
}

func BenchmarkCoordinatorAddContributionByChainCount(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("chains-%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				coordinator, _, err := performanceCoordinator(context.Background(), filepath.Join(b.TempDir(), fmt.Sprintf("contribution-%d", i)), size)
				if err != nil {
					b.Fatal(err)
				}
				at := benchmarkBase.Add(time.Duration(size+2) * time.Second)
				observation := chains.AddObservationCommand{ChainID: "cge-performance-chain-00000", SourceRevision: 1, Observation: chains.ObservationRef{ID: fmt.Sprintf("bench-size-contribution-observation-%d", i), EventType: "vision.identity", Timestamp: at, EntityID: "performance-entity", SequenceKey: "performance-sequence"}, Mutation: chains.MutationContext{At: at, Actor: "benchmark", Reason: "benchmark observation", CorrelationID: fmt.Sprintf("benchmark-size-contribution-observation-%d", i)}}
				if _, err := coordinator.AddObservation(context.Background(), observation, at); err != nil {
					b.Fatal(err)
				}
				command := chains.AddContributionCommand{ChainID: observation.ChainID, SourceRevision: 2, Contribution: chains.ConfidenceContribution{ID: fmt.Sprintf("bench-size-contribution-%d", i), Source: "benchmark", Kind: chains.ContributionSupport, Value: 0.1, ObservationIDs: []string{observation.Observation.ID}, Reason: "benchmark contribution", CreatedAt: at.Add(time.Second)}, Mutation: chains.MutationContext{At: at.Add(time.Second), Actor: "benchmark", Reason: "benchmark contribution", CorrelationID: fmt.Sprintf("benchmark-size-contribution-%d", i)}}
				b.StartTimer()
				if _, err := coordinator.AddContribution(context.Background(), command, at.Add(time.Second)); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				_ = coordinator.Close()
			}
			b.ReportMetric(float64(size), "chains")
		})
	}
}

func BenchmarkCoordinatorRebaseHypothesis(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		fixture := benchmarkDurableFixture(b)
		current, err := fixture.coordinator.GetHypothesis(fixture.commands[0].SetID)
		if err != nil {
			b.Fatal(err)
		}
		at := fixture.commands[0].Mutation.At.Add(time.Second)
		plan := fixture.associationPlan.Clone()
		plan.ReasonCode = "association.rebase.benchmark"
		plan.Reason = "benchmark rebase plan"
		proposal, err := hypotheses.ProposeAssociationRebase(current, plan, at)
		if err != nil {
			b.Fatal(err)
		}
		command, err := proposal.Command(chains.MutationContext{At: at, Actor: "benchmark", Reason: "benchmark rebase", CorrelationID: fmt.Sprintf("rebase-%d", i)})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := fixture.coordinator.RebaseHypothesis(context.Background(), command, at); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		_ = fixture.coordinator.Close()
	}
}

func BenchmarkCoordinatorSupersedeHypothesis(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		coordinator, _, command, err := newEvidenceConcurrencyFixture(filepath.Join(b.TempDir(), "fixture"))
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := coordinator.SupersedeHypothesis(context.Background(), command, command.Mutation.At.Add(time.Second)); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		_ = coordinator.Close()
	}
}

func BenchmarkFileJournalReadAll(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("records-%d", size), func(b *testing.B) {
			coordinator, journalSource, err := performanceCoordinator(context.Background(), filepath.Join(b.TempDir(), "read-all"), size)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = coordinator.Close() })
			b.ReportMetric(float64(size), "records")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := journalSource.ReadAll(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFileJournalAppend(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("records-%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				coordinator, _, err := performanceCoordinator(context.Background(), filepath.Join(b.TempDir(), fmt.Sprintf("append-%d", i)), size)
				if err != nil {
					b.Fatal(err)
				}
				chain, err := chains.New(chains.ChainID(fmt.Sprintf("bench-append-chain-%d", i)), chains.MutationContext{At: benchmarkBase.Add(time.Hour), Actor: "benchmark", Reason: "append fixture", CorrelationID: fmt.Sprintf("append-chain-%d", i)})
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if _, err := coordinator.AddChain(context.Background(), chain, "benchmark", fmt.Sprintf("append-%d", i), benchmarkBase.Add(time.Hour)); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				_ = coordinator.Close()
			}
			b.ReportMetric(float64(size), "records")
		})
	}
}

func BenchmarkFileJournalReadHead(b *testing.B) {
	fixture := benchmarkDurableFixture(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fixture.journal.ReadHead(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCoordinatorValidationAndDigests(b *testing.B) {
	for _, size := range []int{10, 50, 100, 500} {
		b.Run(fmt.Sprintf("chains-%d", size), func(b *testing.B) {
			fixture := benchmarkDurableFixture(b)
			for i := 2; i < size; i++ {
				chain, err := chains.New(chains.ChainID(fmt.Sprintf("bench-extra-%d", i)), chains.MutationContext{At: benchmarkBase.Add(time.Duration(i) * time.Second), Actor: "benchmark", Reason: "extra chain", CorrelationID: fmt.Sprintf("extra-%d", i)})
				if err != nil {
					b.Fatal(err)
				}
				if _, err := fixture.coordinator.AddChain(context.Background(), chain, "benchmark", fmt.Sprintf("extra-%d", i), benchmarkBase.Add(time.Duration(i+1)*time.Second)); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(size), "chains")
			b.Run("state", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					if failures := ValidateCoordinatorState(fixture.coordinator); len(failures) != 0 {
						b.Fatal(failures[0])
					}
				}
			})
			b.Run("digest", func(b *testing.B) {
				status := fixture.coordinator.Status()
				for i := 0; i < b.N; i++ {
					if _, err := StateDigestOf(fixture.coordinator, status.JournalSequence, status.JournalHeadHash); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkCreateSnapshotGeneration(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		fixture := benchmarkDurableFixture(b)
		store, err := generations.NewStore(filepath.Join(b.TempDir(), "generations"), generations.StoreOptions{})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := fixture.coordinator.CreateSnapshotGeneration(context.Background(), store, benchmarkBase.Add(time.Hour), "benchmark", fmt.Sprintf("generation-%d", i)); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
	}
}

func BenchmarkReplayChainsAndHypotheses(b *testing.B) {
	fixture := benchmarkDurableFixture(b)
	snapshot, err := fixture.journal.ReadAll(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := durable.FromJournal(context.Background(), fixture.journal); err != nil {
			b.Fatal(err)
		}
	}
	_ = snapshot
}
