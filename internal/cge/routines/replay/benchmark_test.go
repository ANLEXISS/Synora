package replay

import (
	"context"
	"fmt"
	"testing"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/routines"
)

func BenchmarkReplayRecords(b *testing.B) {
	for _, size := range []int{50, 200, 500, 1000} {
		b.Run(fmt.Sprintf("routines_%d", size), func(b *testing.B) {
			base := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
			journalPath := b.TempDir() + "/routines.ndjson"
			fileJournal, err := journal.NewFileJournal(journalPath, journal.FileJournalOptions{CreateParentDirs: true})
			if err != nil {
				b.Fatal(err)
			}
			if _, err := fileJournal.Initialize(context.Background(), journal.GenesisInput{JournalID: "routine-replay-benchmark", CreatedAt: base, RecordedAt: base, Purpose: "benchmark", Actor: "bench", CorrelationID: "genesis"}); err != nil {
				b.Fatal(err)
			}
			topology := cgecontext.TopologySnapshot{Revision: "bench-topology", CapturedAt: base, Nodes: []cgecontext.Node{{ID: "room", ZoneID: "ground", Kind: cgecontext.NodeRoom}}}
			for i := 0; i < size; i++ {
				at := base.Add(time.Duration(i+1) * time.Minute)
				chain, err := chains.New(chains.ChainID(fmt.Sprintf("bench-chain-%d", i)), chains.MutationContext{At: base, Actor: "bench", Reason: "create", CorrelationID: fmt.Sprintf("create-%d", i)})
				if err != nil {
					b.Fatal(err)
				}
				frame, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: fmt.Sprintf("bench-observation-%d", i), ObservedAt: at, NodeID: "room", Timezone: "UTC", Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome, Topology: topology})
				if err != nil {
					b.Fatal(err)
				}
				observation := chains.ObservationRef{ID: fmt.Sprintf("bench-observation-%d", i), EventType: "vision.identity", Timestamp: at, NodeID: "room", EntityID: fmt.Sprintf("entity-%d", i), Context: &frame}
				if err := chain.AddObservation(observation, chains.MutationContext{At: at, Actor: "bench", Reason: "observation", CorrelationID: fmt.Sprintf("observation-%d", i)}); err != nil {
					b.Fatal(err)
				}
				occurrence, err := routines.ExtractPresenceOccurrence(chain.Snapshot(), observation.ID, routines.DefaultExtractionPolicy())
				if err != nil {
					b.Fatal(err)
				}
				owned, err := routines.NewFromOccurrence(occurrence, chains.MutationContext{At: at, Actor: "bench", Reason: "routine", CorrelationID: fmt.Sprintf("routine-%d", i)})
				if err != nil {
					b.Fatal(err)
				}
				snapshot := owned.Snapshot()
				fingerprint, _ := snapshot.Fingerprint()
				if _, err := fileJournal.AppendRoutineCreated(context.Background(), journal.RoutineCreatedInput{RoutineID: snapshot.ID, NewRevision: 1, Snapshot: snapshot, SnapshotFingerprint: fingerprint, RecordedAt: at, Actor: "bench", CorrelationID: fmt.Sprintf("routine-%d", i)}); err != nil {
					b.Fatal(err)
				}
			}
			source, err := fileJournal.ReadAll(context.Background())
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, _ = ReplayRecords(source.Records)
			}
		})
	}
}
