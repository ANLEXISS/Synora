package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"synora/internal/bus"
	"synora/internal/simulation"
	"synora/pkg/contract"
)

func newBusSender(path string) (EventSender, error) {
	return bus.NewClient(path, defaultBusClient)
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

type Observation struct {
	Observed bool
	Reason   string
	Err      error
}

func sendEventObserved(sender EventSender, client SnapshotClient, cfg Config, opts EventOptions) (contract.Message, Observation, error) {
	before, beforeErr := client.Fetch()
	msg, err := buildMessage(opts)
	if err != nil {
		return contract.Message{}, Observation{}, err
	}
	if cfg.Verbose {
		printVerboseMessage(msg)
	}
	if err := sender.Send(msg); err != nil {
		return msg, Observation{}, err
	}
	if beforeErr != nil {
		return msg, Observation{Err: beforeErr}, nil
	}
	return msg, observeInSnapshot(client, before, msg, 2*time.Second), nil
}

func optionsFromConfig(cfg Config, eventType string) EventOptions {
	mode := simulation.ModeLiveEvents
	if cfg.DryRunActions {
		mode = simulation.ModeDryRun
	}
	run := simulation.BuildRun("single event", "", mode, simulation.GeneratedBySynoraLab, nil)
	return EventOptions{
		Type:         eventType,
		Source:       defaultBusClient,
		SourceType:   contract.SourceSimulator,
		DeviceID:     cfg.DeviceID,
		CameraID:     cfg.CameraID,
		NodeID:       cfg.NodeID,
		Identity:     cfg.Identity,
		Confidence:   cfg.Confidence,
		Run:          &run,
		DryRun:       cfg.DryRunActions,
		GeneratedBy:  simulation.GeneratedBySynoraLab,
		LearningMode: cfg.LearningMode,
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

func printVerboseConfig(cfg Config) {
	fmt.Printf("bus client: %s\n", defaultBusClient)
	fmt.Printf("bus path: %s\n", cfg.BusPath)
	fmt.Printf("api URL: %s\n", cfg.APIURL)
}

func printVerboseMessage(msg contract.Message) {
	fmt.Println(verboseMessageText(msg))
}

func verboseMessageText(msg contract.Message) string {
	data, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return fmt.Sprintf("message JSON unavailable: %v", err)
	}
	return string(data)
}

func observeInSnapshot(client SnapshotClient, before *contract.PublicSnapshot, msg contract.Message, timeout time.Duration) Observation {
	metadata := payloadMetadata(msg.Payload)
	eventInstanceID := valueString(metadata["event_instance_id"])
	testRunID := valueString(metadata["test_run_id"])
	stepID := valueString(metadata["scenario_step_id"])
	beforeProcessed := metricNumber(before, "event_processed")
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		snapshot, err := client.Fetch()
		if err == nil {
			if snapshotHasEventInstance(snapshot, eventInstanceID) {
				return Observation{Observed: true, Reason: "event_instance_id"}
			}
			if snapshotHasEvent(snapshot, testRunID, stepID) {
				return Observation{Observed: true, Reason: "test_run_id+scenario_step_id"}
			}
			if metricNumber(snapshot, "event_processed") > beforeProcessed {
				return Observation{Observed: true, Reason: "metrics"}
			}
			if snapshotHasActionResult(snapshot, testRunID) {
				return Observation{Observed: true, Reason: "action_result metadata"}
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return Observation{Err: lastErr}
}

func snapshotHasEventInstance(snapshot *contract.PublicSnapshot, eventInstanceID string) bool {
	if snapshot == nil || strings.TrimSpace(eventInstanceID) == "" {
		return false
	}
	for _, event := range snapshot.Events {
		metadata := nestedMetadata(event)
		if valueString(metadata["event_instance_id"]) == eventInstanceID {
			return true
		}
	}
	return false
}

func snapshotHasEvent(snapshot *contract.PublicSnapshot, testRunID string, stepID string) bool {
	if snapshot == nil || strings.TrimSpace(testRunID) == "" {
		return false
	}
	for _, event := range snapshot.Events {
		metadata := nestedMetadata(event)
		if valueString(metadata["test_run_id"]) != testRunID {
			continue
		}
		if stepID == "" || valueString(metadata["scenario_step_id"]) == stepID {
			return true
		}
	}
	return false
}

func snapshotHasActionResult(snapshot *contract.PublicSnapshot, testRunID string) bool {
	if snapshot == nil || strings.TrimSpace(testRunID) == "" {
		return false
	}
	for _, result := range snapshot.ActionResults {
		metadata := nestedMetadata(result)
		if valueString(metadata["test_run_id"]) == testRunID {
			return true
		}
	}
	return false
}

func nestedMetadata(item map[string]any) map[string]any {
	payload, _ := item["payload"].(map[string]any)
	metadata, _ := payload["metadata"].(map[string]any)
	if metadata != nil {
		return metadata
	}
	metadata, _ = item["metadata"].(map[string]any)
	return metadata
}

func metricNumber(snapshot *contract.PublicSnapshot, key string) float64 {
	if snapshot == nil || snapshot.Metrics == nil {
		return 0
	}
	switch value := snapshot.Metrics[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}
