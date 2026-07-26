package cge

import (
	"context"
	"testing"
	"time"

	contracts "synora/internal/engine/contracts"
)

func matcherSeed(id string) contracts.CriticalSeed {
	return contracts.CriticalSeed{ID: id, Enabled: true, Version: 1, ExpectedState: "intrusion", DangerScore: .9, Sequence: []contracts.CriticalSeedStep{{EventType: "vision.unknown", ZoneRole: "entrance"}, {EventType: "vision.unknown", ZoneRole: "hallway"}, {EventType: "vision.unknown", ZoneRole: "private_room"}}}
}

func matcherSituation(times ...time.Time) CognitiveSituationSnapshot {
	observations := make([]CognitiveObservationSnapshot, len(times))
	roles := []string{"entrance", "hallway", "private_room"}
	for i, at := range times {
		observations[i] = CognitiveObservationSnapshot{ID: string(rune('a' + i)), EventType: "vision.unknown", Timestamp: at, ZoneRole: roles[i]}
	}
	return CognitiveSituationSnapshot{SituationID: "situation", EpisodeID: "episode", CurrentObservationID: observations[len(observations)-1].ID, Observations: observations, CapturedAt: times[len(times)-1]}
}

func TestCriticalChainMatcherRequiresCompleteOrderedSequence(t *testing.T) {
	base := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	matcher := CriticalChainMatcher{Window: time.Minute}
	one := matcherSituation(base)
	if matches, err := matcher.Match(context.Background(), one, []contracts.CriticalSeed{matcherSeed("critical-three-step")}); err != nil || len(matches) != 0 {
		t.Fatalf("single observation matched: %#v %v", matches, err)
	}
	complete := matcherSituation(base, base.Add(10*time.Second), base.Add(20*time.Second))
	matches, err := matcher.Match(context.Background(), complete, []contracts.CriticalSeed{matcherSeed("critical-three-step")})
	if err != nil || len(matches) != 1 || matches[0].MatchedSteps != 3 {
		t.Fatalf("complete sequence not matched: %#v %v", matches, err)
	}
	wrong := complete
	wrong.Observations[1].ZoneRole = "private_room"
	if matches, err := matcher.Match(context.Background(), wrong, []contracts.CriticalSeed{matcherSeed("critical-three-step")}); err != nil || len(matches) != 0 {
		t.Fatalf("wrong order/role matched: %#v %v", matches, err)
	}
	late := matcherSituation(base, base.Add(2*time.Minute), base.Add(4*time.Minute))
	if matches, err := matcher.Match(context.Background(), late, []contracts.CriticalSeed{matcherSeed("critical-three-step")}); err != nil || len(matches) != 0 {
		t.Fatalf("expired window matched: %#v %v", matches, err)
	}
}

func TestHistoricalChainIDDoesNotInfluenceAutonomousSelection(t *testing.T) {
	base := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	first := matcherSeed("critical-a")
	first.Sequence = []contracts.CriticalSeedStep{{EventType: "vision.unknown", ZoneRole: "entrance"}}
	second := matcherSeed("critical-b")
	second.Sequence = []contracts.CriticalSeedStep{{EventType: "vision.unknown", ZoneRole: "hallway"}}
	selector := NewContractChainSelector([]contracts.CriticalSeed{first, second}, nil, nil, nil)
	input := CognitiveDecisionInput{EventID: "event", ObservedEventType: "vision.unknown", HistoricalChainID: "critical-a", SituationID: "situation", CoreRevision: 1, Target: DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, Confidence: .5, DangerScore: .5, EvidenceRefs: []string{"event"}, CreatedAt: base, ValidUntil: base.Add(time.Minute), Situation: CognitiveSituationSnapshot{SituationID: "situation", CurrentObservationID: "event", Observations: []CognitiveObservationSnapshot{{ID: "event", EventType: "vision.unknown", Timestamp: base, ZoneRole: "hallway"}}, CapturedAt: base}}
	candidate, err := selector.SelectDecisionChain(context.Background(), input)
	if err != nil || candidate.Reference.ChainID != "critical-b" {
		t.Fatalf("historical chain influenced selection: %+v %v", candidate, err)
	}
}

func TestHistoricalChainIDDoesNotInfluenceSynthesizedIdentity(t *testing.T) {
	seed := matcherSeed("critical-autonomous")
	seed.Sequence = []contracts.CriticalSeedStep{{EventType: "vision.unknown", ZoneRole: "hallway"}}
	base := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	input := CognitiveDecisionInput{EventID: "event", ObservedEventType: "vision.unknown", SituationID: "situation", CoreRevision: 1, Target: DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, Confidence: .5, DangerScore: .5, EvidenceRefs: []string{"event"}, CreatedAt: base, ValidUntil: base.Add(time.Minute), Situation: CognitiveSituationSnapshot{SituationID: "situation", CurrentObservationID: "event", Observations: []CognitiveObservationSnapshot{{ID: "event", EventType: "vision.unknown", Timestamp: base, ZoneRole: "hallway"}}, CapturedAt: base}}
	selector := NewContractChainSelector([]contracts.CriticalSeed{seed}, nil, nil, nil)
	candidate, err := selector.SelectDecisionChain(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	first, err := (DefaultDecisionSynthesizer{}).SynthesizeDecision(context.Background(), input, candidate)
	if err != nil {
		t.Fatal(err)
	}
	input.HistoricalChainID = "historical-wrong-chain"
	second, err := (DefaultDecisionSynthesizer{}).SynthesizeDecision(context.Background(), input, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if first.DecisionID != second.DecisionID || first.IdempotencyKey != second.IdempotencyKey {
		t.Fatalf("historical chain changed cognitive identity: first=%+v second=%+v", first, second)
	}
}

func TestCatalogProviderIsDynamicAndDetached(t *testing.T) {
	base := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	firstSeed := matcherSeed("critical-first")
	firstSeed.Sequence = []contracts.CriticalSeedStep{{EventType: "vision.unknown", ZoneRole: "hallway"}}
	secondSeed := matcherSeed("critical-second")
	secondSeed.Sequence = []contracts.CriticalSeedStep{{EventType: "vision.motion", ZoneRole: "hallway"}}
	provider, err := NewStaticCognitiveChainCatalogProvider(CognitiveChainCatalogSnapshot{Revision: 1, CriticalSeeds: []contracts.CriticalSeed{firstSeed}, CapturedAt: base})
	if err != nil {
		t.Fatal(err)
	}
	selector := NewContractChainSelector(nil, nil, nil, nil)
	selector.SetCatalogProvider(provider)
	input := CognitiveDecisionInput{EventID: "event", ObservedEventType: "vision.unknown", SituationID: "situation", CoreRevision: 1, Target: DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, Confidence: .5, DangerScore: .5, EvidenceRefs: []string{"event"}, CreatedAt: base, ValidUntil: base.Add(time.Minute), Situation: CognitiveSituationSnapshot{SituationID: "situation", CurrentObservationID: "event", Observations: []CognitiveObservationSnapshot{{ID: "event", EventType: "vision.unknown", Timestamp: base, ZoneRole: "hallway"}}, CapturedAt: base}}
	if selected, err := selector.SelectDecisionChain(context.Background(), input); err != nil || selected.Reference.ChainID != "critical-first" {
		t.Fatalf("initial catalog selection=%+v err=%v", selected, err)
	}
	updated := CognitiveChainCatalogSnapshot{Revision: 2, CriticalSeeds: []contracts.CriticalSeed{secondSeed}, CapturedAt: base.Add(time.Second)}
	if err := provider.Replace(updated); err != nil {
		t.Fatal(err)
	}
	input.ObservedEventType = "vision.motion"
	input.Situation.Observations[0].EventType = "vision.motion"
	if selected, err := selector.SelectDecisionChain(context.Background(), input); err != nil || selected.Reference.ChainID != "critical-second" {
		t.Fatalf("updated catalog selection=%+v err=%v", selected, err)
	}
	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.CriticalSeeds[0].Sequence[0].EventType = "mutated-outside-provider"
	unchanged, err := provider.Snapshot(context.Background())
	if err != nil || unchanged.CriticalSeeds[0].Sequence[0].EventType != "vision.motion" {
		t.Fatalf("catalog snapshot was not detached: %+v err=%v", unchanged, err)
	}
}
