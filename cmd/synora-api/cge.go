package main

import (
	"net/http"
	"strconv"
	"strings"

	"synora/pkg/contract"
)

const apiCGELimitMax = 100

type cgeProvider interface {
	CGESummary() (map[string]any, error)
	CGESequences(map[string]any) ([]map[string]any, error)
	CGETransitions(map[string]any) ([]map[string]any, error)
	CGELearnedBehaviors(map[string]any) ([]map[string]any, error)
	CGESequence(string) (map[string]any, error)
	CGELearnedBehavior(string) (map[string]any, error)
}

func handleCGESummary(core cgeProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		summary, err := core.CGESummary()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
	}
}

func handleCGESequences(core cgeProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		items, err := core.CGESequences(cgeQueryParams(r))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleCGETransitions(core cgeProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		items, err := core.CGETransitions(cgeQueryParams(r))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleCGELearnedBehaviors(core cgeProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		items, err := core.CGELearnedBehaviors(cgeQueryParams(r))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleCGEDetail(core cgeProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/cge/")
		switch {
		case strings.HasPrefix(path, "sequences/"):
			if r.Method != http.MethodGet {
				writeMethodNotAllowed(w, http.MethodGet)
				return
			}
			id := strings.TrimSpace(strings.TrimPrefix(path, "sequences/"))
			if id == "" || strings.Contains(id, "/") {
				writeRouteNotFound(w, "CGE sequence")
				return
			}
			item, err := core.CGESequence(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case strings.HasPrefix(path, "learned-behaviors/"):
			handleCGELearnedBehaviorDetail(core, w, r, strings.TrimPrefix(path, "learned-behaviors/"))
		default:
			writeRouteNotFound(w, "CGE")
		}
	}
}

func handleCGELearnedBehaviorDetail(core cgeProvider, w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(strings.TrimSpace(path), "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeRouteNotFound(w, "learned behavior")
		return
	}
	id := strings.TrimSpace(parts[0])

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			item, err := core.CGELearnedBehavior(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodPatch:
			mutator, ok := core.(cgeLearnedBehaviorMutationProvider)
			if !ok {
				writeError(w, contract.NewAPIError(contract.ErrorInternal, "learned behavior mutation unavailable"))
				return
			}
			body, valid := readJSONObject(w, r, true)
			if !valid {
				return
			}
			item, err := mutator.UpdateCGELearnedBehavior(id, body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			mutator, ok := core.(cgeLearnedBehaviorMutationProvider)
			if !ok {
				writeError(w, contract.NewAPIError(contract.ErrorInternal, "learned behavior mutation unavailable"))
				return
			}
			item, err := mutator.DeleteCGELearnedBehavior(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
		}
		return
	}

	if len(parts) != 2 || !isLearnedBehaviorAction(parts[1]) {
		writeRouteNotFound(w, "learned behavior")
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	mutator, ok := core.(cgeLearnedBehaviorMutationProvider)
	if !ok {
		writeError(w, contract.NewAPIError(contract.ErrorInternal, "learned behavior mutation unavailable"))
		return
	}
	body, valid := readJSONObject(w, r, false)
	if !valid {
		return
	}
	item, err := mutator.ActOnCGELearnedBehavior(id, parts[1], body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func isLearnedBehaviorAction(value string) bool {
	switch strings.TrimSpace(value) {
	case "approve", "reject", "disable", "reset":
		return true
	default:
		return false
	}
}

func cgeQueryParams(r *http.Request) map[string]any {
	query := r.URL.Query()
	params := map[string]any{
		"limit":  boundedQueryInt(query.Get("limit"), 20, apiCGELimitMax),
		"offset": boundedQueryInt(query.Get("offset"), 0, 1<<30),
	}
	for _, key := range []string{"simulated", "signature_contains", "scenario_id"} {
		if value := strings.TrimSpace(query.Get(key)); value != "" {
			params[key] = value
		}
	}
	if value := boundedQueryInt(query.Get("min_count"), 0, 1<<30); value > 0 {
		params["min_count"] = value
	}
	return params
}

func boundedQueryInt(raw string, fallback int, max int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return fallback
	}
	if max > 0 && value > max {
		return max
	}
	return value
}
