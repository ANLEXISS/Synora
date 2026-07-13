package main

import (
	"net/http"
	"strings"
	"time"

	"synora/pkg/contract"
)

// handleRuntimeDiagnostics intentionally derives its read model from the
// public snapshot and the bounded runtime health probe. It does not expose
// internal state-store contents or raw filesystem paths.
func handleRuntimeDiagnostics(core interface {
	State() (*contract.PublicSnapshot, error)
	SystemHealth() (*contract.RuntimeHealth, error)
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		type stateResult struct {
			value *contract.PublicSnapshot
			err   error
		}
		stateCh := make(chan stateResult, 1)
		go func() {
			value, err := core.State()
			stateCh <- stateResult{value: value, err: err}
		}()

		type healthResult struct {
			value *contract.RuntimeHealth
			err   error
		}
		healthCh := make(chan healthResult, 1)
		go func() {
			value, err := core.SystemHealth()
			healthCh <- healthResult{value: value, err: err}
		}()

		var snapshot *contract.PublicSnapshot
		var runtimeHealth *contract.RuntimeHealth
		var stateErr, healthErr error
		select {
		case result := <-stateCh:
			snapshot, stateErr = result.value, result.err
		case <-time.After(500 * time.Millisecond):
			stateErr = errRuntimeProbeTimeout
		}
		select {
		case result := <-healthCh:
			runtimeHealth, healthErr = result.value, result.err
		case <-time.After(500 * time.Millisecond):
			healthErr = errRuntimeProbeTimeout
		}

		response := runtimeDiagnosticsResponse(snapshot, runtimeHealth, stateErr, healthErr)
		writeJSON(w, http.StatusOK, response)
	}
}

var errRuntimeProbeTimeout = runtimeProbeError("probe timed out")

type runtimeProbeError string

func (e runtimeProbeError) Error() string { return string(e) }

func runtimeDiagnosticsResponse(snapshot *contract.PublicSnapshot, runtimeHealth *contract.RuntimeHealth, stateErr, healthErr error) map[string]any {
	now := time.Now().UTC()
	response := map[string]any{
		"status":                         "degraded",
		"generated_at":                   now,
		"current_state":                  "unknown",
		"danger_level":                   "unknown",
		"danger_score":                   nil,
		"open_chains_count":              0,
		"real_open_chains_count":         0,
		"simulated_open_chains_count":    0,
		"last_real_significant_event_at": nil,
		"last_chain_evaluation_at":       nil,
		"last_action_request_at":         nil,
		"actions_enabled":                true,
		"dry_run_mode":                   false,
		"blocked_actions_recent":         []any{},
		"blocking_reasons":               []string{},
	}
	if stateErr != nil {
		response["state_error"] = stateErr.Error()
	}
	if healthErr != nil {
		response["health_error"] = healthErr.Error()
	}
	if snapshot != nil {
		populateSnapshotDiagnostics(response, snapshot)
	}
	if runtimeHealth != nil {
		response["runtime"] = runtimeHealth
		response["discovery_status"] = healthServiceStatus(runtimeHealth, "synora-discovery")
		response["vision_worker_status"] = healthServiceStatus(runtimeHealth, "synora-discovery")
		response["actions_status"] = healthServiceStatus(runtimeHealth, "synora-actions")
		if runtimeHealth.Status == "ok" && stateErr == nil {
			response["status"] = "ok"
		}
	} else {
		response["runtime"] = map[string]any{"status": "unknown"}
	}
	if healthErr != nil || stateErr != nil {
		response["status"] = "degraded"
	}
	return response
}

func populateSnapshotDiagnostics(response map[string]any, snapshot *contract.PublicSnapshot) {
	if snapshot == nil {
		return
	}
	if state := snapshot.System; state != nil {
		response["current_state"] = firstDiagnosticString(state["current_state"], state["last_state"], "unknown")
		response["current_state_since"] = firstDiagnosticValue(state["current_state_since"], state["last_state_time"])
		if known, ok := state["danger_known"].(bool); ok && known {
			response["danger_level"] = firstDiagnosticString(state["danger_level"], "unknown")
			response["danger_score"] = state["danger_score"]
		}
		if degraded, ok := state["degraded"].(bool); ok {
			response["degraded"] = degraded
		}
		response["last_real_significant_event_at"] = firstDiagnosticValue(state["last_real_event_at"])
		response["last_action_request_at"] = firstDiagnosticValue(state["last_action_at"])
	}
	chains := snapshot.EventChains
	if chains != nil {
		for source, target := range map[string]string{
			"open_count": "open_chains_count", "real_open_count": "real_open_chains_count",
			"simulated_open_count": "simulated_open_chains_count", "highest_real_danger_level": "danger_level",
		} {
			if value, exists := chains[source]; exists {
				response[target] = value
			}
		}
		response["critical_open_count"] = chains["critical_open_count"]
		response["recent_closed_count"] = chains["recent_closed_count"]
	}
	if len(snapshot.ActionResults) > 0 {
		last := snapshot.ActionResults[len(snapshot.ActionResults)-1]
		response["last_action_at"] = firstDiagnosticValue(last["finished_at"], last["timestamp"], last["started_at"])
	}
	if len(snapshot.Events) > 0 {
		response["last_real_event_at"] = snapshot.Events[len(snapshot.Events)-1]["timestamp"]
	}
	if len(snapshot.CGE) > 0 {
		response["model_status"] = snapshot.CGE["model_status"]
	}
}

func firstDiagnosticString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return "unknown"
}

func healthServiceStatus(health *contract.RuntimeHealth, service string) string {
	if health != nil {
		if item, ok := health.Services[service]; ok && strings.TrimSpace(item.Status) != "" {
			return item.Status
		}
	}
	return "unknown"
}

func firstDiagnosticValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
