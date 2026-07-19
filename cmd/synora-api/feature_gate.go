package main

import "net/http"

// withFeature keeps compatibility routes registered while making optional
// surfaces explicit and fail-closed when disabled. It deliberately returns
// not-found so disabled developer endpoints are not advertised.
func withFeature(enabled bool, feature string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !enabled {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error":   "feature_disabled",
				"feature": feature,
			})
			return
		}
		next.ServeHTTP(w, r)
	}
}
