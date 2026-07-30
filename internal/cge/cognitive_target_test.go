package cge

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCognitiveTargetResolverResolvesTypedTargets(t *testing.T) {
	at := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	situation := CognitiveSituationSnapshot{SituationID: "situation", CurrentObservationID: "event", CapturedAt: at, Observations: []CognitiveObservationSnapshot{{ID: "event", EventType: "vision.unknown", Timestamp: at, NodeID: "hall", ZoneID: "upstairs", EntityID: "resident", DeviceID: "camera", ClipID: "clip"}}}
	base := CognitiveChainCandidate{Reference: ChainReference{ChainID: "chain", Version: 1, Class: ChainClassCritical, RevisionHash: "sha256:chain"}, Source: ChainSourceCriticalSeed, Status: ChainStatusActive, SituationID: "situation", ExpectedState: "suspicious", DangerScore: .8, Confidence: .8, EvidenceRefs: []string{"event"}, Scope: "critical/chain"}
	resolver := DefaultCognitiveDecisionTargetResolver{}
	for _, test := range []struct {
		action string
		kind   DecisionTargetKind
		id     string
	}{
		{action: "record_clip", kind: DecisionTargetDevice, id: "camera"},
		{action: "turn_on_relevant_lights", kind: DecisionTargetZone, id: "upstairs"},
		{action: "lock.device", kind: DecisionTargetDevice, id: "camera"},
		{action: "notify_resident", kind: DecisionTargetSystem, id: "system"},
	} {
		candidate := base
		candidate.ProposedActions = []string{test.action}
		target, err := resolver.ResolveTarget(context.Background(), situation, candidate)
		if err != nil || target.Kind != test.kind || target.ID != test.id {
			t.Fatalf("action %q target=%+v err=%v", test.action, target, err)
		}
	}
	ambiguous := situation.clone()
	ambiguous.Observations[0].DeviceID = ""
	ambiguousChain := base
	ambiguousChain.ProposedActions = []string{"record_clip"}
	if _, err := resolver.ResolveTarget(context.Background(), ambiguous, ambiguousChain); !errors.Is(err, ErrAmbiguousTarget) {
		t.Fatalf("ambiguous target accepted: %v", err)
	}
	changeMode := base
	changeMode.ProposedActions = []string{"change_mode:intrusion"}
	if target, err := resolver.ResolveTarget(context.Background(), situation, changeMode); err != nil || target.Kind != DecisionTargetSystem || target.ID != "system" {
		t.Fatalf("change_mode target=%+v err=%v", target, err)
	}
	deviceAction := base
	deviceAction.ProposedActions = []string{"turn_on_device"}
	if target, err := resolver.ResolveTarget(context.Background(), situation, deviceAction); err != nil || target.Kind != DecisionTargetDevice || target.ID != "camera" {
		t.Fatalf("device action target=%+v err=%v", target, err)
	}
	unknownAction := base
	unknownAction.ProposedActions = []string{"some_unclassified_action"}
	if _, err := resolver.ResolveTarget(context.Background(), situation, unknownAction); !errors.Is(err, ErrAmbiguousTarget) {
		t.Fatalf("unclassified target accepted: %v", err)
	}
}
