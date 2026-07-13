package discovery

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type discoveryHealth struct {
	mu sync.RWMutex

	KnownCams int `json:"known_cameras"`

	LastSuccess time.Time `json:"last_success"`

	LastError string `json:"last_error,omitempty"`

	NetworkStatus string `json:"network_status"`

	VisionWorkerStatus string `json:"vision_worker_status"`

	VisionWorkerError string `json:"vision_worker_error,omitempty"`
}

var healthState = &discoveryHealth{}

func startHealthServer() {

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {

		status := healthState.snapshot()

		healthy :=
			!status.LastSuccess.IsZero() &&
				time.Since(status.LastSuccess) < 15*time.Second &&
				status.NetworkStatus != "degraded" &&
				status.VisionWorkerStatus != "unavailable" &&
				status.VisionWorkerStatus != "error"

		payload := map[string]any{
			"service":       "discovery",
			"status":        map[bool]string{true: "ok", false: "degraded"}[healthy],
			"known_cameras": status.KnownCams,
			"last_success":  status.LastSuccess,
			"network": map[string]any{
				"status": status.NetworkStatus,
			},
			"vision_worker": map[string]any{
				"status": status.VisionWorkerStatus,
			},
		}

		if status.LastError != "" {
			payload["last_error"] = status.LastError
		}
		if status.VisionWorkerError != "" {
			payload["vision_worker"].(map[string]any)["message"] = status.VisionWorkerError
		}

		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(payload)
	})

	go http.ListenAndServe(
		HealthAddr,
		mux,
	)
}

func (h *discoveryHealth) setSuccess(
	known int,
) {

	h.mu.Lock()
	defer h.mu.Unlock()

	h.KnownCams = known
	h.LastSuccess = time.Now().UTC()
	h.LastError = ""
	if h.NetworkStatus == "" {
		h.NetworkStatus = "ok"
	}
}

func (h *discoveryHealth) setError(
	message string,
) {

	h.mu.Lock()
	defer h.mu.Unlock()

	h.LastError = message
}

func (h *discoveryHealth) setNetwork(status, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.NetworkStatus = status
	if message != "" {
		h.LastError = message
	}
}

func (h *discoveryHealth) setVisionWorker(status, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.VisionWorkerStatus = status
	h.VisionWorkerError = message
}

func (h *discoveryHealth) snapshot() discoveryHealth {

	h.mu.RLock()
	defer h.mu.RUnlock()

	return discoveryHealth{
		KnownCams:          h.KnownCams,
		LastSuccess:        h.LastSuccess,
		LastError:          h.LastError,
		NetworkStatus:      h.NetworkStatus,
		VisionWorkerStatus: h.VisionWorkerStatus,
		VisionWorkerError:  h.VisionWorkerError,
	}
}
