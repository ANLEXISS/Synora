package main

import (
	"encoding/json"
	"net/http"
)

func handleIntelligenceTopology(hub *websocketHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		topology, _ := hub.intelligenceSnapshot()
		writeJSON(w, http.StatusOK, topology)
	}
}

func handleIntelligenceTraces(hub *websocketHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		_, traces := hub.intelligenceSnapshot()
		writeJSON(w, http.StatusOK, traces)
	}
}

func handleIntelligenceEvents(hub *websocketHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, http.StatusOK, hub.recentEventsSnapshot())
	}
}

func handlePilotState(hub *websocketHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		state := hub.pilotStateSnapshot()
		if state == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unknown", "reason": "no_core_state_observed"})
			return
		}
		writeJSON(w, http.StatusOK, state)
	}
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	w.WriteHeader(http.StatusMethodNotAllowed)
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
