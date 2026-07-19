package main

import (
	"encoding/json"
	"net/http"
	"time"

	"synora/pkg/contract"
	"synora/pkg/contracts"
)

type connectivityStatusRequester interface {
	RequestWithTimeout(string, string, []byte, string, time.Duration) (*contract.Message, error)
}

func handleConnectivityStatus(requester connectivityStatusRequester) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		if requester == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "connectivity_unavailable"})
			return
		}
		response, err := requester.RequestWithTimeout("connectivity.status", "api", nil, "connectivity", 2*time.Second)
		if err != nil || response == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "connectivity_unavailable"})
			return
		}
		var status contracts.Status
		if err := json.Unmarshal(response.Payload, &status); err != nil || status.SchemaVersion != contracts.ConnectivitySchemaVersion {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "connectivity_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}
