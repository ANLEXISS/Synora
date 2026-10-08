package systemstate

import (
	"encoding/json"
	"net/http"
)

// Handler is the shared GET boundary used by the API and the ephemeral E2E
// API. The supplied state must already be redacted and contract-shaped.
func Handler(snapshot func() map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var state map[string]any
		if snapshot != nil {
			state = snapshot()
		}
		w.Header().Set("Content-Type", "application/json")
		if state == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "unknown", "reason": "no_core_state_observed"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(state)
	}
}
