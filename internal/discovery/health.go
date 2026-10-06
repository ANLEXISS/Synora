package discovery

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type discoveryHealth struct {
	mu sync.RWMutex

	KnownCams  int `json:"known_cameras"`
	OnlineCams int `json:"online_cameras"`

	CameraStatus string `json:"camera_status"`

	CameraError string `json:"camera_error,omitempty"`

	LastSuccess time.Time `json:"last_success"`

	LastError string `json:"last_error,omitempty"`

	NetworkStatus string `json:"network_status"`

	VisionWorkerStatus string `json:"vision_worker_status"`

	VisionWorkerError string `json:"vision_worker_error,omitempty"`

	VisionIngressStatus string `json:"vision_ingress_status"`

	VisionIngressError string `json:"vision_ingress_error,omitempty"`

	MediaMTXStatus string `json:"mediamtx_status"`

	MediaMTXError string `json:"mediamtx_error,omitempty"`
}

var healthState = &discoveryHealth{
	NetworkStatus:       "unknown",
	VisionWorkerStatus:  "unknown",
	VisionIngressStatus: "unknown",
	MediaMTXStatus:      "unknown",
	CameraStatus:        "unknown",
}

func startHealthServer(address string) *http.Server {

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {

		status := healthState.snapshot()

		healthy :=
			!status.LastSuccess.IsZero() &&
				time.Since(status.LastSuccess) < 15*time.Second &&
				status.NetworkStatus != "degraded" &&
				status.VisionWorkerStatus != "unavailable" &&
				status.VisionWorkerStatus != "error" &&
				status.VisionIngressStatus != "disabled" &&
				status.VisionIngressStatus != "degraded" &&
				status.VisionIngressStatus != "error" &&
				status.MediaMTXStatus != "degraded" &&
				status.MediaMTXStatus != "unavailable" &&
				status.MediaMTXStatus != "error" &&
				status.CameraStatus == "ok"

		payload := map[string]any{
			"service":        "discovery",
			"status":         map[bool]string{true: "ok", false: "degraded"}[healthy],
			"known_cameras":  status.KnownCams,
			"online_cameras": status.OnlineCams,
			"last_success":   status.LastSuccess,
			"network": map[string]any{
				"status": status.NetworkStatus,
			},
			"vision_worker": map[string]any{
				"status": status.VisionWorkerStatus,
			},
			"vision_ingress": map[string]any{
				"status": status.VisionIngressStatus,
			},
			"mediamtx": map[string]any{
				"status": status.MediaMTXStatus,
			},
			"cameras": map[string]any{
				"status": status.CameraStatus,
			},
		}

		if status.LastError != "" {
			payload["last_error"] = status.LastError
		}
		if status.VisionWorkerError != "" {
			payload["vision_worker"].(map[string]any)["message"] = status.VisionWorkerError
		}
		if status.VisionIngressError != "" {
			payload["vision_ingress"].(map[string]any)["message"] = status.VisionIngressError
		}
		if status.MediaMTXError != "" {
			payload["mediamtx"].(map[string]any)["message"] = status.MediaMTXError
		}
		if status.CameraError != "" {
			payload["cameras"].(map[string]any)["message"] = status.CameraError
		}

		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(payload)
	})

	server := &http.Server{Addr: address, Handler: mux}
	go server.ListenAndServe()
	return server
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

func (h *discoveryHealth) setVisionWorker(status, message string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	changed := h.VisionWorkerStatus != status || h.VisionWorkerError != message
	h.VisionWorkerStatus = status
	h.VisionWorkerError = message
	return changed
}

func (h *discoveryHealth) setVisionIngress(status, message string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	changed := h.VisionIngressStatus != status || h.VisionIngressError != message
	h.VisionIngressStatus = status
	h.VisionIngressError = message
	return changed
}

func (h *discoveryHealth) setMediaMTX(status, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.MediaMTXStatus = status
	h.MediaMTXError = message
	if message != "" {
		h.LastError = message
	}
}

func (h *discoveryHealth) setCameraStatus(known, online int) bool {
	if known < 0 {
		known = 0
	}
	if online < 0 {
		online = 0
	}
	if online > known {
		online = known
	}
	status, message := cameraHealthStatus(known, online)
	h.mu.Lock()
	defer h.mu.Unlock()
	changed := h.CameraStatus != status || h.CameraError != message || h.KnownCams != known || h.OnlineCams != online
	h.KnownCams = known
	h.OnlineCams = online
	h.CameraStatus = status
	h.CameraError = message
	if message != "" {
		h.LastError = message
	}
	return changed
}

func cameraHealthStatus(known, online int) (string, string) {
	switch {
	case known <= 0:
		return "unknown", "no enabled camera is configured"
	case online <= 0:
		return "offline", "no configured camera is online"
	case online < known:
		return "degraded", "only some configured cameras are online"
	default:
		return "ok", ""
	}
}

func (h *discoveryHealth) snapshot() discoveryHealth {

	h.mu.RLock()
	defer h.mu.RUnlock()

	return discoveryHealth{
		KnownCams:           h.KnownCams,
		OnlineCams:          h.OnlineCams,
		CameraStatus:        h.CameraStatus,
		CameraError:         h.CameraError,
		LastSuccess:         h.LastSuccess,
		LastError:           h.LastError,
		NetworkStatus:       h.NetworkStatus,
		VisionWorkerStatus:  h.VisionWorkerStatus,
		VisionWorkerError:   h.VisionWorkerError,
		VisionIngressStatus: h.VisionIngressStatus,
		VisionIngressError:  h.VisionIngressError,
		MediaMTXStatus:      h.MediaMTXStatus,
		MediaMTXError:       h.MediaMTXError,
	}
}
