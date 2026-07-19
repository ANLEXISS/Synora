package connectivity

import (
	"encoding/json"
	"testing"

	"synora/pkg/contract"
	"synora/pkg/contracts"
)

func TestStatusRPCContainsOnlyPublicState(t *testing.T) {
	dataDir := t.TempDir()
	agent, err := NewAgent(DefaultConfig(), dataDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := agent.StatusResponse(contract.Message{ID: "status-1", Type: "connectivity.status", Kind: contract.KindRPC, Source: "api"})
	if err != nil {
		t.Fatal(err)
	}
	var status contracts.Status
	if err := json.Unmarshal(response.Payload, &status); err != nil {
		t.Fatal(err)
	}
	if status.State != contracts.StateDisabled || status.Mode != contracts.ModeNone {
		t.Fatalf("status=%+v", status)
	}
	if containsSecretField(response.Payload) {
		t.Fatalf("status contains secret field: %s", response.Payload)
	}
}
