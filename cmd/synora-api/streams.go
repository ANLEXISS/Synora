package main

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"synora/internal/discovery/network"
	"synora/internal/runtimeconfig"
	"synora/pkg/contract"
)

type StreamDescriptor struct {
	DeviceID       string `json:"device_id"`
	RTSPPublishURL string `json:"rtsp_publish_url"`
	WebRTCURL      string `json:"webrtc_url,omitempty"`
	HLSURL         string `json:"hls_url,omitempty"`
	Status         string `json:"status"`
	LiveAvailable  bool   `json:"live_available"`
}

func handleStreams(core deviceConfigurationProvider) http.HandlerFunc {
	return handleStreamsWithAuthorization(core, nil)
}

func handleStreamsWithAuthorization(core deviceConfigurationProvider, authorized func(string, map[string]any) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		items, err := core.Devices()
		if err != nil {
			writeError(w, err)
			return
		}
		status := mediaMTXStreamStatus(core)
		pathID := strings.TrimPrefix(r.URL.Path, "/api/streams/")
		if pathID == r.URL.Path {
			pathID = ""
		}
		if pathID != "" {
			pathID, err = url.PathUnescape(strings.TrimSuffix(pathID, "/"))
			if err != nil {
				writeError(w, err)
				return
			}
		}
		result := make([]StreamDescriptor, 0)
		for _, item := range items {
			id := streamStringValue(item["id"])
			if id == "" || (pathID == "" && !isCameraDevice(item)) || (pathID != "" && id != pathID) {
				continue
			}
			descriptor := streamDescriptorWithStatus(id, status)
			if authorized != nil && !authorized(id, item) {
				descriptor.Status = "unauthorized"
				descriptor.RTSPPublishURL = ""
				descriptor.WebRTCURL = ""
				descriptor.HLSURL = ""
				descriptor.LiveAvailable = false
			}
			result = append(result, descriptor)
		}
		if pathID != "" && len(result) == 0 {
			http.NotFound(w, r)
			return
		}
		if pathID != "" {
			writeJSON(w, http.StatusOK, result[0])
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func isCameraDevice(item map[string]any) bool {
	for _, key := range []string{"type", "device_type", "role"} {
		if value := strings.ToLower(streamStringValue(item[key])); value == "camera" || strings.Contains(value, "camera") {
			return true
		}
	}
	return false
}

func streamDescriptor(deviceID string) StreamDescriptor {
	return streamDescriptorWithStatus(deviceID, "unknown")
}

func streamDescriptorWithStatus(deviceID, status string) StreamDescriptor {
	cfg, _ := network.LoadConfig(os.Getenv("SYNORA_NETWORK_CONFIG"))
	runtime, _ := runtimeconfig.Load(os.Getenv)
	baseRTSP := cfg.SynoraNet.Services.RTSPURL
	if baseRTSP == "" {
		baseRTSP = runtime.Endpoints.MediaMTXRTSPURL
	}
	baseWebRTC := cfg.SynoraNet.Services.WebRTCBaseURL
	baseHLS := cfg.SynoraNet.Services.HLSBaseURL
	if value := strings.TrimSpace(os.Getenv("SYNORA_WEBRTC_BASE_URL")); value != "" {
		baseWebRTC = value
	}
	if value := strings.TrimSpace(os.Getenv("SYNORA_HLS_BASE_URL")); value != "" {
		baseHLS = value
	}
	baseRTSP = publicStreamBase(baseRTSP)
	baseWebRTC = publicStreamBase(baseWebRTC)
	baseHLS = publicStreamBase(baseHLS)
	path := url.PathEscape(deviceID)
	if status == "" {
		status = "unknown"
	}
	descriptor := StreamDescriptor{DeviceID: deviceID, RTSPPublishURL: strings.TrimRight(baseRTSP, "/") + "/" + path, Status: status}
	if baseWebRTC != "" {
		descriptor.WebRTCURL = strings.TrimRight(baseWebRTC, "/") + "/" + path + "/whep"
	}
	if baseHLS != "" {
		descriptor.HLSURL = strings.TrimRight(baseHLS, "/") + "/" + path + "/index.m3u8"
	}
	descriptor.LiveAvailable = status != "degraded" && (descriptor.WebRTCURL != "" || descriptor.HLSURL != "")
	return descriptor
}

func mediaMTXStreamStatus(core any) string {
	provider, ok := core.(systemHealthProvider)
	if !ok || provider == nil {
		return "unknown"
	}
	result := make(chan *contract.RuntimeHealth, 1)
	go func() {
		health, err := provider.SystemHealth()
		if err != nil {
			result <- nil
			return
		}
		result <- health
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	select {
	case health := <-result:
		if health == nil {
			return "degraded"
		}
		switch health.MediaMTX.Status {
		case "ok", "active", "ready":
			return "ready"
		case "degraded", "unavailable", "failed":
			return "degraded"
		default:
			return "unknown"
		}
	case <-ctx.Done():
		return "degraded"
	}
}

func publicStreamBase(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func streamStringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}
