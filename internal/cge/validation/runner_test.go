package validation

import (
	"context"
	"testing"
	"time"
)

func TestCatalogScenarios(t *testing.T) {
	for _, scenario := range Catalog() {
		scenario := scenario
		t.Run(scenario.ID, func(t *testing.T) {
			report, err := (&Runner{RootDir: t.TempDir()}).Run(context.Background(), scenario)
			if err != nil || !report.Success {
				t.Fatalf("scenario failed: err=%v report=%+v", err, report)
			}
		})
	}
}

func TestCheckpointMatrix(t *testing.T) {
	for _, scenario := range CheckpointMatrix() {
		scenario := scenario
		t.Run(scenario.ID, func(t *testing.T) {
			report, err := (&Runner{RootDir: t.TempDir()}).Run(context.Background(), scenario)
			if err != nil || !report.Success {
				t.Fatalf("checkpoint scenario failed: err=%v report=%+v", err, report)
			}
		})
	}
}

func TestExplicitResolutionIsIdempotent(t *testing.T) {
	scenario := associationAttachScenario()
	last := scenario.Steps[len(scenario.Steps)-1].At
	scenario.Steps = append(scenario.Steps, step("resolve-repeat", StepResolveHypothesis, last.Add(time.Second), ResolveHypothesisInput{PlanStepID: "plan-resolution"}))
	report, err := (&Runner{RootDir: t.TempDir()}).Run(context.Background(), scenario)
	if err != nil || !report.Success {
		t.Fatalf("idempotence scenario failed: %v", err)
	}
	if report.Metrics.IdempotentOperations == 0 {
		t.Fatal("repeat resolution was not reported idempotent")
	}
}
