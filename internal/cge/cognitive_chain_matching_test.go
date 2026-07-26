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
