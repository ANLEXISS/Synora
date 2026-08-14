package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"synora/pkg/contract"
)

type incidentProvider interface {
	Incidents(int) ([]contract.Incident, error)
	Incident(string) (*contract.Incident, error)
	MarkIncidentViewed(string) (*contract.Incident, error)
	AcknowledgeIncident(string) (*contract.Incident, error)
}

type incidentResolver interface {
	ResolveIncident(string) (*contract.Incident, error)
}

func handleIncidentCollection(core incidentProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 100 {
				writeError(w, contract.NewAPIError(contract.ErrorInvalidRequest, "incident limit must be between 1 and 100"))
				return
			}
			limit = parsed
		}
		items, err := core.Incidents(limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleIncidentRoute(core incidentProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/api/incidents/")
		parts := strings.Split(raw, "/")
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			writeRouteNotFound(w, "incident")
			return
		}
		id, err := url.PathUnescape(strings.TrimSpace(parts[0]))
		if err != nil || id == "" || strings.Contains(id, "/") {
			writeRouteNotFound(w, "incident")
			return
		}

		if len(parts) == 1 {
			if !requireMethod(w, r, http.MethodGet) {
				return
			}
			item, err := core.Incident(id)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
			return
		}
		if len(parts) != 2 || r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
			return
		}

		var item *contract.Incident
		switch parts[1] {
		case "view":
			item, err = core.MarkIncidentViewed(id)
		case "acknowledge":
			item, err = core.AcknowledgeIncident(id)
		case "resolve":
			resolver, ok := core.(incidentResolver)
			if !ok {
				writeError(w, contract.NewAPIError(contract.ErrorInternal, "incident resolution unavailable"))
				return
			}
			item, err = resolver.ResolveIncident(id)
		default:
			writeRouteNotFound(w, "incident action")
			return
		}
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}
