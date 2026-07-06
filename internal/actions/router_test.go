package actions

import (
	"context"
	"testing"

	"synora/pkg/contract"
)

func TestRouterSelectsDeviceExecutor(t *testing.T) {
	device := &recordingExecutor{}
	mqtt := &recordingExecutor{}
	router := Router{
		DeviceCmd: device,
		MQTT:      mqtt,
	}

	_, err := router.Execute(context.Background(), contract.ActionRequest{
		Action: contract.Action{
			Device:  "light-1",
			Command: "on",
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if device.calls != 1 || mqtt.calls != 0 {
		t.Fatalf("unexpected calls device=%d mqtt=%d", device.calls, mqtt.calls)
	}
}

func TestRouterSelectsRecorderExecutor(t *testing.T) {
	recorder := &recordingExecutor{}
	router := Router{
		Recorder: recorder,
	}

	_, err := router.Execute(context.Background(), contract.ActionRequest{
		Action: contract.Action{
			Type:    "recorder.start",
			Channel: "front",
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if recorder.calls != 1 {
		t.Fatalf("expected recorder call, got %d", recorder.calls)
	}
}
