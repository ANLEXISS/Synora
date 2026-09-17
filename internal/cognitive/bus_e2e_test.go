package cognitive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/bus"
	"synora/pkg/contract"
)

func TestUnixBusCPUBackendDryRun(t *testing.T) {
	modelDir := os.Getenv("SYNORA_COGNITIVE_MODEL_DIR")
	if modelDir == "" {
		t.Skip("set SYNORA_COGNITIVE_MODEL_DIR for the Unix bus CPU scenario")
	}
	backend, err := NewCPUMLPBackend(modelDir)
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(t.TempDir(), "bus.sock")
	server := bus.NewServer(socket)
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Start() }()
	t.Cleanup(func() { _ = server.Close() })
	var serviceClient, senderClient *bus.Client
	for attempt := 0; attempt < 30; attempt++ {
		serviceClient, err = bus.NewClient(socket, "cognitive")
		if err == nil {
			senderClient, err = bus.NewClient(socket, "e2e-sender")
			if err == nil {
				break
			}
			_ = serviceClient.Close()
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serviceClient.Close(); _ = senderClient.Close() })
	service := &Service{Bus: serviceClient, Scheduler: NewScheduler(RegistryWithBackend(backend)), Name: "cognitive"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = service.Run(ctx) }()
	frame := StateFrame{SchemaVersion: StateFrameSchemaVersion, CapturedAt: time.Unix(3, 0).UTC()}
	encoded, err := (V4StateEncoder{}).Encode(context.Background(), frame)
	if err != nil {
		t.Fatal(err)
	}
	catalog := ActionCatalog{SchemaVersion: ActionCatalogSchemaVersion}
	input := CognitiveInput{SchemaVersion: SchemaVersion, RequestID: "bus-request", Task: Task{ID: "bus-task", Kind: "sensor", RequestedCapabilities: []string{CapabilityEventReasoning}}, EncodedState: encoded, ActionCatalog: catalog, ActionLedger: ActionLedgerSnapshot{SchemaVersion: ActionLedgerSchemaVersion}}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := senderClient.Send(contract.Message{ID: "bus-message", Type: MessageTypeTask, Kind: contract.KindCommand, Source: "e2e-sender", Target: "cognitive", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case message := <-senderClient.SubscribeChannel("e2e-sender"):
			if message.Type != MessageTypeOutput {
				continue
			}
			var output CognitiveOutput
			if err := json.Unmarshal(message.Payload, &output); err != nil {
				t.Fatal(err)
			}
			if !output.AdvisoryOnly || len(output.ExecutableActions) != 0 || len(output.ProposedActionIDs) != 0 {
				t.Fatalf("unsafe bus output: %+v", output)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for cognitive output")
		}
	}
}
