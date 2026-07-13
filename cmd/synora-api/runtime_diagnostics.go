package main

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"synora/pkg/contract"
)

const runtimeProbeTimeout = 2 * time.Second

var runtimeDiagnosticsCache struct {
	sync.RWMutex
	snapshot *contract.PublicSnapshot
	health   *contract.RuntimeHealth
}

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
		stateDone, healthDone := false, false
		deadline := time.NewTimer(runtimeProbeTimeout)
		defer deadline.Stop()
		for !stateDone || !healthDone {
			select {
			case result := <-stateCh:
				snapshot, stateErr, stateDone = result.value, result.err, true
				if result.err == nil && result.value != nil {
					cacheRuntimeSnapshot(result.value)
				}
			case result := <-healthCh:
				runtimeHealth, healthErr, healthDone = result.value, result.err, true
				if result.err == nil && result.value != nil {
					cacheRuntimeHealth(result.value)
				}
			case <-deadline.C:
				if !stateDone {
					stateErr, stateDone = errRuntimeProbeTimeout, true
				}
				if !healthDone {
					healthErr, healthDone = errRuntimeProbeTimeout, true
				}
			}
		}
		if snapshot == nil {
			snapshot = cachedRuntimeSnapshot()
		}
		if runtimeHealth == nil {
			runtimeHealth = cachedRuntimeHealth()
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
		"danger_source":                  "unknown",
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
		"discovery_status":               "degraded",
		"vision_worker_status":           "unavailable",
		"vision_ingress_status":          "disabled",
		"actions_status":                 "unavailable",
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
		mergedHealth := contract.MergeRuntimeComponentStatus(*runtimeHealth, snapshotRuntimeComponents(snapshot), time.Now().UTC())
		runtimeHealth = &mergedHealth
		markServingHealth(runtimeHealth, stateErr == nil || healthErr == nil)
		response["runtime_components"] = componentStatusSummary(runtimeHealth)
		response["runtime"] = runtimeHealth
		response["discovery_status"] = runtimeComponentStatus(response, runtimeHealth, "discovery", "synora-discovery")
		response["vision_worker_status"] = runtimeComponentStatus(response, runtimeHealth, "vision_worker", "synora-discovery")
		response["vision_ingress_status"] = runtimeComponentStatus(response, runtimeHealth, "vision_ingress", "synora-discovery")
		response["actions_status"] = runtimeComponentStatus(response, runtimeHealth, "actions", "synora-actions")
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
			response["danger_source"] = firstDiagnosticString(state["danger_source"], "unknown")
		}
		if degraded, ok := state["degraded"].(bool); ok {
			response["degraded"] = degraded
		}
		response["last_real_significant_event_at"] = firstDiagnosticValue(state["last_real_event_at"])
		response["last_action_request_at"] = firstDiagnosticValue(state["last_action_at"])
		response["last_action_request_at"] = firstDiagnosticValue(state["last_action_request_at"], response["last_action_request_at"])
		response["last_action_result_at"] = firstDiagnosticValue(state["last_action_at"])
		response["manual_risk_active"] = state["manual_risk_active"]
		response["manual_risk_test"] = state["manual_risk_test"]
		response["manual_risk_level"] = state["manual_risk_level"]
		response["manual_risk_score"] = state["manual_risk_score"]
		if test, ok := state["manual_risk_test"].(bool); ok && test {
			response["test_danger_level"] = state["manual_risk_level"]
		}
		response["manual_risk_expires_at"] = state["manual_risk_expires_at"]
		if blocked, ok := state["blocked_actions_recent"].([]any); ok {
			response["blocked_actions_recent"] = blocked
		} else if blocked, ok := state["blocked_actions_recent"].([]map[string]any); ok {
			response["blocked_actions_recent"] = blocked
		}
		if reasons, ok := state["blocking_reasons"].([]any); ok {
			response["blocking_reasons"] = reasons
		} else if reasons, ok := state["blocking_reasons"].([]string); ok {
			response["blocking_reasons"] = reasons
		}
		if components, ok := state["runtime_components"].(map[string]any); ok {
			response["runtime_components"] = components
		}
		if models, ok := state["runtime_models"].(map[string]any); ok {
			response["model_status"] = models
		}
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
		response["test_open_chains_count"] = chains["simulated_open_count"]
		if active, ok := response["manual_risk_test"].(bool); ok && active && response["test_open_chains_count"] == 0 {
			response["test_open_chains_count"] = 1
		}
	}
	if len(snapshot.ActionResults) > 0 {
		last := snapshot.ActionResults[len(snapshot.ActionResults)-1]
		response["last_action_at"] = firstDiagnosticValue(last["finished_at"], last["timestamp"], last["started_at"])
	}
	if len(snapshot.Events) > 0 && response["last_real_significant_event_at"] == nil {
		response["last_real_significant_event_at"] = snapshot.Events[len(snapshot.Events)-1]["timestamp"]
	}
	if len(snapshot.CGE) > 0 {
		if value := snapshot.CGE["model_status"]; value != nil {
			response["model_status"] = value
		}
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
			return diagnosticStatus(item.Status, item.Active)
		}
	}
	return "unknown"
}

func runtimeComponentStatus(response map[string]any, health *contract.RuntimeHealth, component, service string) string {
	if components, ok := response["runtime_components"].(map[string]any); ok {
		if value, ok := components[component].(string); ok && value != "" {
			return value
		}
	}
	if health != nil {
		if item, ok := health.Components[component]; ok && item.Status != "" {
			return diagnosticStatus(item.Status, item.Active)
		}
	}
	return healthServiceStatus(health, service)
}

func snapshotRuntimeComponents(snapshot *contract.PublicSnapshot) map[string]string {
	result := map[string]string{}
	if snapshot == nil || snapshot.System == nil {
		return result
	}
	if values, ok := snapshot.System["runtime_components"].(map[string]any); ok {
		for name, value := range values {
			if status, ok := value.(string); ok && status != "" {
				result[name] = status
			}
		}
	}
	if values, ok := snapshot.System["runtime_components"].(map[string]string); ok {
		for name, status := range values {
			if status != "" {
				result[name] = status
			}
		}
	}
	return result
}

func componentStatusSummary(health *contract.RuntimeHealth) map[string]string {
	result := map[string]string{}
	if health == nil {
		return result
	}
	for _, name := range []string{"api", "bus", "core", "actions", "discovery", "vision_worker", "vision_ingress"} {
		if item, ok := health.Components[name]; ok && item.Status != "" {
			result[name] = diagnosticStatus(item.Status, item.Active)
		}
	}
	return result
}

func diagnosticStatus(status string, active bool) string {
	if status == "active" {
		return "ok"
	}
	if status == "inactive" || status == "unknown" || status == "" {
		if !active {
			return "degraded"
		}
		return "unknown"
	}
	return status
}

func cacheRuntimeSnapshot(snapshot *contract.PublicSnapshot) {
	runtimeDiagnosticsCache.Lock()
	runtimeDiagnosticsCache.snapshot = snapshot
	runtimeDiagnosticsCache.Unlock()
}

func cacheRuntimeHealth(health *contract.RuntimeHealth) {
	runtimeDiagnosticsCache.Lock()
	runtimeDiagnosticsCache.health = health
	runtimeDiagnosticsCache.Unlock()
}

func cachedRuntimeSnapshot() *contract.PublicSnapshot {
	runtimeDiagnosticsCache.RLock()
	defer runtimeDiagnosticsCache.RUnlock()
	return runtimeDiagnosticsCache.snapshot
}

func cachedRuntimeHealth() *contract.RuntimeHealth {
	runtimeDiagnosticsCache.RLock()
	defer runtimeDiagnosticsCache.RUnlock()
	return runtimeDiagnosticsCache.health
}

func firstDiagnosticValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
