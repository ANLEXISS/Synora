package main

import (
	"encoding/json"
	"net/http"
	"time"

	"synora/pkg/contract"
)

type securityModeProvider interface {
	SecurityMode() (contract.SecurityModeState, error)
	SetSecurityMode(json.RawMessage) (contract.SecurityModeState, error)
	ArmSecurity(json.RawMessage) (contract.SecurityModeState, error)
	DisarmSecurity(json.RawMessage) (contract.SecurityModeState, error)
}

func handleSecurityMode(core securityModeProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			mode, err := core.SecurityMode()
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, mode)
		case http.MethodPost:
			if !isAdminRequest(r) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden", "message": "admin access required"})
				return
			}
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}
			mode, err := core.SetSecurityMode(body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, mode)
		case http.MethodPatch:
			if !isAdminRequest(r) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden", "message": "admin access required"})
				return
			}
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}
			var patch map[string]any
			if err := json.Unmarshal(body, &patch); err != nil {
				writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid security mode payload"))
				return
			}
			current, err := core.SecurityMode()
			if err != nil {
				writeError(w, err)
				return
			}
			if _, exists := patch["mode"]; !exists {
				patch["mode"] = current.Mode
			}
			if _, exists := patch["reason"]; !exists {
				patch["reason"] = current.Reason
			}
			if _, exists := patch["set_by"]; !exists {
				patch["set_by"] = current.SetBy
			}
			if _, exists := patch["source"]; !exists {
				patch["source"] = current.Source
			}
			if _, exists := patch["duration_seconds"]; !exists && current.ExpiresAt != nil {
				seconds := int(time.Until(*current.ExpiresAt).Seconds())
				if seconds > 0 {
					patch["duration_seconds"] = seconds
				}
			}
			body, _ = json.Marshal(patch)
			mode, err := core.SetSecurityMode(body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, mode)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		}
	}
}

func handleSecurityArm(core securityModeProvider) http.HandlerFunc {
	return securityModeWriteHandler(core, func(body json.RawMessage) (contract.SecurityModeState, error) { return core.ArmSecurity(body) })
}

func handleSecurityDisarm(core securityModeProvider) http.HandlerFunc {
	return securityModeWriteHandler(core, func(body json.RawMessage) (contract.SecurityModeState, error) { return core.DisarmSecurity(body) })
}

func securityModeWriteHandler(_ securityModeProvider, action func(json.RawMessage) (contract.SecurityModeState, error)) http.HandlerFunc {
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
		mode, err := action(body)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, mode)
	}
}
