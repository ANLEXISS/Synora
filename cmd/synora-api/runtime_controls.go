package main

import (
	"encoding/json"
	"net/http"

	"synora/pkg/contract"
)

func handleIntrusionReset(core runtimeControlProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if !isAdminRequest(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden", "message": "admin access required"})
			return
		}
		payload := map[string]any{}
		if principal, ok := authPrincipalFromRequest(r); ok {
			payload["created_by"] = principal.ID
		}
		body, _ := json.Marshal(payload)
		result, err := core.ResetIntrusion(body)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleSystemStateReset(core runtimeControlProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if !isAdminRequest(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden", "message": "admin access required"})
			return
		}
		body, ok := readJSONObject(w, r, false)
		if !ok {
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid state reset payload"))
			return
		}
		if payload["target_state"] == nil {
			payload["target_state"] = "idle"
		}
		if payload["reason"] == nil {
			payload["reason"] = "manual_admin_reset"
		}
		if principal, ok := authPrincipalFromRequest(r); ok && payload["created_by"] == nil {
			payload["created_by"] = principal.ID
		}
		body, _ = json.Marshal(payload)
		result, err := core.ResetSystemState(body)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleManualRisk(core runtimeControlProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if !isAdminRequest(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden", "message": "admin access required"})
			return
		}
		body, ok := readJSONObject(w, r, true)
		if !ok {
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid manual risk payload"))
			return
		}
		if principal, ok := authPrincipalFromRequest(r); ok && payload["created_by"] == nil {
			payload["created_by"] = principal.ID
		}
		body, _ = json.Marshal(payload)
		result, err := core.ManualRisk(body)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, result)
	}
}
