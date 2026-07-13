package event

import (
	"testing"
	"time"

	"synora/internal/device"
	"synora/pkg/contract"
)

var chainTestStart = time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

func testChainEvent(eventType, activation string, at time.Time) *contract.Event {
	return &contract.Event{
		ID: "evt-" + eventType + at.Format("150405"), Type: eventType, Source: "test",
		Timestamp: at, DeviceID: "cam_01", NodeID: "entry", ActivationID: activation,
		TrackID: "track-01", ClipID: "clip-01", Payload: map[string]any{},
	}
}

func testEvaluation(eventID, state, level string, score float64) *contract.ChainEvaluation {
	return &contract.ChainEvaluation{
		EventID: eventID, State: state, DangerLevel: level, DangerScore: score,
		Reasons: []string{"test evaluation"}, EngineVersion: "test",
	}
}

func TestSignificantEventCreatesChain(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	event := testChainEvent(contract.EventVisionUnknown, "activation-1", chainTestStart)
	updates := manager.Process(event, testEvaluation(event.ID, "suspicious", "medium", 0.7))
	if len(updates) != 2 || updates[0].Type != "event.chain.created" || updates[1].Type != "event.chain.updated" {
		t.Fatalf("updates=%#v", updates)
	}
	chains := manager.List(ChainFilter{Status: "open"})
	if len(chains) != 1 || chains[0].LastSignificantEventAt.IsZero() || len(chains[0].Evaluations) != 1 {
		t.Fatalf("chains=%#v", chains)
	}
}

func TestSignificantEventEnrichesNodeIDFromDeviceRegistry(t *testing.T) {
	registry := device.NewRegistry()
	registry.Register([]device.DeviceConfig{{ID: "cam_01", Type: "camera", NodeID: "zoneA.L0.entree"}})
	manager := NewChainManager(DefaultChainConfig())
	manager.SetDeviceRegistry(registry)
	event := testChainEvent(contract.EventVisionUnknown, "activation-node", chainTestStart)
	event.NodeID = ""
	event.Payload = map[string]any{"device_id": "cam_01"}
	manager.Process(event, testEvaluation(event.ID, "suspicious", "medium", 0.7))
	chains := manager.List(ChainFilter{Status: "open"})
	if len(chains) != 1 || chains[0].PrimaryNodeID != "zoneA.L0.entree" {
		t.Fatalf("chains=%#v", chains)
	}
	if len(chains[0].RecentEvents) != 1 || chains[0].RecentEvents[0].NodeID != "zoneA.L0.entree" || chains[0].RecentEvents[0].Payload["node_id"] != "zoneA.L0.entree" {
		t.Fatalf("recent event=%#v", chains[0].RecentEvents)
	}
}

func TestProvidedNodeIDIsNotOverwritten(t *testing.T) {
	registry := device.NewRegistry()
	registry.Register([]device.DeviceConfig{{ID: "cam_01", Type: "camera", NodeID: "zoneA.L0.entree"}})
	manager := NewChainManager(DefaultChainConfig())
	manager.SetDeviceRegistry(registry)
	event := testChainEvent(contract.EventVisionUnknown, "activation-node-provided", chainTestStart)
	event.NodeID = "zoneA.L1.couloir"
	event.Payload = map[string]any{"node_id": event.NodeID}
	manager.Process(event, testEvaluation(event.ID, "suspicious", "medium", 0.7))
	chain := manager.List(ChainFilter{Status: "open"})[0]
	if chain.PrimaryNodeID != "zoneA.L1.couloir" || chain.RecentEvents[0].NodeID != "zoneA.L1.couloir" {
		t.Fatalf("chain=%#v", chain)
	}
}

func TestSecondSignificantEventUpdatesSameChain(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	first := testChainEvent(contract.EventVisionUnknown, "activation-1", chainTestStart)
	second := testChainEvent(contract.EventVisionIdentity, "activation-1", chainTestStart.Add(5*time.Second))
	manager.Process(first, testEvaluation(first.ID, "suspicious", "medium", 0.7))
	manager.Process(second, testEvaluation(second.ID, "activity", "low", 0.3))
	chains := manager.List(ChainFilter{Status: "open"})
	if len(chains) != 1 || chains[0].SignificantEventsCount != 2 || len(chains[0].Evaluations) != 2 {
		t.Fatalf("chains=%#v", chains)
	}
}

func TestMotionDoesNotCreateChainByDefault(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	manager.Process(testChainEvent(contract.EventVisionMotion, "", chainTestStart), nil)
	if chains := manager.List(ChainFilter{Status: "all"}); len(chains) != 0 {
		t.Fatalf("motion created chains=%#v", chains)
	}
}

func TestContextualEventDoesNotCreateChainByDefault(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	manager.Process(testChainEvent("camera.heartbeat", "", chainTestStart), nil)
	if chains := manager.List(ChainFilter{Status: "all"}); len(chains) != 0 {
		t.Fatalf("contextual event created chains=%#v", chains)
	}
}

func TestRuntimeDiagnosticsDoNotCreateSecurityChains(t *testing.T) {
	for _, eventType := range []string{
		contract.EventDiscoveryWorkerStarted,
		contract.EventDiscoveryWorkerCrashed,
		contract.EventDiscoveryVisionWorkerUnavailable,
		contract.EventRuntimeComponentFlapping,
		contract.EventRuntimeModelMissing,
	} {
		if role := ClassifyEventForChain(testChainEvent(eventType, "", chainTestStart)); role != ChainRoleIgnored {
			t.Fatalf("event %s role=%s, want ignored", eventType, role)
		}
	}
}

func TestManualRiskCreatesSignificantChain(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	event := testChainEvent(contract.EventManualRisk, "manual-risk", chainTestStart)
	manager.Process(event, testEvaluation(event.ID, "manual-risk", "high", 0.8))
	if got := manager.List(ChainFilter{Status: "open"}); len(got) != 1 {
		t.Fatalf("manual risk chains=%#v", got)
	}
}

func TestMotionDoesNotExtendChain(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	first := testChainEvent(contract.EventVisionUnknown, "activation-1", chainTestStart)
	manager.Process(first, testEvaluation(first.ID, "suspicious", "medium", 0.7))
	motion := testChainEvent(contract.EventVisionMotion, "activation-1", chainTestStart.Add(20*time.Second))
	updates := manager.Process(motion, nil)
	if len(updates) == 0 || manager.List(ChainFilter{Status: "open"})[0].LastSignificantEventAt != chainTestStart {
		t.Fatalf("motion changed significant timestamp: %#v", manager.List(ChainFilter{Status: "all"}))
	}
	closed := manager.CloseInactive(chainTestStart.Add(31 * time.Second))
	if len(closed) != 1 || closed[0].Type != "event.chain.closed" {
		t.Fatalf("closed=%#v", closed)
	}
}

func TestSignificantEventKeepsLongChainOpen(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	manager.Process(testChainEvent(contract.EventVisionUnknown, "activation-1", chainTestStart), testEvaluation("one", "suspicious", "medium", 0.7))
	manager.Process(testChainEvent(contract.EventVisionUncertain, "activation-1", chainTestStart.Add(25*time.Second)), testEvaluation("two", "suspicious", "medium", 0.7))
	manager.Process(testChainEvent(contract.EventVisionIdentity, "activation-1", chainTestStart.Add(50*time.Second)), testEvaluation("three", "activity", "low", 0.3))
	if chains := manager.List(ChainFilter{Status: "open"}); len(chains) != 1 || chains[0].SignificantEventsCount != 3 {
		t.Fatalf("long chain=%#v", chains)
	}
}

func TestChainClosesAfter30sWithoutSignificantEvent(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	manager.Process(testChainEvent(contract.EventVisionUnknown, "activation-1", chainTestStart), testEvaluation("one", "suspicious", "medium", 0.7))
	updates := manager.CloseInactive(chainTestStart.Add(30 * time.Second))
	if len(updates) != 1 || updates[0].Chain.ClosedReason != "significant_inactivity_timeout" {
		t.Fatalf("updates=%#v", updates)
	}
	if chains := manager.List(ChainFilter{Status: "open"}); len(chains) != 0 {
		t.Fatalf("open chains=%#v", chains)
	}
}

func TestEngineEvaluationProducedAtEachSignificantLink(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	manager.Process(testChainEvent(contract.EventVisionUnknown, "activation-1", chainTestStart), testEvaluation("one", "suspicious", "medium", 0.7))
	manager.Process(testChainEvent(contract.EventVisionWeapon, "activation-1", chainTestStart.Add(time.Second)), testEvaluation("two", "break-in", "critical", 0.98))
	chain := manager.List(ChainFilter{Status: "open"})[0]
	if len(chain.Evaluations) != 2 || chain.CurrentState != "break-in" || chain.DangerLevel != contract.DangerCritical {
		t.Fatalf("chain=%#v", chain)
	}
}

func TestCriticalChainMemoryMergesSimilarPattern(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	first := testChainEvent(contract.EventVisionWeapon, "activation-1", chainTestStart)
	second := testChainEvent(contract.EventVisionWeapon, "activation-2", chainTestStart.Add(40*time.Second))
	manager.Process(first, testEvaluation(first.ID, "break-in", "critical", 0.98))
	manager.Process(second, testEvaluation(second.ID, "break-in", "critical", 0.98))
	memories := manager.CriticalMemories(10)
	if len(memories) != 1 || memories[0].Occurrences != 2 {
		t.Fatalf("memories=%#v", memories)
	}
}

func TestCriticalChainMemoryMarksSimulationSource(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	event := testChainEvent(contract.EventVisionWeapon, "simulation-1", chainTestStart)
	event.Payload = map[string]any{"metadata": map[string]any{"simulated": true, "test_run_id": "run-1"}}
	manager.Process(event, testEvaluation(event.ID, "break-in", "critical", 0.98))
	memories := manager.CriticalMemories(10)
	if len(memories) != 1 || memories[0].Source != "simulation" || !memories[0].Simulated || memories[0].SimulatedOccurrences != 1 || memories[0].RealOccurrences != 0 {
		t.Fatalf("simulation memory metadata=%#v", memories)
	}
}

func TestChainFeedbackChangesCriticalMemoryWithoutChangingChain(t *testing.T) {
	manager := NewChainManager(DefaultChainConfig())
	event := testChainEvent(contract.EventVisionWeapon, "feedback-chain", chainTestStart)
	manager.Process(event, testEvaluation(event.ID, "break-in", "critical", 0.98))
	original := manager.List(ChainFilter{Status: "all"})[0]
	before := original.MaxDangerScore
	memories := manager.CriticalMemories(10)
	if len(memories) != 1 {
		t.Fatalf("memories=%#v", memories)
	}
	beforeConfidence := memories[0].Confidence
	if _, err := manager.ApplyChainFeedback(original.ID, contract.CgeChainFeedback{ChainID: original.ID, FinalOutcome: contract.CgeOutcomeFalsePositive}); err != nil {
		t.Fatalf("apply feedback: %v", err)
	}
	after := manager.List(ChainFilter{Status: "all"})[0]
	if after.MaxDangerScore != before || after.DangerLevel != original.DangerLevel || after.Summary != original.Summary {
		t.Fatalf("chain changed after feedback: before=%#v after=%#v", original, after)
	}
	if memory := manager.CriticalMemories(10)[0]; memory.Confidence >= beforeConfidence {
		t.Fatalf("confidence did not decrease: before=%v after=%v", beforeConfidence, memory.Confidence)
	}
}
