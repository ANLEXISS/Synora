package rpc

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"synora/internal/automation"
	"synora/internal/device"
	"synora/internal/event"
	"synora/internal/snapshot"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

type testSender struct {
	messages []contract.Message
}

func (s *testSender) Send(msg contract.Message) error {
	s.messages = append(s.messages, msg)
	return nil
}

type testMetrics struct{}

func (testMetrics) Snapshot(*state.Store) map[string]any {
	return map[string]any{"event_processed": float64(1)}
}

func (testMetrics) SourceStatus(string, time.Duration) map[string]any {
	return map[string]any{"status": "ok"}
}

func TestRPCStateAndSnapshot(t *testing.T) {
	store := state.NewStore()
	devices := device.NewRegistry()
	devices.Register([]device.DeviceConfig{{
		ID:     "camera-1",
		Type:   "camera",
		Room:   "entry",
		NodeID: "entry",
	}})
	topo := &topology.Topology{
		Nodes: map[string]*topology.Node{
			"entry": {ID: "entry", Name: "Entry", Type: topology.NodeRoom},
		},
	}
	residents := map[string]*topology.Resident{
		"alexis": {ID: "alexis", Name: "Alexis"},
	}
	mu := &sync.RWMutex{}
	builder := &snapshot.Builder{
		Mu:         mu,
		State:      store,
		Devices:    devices,
		Topology:   topo,
		Residents:  residents,
		Automation: automation.NewEngine(t.TempDir() + "/automations.yaml"),
		Events:     event.NewStore(10),
		Metrics:    testMetrics{},
	}
	sender := &testSender{}
	server := NewServer(Config{
		Bus:        sender,
		State:      store,
		Events:     builder.Events,
		Devices:    devices,
		Automation: builder.Automation,
		Snapshot:   builder,
		Metrics:    testMetrics{},
	})

	server.Handle(contract.Message{
		ID:     "rpc-1",
		Type:   "core.state",
		Kind:   contract.KindRPC,
		Source: "api",
	})
	if len(sender.messages) != 1 {
		t.Fatalf("expected core.state response, got %d messages", len(sender.messages))
	}
	if sender.messages[0].Target != "api" || sender.messages[0].Kind != contract.KindRPC {
		t.Fatalf("unexpected core.state response: %#v", sender.messages[0])
	}
	var coreState map[string]any
	if err := json.Unmarshal(sender.messages[0].Payload, &coreState); err != nil {
		t.Fatalf("unmarshal core.state: %v", err)
	}
	if coreState["devices"] == nil || coreState["state_store"] == nil {
		t.Fatalf("core.state missing expected keys: %#v", coreState)
	}

	server.Handle(contract.Message{
		ID:     "rpc-2",
		Type:   "core.snapshot",
		Kind:   contract.KindRPC,
		Source: "api",
	})
	if len(sender.messages) != 2 {
		t.Fatalf("expected core.snapshot response, got %d messages", len(sender.messages))
	}
	var legacy contract.Snapshot
	if err := json.Unmarshal(sender.messages[1].Payload, &legacy); err != nil {
		t.Fatalf("unmarshal core.snapshot: %v", err)
	}
	if len(legacy.Structure.Devices) != 1 || len(legacy.Residents.Residents) != 1 {
		t.Fatalf("unexpected snapshot payload: %#v", legacy)
	}
}
