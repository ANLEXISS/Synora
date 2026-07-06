package mqtt

import (
	"context"
	"testing"

	"synora/internal/actions"
	"synora/pkg/contract"
)

func TestAdapterPublishesWhenConfigured(t *testing.T) {
	publisher := &FakePublisher{}
	result, err := (Adapter{Publisher: publisher}).Execute(context.Background(), contract.ActionRequest{
		Action: contract.Action{
			Type:    "mqtt",
			Channel: "synora/light-1/set",
			Value:   true,
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != actions.StatusAccepted || len(publisher.Messages) != 1 {
		t.Fatalf("unexpected result=%#v messages=%#v", result, publisher.Messages)
	}
	if publisher.Messages[0].Topic != "synora/light-1/set" {
		t.Fatalf("unexpected topic: %#v", publisher.Messages[0])
	}
}

func TestAdapterDryRunsWithoutPublisher(t *testing.T) {
	result, err := (Adapter{}).Execute(context.Background(), contract.ActionRequest{
		Action: contract.Action{
			Type:    "mqtt",
			Channel: "synora/light-1/set",
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Details["dry_run"] != true {
		t.Fatalf("expected dry run result: %#v", result)
	}
}
