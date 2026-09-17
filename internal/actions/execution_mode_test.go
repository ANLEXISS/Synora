package actions

import (
	"context"
	"encoding/json"
	"testing"

	"synora/pkg/contract"
)

func TestResolveExecutionModeFailsClosedAndRequiresArmedConfirmation(t *testing.T) {
	mode, err := ResolveExecutionMode(func(string) string { return "" })
	if err != nil || mode != ExecutionDryRun {
		t.Fatalf("default execution mode=%q err=%v", mode, err)
	}
	mode, err = ResolveExecutionMode(func(key string) string {
		if key == ExecutionModeEnv {
			return "armed"
		}
		return ""
	})
	if err == nil || mode != ExecutionDisabled {
		t.Fatalf("armed mode without confirmation should fail closed: mode=%q err=%v", mode, err)
	}
	mode, err = ResolveExecutionMode(func(key string) string {
		switch key {
		case ExecutionModeEnv:
			return "armed"
		case ArmedConfirmationEnv:
			return ArmedConfirmationValue
		default:
			return ""
		}
	})
	if err != nil || mode != ExecutionArmed {
		t.Fatalf("confirmed armed mode=%q err=%v", mode, err)
	}
}

func TestServiceExecutionModesNeverInvokeExecutorUnlessArmed(t *testing.T) {
	payload, err := json.Marshal(contract.ActionRequest{ID: "mode-test", Type: "device.command", Target: "light-1", Action: contract.Action{Device: "light-1", Command: "on"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []ExecutionMode{ExecutionDisabled, ExecutionDryRun} {
		bus := &recordingBus{}
		executor := &recordingExecutor{result: ExecutionResult{Status: StatusAccepted}}
		service := &Service{Bus: bus, Executor: executor, Deduper: NewDeduper(), ExecutionMode: mode, EnforceExecutionMode: true}
		service.HandleMessage(context.Background(), contract.Message{ID: "msg-" + string(mode), Type: contract.EventActionRequest, Kind: contract.KindCommand, Source: "core", Payload: payload})
		if executor.calls != 0 {
			t.Fatalf("mode %s invoked executor", mode)
		}
		result := decodeOnlyResult(t, bus)
		if mode == ExecutionDryRun && result.Status != StatusSimulatedSuccess {
			t.Fatalf("dry_run status=%q", result.Status)
		}
		if mode == ExecutionDisabled && result.Status != StatusSkipped {
			t.Fatalf("disabled status=%q", result.Status)
		}
	}
}
