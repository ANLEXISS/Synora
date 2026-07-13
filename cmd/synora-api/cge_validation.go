package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"synora/pkg/contract"
)

type cgeValidationProvider interface {
	InjectCGEValidationEvent(json.RawMessage) (map[string]any, error)
	InjectCGEValidationSequence(json.RawMessage) (map[string]any, error)
	CGEValidationHistory() ([]map[string]any, error)
	ClearCGEValidationHistory() (map[string]any, error)
}

func handleCGEValidationEvents(core cgeValidationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) || !requireAdmin(w, r) {
			return
		}
		body, ok := readJSONObject(w, r, true)
		if !ok {
			return
		}
		result, err := core.InjectCGEValidationEvent(body)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, result)
	}
}

func handleCGEValidationSequence(core cgeValidationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) || !requireAdmin(w, r) {
			return
		}
		body, ok := readJSONObject(w, r, true)
		if !ok {
			return
		}
		result, err := core.InjectCGEValidationSequence(body)
		if err != nil {
			if contract.APIErrorCode(err) == contract.ErrorValidationFailed && strings.Contains(err.Error(), " at events[") {
				response := map[string]any{"error": contract.ErrorValidationFailed, "message": err.Error()}
				if typed, ok := err.(*contract.APIError); ok && typed.Details != nil {
					response["details"] = typed.Details
				}
				writeJSON(w, http.StatusBadRequest, response)
				return
			}
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, result)
	}
}

func handleCGEValidationHistory(core cgeValidationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if !requireAdmin(w, r) {
				return
			}
			items, err := core.CGEValidationHistory()
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, items)
		case http.MethodDelete:
			if !requireAdmin(w, r) {
				return
			}
			result, err := core.ClearCGEValidationHistory()
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
		}
	}
}

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if isAdminRequest(r) {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden", "message": "admin access required"})
	return false
}
