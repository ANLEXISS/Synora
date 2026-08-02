package situationhypotheses

import (
	"fmt"
	"testing"
	"time"

	"synora/internal/cge/episodes"
	"synora/internal/cge/situationfacts"
)

func benchmarkFactSet(b *testing.B, count int) situationfacts.FactSet {
	b.Helper()
	values := make([]episodes.ObservationRef, 0, count)
	for i := 0; i < count; i++ {
		values = append(values, makeObservation(fmt.Sprintf("benchmark-%03d", i), hypothesisBase.Add(time.Duration(i)*time.Second), unknownSubject(), fmt.Sprintf("node-%d", i%4), "benchmark-track"))
	}
	return makeFactSetForBenchmark(b, values, 1)
}

func makeFactSetForBenchmark(b *testing.B, values []episodes.ObservationRef, revision int) situationfacts.FactSet {
	b.Helper()
	episode := episodes.Episode{ID: "episode-benchmark", Status: episodes.StatusOpen, CreatedAt: values[0].ObservedAt, StartedAt: values[0].ObservedAt, LastObservedAt: values[len(values)-1].ObservedAt, StatusChangedAt: values[0].ObservedAt, Observations: values, Subjects: []episodes.SubjectRef{unknownSubject()}, Nodes: []episodes.NodeRef{{ID: "node-0", ZoneID: "ground"}, {ID: "node-1", ZoneID: "ground"}, {ID: "node-2", ZoneID: "ground"}, {ID: "node-3", ZoneID: "ground"}}, ChainRefs: []episodes.ChainRef{{ID: "chain-1"}}, RoutineRefs: []episodes.RoutineRef{{ID: "routine-1"}}, EventTypes: []string{"vision.motion"}, ContextQualities: []string{"complete"}, DurationObserved: values[len(values)-1].ObservedAt.Sub(values[0].ObservedAt), Revision: uint64(revision)}
	set, err := situationfacts.Extract(situationfacts.ExtractionInput{Episode: episode, Topology: episodes.MapTopology{Relationships: map[string]episodes.TopologyRelationship{"node-0\x00node-1": episodes.TopologyAdjacent, "node-1\x00node-2": episodes.TopologyAdjacent, "node-2\x00node-3": episodes.TopologyAdjacent}}, ExtractedAt: hypothesisBase.Add(time.Hour)}, situationfacts.DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	return set
}

func BenchmarkEvaluateSmall(b *testing.B)   { benchmarkEvaluate(b, 1) }
func BenchmarkEvaluateMedium(b *testing.B)  { benchmarkEvaluate(b, 10) }
func BenchmarkEvaluateMaximal(b *testing.B) { benchmarkEvaluate(b, 50) }

func benchmarkEvaluate(b *testing.B, count int) {
	set := benchmarkFactSet(b, count)
	schema, policy := Schema(), DefaultPolicy()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Evaluate(EvaluationInput{FactSet: set}, schema, policy); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReevaluateFromDiffAdded(b *testing.B) {
	previous := benchmarkFactSet(b, 1)
	current := benchmarkFactSet(b, 2)
	current.EpisodeRevision = 2
	current.Fingerprint = situationfacts.FactSetFingerprint(current)
	diff, err := situationfacts.Diff(previous, current)
	if err != nil {
		b.Fatal(err)
	}
	previousEvaluation, err := Evaluate(EvaluationInput{FactSet: previous}, Schema(), DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ReevaluateFromDiff(previous, current, diff, previousEvaluation.Set, Schema(), DefaultPolicy()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReevaluateFromDiffModified10(b *testing.B) {
	previous := benchmarkFactSet(b, 10)
	current := benchmarkFactSet(b, 11)
	current.EpisodeRevision = 2
	current.Fingerprint = situationfacts.FactSetFingerprint(current)
	diff, err := situationfacts.Diff(previous, current)
	if err != nil {
		b.Fatal(err)
	}
	previousEvaluation, err := Evaluate(EvaluationInput{FactSet: previous}, Schema(), DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ReevaluateFromDiff(previous, current, diff, previousEvaluation.Set, Schema(), DefaultPolicy()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFullEvaluationEquivalent(b *testing.B) { benchmarkEvaluate(b, 10) }

func BenchmarkPlan8Hypotheses(b *testing.B) {
	set := benchmarkFactSet(b, 10)
	registry := NewRegistry()
	schema, policy := Schema(), DefaultPolicy()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Plan(set, registry.Snapshot(), schema, policy); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyPlan(b *testing.B) {
	sets := make([]situationfacts.FactSet, b.N)
	for i := range sets {
		set := benchmarkFactSet(b, 1)
		set.EpisodeRevision = uint64(i + 1)
		set.Fingerprint = situationfacts.FactSetFingerprint(set)
		sets[i] = set
	}
	registry := NewRegistry()
	b.ResetTimer()
	for _, set := range sets {
		plan, err := Plan(set, registry.Snapshot(), Schema(), DefaultPolicy())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := registry.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublicSnapshot10(b *testing.B)  { benchmarkSnapshot(b, 10) }
func BenchmarkPublicSnapshot100(b *testing.B) { benchmarkSnapshot(b, 100) }

func benchmarkSnapshot(b *testing.B, count int) {
	registry := NewRegistry()
	for i := 0; i < count; i++ {
		set := benchmarkFactSet(b, 1)
		set.EpisodeRevision = uint64(i + 1)
		set.EpisodeID = episodes.EpisodeID(fmt.Sprintf("episode-snapshot-%03d", i))
		set.Fingerprint = situationfacts.FactSetFingerprint(set)
		plan, err := Plan(set, registry.Snapshot(), Schema(), DefaultPolicy())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := registry.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry.Snapshot()
	}
}

func BenchmarkRegistryDigest(b *testing.B) {
	registry := NewRegistry()
	for i := 0; i < 10; i++ {
		set := benchmarkFactSet(b, 1)
		set.EpisodeID = episodes.EpisodeID(fmt.Sprintf("episode-digest-%03d", i))
		set.Fingerprint = situationfacts.FactSetFingerprint(set)
		plan, err := Plan(set, registry.Snapshot(), Schema(), DefaultPolicy())
		if err != nil {
			b.Fatal(err)
		}
		if _, err := registry.ApplyPlan(plan); err != nil {
			b.Fatal(err)
		}
	}
	snapshot := registry.Snapshot()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RegistryDigest(snapshot)
	}
}

func BenchmarkExplanation(b *testing.B) {
	set := benchmarkFactSet(b, 10)
	result, err := Evaluate(EvaluationInput{FactSet: set}, Schema(), DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	hypothesis, ok := findHypothesis(result.Set, KindCoherentUnrecognizedActivity)
	if !ok {
		b.Fatal("benchmark hypothesis missing")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Explain(hypothesis, set); err != nil {
			b.Fatal(err)
		}
	}
}
