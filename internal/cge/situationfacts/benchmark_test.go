package situationfacts

import (
	"fmt"
	"testing"
	"time"

	"synora/internal/cge/episodes"
)

func benchmarkEpisode(count int) episodes.EpisodeSnapshot {
	values := make([]episodes.ObservationRef, 0, count)
	for i := 0; i < count; i++ {
		values = append(values, episodes.ObservationRef{EventID: fmt.Sprintf("bench-%03d", i), ObservedAt: factsBase.Add(time.Duration(i) * time.Second), ReceivedAt: factsBase.Add(time.Duration(i) * time.Second), EventType: "vision.motion", Subject: episodes.SubjectRef{Kind: episodes.SubjectKnown, EntityID: "resident-a"}, NodeID: "node-" + fmt.Sprint(i%4), ZoneID: "zone", HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete", ActivationID: "activation", TrackID: "track", SequenceKey: "sequence", ChainID: "chain", RoutineIDs: []string{"routine"}})
	}
	episode := episodes.Episode{ID: episodes.EpisodeID(fmt.Sprintf("episode-bench-%d", count)), Status: episodes.StatusOpen, CreatedAt: values[0].ObservedAt, StartedAt: values[0].ObservedAt, LastObservedAt: values[len(values)-1].ObservedAt, StatusChangedAt: values[0].ObservedAt, Observations: values, Subjects: []episodes.SubjectRef{{Kind: episodes.SubjectKnown, EntityID: "resident-a"}}, Nodes: []episodes.NodeRef{{ID: "node-0", ZoneID: "zone"}, {ID: "node-1", ZoneID: "zone"}, {ID: "node-2", ZoneID: "zone"}, {ID: "node-3", ZoneID: "zone"}}, ChainRefs: []episodes.ChainRef{{ID: "chain"}}, RoutineRefs: []episodes.RoutineRef{{ID: "routine"}}, EventTypes: []string{"vision.motion"}, ContextQualities: []string{"complete"}, DurationObserved: values[len(values)-1].ObservedAt.Sub(values[0].ObservedAt), Revision: 1}
	return episode
}

func BenchmarkExtract1(b *testing.B)   { benchmarkExtract(b, 1) }
func BenchmarkExtract10(b *testing.B)  { benchmarkExtract(b, 10) }
func BenchmarkExtract100(b *testing.B) { benchmarkExtract(b, 100) }
func BenchmarkExtract128(b *testing.B) { benchmarkExtract(b, 128) }
func BenchmarkIncremental99To100(b *testing.B) {
	previousEpisode := benchmarkEpisode(99)
	currentEpisode := benchmarkEpisode(100)
	currentEpisode.ID = previousEpisode.ID
	currentEpisode.Revision = 2
	policy := benchmarkPolicy(100)
	previous, err := Extract(ExtractionInput{Episode: previousEpisode, ExtractedAt: factsBase.Add(time.Hour)}, policy)
	if err != nil {
		b.Fatal(err)
	}
	input := IncrementalExtractionInput{PreviousEpisode: previousEpisode, CurrentEpisode: currentEpisode, PreviousFactSet: previous, ExtractedAt: factsBase.Add(time.Hour)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := ExtractIncremental(input, policy)
		if err != nil || result.Mode != IncrementalModeIncremental {
			b.Fatalf("incremental mode=%s err=%v", result.Mode, err)
		}
	}
}
func benchmarkExtract(b *testing.B, count int) {
	input := ExtractionInput{Episode: benchmarkEpisode(count), ExtractedAt: factsBase.Add(time.Hour)}
	policy := benchmarkPolicy(count)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Extract(input, policy); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiffSmall(b *testing.B)  { benchmarkDiff(b, 10) }
func BenchmarkDiffMedium(b *testing.B) { benchmarkDiff(b, 100) }
func BenchmarkDiffLarge(b *testing.B)  { benchmarkDiff(b, 128) }
func benchmarkDiff(b *testing.B, count int) {
	policy := benchmarkPolicy(count + 1)
	before, err := Extract(ExtractionInput{Episode: benchmarkEpisode(count), ExtractedAt: factsBase.Add(time.Hour)}, policy)
	if err != nil {
		b.Fatal(err)
	}
	changed := benchmarkEpisode(count + 1)
	changed.ID = before.EpisodeID
	changed.Revision = 2
	after, err := Extract(ExtractionInput{Episode: changed, ExtractedAt: factsBase.Add(time.Hour)}, policy)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Diff(before, after)
	}
}

func benchmarkPolicy(count int) Policy {
	policy := DefaultPolicy()
	if policy.MaxFactsPerEpisode < count*64 {
		policy.MaxFactsPerEpisode = count * 64
	}
	if policy.MaxProvenancePerFact < count+1 {
		policy.MaxProvenancePerFact = count + 1
	}
	return policy
}

func BenchmarkApply(b *testing.B) {
	sets := make([]FactSet, b.N)
	for i := range sets {
		episode := benchmarkEpisode(1)
		episode.ID = episodes.EpisodeID(fmt.Sprintf("episode-apply-%d", i))
		set, err := Extract(ExtractionInput{Episode: episode, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
		if err != nil {
			b.Fatal(err)
		}
		sets[i] = set
	}
	registry := NewRegistry()
	b.ResetTimer()
	for _, set := range sets {
		if _, err := registry.Apply(set); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyIdempotent(b *testing.B) {
	set := benchmarkSet(b, 1)
	registry := NewRegistry()
	if _, err := registry.Apply(set); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := registry.Apply(set); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyChanged(b *testing.B) {
	sets := make([]FactSet, b.N)
	for i := range sets {
		episode := benchmarkEpisode(1)
		episode.ID = "episode-apply-changed"
		episode.Revision = uint64(i + 1)
		set, err := Extract(ExtractionInput{Episode: episode, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
		if err != nil {
			b.Fatal(err)
		}
		sets[i] = set
	}
	registry := NewRegistry()
	b.ResetTimer()
	for _, set := range sets {
		if _, err := registry.Apply(set); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkSet(b *testing.B, count int) FactSet {
	b.Helper()
	set, err := Extract(ExtractionInput{Episode: benchmarkEpisode(count), ExtractedAt: factsBase.Add(time.Hour)}, benchmarkPolicy(count+1))
	if err != nil {
		b.Fatal(err)
	}
	return set
}

func BenchmarkSnapshot10(b *testing.B)          { benchmarkSnapshot(b, 10) }
func BenchmarkSnapshot100(b *testing.B)         { benchmarkSnapshot(b, 100) }
func BenchmarkSnapshot500(b *testing.B)         { benchmarkSnapshot(b, 500) }
func BenchmarkPlanningSnapshot100(b *testing.B) { benchmarkPlanningSnapshot(b, 100) }
func benchmarkSnapshot(b *testing.B, count int) {
	registry := NewRegistry()
	for i := 0; i < count; i++ {
		episode := benchmarkEpisode(1)
		episode.ID = episodes.EpisodeID(fmt.Sprintf("episode-snapshot-%03d", i))
		set, _ := Extract(ExtractionInput{Episode: episode, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
		_, _ = registry.Apply(set)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry.Snapshot()
	}
}

func benchmarkPlanningSnapshot(b *testing.B, count int) {
	registry := NewRegistry()
	for i := 0; i < count; i++ {
		episode := benchmarkEpisode(1)
		episode.ID = episodes.EpisodeID(fmt.Sprintf("episode-planning-%03d", i))
		set, err := Extract(ExtractionInput{Episode: episode, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := registry.Apply(set); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry.planningSnapshot()
	}
}
func BenchmarkRegistryDigest(b *testing.B) {
	registry := NewRegistry()
	for i := 0; i < 10; i++ {
		episode := benchmarkEpisode(1)
		episode.ID = episodes.EpisodeID(fmt.Sprintf("episode-digest-%03d", i))
		set, _ := Extract(ExtractionInput{Episode: episode, ExtractedAt: factsBase.Add(time.Hour)}, DefaultPolicy())
		_, _ = registry.Apply(set)
	}
	snapshot := registry.Snapshot()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RegistryDigest(snapshot)
	}
}

func BenchmarkFactSetFingerprint(b *testing.B) {
	set := benchmarkSet(b, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FactSetFingerprint(set)
	}
}

func BenchmarkFactCanonicalization(b *testing.B) {
	value := StringListFactValue([]string{"node-3", "node-1", "node-2"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = value.Canonical()
	}
}

func BenchmarkProvenanceCanonicalization(b *testing.B) {
	values := make([]ProvenanceRef, 128)
	for i := range values {
		values[i] = ProvenanceRef{SourceKind: "observation", SourceID: fmt.Sprintf("event-%03d", 127-i), SourceRevision: 1, ObservedAt: factsBase.Add(time.Duration(127-i) * time.Second), AlgorithmID: "benchmark", AlgorithmVersion: "v1"}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = canonicalProvenance(values)
	}
}
