package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"synora/pkg/contract"
)

type clipProvider interface {
	Clips(int) ([]contract.Clip, error)
	Clip(string) (*contract.Clip, error)
}

func handleClipCollection(core clipProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		limit := contract.DefaultClipListLimit
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > contract.MaxClipListLimit {
				writeError(w, contract.NewAPIError(contract.ErrorInvalidRequest, "clip limit must be between 1 and 100"))
				return
			}
			limit = parsed
		}
		items, err := core.Clips(limit)
		if err != nil {
			writeError(w, err)
			return
		}
		for index := range items {
			items[index].Path = ""
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleClipRoute(core clipProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/api/clips/")
		id, err := url.PathUnescape(strings.TrimSpace(raw))
		if err != nil || id == "" || strings.Contains(id, "/") {
			writeRouteNotFound(w, "clip")
			return
		}
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		item, err := core.Clip(id)
		if err != nil {
			writeError(w, err)
			return
		}
		item.Path = ""
		writeJSON(w, http.StatusOK, item)
	}
}
