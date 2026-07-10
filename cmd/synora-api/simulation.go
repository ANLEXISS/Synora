package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"synora/internal/simulation"
	"synora/pkg/contract"
)

type simulationSender interface {
	Send(contract.Message) error
}

type simulationRunRequest struct {
	ScenarioID    string `json:"scenario_id"`
	DeviceID      string `json:"device_id"`
	NodeID        string `json:"node_id"`
	Repeat        int    `json:"repeat"`
	DryRunActions *bool  `json:"dry_run_actions"`
	LearningMode  string `json:"learning_mode"`
}

type simulationRunResponse struct {
	TestRunID     string `json:"test_run_id"`
	ScenarioID    string `json:"scenario_id"`
	Status        string `json:"status"`
	Repeat        int    `json:"repeat"`
	DryRunActions bool   `json:"dry_run_actions"`
	LearningMode  string `json:"learning_mode"`
}

type simulationRunStatus struct {
	TestRunID        string     `json:"test_run_id"`
	ScenarioID       string     `json:"scenario_id"`
	Status           string     `json:"status"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	StepsSent        int        `json:"steps_sent"`
	StepsObserved    int        `json:"steps_observed"`
	Errors           []string   `json:"errors"`
	EventInstanceIDs []string   `json:"event_instance_ids"`
	Repeat           int        `json:"repeat"`
	DryRunActions    bool       `json:"dry_run_actions"`
	LearningMode     string     `json:"learning_mode"`
}

type simulationRunner struct {
	sender simulationSender
	hub    *websocketHub

	mu   sync.RWMutex
	runs map[string]*simulationRunStatus
}

func newSimulationRunner(sender simulationSender, hub *websocketHub) *simulationRunner {
	return &simulationRunner{
		sender: sender,
		hub:    hub,
		runs:   make(map[string]*simulationRunStatus),
	}
}

func handleSimulationScenarios() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, http.StatusOK, simulation.ListScenarios())
	}
}

func handleSimulationRun(runner *simulationRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if runner == nil || runner.sender == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "simulation unavailable"})
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, err)
			return
		}
		var req simulationRunRequest
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
		}

		response, err := runner.Start(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, response)
	}
}

func handleSimulationRunStatus(runner *simulationRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/simulation/runs/"))
		if id == "" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "simulation run id required"})
			return
		}
		status, ok := runner.Status(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "simulation run not found"})
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func (r *simulationRunner) Start(req simulationRunRequest) (simulationRunResponse, error) {
	req.ScenarioID = strings.TrimSpace(req.ScenarioID)
	scenario, ok := simulation.ScenarioByID(req.ScenarioID)
	if !ok {
		return simulationRunResponse{}, fmt.Errorf("unknown scenario %q", req.ScenarioID)
	}
	repeat := req.Repeat
	if repeat < 1 {
		repeat = 1
	}
	dryRun := true
	if req.DryRunActions != nil {
		dryRun = *req.DryRunActions
	}
	if !dryRun {
		return simulationRunResponse{}, fmt.Errorf("dry_run_actions=false is not supported by API simulation V1")
	}
	learningMode := strings.ToLower(strings.TrimSpace(req.LearningMode))
	if learningMode == "" {
		learningMode = "simulation"
	}
	if learningMode != "simulation" && learningMode != "disabled" {
		return simulationRunResponse{}, fmt.Errorf("learning_mode %q is not supported", req.LearningMode)
	}

	run := simulation.BuildRun(scenario.Name, scenario.ID, simulation.ModeDryRun, simulation.GeneratedBySynoraAPI, map[string]any{
		"tool": "synora-api",
	})
	status := &simulationRunStatus{
		TestRunID:     run.ID,
		ScenarioID:    scenario.ID,
		Status:        "started",
		StartedAt:     run.StartedAt,
		Repeat:        repeat,
		DryRunActions: dryRun,
		LearningMode:  learningMode,
	}

	r.mu.Lock()
	r.runs[run.ID] = status
	r.mu.Unlock()

	r.publish("simulation.started", status.publicCopy())
	go r.runScenario(run, scenario, req, repeat, dryRun, learningMode)

	return simulationRunResponse{
		TestRunID:     run.ID,
		ScenarioID:    scenario.ID,
		Status:        "started",
		Repeat:        repeat,
		DryRunActions: dryRun,
		LearningMode:  learningMode,
	}, nil
}

func (r *simulationRunner) Status(id string) (simulationRunStatus, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	status, ok := r.runs[id]
	if !ok || status == nil {
		return simulationRunStatus{}, false
	}
	return status.publicCopy(), true
}

func (r *simulationRunner) runScenario(run simulation.SimulationRun, scenario simulation.Scenario, req simulationRunRequest, repeat int, dryRun bool, learningMode string) {
	for iteration := 1; iteration <= repeat; iteration++ {
		for index, step := range scenario.Steps {
			if step.DelayMs > 0 {
				time.Sleep(time.Duration(step.DelayMs) * time.Millisecond)
			}
			eventInstanceID := fmt.Sprintf("%s:%d:%s", run.ID, iteration, step.ID)
			msg, err := simulation.BuildMessage(simulation.EventBuildOptions{
				Type:            step.EventType,
				Source:          "api",
				SourceType:      contract.SourceSimulator,
				DeviceID:        firstNonEmptyString(req.DeviceID, step.DeviceID),
				CameraID:        firstNonEmptyString(req.DeviceID, step.CameraID),
				NodeID:          firstNonEmptyString(req.NodeID, step.NodeID),
				Identity:        step.Identity,
				Confidence:      step.Confidence,
				Run:             &run,
				ScenarioID:      scenario.ID,
				StepID:          step.ID,
				EventInstanceID: eventInstanceID,
				DryRun:          dryRun,
				GeneratedBy:     simulation.GeneratedBySynoraAPI,
				LearningMode:    learningMode,
				Data:            step.Data,
			})
			if err != nil {
				r.fail(run.ID, err)
				return
			}
			if err := r.sender.Send(msg); err != nil {
				r.fail(run.ID, err)
				return
			}
			stepData := map[string]any{
				"test_run_id":       run.ID,
				"scenario_id":       scenario.ID,
				"repeat_index":      iteration,
				"step_index":        index + 1,
				"step_count":        len(scenario.Steps),
				"scenario_step":     step,
				"event_type":        msg.Type,
				"event_instance_id": eventInstanceID,
			}
			r.recordStep(run.ID, eventInstanceID)
			r.publish("simulation.step", stepData)
		}
	}
	r.complete(run.ID)
}

func (r *simulationRunner) recordStep(runID string, eventInstanceID string) {
	r.mu.Lock()
	status := r.runs[runID]
	if status != nil {
		status.Status = "running"
		status.StepsSent++
		status.EventInstanceIDs = append(status.EventInstanceIDs, eventInstanceID)
	}
	r.mu.Unlock()
}

func (r *simulationRunner) complete(runID string) {
	completedAt := time.Now().UTC()
	var payload simulationRunStatus
	r.mu.Lock()
	status := r.runs[runID]
	if status != nil {
		status.Status = "completed"
		status.CompletedAt = &completedAt
		payload = status.publicCopy()
	}
	r.mu.Unlock()
	if status != nil {
		r.publish("simulation.completed", payload)
	}
}

func (r *simulationRunner) fail(runID string, err error) {
	completedAt := time.Now().UTC()
	var payload simulationRunStatus
	r.mu.Lock()
	status := r.runs[runID]
	if status != nil {
		status.Status = "failed"
		status.CompletedAt = &completedAt
		if err != nil {
			status.Errors = append(status.Errors, err.Error())
		}
		payload = status.publicCopy()
	}
	r.mu.Unlock()
	if status != nil {
		r.publish("simulation.failed", payload)
	}
}

func (r *simulationRunner) publish(messageType string, data any) {
	if r != nil && r.hub != nil {
		r.hub.Publish(messageType, data)
	}
}

func (s *simulationRunStatus) publicCopy() simulationRunStatus {
	if s == nil {
		return simulationRunStatus{}
	}
	out := *s
	out.Errors = append([]string(nil), s.Errors...)
	out.EventInstanceIDs = append([]string(nil), s.EventInstanceIDs...)
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
