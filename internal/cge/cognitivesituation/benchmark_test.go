package cognitivesituation

import (
	"testing"

	"synora/internal/cge/durableworkflow"
)

func BenchmarkBuildEpisode(b *testing.B) {
	state := stateWithEpisodeBenchmark()
	input := BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthEpisode}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Build(input, DefaultPolicy()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompareIdentical(b *testing.B) {
	state := stateWithEpisodeBenchmark()
	value, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthEpisode}, DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Compare(value, value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSnapshotFingerprint(b *testing.B) {
	state := stateWithEpisodeBenchmark()
	value, err := Build(BuildInput{Workflow: state, EpisodeID: "episode-cognitive-test", ExpectedDepth: DepthEpisode}, DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	snapshot := CognitiveSituationSnapshot{WorkflowRevision: state.Revision, Situations: []CognitiveSituation{value}, EpisodeIndex: map[string]int{value.EpisodeID: 0}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SnapshotFingerprint(snapshot)
	}
}

func stateWithEpisodeBenchmark() durableworkflow.WorkflowState {
	policy := durableworkflow.DefaultPolicy()
	state := durableworkflow.WorkflowState{SchemaFingerprint: durableworkflow.SchemaFingerprint(), PolicyFingerprint: policy.Fingerprint()}
	state.Digest = durableworkflow.WorkflowStateFingerprint(state)
	episode := qualificationEpisode()
	mutation := durableworkflow.WorkflowMutation{EpisodeID: string(episode.ID), Episode: episode, SourceWorkflowRevision: state.Revision, SourceWorkflowDigest: state.Digest}
	_, result, _ := durableworkflow.PlanTransaction(state, mutation, "benchmark-cognitive-tx", 1, episode.StartedAt, policy)
	return result
}
