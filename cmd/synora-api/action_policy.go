package main

import (
	"encoding/json"
	"net/http"
)

type actionPolicyProvider interface {
	ActionPolicy() (map[string]any, error)
	UpdateActionPolicy(json.RawMessage) (map[string]any, error)
	ResetActionPolicy() (map[string]any, error)
	ActionCatalog() ([]map[string]any, error)
	TestAction(json.RawMessage) (map[string]any, error)
}

func handleActionPolicy(core actionPolicyProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if !requireAdmin(w, r) {
				return
			}
			value, err := core.ActionPolicy()
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, value)
		case http.MethodPatch:
			if !requireAdmin(w, r) {
				return
			}
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}
			value, err := core.UpdateActionPolicy(body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, value)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch)
		}
	}
}

func handleActionPolicyReset(core actionPolicyProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) || !requireAdmin(w, r) {
			return
		}
		value, err := core.ResetActionPolicy()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}
}

func handleActionCatalog(core actionPolicyProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) || !requireAdmin(w, r) {
			return
		}
		value, err := core.ActionCatalog()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}
}

func handleActionTest(core actionPolicyProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) || !requireAdmin(w, r) {
			return
		}
		body, ok := readJSONObject(w, r, true)
		if !ok {
			return
		}
		value, err := core.TestAction(body)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}
}
