package rpc

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"synora/internal/automation"
	"synora/internal/device"
	"synora/internal/event"
	"synora/internal/security"
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

type testRuntimeBus struct {
	testSender

	response *contract.Message
	err      error

	requests []contract.Message
}

func (b *testRuntimeBus) Request(
	msgType string,
	source string,
	payload []byte,
	target string,
) (*contract.Message, error) {
	b.requests = append(
		b.requests,
		contract.Message{
			Type:    msgType,
			Source:  source,
			Target:  target,
			Payload: payload,
		},
	)

	if b.err != nil {
		return nil, b.err
	}

	return b.response, nil
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

func TestPublicSnapshotAbsentResidentKeepsLastSeen(t *testing.T) {
	store := state.NewStore()
	lastSeen := time.Date(2026, 7, 11, 17, 3, 56, 742582666, time.UTC)
	store.SetPresence(&state.PresenceState{
		ID:         "alexis",
		ResidentID: "alexis",
		State:      "absent",
		Confidence: 0,
		Location:   "",
		LastSeen:   lastSeen,
	})

	builder := &snapshot.Builder{
		Mu:        &sync.RWMutex{},
		State:     store,
		Devices:   device.NewRegistry(),
		Topology:  &topology.Topology{Nodes: map[string]*topology.Node{}},
		Residents: map[string]*topology.Resident{"alexis": {ID: "alexis", Name: "Alexis"}},
		Events:    event.NewStore(10),
	}

	public := contract.PublicSnapshotFromCoreState(builder.CoreState())
	if len(public.Residents) != 1 {
		t.Fatalf("expected one resident in public snapshot: %#v", public.Residents)
	}
	resident := public.Residents[0]
	if resident["state"] != "absent" || resident["confidence"] != float64(0) {
		t.Fatalf("unexpected absent resident projection: %#v", resident)
	}
	if value, ok := resident["last_seen"].(time.Time); !ok || !value.Equal(lastSeen) {
		t.Fatalf("runtime last_seen missing from public snapshot: %#v", resident)
	}

	body, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal public snapshot: %v", err)
	}
	var decoded struct {
		Residents []map[string]any `json:"residents"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode public snapshot: %v", err)
	}
	if value, ok := decoded.Residents[0]["last_seen"].(string); !ok || value != "2026-07-11T17:03:56.742582666Z" {
		t.Fatalf("JSON public snapshot lost last_seen: %#v", decoded.Residents[0])
	}
}

func TestSystemHealthUsesRuntimeManager(t *testing.T) {
	store := state.NewStore()
	runtimeHealth := contract.RuntimeHealth{
		Services: map[string]contract.RuntimeServiceHealth{
			"synora-core": {
				Name:   "synora-core",
				Status: "active",
				Active: true,
			},
		},
		Network: contract.RuntimeNetworkHealth{
			Status: "ok",
		},
		MediaMTX: contract.RuntimeMediaMTXHealth{
			Status: "active",
		},
		Disk: contract.RuntimeDiskHealth{
			Status: "ok",
		},
		Timestamp: time.Date(
			2026,
			7,
			8,
			12,
			0,
			0,
			0,
			time.UTC,
		),
	}

	payload, err := json.Marshal(
		runtimeHealth,
	)
	if err != nil {
		t.Fatal(err)
	}

	bus := &testRuntimeBus{
		response: &contract.Message{
			Payload: payload,
		},
	}

	server := NewServer(Config{
		Bus:     bus,
		State:   store,
		Metrics: testMetrics{},
	})

	server.Handle(contract.Message{
		ID:     "rpc-health",
		Type:   "system.health",
		Kind:   contract.KindRPC,
		Source: "api",
	})

	if len(bus.requests) != 1 {
		t.Fatalf("runtime requests=%d", len(bus.requests))
	}

	if bus.requests[0].Type != contract.RPCRuntimeHealth || bus.requests[0].Target != "runtime-manager" {
		t.Fatalf("unexpected runtime request: %#v", bus.requests[0])
	}

	if len(bus.messages) != 1 {
		t.Fatalf("responses=%d", len(bus.messages))
	}

	var got contract.RuntimeHealth
	if err := json.Unmarshal(bus.messages[0].Payload, &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got.Network.Status != "ok" {
		t.Fatalf("health=%#v", got)
	}
}

func TestValidationRPCListAndResolve(t *testing.T) {
	store := state.NewStore()
	store.SetValidation(&contract.ValidationRequest{
		ID:               "validation-1",
		DecisionID:       "dec-1",
		EventID:          "evt-1",
		Reason:           "rapid_novel_transition",
		Evidence:         []string{"event:evt-1"},
		ProposedIdentity: "alexis",
		NodeID:           "entry",
		Status:           contract.ValidationStatusPending,
		CreatedAt:        time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC),
	})

	sender := &testSender{}
	server := NewServer(Config{
		Bus:   sender,
		State: store,
	})

	server.Handle(contract.Message{
		ID:     "rpc-validations-list",
		Type:   "validations.list",
		Kind:   contract.KindRPC,
		Source: "api",
	})

	if len(sender.messages) != 1 {
		t.Fatalf("expected validations.list response, got %d", len(sender.messages))
	}
	var listed []contract.ValidationRequest
	if err := json.Unmarshal(sender.messages[0].Payload, &listed); err != nil {
		t.Fatalf("unmarshal validations.list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "validation-1" {
		t.Fatalf("unexpected validations.list payload: %#v", listed)
	}

	payload, err := json.Marshal(contract.ValidationResolveRequest{
		ID:               "validation-1",
		Action:           contract.ValidationActionAssignIdentity,
		ProposedIdentity: "camille",
	})
	if err != nil {
		t.Fatal(err)
	}
	server.Handle(contract.Message{
		ID:      "rpc-validations-resolve",
		Type:    "validations.resolve",
		Kind:    contract.KindRPC,
		Source:  "api",
		Payload: payload,
	})

	if len(sender.messages) != 2 {
		t.Fatalf("expected validations.resolve response, got %d", len(sender.messages))
	}
	var resolved contract.ValidationRequest
	if err := json.Unmarshal(sender.messages[1].Payload, &resolved); err != nil {
		t.Fatalf("unmarshal validations.resolve: %v", err)
	}
	if resolved.Status != contract.ValidationStatusAccepted || resolved.ProposedIdentity != "camille" || resolved.ResolvedAt == nil {
		t.Fatalf("unexpected resolved validation: %#v", resolved)
	}

	stored, ok := store.Validation("validation-1")
	if !ok || stored.Status != contract.ValidationStatusAccepted || stored.ProposedIdentity != "camille" {
		t.Fatalf("validation feedback was not stored: %#v", stored)
	}
}

func TestDevicePairingRPCPersistsSecretHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security.yaml")
	cfg := &security.Config{
		APITokenHash:   security.HashSecret("dev-token"),
		AllowedOrigins: []string{"http://localhost:3000"},
		DeviceSecrets:  map[string]string{},
		PairingEnabled: true,
	}
	if err := security.Save(path, cfg); err != nil {
		t.Fatalf("save security config: %v", err)
	}

	sender := &testSender{}
	server := NewServer(Config{
		Bus:          sender,
		SecurityPath: path,
	})
	server.Handle(contract.Message{
		ID:     "rpc-pairing-start",
		Type:   "devices.pairing.start",
		Kind:   contract.KindRPC,
		Source: "api",
	})
	if len(sender.messages) != 1 {
		t.Fatalf("expected pairing start response, got %d", len(sender.messages))
	}

	var start security.PairingStartResponse
	if err := json.Unmarshal(sender.messages[0].Payload, &start); err != nil {
		t.Fatalf("unmarshal pairing start: %v", err)
	}
	if start.PairingID == "" {
		t.Fatalf("missing pairing id: %#v", start)
	}

	payload, err := json.Marshal(security.PairingCompleteRequest{
		PairingID: start.PairingID,
		DeviceID:  "cam_new",
	})
	if err != nil {
		t.Fatal(err)
	}
	server.Handle(contract.Message{
		ID:      "rpc-pairing-complete",
		Type:    "devices.pairing.complete",
		Kind:    contract.KindRPC,
		Source:  "api",
		Payload: payload,
	})
	if len(sender.messages) != 2 {
		t.Fatalf("expected pairing complete response, got %d", len(sender.messages))
	}

	var complete security.PairingCompleteResponse
	if err := json.Unmarshal(sender.messages[1].Payload, &complete); err != nil {
		t.Fatalf("unmarshal pairing complete: %v", err)
	}
	if complete.DeviceID != "cam_new" || complete.Secret == "" || complete.SecretHash == "" {
		t.Fatalf("unexpected pairing complete: %#v", complete)
	}

	stored, err := security.Load(path)
	if err != nil {
		t.Fatalf("load security config: %v", err)
	}
	if stored.DeviceSecrets["cam_new"] != complete.SecretHash {
		t.Fatalf("stored secret hash mismatch: %#v", stored.DeviceSecrets)
	}
}
