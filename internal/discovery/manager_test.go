package discovery

import (
	"testing"

	"synora/internal/discovery/vision"
)

func TestClassifyVisionWorkerRunningWithMissingModelsIsDegradedButActive(t *testing.T) {
	status, reason := classifyVisionWorkerStatus(vision.WorkerSnapshot{
		Status: vision.WorkerStatusRunning,
		PID:    1234,
	}, true)
	if status != "degraded" || reason != "running with missing models" {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
}

func TestClassifyVisionWorkerStoppedIsUnavailable(t *testing.T) {
	status, reason := classifyVisionWorkerStatus(vision.WorkerSnapshot{
		Status: vision.WorkerStatusStopped,
	}, false)
	if status != "unavailable" || reason != vision.WorkerStatusStopped {
		t.Fatalf("status=%q reason=%q", status, reason)
	}
}
