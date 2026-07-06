package devicecmd

import (
	"context"
	"testing"

	"synora/internal/actions"
	"synora/pkg/contract"
)

func TestAdapterUsesCommanderWhenConfigured(t *testing.T) {
	commander := &FakeCommander{}
	result, err := (Adapter{Commander: commander}).Execute(context.Background(), contract.ActionRequest{
		Action: contract.Action{
			Device:  "light-1",
			Command: "on",
			Value:   true,
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != actions.StatusAccepted || len(commander.Commands) != 1 {
		t.Fatalf("unexpected result=%#v commands=%#v", result, commander.Commands)
	}
}

func TestAdapterDryRunsWithoutCommander(t *testing.T) {
	result, err := (Adapter{}).Execute(context.Background(), contract.ActionRequest{
		Action: contract.Action{
			Device:  "light-1",
			Command: "on",
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Details["dry_run"] != true {
		t.Fatalf("expected dry run result: %#v", result)
	}
}
