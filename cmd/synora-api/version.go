package main

import (
	"net/http"
	"strings"

	"synora/internal/version"
)

func handleVersion(w http.ResponseWriter, r *http.Request, path string) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	_, err := version.Load(strings.TrimSpace(path))
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"service": "synora-api",
			"status":  "unavailable",
			"error":   "version manifest unavailable",
		})
		return
	}
	runtime := version.Current(path)
	writeJSON(w, http.StatusOK, runtime)
}
