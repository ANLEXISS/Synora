package validation

import (
	"context"
	"testing"
)

func TestPhysicalDeploymentQualificationUsesOfflineReadiness(t *testing.T) {
	results, readiness, err := RunPhysicalDeploymentQualification(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !readiness.ReadyForManualInstallation {
		t.Fatalf("readiness=%+v results=%v", readiness, results)
	}
	for name, passed := range results {
		if !passed {
			t.Fatalf("qualification %s failed", name)
		}
	}
}
