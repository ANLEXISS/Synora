package main

import (
	"encoding/json"
	"net/http"
)

type cgeCriticalSeedProvider interface {
	CGECriticalSeeds(map[string]any) ([]map[string]any, error)
	CGECriticalSeed(string) (map[string]any, error)
	CreateCGECriticalSeed(json.RawMessage) (map[string]any, error)
	UpdateCGECriticalSeed(string, json.RawMessage) (map[string]any, error)
	DeleteCGECriticalSeed(string) (map[string]any, error)
}

type cgeDangerAssessmentProvider interface {
	CGEDangerAssessments(map[string]any) ([]map[string]any, error)
	CGEDangerAssessment(string) (map[string]any, error)
}

type cgeLearnedBehaviorMutationProvider interface {
	UpdateCGELearnedBehavior(string, json.RawMessage) (map[string]any, error)
	DeleteCGELearnedBehavior(string) (map[string]any, error)
	ActOnCGELearnedBehavior(string, string, json.RawMessage) (map[string]any, error)
}

func handleCGECriticalSeeds(core cgeCriticalSeedProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			items, err := core.CGECriticalSeeds(cgeQueryParams(r))
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, items)
		case http.MethodPost:
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}
			item, err := core.CreateCGECriticalSeed(body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	}
}

func handleCGECriticalSeed(core cgeCriticalSeedProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := resourceID(r.URL.Path, "/api/cge/critical-seeds/")
		if !ok {
			writeRouteNotFound(w, "critical seed")
			return
		}

		switch r.Method {
		case http.MethodGet:
			item, err := core.CGECriticalSeed(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodPatch:
			body, valid := readJSONObject(w, r, true)
			if !valid {
				return
			}
			item, err := core.UpdateCGECriticalSeed(id, body)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			item, err := core.DeleteCGECriticalSeed(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		default:
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
		}
	}
}

func handleCGEDangerAssessments(core cgeDangerAssessmentProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		items, err := core.CGEDangerAssessments(cgeQueryParams(r))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleCGEDangerAssessment(core cgeDangerAssessmentProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		id, ok := resourceID(r.URL.Path, "/api/cge/danger-assessments/")
		if !ok {
			writeRouteNotFound(w, "danger assessment")
			return
		}
		item, err := core.CGEDangerAssessment(id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}
