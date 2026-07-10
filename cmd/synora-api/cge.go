package main

import (
	"net/http"
	"strconv"
	"strings"
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
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/cge/")
		switch {
		case strings.HasPrefix(path, "sequences/"):
			id := strings.TrimSpace(strings.TrimPrefix(path, "sequences/"))
			item, err := core.CGESequence(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case strings.HasPrefix(path, "learned-behaviors/"):
			id := strings.TrimSpace(strings.TrimPrefix(path, "learned-behaviors/"))
			item, err := core.CGELearnedBehavior(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		default:
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "cge route not found"})
		}
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
