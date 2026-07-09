package main

import (
	"encoding/json"
	"fmt"

	"synora/internal/bus"
	"synora/internal/simulation"
	"synora/pkg/contract"
)

func newBusSender(path string) (EventSender, error) {
	return bus.NewClient(path, "synora-lab")
}

func buildPayload(opts EventOptions) map[string]any {
	return simulation.BuildPayload(opts)
}

func buildMessage(opts EventOptions) (contract.Message, error) {
	return simulation.BuildMessage(opts)
}

func sendEvent(sender EventSender, opts EventOptions) (contract.Message, error) {
	if sender == nil {
		return contract.Message{}, fmt.Errorf("bus sender is not configured")
	}
	msg, err := buildMessage(opts)
	if err != nil {
		return contract.Message{}, err
	}
	return msg, sender.Send(msg)
}

func optionsFromConfig(cfg Config, eventType string) EventOptions {
	mode := simulation.ModeLiveEvents
	if cfg.DryRunActions {
		mode = simulation.ModeDryRun
	}
	run := simulation.BuildRun("single event", "", mode, simulation.GeneratedBySynoraLab, nil)
	return EventOptions{
		Type:        eventType,
		DeviceID:    cfg.DeviceID,
		CameraID:    cfg.CameraID,
		NodeID:      cfg.NodeID,
		Identity:    cfg.Identity,
		Confidence:  cfg.Confidence,
		Run:         &run,
		DryRun:      cfg.DryRunActions,
		GeneratedBy: simulation.GeneratedBySynoraLab,
	}
}

func payloadMetadata(payload []byte) map[string]any {
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil
	}
	metadata, _ := decoded["metadata"].(map[string]any)
	return metadata
}
