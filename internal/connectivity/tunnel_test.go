package connectivity

import (
	"context"
	"errors"
	"testing"
)

func TestNoopTunnelDoesNotReportInterface(t *testing.T) {
	status, err := (NoopTunnelController{}).Status(context.Background())
	if err != nil || status.InterfacePresent {
		t.Fatalf("noop tunnel status=%+v err=%v", status, err)
	}
}

func TestMemoryTunnelObservesCallsAndErrors(t *testing.T) {
	controller := &MemoryTunnelController{Err: errors.New("controlled")}
	if err := controller.EnsureInterface(context.Background(), DefaultConfig().Interface); err == nil {
		t.Fatal("expected controlled error")
	}
	if controller.EnsureCalls != 1 {
		t.Fatalf("ensure calls=%d", controller.EnsureCalls)
	}
}
