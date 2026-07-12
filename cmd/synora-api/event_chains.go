package main

import (
	"net/http"
	"strconv"
	"strings"
)

type eventChainProvider interface {
	EventChains(map[string]any) (map[string]any, error)
	EventChain(string) (map[string]any, error)
}

type rawEventsProvider interface {
	Events() ([]map[string]any, error)
}

func handleEvents(core rawEventsProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		items, err := core.Events()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

type criticalChainProvider interface {
	CriticalChains(map[string]any) ([]map[string]any, error)
	CriticalChain(string) (map[string]any, error)
}

func handleEventChains(core eventChainProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		query := r.URL.Query()
		filter := map[string]any{}
		for _, key := range []string{"status", "since", "severity", "simulated"} {
			if value := strings.TrimSpace(query.Get(key)); value != "" {
				if key == "simulated" {
					if parsed, err := strconv.ParseBool(value); err == nil {
						filter[key] = parsed
					}
				} else {
					filter[key] = value
				}
			}
		}
		if value := boundedQueryInt(query.Get("limit"), 50, 100); value > 0 {
			filter["limit"] = value
		}
		result, err := core.EventChains(filter)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleEventChain(core eventChainProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/events/chains/"))
		if id == "" || strings.Contains(id, "/") {
			writeRouteNotFound(w, "event chain")
			return
		}
		item, err := core.EventChain(id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func handleCriticalChains(core criticalChainProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		items, err := core.CriticalChains(map[string]any{"limit": boundedQueryInt(r.URL.Query().Get("limit"), 50, 100)})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleCriticalChain(core criticalChainProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/cge/critical-chains/"))
		if id == "" || strings.Contains(id, "/") {
			writeRouteNotFound(w, "critical chain")
			return
		}
		item, err := core.CriticalChain(id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}
