package simulation

import (
	"time"

	"synora/internal/idgen"
)

func BuildRun(name string, scenarioID string, mode string, createdBy string, metadata map[string]any) SimulationRun {
	if mode == "" {
		mode = ModeLiveEvents
	}
	if createdBy == "" {
		createdBy = GeneratedBySimulationEngine
	}
	return SimulationRun{
		ID:         idgen.New("sim"),
		Name:       name,
		ScenarioID: scenarioID,
		StartedAt:  time.Now().UTC(),
		Status:     StatusRunning,
		Mode:       mode,
		CreatedBy:  createdBy,
		Metadata:   cloneMap(metadata),
	}
}

func CompleteRun(run SimulationRun, status string) SimulationRun {
	if status == "" {
		status = StatusCompleted
	}
	finishedAt := time.Now().UTC()
	run.FinishedAt = &finishedAt
	run.Status = status
	return run
}
