package recorder

import (
	"context"
	"testing"

	"synora/internal/actions"
	"synora/pkg/contract"
)

func TestAdapterRecordsWhenConfigured(t *testing.T) {
	recorder := &FakeRecorder{Details: map[string]any{"clip_id": "clip-1"}}
	result, err := (Adapter{Recorder: recorder}).Execute(context.Background(), contract.ActionRequest{
		Action: contract.Action{
			Type:      "recorder.start",
			Channel:   "front",
			Residents: []string{"alice"},
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != actions.StatusAccepted || len(recorder.Requests) != 1 {
		t.Fatalf("unexpected result=%#v requests=%#v", result, recorder.Requests)
	}
	if result.Details["clip_id"] != "clip-1" {
		t.Fatalf("expected details to include clip id: %#v", result.Details)
	}
}

func TestAdapterDryRunsWithoutRecorder(t *testing.T) {
	result, err := (Adapter{}).Execute(context.Background(), contract.ActionRequest{
		Action: contract.Action{
			Type:    "record",
			Channel: "front",
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Details["dry_run"] != true {
		t.Fatalf("expected dry run result: %#v", result)
	}
}
