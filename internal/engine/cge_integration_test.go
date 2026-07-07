package engine

import (
	"strings"
	"testing"
	"time"

	"synora/internal/device"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

func TestCGEEngineLearnsEventInGraphMemory(t *testing.T) {
	engineInstance := newCGETestEngine()
	store := state.NewStore()
	at := time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC)

	result := engineInstance.Analyze(&contract.Event{
		ID:         "evt-identity-entry",
		Type:       contract.EventVisionIdentity,
		Source:     "vision-worker",
		Timestamp:  at,
		DeviceID:   "cam_01",
		Identity:   "alexis",
		Confidence: 0.96,
		Payload:    map[string]any{"identity": "alexis", "confidence": 0.96},
	}, store)

	if result == nil || result.Decision == nil {
		t.Fatalf("expected CGE decision result, got %#v", result)
	}

	node, ok := engineInstance.graphMemory.GetLastNode("resident:alexis")
	if !ok || node == nil {
		t.Fatalf("expected graph memory last node for alexis")
	}
	if node.SubjectID != "alexis" || node.Event != "vision.id.seen" || node.TopologyNode != "entry" {
		t.Fatalf("unexpected learned node: %#v", node)
	}
	if !node.LastSeen.Equal(at) {
		t.Fatalf("learned node timestamp mismatch: got %s want %s", node.LastSeen, at)
	}
	if node.Context["node_id"] != "entry" {
		t.Fatalf("expected topology node in learned context: %#v", node.Context)
	}
}

func TestCGESequenceLearnedAndCognitiveUsesSameKey(t *testing.T) {
	engineInstance := newCGETestEngine()
	store := state.NewStore()

	engineInstance.Analyze(cgeEvent("evt-entry", contract.EventVisionIdentity, "cam_01", "alexis", cgeTime(0), nil), store)
	engineInstance.Analyze(cgeEvent("evt-motion", contract.EventVisionMotion, "cam_02", "", cgeTime(10*time.Second), map[string]any{"motion": true}), store)
	engineInstance.Analyze(cgeEvent("evt-salon", contract.EventVisionIdentity, "cam_02", "alexis", cgeTime(20*time.Second), nil), store)

	node, ok := engineInstance.graphMemory.GetLastNode("resident:alexis")
	if !ok || node == nil || node.TopologyNode != "salon" {
		t.Fatalf("expected alexis last graph node salon, got %#v", node)
	}

	sequence, ok := engineInstance.cognitive.Sequence("resident:alexis")
	if !ok || sequence == nil {
		t.Fatalf("expected cognitive sequence under same key resident:alexis")
	}
	if sequence.CurrentNode == nil || sequence.CurrentNode.TopologyNode != "salon" {
		t.Fatalf("cognitive sequence should point to salon with same key, got %#v", sequence)
	}
	if len(sequence.Events) != 2 {
		t.Fatalf("identity sequence should contain alexis events, got %d", len(sequence.Events))
	}
}

func TestCGEGraphUsageDifferentiatesNovelTransition(t *testing.T) {
	engineInstance := newCGETestEngine()
	store := state.NewStore()

	engineInstance.Analyze(cgeEvent("evt-train-entry-1", contract.EventVisionIdentity, "cam_01", "alexis", cgeTime(0), nil), store)
	engineInstance.Analyze(cgeEvent("evt-train-salon-1", contract.EventVisionIdentity, "cam_02", "alexis", cgeTime(10*time.Second), nil), store)
	engineInstance.Analyze(cgeEvent("evt-train-entry-2", contract.EventVisionIdentity, "cam_01", "alexis", cgeTime(20*time.Second), nil), store)
	coherent := engineInstance.Analyze(cgeEvent("evt-coherent-salon", contract.EventVisionIdentity, "cam_02", "alexis", cgeTime(30*time.Second), nil), store)
	incoherent := engineInstance.Analyze(cgeEvent("evt-rapid-remote", contract.EventVisionIdentity, "cam_05", "alexis", cgeTime(30500*time.Millisecond), nil), store)

	if coherent == nil || coherent.Decision == nil || incoherent == nil || incoherent.Decision == nil {
		t.Fatalf("expected decisions coherent=%#v incoherent=%#v", coherent, incoherent)
	}
	if !coherent.Decision.GraphUsed || !incoherent.Decision.GraphUsed {
		t.Fatalf("expected both decisions to use graph coherent=%#v incoherent=%#v", coherent.Decision, incoherent.Decision)
	}
	if !incoherent.Decision.ValidationRequired {
		t.Fatalf("rapid novel transition should require validation: %#v", incoherent.Decision)
	}
	if coherent.Decision.ValidationRequired {
		t.Fatalf("replayed coherent transition should not require validation: %#v", coherent.Decision)
	}
	if coherent.Decision.Reason == incoherent.Decision.Reason && coherent.Decision.EffectiveScore == incoherent.Decision.EffectiveScore {
		t.Fatalf("graph-backed coherent and incoherent decisions should differ: coherent=%#v incoherent=%#v", coherent.Decision, incoherent.Decision)
	}
	if !strings.Contains(incoherent.Decision.Reason, "rapid_novel_transition") {
		t.Fatalf("expected trace reason for rapid transition, got %q", incoherent.Decision.Reason)
	}
}

func newCGETestEngine() *Engine {
	devices := device.NewRegistry()
	devices.Register([]device.DeviceConfig{
		{ID: "cam_01", Type: "camera", Room: "entry", NodeID: "entry"},
		{ID: "cam_02", Type: "camera", Room: "salon", NodeID: "salon"},
		{ID: "cam_05", Type: "camera", Room: "remote_room", NodeID: "remote_room"},
	})
	topo := &topology.Topology{
		Nodes: map[string]*topology.Node{
			"entry":       {ID: "entry", Name: "Entry", Type: topology.NodeRoom},
			"salon":       {ID: "salon", Name: "Salon", Type: topology.NodeRoom},
			"cuisine":     {ID: "cuisine", Name: "Cuisine", Type: topology.NodeRoom},
			"remote_room": {ID: "remote_room", Name: "Remote Room", Type: topology.NodeRoom},
		},
	}
	return NewEngine(topo, devices, map[string]*topology.Resident{
		"alexis": {ID: "alexis", Name: "Alexis"},
	})
}

func cgeEvent(id string, eventType string, deviceID string, identity string, at time.Time, payload map[string]any) *contract.Event {
	if payload == nil {
		payload = map[string]any{}
	}
	if identity != "" {
		payload["identity"] = identity
	}
	return &contract.Event{
		ID:         id,
		Type:       eventType,
		Source:     "vision-worker",
		Timestamp:  at,
		DeviceID:   deviceID,
		Identity:   identity,
		Confidence: 0.90,
		Payload:    payload,
	}
}

func cgeTime(offset time.Duration) time.Time {
	return time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC).Add(offset)
}
