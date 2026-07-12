package main

import (
	"encoding/json"
	"net/http"
	"strings"

	webapi "synora/internal/api"
	"synora/internal/cge"
)

type cgeProfileProvider interface {
	CGESecurityProfile() (map[string]any, error)
	UpdateCGESecurityProfile(json.RawMessage) (map[string]any, error)
}

type cgeFeedbackProvider interface {
	CgeFeedbackList(map[string]any) ([]map[string]any, error)
	SubmitCgeEvaluationFeedback(json.RawMessage) (map[string]any, error)
	SubmitCgeChainFeedback(json.RawMessage) (map[string]any, error)
}

func handleCGESecurityProfile(core cgeProfileProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			value, err := core.CGESecurityProfile()
			if err != nil {
				writeError(w, err)
				return
			}
			value, err = normalizeCGESecurityProfileResponse(value)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, value)
		case http.MethodPatch:
			if !isAdminRequest(r) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
				return
			}
			body, ok := readJSONObject(w, r, false)
			if !ok {
				return
			}
			value, err := core.UpdateCGESecurityProfile(body)
			if err != nil {
				writeError(w, err)
				return
			}
			value, err = normalizeCGESecurityProfileResponse(value)
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

func normalizeCGESecurityProfileResponse(value map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	profile, err := cge.NormalizeCgeSecurityProfileJSON(raw)
	if err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(normalized, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func handleCGEFeedbackList(core cgeFeedbackProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		items, err := core.CgeFeedbackList(map[string]any{"chain_id": strings.TrimSpace(r.URL.Query().Get("chain_id"))})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleCGEFeedbackEvaluation(core cgeFeedbackProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if !isAdminRequest(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		body, ok := feedbackBody(w, r)
		if !ok {
			return
		}
		value, err := core.SubmitCgeEvaluationFeedback(body)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}
}

func handleCGEFeedbackChain(core cgeFeedbackProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if !isAdminRequest(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}
		body, ok := feedbackBody(w, r)
		if !ok {
			return
		}
		value, err := core.SubmitCgeChainFeedback(body)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}
}

func feedbackBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, ok := readJSONObject(w, r, false)
	if !ok {
		return nil, false
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, false
	}
	principal, exists := authPrincipalFromRequest(r)
	if exists {
		value["created_by"] = principal.ID
		if value["created_by"] == "" {
			value["created_by"] = principal.Login
		}
	} else {
		value["created_by"] = webapi.AdminAuthUser().ID
	}
	updated, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	return updated, true
}
