package main

import (
	"fmt"
	"time"

	"synora/internal/simulation"
	"synora/pkg/contract"
)

func scenarios() map[string]Scenario {
	out := map[string]Scenario{}
	for _, scenario := range simulation.ListScenarios() {
		out[scenario.ID] = scenario
	}
	return out
}

func runScenario(sender EventSender, client SnapshotClient, cfg Config, name string, refresh func(*contract.PublicSnapshot, string)) error {
	scenario, ok := scenarios()[name]
	if !ok {
		return fmt.Errorf("unknown scenario %q", name)
	}
	mode := simulation.ModeLiveEvents
	if cfg.DryRunActions {
		mode = simulation.ModeDryRun
	}
	run := simulation.BuildRun(scenario.Name, scenario.ID, mode, simulation.GeneratedBySynoraLab, map[string]any{
		"tool": "synora-lab",
	})
	fmt.Printf("simulation run id=%s scenario=%s mode=%s\n", run.ID, scenario.ID, run.Mode)
	for i, step := range scenario.Steps {
		if step.DelayMs > 0 {
			time.Sleep(time.Duration(step.DelayMs) * time.Millisecond)
		}
		opts := EventOptions{
			Type:        step.EventType,
			DeviceID:    firstNonEmpty(step.DeviceID, cfg.DeviceID),
			CameraID:    firstNonEmpty(step.CameraID, cfg.CameraID),
			NodeID:      firstNonEmpty(step.NodeID, cfg.NodeID),
			Identity:    firstNonEmpty(step.Identity, cfg.Identity),
			Confidence:  nonZeroFloat(step.Confidence, cfg.Confidence),
			Run:         &run,
			ScenarioID:  scenario.ID,
			StepID:      step.ID,
			DryRun:      cfg.DryRunActions,
			GeneratedBy: simulation.GeneratedBySynoraLab,
			Data:        step.Data,
		}
		msg, err := sendEvent(sender, opts)
		if err != nil {
			return err
		}
		status := fmt.Sprintf("SIMULATION %s run=%s step=%s %d/%d sent %s from %s", scenario.ID, run.ID, step.ID, i+1, len(scenario.Steps), msg.Type, msg.Source)
		snapshot, _ := client.Fetch()
		if refresh != nil {
			refresh(snapshot, status)
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func nonZeroFloat(value float64, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}
